package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relay/channel"
	openairelay "github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	responsesWSFirstMessageTimeout = 15 * time.Second
	responsesWSHeartbeatInterval   = 25 * time.Second
	responsesWSPongTimeout         = 90 * time.Second
	responsesWSMaxConnectionAge    = 60 * time.Minute
)

type responsesWSClientEvent struct {
	Type string `json:"type"`
}

type responsesWSErrorEvent struct {
	Type   string            `json:"type"`
	Status int               `json:"status,omitempty"`
	Error  types.OpenAIError `json:"error"`
}

var responsesWSRequestJSONFields = responseRequestJSONFieldNames()

type responsesWebSocketTurn struct {
	ctx               *gin.Context
	info              *relaycommon.RelayInfo
	accumulator       *openairelay.ResponsesEventAccumulator
	outbound          []byte
	channel           *model.Channel
	reconnect         func() (*websocket.Conn, *http.Response, error)
	replayEnabled     bool
	requestDispatched bool
	upstreamEvent     bool
	replayCount       int
	rateLimitFinish   func(bool)
	releaseUser       func()
	releaseBusiness   func()
	finishOnce        sync.Once
}

type responsesWSUpstreamConnection struct {
	conn      *websocket.Conn
	done      chan struct{}
	closeOnce sync.Once
}

func newResponsesWSUpstreamConnection(conn *websocket.Conn) *responsesWSUpstreamConnection {
	return &responsesWSUpstreamConnection{
		conn: conn,
		done: make(chan struct{}),
	}
}

func (u *responsesWSUpstreamConnection) close() {
	if u == nil {
		return
	}
	u.closeOnce.Do(func() {
		close(u.done)
		if u.conn != nil {
			_ = u.conn.Close()
		}
	})
}

func (t *responsesWebSocketTurn) finish(success bool) {
	if t == nil {
		return
	}
	t.finishOnce.Do(func() {
		// The replay decision is finished with the turn. Drop retained request and
		// adaptor state before quota settlement and concurrency release.
		t.outbound = nil
		t.reconnect = nil
		usage := t.accumulator.Usage(t.info)
		if success || usage.TotalTokens > 0 {
			service.PostTextConsumeQuota(t.ctx, t.info, usage, nil)
		} else if t.info.Billing != nil {
			t.info.Billing.Refund(t.ctx)
		}
		if t.rateLimitFinish != nil {
			t.rateLimitFinish(success)
		}
		if t.releaseBusiness != nil {
			t.releaseBusiness()
		}
		if t.releaseUser != nil {
			t.releaseUser()
		}
	})
}

type responsesWebSocketSession struct {
	baseCtx         *gin.Context
	client          *websocket.Conn
	upstream        *responsesWSUpstreamConnection
	channel         *model.Channel
	lockedModel     string
	activeTurn      *responsesWebSocketTurn
	mu              sync.Mutex
	clientWriteMu   sync.Mutex
	upstreamWriteMu sync.Mutex
	closeOnce       sync.Once
	closed          chan struct{}
	wg              sync.WaitGroup
}

func RelayResponsesWebSocket(c *gin.Context) {
	client, err := responsesWebSocketUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.LogError(c, "responses websocket upgrade failed: "+err.Error())
		return
	}

	maxMessageBytes := int64(appconstant.MaxRequestBodyMB) * 1024 * 1024
	if maxMessageBytes <= 0 {
		maxMessageBytes = 128 * 1024 * 1024
	}
	client.SetReadLimit(maxMessageBytes)
	_ = client.SetReadDeadline(time.Now().Add(responsesWSFirstMessageTimeout))

	session := &responsesWebSocketSession{
		baseCtx: c,
		client:  client,
		closed:  make(chan struct{}),
	}
	defer func() {
		session.close()
		session.wg.Wait()
	}()
	session.configureClientLiveness()
	session.startSessionBackgroundLoops()

	for {
		messageType, data, readErr := client.ReadMessage()
		if readErr != nil {
			if !websocket.IsCloseError(readErr, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				logger.LogDebug(c, "responses websocket client read ended: %v", readErr)
			}
			return
		}
		_ = client.SetReadDeadline(time.Now().Add(responsesWSPongTimeout))
		if messageType != websocket.TextMessage {
			session.sendError(types.NewErrorWithStatusCode(errors.New("Responses WebSocket v2 only accepts text JSON messages"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry()))
			continue
		}

		event, request, envelope, apiErr := parseResponsesWSClientEvent(data)
		if apiErr != nil {
			session.sendError(apiErr)
			continue
		}
		if event.Type != "response.create" {
			session.sendError(types.NewErrorWithStatusCode(fmt.Errorf("unsupported Responses WebSocket event type %q", event.Type), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry()))
			continue
		}

		session.mu.Lock()
		busy := session.activeTurn != nil
		session.mu.Unlock()
		if busy {
			session.sendError(types.NewErrorWithStatusCode(errors.New("only one response may be in flight on a Responses WebSocket connection"), types.ErrorCodeInvalidRequest, http.StatusConflict, types.ErrOptionWithSkipRetry()))
			continue
		}

		firstTurn := session.lockedModel == ""
		if firstTurn {
			if strings.TrimSpace(request.Model) == "" {
				session.sendError(types.NewErrorWithStatusCode(errors.New("model is required on the first response.create event"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry()))
				continue
			}
		} else {
			if request.Model == "" {
				request.Model = session.lockedModel
			} else if request.Model != session.lockedModel {
				session.sendError(types.NewErrorWithStatusCode(fmt.Errorf("model cannot change within a Responses WebSocket connection: %s -> %s", session.lockedModel, request.Model), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry()))
				continue
			}
		}

		selectedChannel := session.getChannel()
		if selectedChannel == nil {
			candidateChannel, selectErr := middleware.SelectChannelForModelFiltered(c, request.Model, func(candidate *model.Channel) bool {
				return candidate.GetOtherSettings().ResponsesWebSocketV2Enabled
			})
			if selectErr != nil {
				session.sendError(selectErr)
				continue
			}
			if !candidateChannel.GetOtherSettings().ResponsesWebSocketV2Enabled {
				session.sendError(types.NewErrorWithStatusCode(fmt.Errorf("selected channel %d does not enable Responses WebSocket v2", candidateChannel.Id), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry()))
				continue
			}
			selectedChannel = candidateChannel
			session.setChannel(selectedChannel)
		}
		if firstTurn {
			session.lockedModel = request.Model
		}

		turn, preparedResponse, adaptor, prepareErr := session.prepareTurn(request)
		if prepareErr != nil {
			session.sendError(prepareErr)
			continue
		}
		outbound, marshalErr := buildResponsesWSOutboundEvent(envelope, preparedResponse)
		if marshalErr != nil {
			turn.finish(false)
			session.sendError(types.NewError(marshalErr, types.ErrorCodeJsonMarshalFailed, types.ErrOptionWithSkipRetry()))
			continue
		}
		// outbound is newly marshaled for this turn and is never mutated. Transfer
		// ownership instead of copying a message that may be as large as the
		// configured WebSocket request limit.
		turn.outbound = outbound
		turn.channel = selectedChannel
		turn.replayEnabled = selectedChannel.GetOtherSettings().ResponsesWebSocketV2ReplayEnabled
		turn.reconnect = func() (*websocket.Conn, *http.Response, error) {
			return channel.DoResponsesWssRequest(adaptor, turn.ctx, turn.info)
		}

		upstream := session.getUpstream()
		connectedUpstream := false
		if upstream == nil {
			upstreamConn, resp, dialErr := channel.DoResponsesWssRequest(adaptor, turn.ctx, turn.info)
			if dialErr != nil {
				turn.finish(false)
				status := http.StatusBadGateway
				if resp != nil {
					status = resp.StatusCode
					_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
					_ = resp.Body.Close()
				}
				apiErr := types.NewErrorWithStatusCode(dialErr, types.ErrorCodeDoRequestFailed, status, types.ErrOptionWithSkipRetry())
				processChannelError(turn.ctx, *types.NewChannelError(selectedChannel.Id, selectedChannel.Type, selectedChannel.Name, selectedChannel.ChannelInfo.IsMultiKey, common.GetContextKeyString(turn.ctx, appconstant.ContextKeyChannelKey), selectedChannel.GetAutoBan()), apiErr)
				session.clearChannel(selectedChannel)
				session.sendError(apiErr)
				continue
			}
			upstream = session.attachUpstream(upstreamConn)
			if upstream == nil {
				turn.finish(false)
				return
			}
			connectedUpstream = true
		}

		if !session.activateTurn(upstream, turn) {
			turn.finish(false)
			session.sendError(types.NewErrorWithStatusCode(errors.New("upstream Responses WebSocket disconnected before request dispatch"), types.ErrorCodeDoRequestFailed, http.StatusBadGateway, types.ErrOptionWithSkipRetry()))
			continue
		}
		// From this point the request may be observed by the upstream even when
		// WriteMessage returns an error. Replay remains opt-in because dispatch
		// acknowledgement is inherently ambiguous on a broken connection.
		session.mu.Lock()
		if session.activeTurn == turn {
			turn.requestDispatched = true
		}
		session.mu.Unlock()
		session.upstreamWriteMu.Lock()
		writeErr := upstream.conn.WriteMessage(websocket.TextMessage, outbound)
		session.upstreamWriteMu.Unlock()
		if writeErr != nil {
			session.handleUpstreamFailure(upstream, writeErr)
			continue
		}
		if connectedUpstream {
			service.RecordChannelAffinity(c, selectedChannel.Id)
		}
	}
}

func parseResponsesWSClientEvent(data []byte) (*responsesWSClientEvent, *dto.OpenAIResponsesRequest, map[string]json.RawMessage, *types.NewAPIError) {
	var envelope map[string]json.RawMessage
	if err := common.Unmarshal(data, &envelope); err != nil {
		return nil, nil, nil, types.NewErrorWithStatusCode(fmt.Errorf("invalid Responses WebSocket JSON: %w", err), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	var event responsesWSClientEvent
	if err := common.Unmarshal(data, &event); err != nil {
		return nil, nil, nil, types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	var request dto.OpenAIResponsesRequest
	if err := common.Unmarshal(data, &request); err != nil {
		return nil, nil, nil, types.NewErrorWithStatusCode(fmt.Errorf("invalid response.create payload: %w", err), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	return &event, &request, envelope, nil
}

func buildResponsesWSOutboundEvent(original map[string]json.RawMessage, prepared json.RawMessage) ([]byte, error) {
	var preparedFields map[string]json.RawMessage
	if err := common.Unmarshal(prepared, &preparedFields); err != nil {
		return nil, err
	}

	outbound := make(map[string]json.RawMessage, len(original)+len(preparedFields))
	for key, value := range original {
		if key == "type" || key == "background" {
			continue
		}
		if _, knownRequestField := responsesWSRequestJSONFields[key]; knownRequestField {
			continue
		}
		outbound[key] = value
	}
	for key, value := range preparedFields {
		outbound[key] = value
	}
	if previousResponseID, ok := original["previous_response_id"]; ok && strings.TrimSpace(string(previousResponseID)) == "null" {
		outbound["previous_response_id"] = previousResponseID
	}
	eventType, err := common.Marshal("response.create")
	if err != nil {
		return nil, err
	}
	outbound["type"] = eventType
	return common.Marshal(outbound)
}

func responseRequestJSONFieldNames() map[string]struct{} {
	requestType := reflect.TypeOf(dto.OpenAIResponsesRequest{})
	fields := make(map[string]struct{}, requestType.NumField())
	for i := 0; i < requestType.NumField(); i++ {
		name := strings.Split(requestType.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			fields[name] = struct{}{}
		}
	}
	return fields
}

func (s *responsesWebSocketSession) prepareTurn(request *dto.OpenAIResponsesRequest) (*responsesWebSocketTurn, json.RawMessage, channel.Adaptor, *types.NewAPIError) {
	turnCtx := newResponsesWSTurnContext(s.baseCtx, s.lockedModel)
	rateFinish, rateErr := middleware.AcquireModelRequestRateLimit(turnCtx)
	if rateErr != nil {
		return nil, nil, nil, rateErr
	}
	releaseUser, acquired, concurrencyErr := middleware.AcquireUserConcurrency(turnCtx)
	if concurrencyErr != nil {
		logger.LogWarn(turnCtx, "responses websocket user concurrency check failed open: "+concurrencyErr.Error())
		releaseUser = func() {}
		acquired = true
	}
	if !acquired {
		rateFinish(false)
		return nil, nil, nil, types.NewErrorWithStatusCode(errors.New("user concurrency limit exceeded"), types.ErrorCodeAccessDenied, http.StatusTooManyRequests, types.ErrOptionWithSkipRetry())
	}
	releaseBusiness := middleware.AcquireRelayConcurrency(turnCtx.Request.Context())

	cleanup := func() {
		rateFinish(false)
		releaseBusiness()
		releaseUser()
	}
	relayInfo, err := relaycommon.GenRelayInfo(turnCtx, types.RelayFormatOpenAIResponses, request, nil)
	if err != nil {
		cleanup()
		return nil, nil, nil, types.NewError(err, types.ErrorCodeGenRelayInfoFailed, types.ErrOptionWithSkipRetry())
	}
	relayInfo.IsStream = true
	turnCtx.Set(string(appconstant.ContextKeyIsStream), true)

	needSensitiveCheck := setting.ShouldCheckPromptSensitive()
	var meta *types.TokenCountMeta
	if needSensitiveCheck || appconstant.CountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}
	if needSensitiveCheck && meta != nil {
		contains, words := service.CheckSensitiveText(meta.CombineText)
		if contains {
			cleanup()
			return nil, nil, nil, types.NewErrorWithStatusCode(fmt.Errorf("sensitive words detected: %s", strings.Join(words, ", ")), types.ErrorCodeSensitiveWordsDetected, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
	}
	tokens, err := service.EstimateRequestToken(turnCtx, meta, relayInfo)
	if err != nil {
		cleanup()
		return nil, nil, nil, types.NewError(err, types.ErrorCodeCountTokenFailed, types.ErrOptionWithSkipRetry())
	}
	relayInfo.SetEstimatePromptTokens(tokens)
	priceData, err := helper.ModelPriceHelper(turnCtx, relayInfo, tokens, meta)
	if err != nil {
		cleanup()
		return nil, nil, nil, types.NewErrorWithStatusCode(err, types.ErrorCodeModelPriceError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if !priceData.FreeModel {
		if apiErr := service.PreConsumeBilling(turnCtx, priceData.QuotaToPreConsume, relayInfo); apiErr != nil {
			cleanup()
			return nil, nil, nil, apiErr
		}
	}

	turn := &responsesWebSocketTurn{
		ctx:             turnCtx,
		info:            relayInfo,
		accumulator:     openairelay.NewResponsesEventAccumulator(),
		rateLimitFinish: rateFinish,
		releaseUser:     releaseUser,
		releaseBusiness: releaseBusiness,
	}
	prepared, adaptor, apiErr := relay.PrepareResponsesWebSocketRequest(turnCtx, relayInfo, request)
	if apiErr != nil {
		turn.finish(false)
		return nil, nil, nil, apiErr
	}
	return turn, json.RawMessage(prepared), adaptor, nil
}

func newResponsesWSTurnContext(base *gin.Context, modelName string) *gin.Context {
	turnCtx := base.Copy()
	requestCopy := base.Request.Clone(base.Request.Context())
	requestURL := *base.Request.URL
	requestURL.Path = "/v1/responses"
	requestURL.RawQuery = ""
	requestCopy.URL = &requestURL
	requestCopy.Method = http.MethodPost
	requestCopy.Header = base.Request.Header.Clone()
	requestCopy.Header.Set("Content-Type", "application/json")
	turnCtx.Request = requestCopy
	turnCtx.Set(common.RequestIdKey, common.GetTimeString()+common.GetRandomString(8))
	common.SetContextKey(turnCtx, appconstant.ContextKeyRequestStartTime, time.Now())
	common.SetContextKey(turnCtx, appconstant.ContextKeyOriginalModel, modelName)
	return turnCtx
}

func (s *responsesWebSocketSession) configureClientLiveness() {
	s.client.SetPongHandler(func(string) error {
		return s.client.SetReadDeadline(time.Now().Add(responsesWSPongTimeout))
	})
}

func (s *responsesWebSocketSession) configureUpstreamLiveness(upstream *responsesWSUpstreamConnection) {
	if upstream == nil || upstream.conn == nil {
		return
	}
	_ = upstream.conn.SetReadDeadline(time.Now().Add(responsesWSPongTimeout))
	upstream.conn.SetPongHandler(func(string) error {
		return upstream.conn.SetReadDeadline(time.Now().Add(responsesWSPongTimeout))
	})
	maxMessageBytes := int64(appconstant.MaxRequestBodyMB) * 1024 * 1024
	if maxMessageBytes > 0 {
		upstream.conn.SetReadLimit(maxMessageBytes)
	}
}

func (s *responsesWebSocketSession) startSessionBackgroundLoops() {
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		s.clientHeartbeat()
	}()
	go func() {
		defer s.wg.Done()
		timer := time.NewTimer(responsesWSMaxConnectionAge)
		defer timer.Stop()
		select {
		case <-timer.C:
			s.closeWithCode(websocket.CloseNormalClosure, "Responses WebSocket maximum connection age reached")
		case <-s.closed:
		}
	}()
}

func (s *responsesWebSocketSession) startUpstreamLoops(upstream *responsesWSUpstreamConnection) {
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		s.readUpstream(upstream)
	}()
	go func() {
		defer s.wg.Done()
		s.upstreamHeartbeat(upstream)
	}()
}

func (s *responsesWebSocketSession) readUpstream(upstream *responsesWSUpstreamConnection) {
	for {
		messageType, data, err := upstream.conn.ReadMessage()
		if err != nil {
			s.handleUpstreamFailure(upstream, err)
			return
		}
		if !s.isCurrentUpstream(upstream) {
			return
		}
		_ = upstream.conn.SetReadDeadline(time.Now().Add(responsesWSPongTimeout))

		var finished *responsesWebSocketTurn
		var successful bool
		var turn *responsesWebSocketTurn
		// Serialize state transition and downstream delivery with sendError. This
		// prevents a heartbeat failure from writing an error before a frame that
		// has already been accepted by this reader.
		s.clientWriteMu.Lock()
		s.mu.Lock()
		if s.upstream != upstream {
			s.mu.Unlock()
			s.clientWriteMu.Unlock()
			return
		}
		turn = s.activeTurn
		if messageType == websocket.TextMessage {
			if turn != nil {
				event, consumeErr := turn.accumulator.Consume(turn.ctx, turn.info, data)
				if consumeErr != nil {
					logger.LogError(turn.ctx, "failed to parse Responses WebSocket event: "+consumeErr.Error())
				} else {
					turn.upstreamEvent = true
					if openairelay.IsResponsesFirstOutputEvent(event) {
						turn.info.SetFirstResponseTime()
					}
					if event != nil && turn.accumulator.Terminal() {
						finished = turn
						successful = turn.accumulator.Successful()
						s.activeTurn = nil
					}
				}
			}
		}
		s.mu.Unlock()
		writeErr := s.client.WriteMessage(messageType, data)
		s.clientWriteMu.Unlock()
		if finished != nil {
			finished.finish(successful)
		}
		if writeErr != nil {
			s.close()
			return
		}
	}
}

func (s *responsesWebSocketSession) clientHeartbeat() {
	ticker := time.NewTicker(responsesWSHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			deadline := now.Add(10 * time.Second)
			s.clientWriteMu.Lock()
			clientErr := s.client.WriteControl(websocket.PingMessage, nil, deadline)
			s.clientWriteMu.Unlock()
			if clientErr != nil {
				s.close()
				return
			}
		case <-s.closed:
			return
		}
	}
}

func (s *responsesWebSocketSession) upstreamHeartbeat(upstream *responsesWSUpstreamConnection) {
	ticker := time.NewTicker(responsesWSHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			if !s.isCurrentUpstream(upstream) {
				return
			}
			s.upstreamWriteMu.Lock()
			err := upstream.conn.WriteControl(websocket.PingMessage, nil, now.Add(10*time.Second))
			s.upstreamWriteMu.Unlock()
			if err != nil {
				s.handleUpstreamFailure(upstream, err)
				return
			}
		case <-upstream.done:
			return
		case <-s.closed:
			return
		}
	}
}

func (s *responsesWebSocketSession) getUpstream() *responsesWSUpstreamConnection {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.upstream
}

func (s *responsesWebSocketSession) getChannel() *model.Channel {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.channel
}

func (s *responsesWebSocketSession) setChannel(channel *model.Channel) {
	s.mu.Lock()
	s.channel = channel
	s.mu.Unlock()
}

func (s *responsesWebSocketSession) clearChannel(channel *model.Channel) {
	s.mu.Lock()
	if s.channel == channel {
		s.channel = nil
	}
	s.mu.Unlock()
}

func (s *responsesWebSocketSession) attachUpstream(conn *websocket.Conn) *responsesWSUpstreamConnection {
	if conn == nil {
		return nil
	}
	upstream := newResponsesWSUpstreamConnection(conn)
	s.configureUpstreamLiveness(upstream)

	s.mu.Lock()
	select {
	case <-s.closed:
		s.mu.Unlock()
		upstream.close()
		return nil
	default:
	}
	if s.upstream != nil {
		existing := s.upstream
		s.mu.Unlock()
		upstream.close()
		return existing
	}
	s.upstream = upstream
	s.mu.Unlock()

	s.startUpstreamLoops(upstream)
	return upstream
}

func (s *responsesWebSocketSession) activateTurn(upstream *responsesWSUpstreamConnection, turn *responsesWebSocketTurn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.upstream != upstream || s.activeTurn != nil {
		return false
	}
	s.activeTurn = turn
	return true
}

func (s *responsesWebSocketSession) isCurrentUpstream(upstream *responsesWSUpstreamConnection) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.upstream == upstream
}

func (s *responsesWebSocketSession) detachUpstream(upstream *responsesWSUpstreamConnection, allowReplay bool) (*responsesWebSocketTurn, bool, bool) {
	s.mu.Lock()
	if s.upstream != upstream {
		s.mu.Unlock()
		return nil, false, false
	}
	s.upstream = nil
	s.channel = nil
	turn := s.activeTurn
	replay := allowReplay && turn != nil && turn.replayEnabled && turn.requestDispatched && !turn.upstreamEvent && turn.replayCount == 0 &&
		len(turn.outbound) > 0 && turn.channel != nil && turn.reconnect != nil
	if replay {
		turn.replayCount++
		turn.accumulator = openairelay.NewResponsesEventAccumulator()
	} else {
		s.activeTurn = nil
	}
	s.mu.Unlock()

	s.upstreamWriteMu.Lock()
	upstream.close()
	s.upstreamWriteMu.Unlock()
	return turn, true, replay
}

func (s *responsesWebSocketSession) handleUpstreamFailure(upstream *responsesWSUpstreamConnection, err error) {
	recoverable := isRecoverableResponsesWSDisconnect(err)
	turn, detached, replay := s.detachUpstream(upstream, recoverable)
	if !detached {
		return
	}
	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
		logger.LogDebug(s.baseCtx, "responses websocket upstream closed: %v", err)
	} else {
		logger.LogWarn(s.baseCtx, fmt.Sprintf("responses websocket upstream disconnected: %v", err))
	}
	if turn == nil {
		return
	}
	if replay {
		logger.LogWarn(s.baseCtx, fmt.Sprintf("responses websocket upstream disconnected before any upstream event; replaying once: %v", err))
		if s.replayTurn(turn) {
			return
		}
		logger.LogWarn(s.baseCtx, "responses websocket internal replay failed; returning upstream error to client")
	} else if recoverable && turn.replayEnabled && turn.requestDispatched && turn.upstreamEvent {
		logger.LogWarn(s.baseCtx, "responses websocket replay skipped because the upstream request was already acknowledged or delivered")
	}
	turn.finish(false)
	if recoverable {
		// Do not turn a transient upstream transport failure into a client-visible
		// Responses API 502 event. Codex already reconnects a WebSocket closed with
		// Service Restart; using the transport signal preserves that behavior while
		// avoiding a prominent application-error banner for a short reconnect that
		// is otherwise invisible to the user. Keep the detailed cause in server logs
		// and leave non-recoverable failures on the explicit error-event path below.
		logger.LogWarn(s.baseCtx, "responses websocket recoverable upstream failure; requesting client transport reconnect")
		s.closeWithCode(websocket.CloseServiceRestart, "")
		return
	}
	s.sendError(types.NewErrorWithStatusCode(fmt.Errorf("upstream Responses WebSocket disconnected: %w", err), types.ErrorCodeDoRequestFailed, http.StatusBadGateway, types.ErrOptionWithSkipRetry()))
}

func isRecoverableResponsesWSDisconnect(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, websocket.ErrReadLimit) || errors.Is(err, websocket.ErrCloseSent) {
		return false
	}
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		switch closeErr.Code {
		case websocket.CloseGoingAway,
			websocket.CloseAbnormalClosure,
			websocket.CloseInternalServerErr,
			websocket.CloseServiceRestart,
			websocket.CloseTryAgainLater:
			return true
		default:
			return false
		}
	}
	var netErr interface {
		Timeout() bool
		Temporary() bool
	}
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}
	errorText := strings.ToLower(err.Error())
	return strings.Contains(errorText, "broken pipe") ||
		strings.Contains(errorText, "connection reset") ||
		strings.Contains(errorText, "unexpected eof") ||
		errors.Is(err, io.EOF)
}

func (s *responsesWebSocketSession) replayTurn(turn *responsesWebSocketTurn) bool {
	if turn == nil || turn.reconnect == nil {
		return false
	}
	conn, resp, err := turn.reconnect()
	status := 0
	if resp != nil {
		status = resp.StatusCode
		if resp.Body != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
			_ = resp.Body.Close()
		}
	}
	if err != nil || conn == nil {
		if conn != nil {
			_ = conn.Close()
		}
		if err != nil {
			logger.LogWarn(s.baseCtx, fmt.Sprintf("responses websocket internal replay dial failed (status=%d): %v", status, err))
		} else {
			logger.LogWarn(s.baseCtx, "responses websocket internal replay dial returned no connection")
		}
		s.abandonReplay(turn)
		return false
	}

	// Publish the channel before starting the upstream reader so an immediate
	// close cannot race with a later setChannel and leave a stale channel pinned.
	s.setChannel(turn.channel)
	upstream := s.attachUpstream(conn)
	if upstream == nil {
		s.abandonReplay(turn)
		return false
	}
	s.upstreamWriteMu.Lock()
	err = upstream.conn.WriteMessage(websocket.TextMessage, turn.outbound)
	s.upstreamWriteMu.Unlock()
	if err != nil {
		// replayCount is already one, so this second failure follows the normal
		// client-visible error path without recursing into another replay.
		s.handleUpstreamFailure(upstream, err)
		return true
	}
	if !s.isReplayCurrent(upstream, turn) {
		return true
	}
	// This is the only replay attempt. Once it has been dispatched, retaining the
	// request body cannot enable another retry and only increases memory usage.
	s.mu.Lock()
	if s.activeTurn == turn {
		turn.outbound = nil
		turn.reconnect = nil
	}
	s.mu.Unlock()
	logger.LogInfo(s.baseCtx, fmt.Sprintf("responses websocket internal replay connected on channel #%d", turn.channel.Id))
	service.RecordChannelAffinity(s.baseCtx, turn.channel.Id)
	return true
}

func (s *responsesWebSocketSession) isReplayCurrent(upstream *responsesWSUpstreamConnection, turn *responsesWebSocketTurn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.upstream == upstream && s.activeTurn == turn && s.channel == turn.channel
}

func (s *responsesWebSocketSession) abandonReplay(turn *responsesWebSocketTurn) {
	s.mu.Lock()
	if s.activeTurn == turn {
		s.activeTurn = nil
	}
	s.channel = nil
	s.mu.Unlock()
}

func (s *responsesWebSocketSession) sendError(apiErr *types.NewAPIError) {
	if apiErr == nil || s.client == nil {
		return
	}
	payload, err := common.Marshal(responsesWSErrorEvent{Type: "error", Status: apiErr.StatusCode, Error: apiErr.ToOpenAIError()})
	if err != nil {
		return
	}
	s.clientWriteMu.Lock()
	_ = s.client.WriteMessage(websocket.TextMessage, payload)
	s.clientWriteMu.Unlock()
}

func (s *responsesWebSocketSession) closeWithCode(code int, reason string) {
	s.clientWriteMu.Lock()
	_ = s.client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(5*time.Second))
	s.clientWriteMu.Unlock()
	s.close()
}

func (s *responsesWebSocketSession) close() {
	s.closeOnce.Do(func() {
		close(s.closed)
		s.mu.Lock()
		turn := s.activeTurn
		s.activeTurn = nil
		upstream := s.upstream
		s.upstream = nil
		s.mu.Unlock()
		if turn != nil {
			turn.finish(false)
		}
		if upstream != nil {
			s.upstreamWriteMu.Lock()
			upstream.close()
			s.upstreamWriteMu.Unlock()
		}
		if s.client != nil {
			_ = s.client.Close()
		}
	})
}

var responsesWebSocketUpgrader = websocket.Upgrader{
	EnableCompression: true,
	CheckOrigin: func(*http.Request) bool {
		return true
	},
}
