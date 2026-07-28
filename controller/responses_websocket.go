package controller

import (
	"bytes"
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

type responsesWSFailedEvent struct {
	Type     string                    `json:"type"`
	Response responsesWSFailedResponse `json:"response"`
}

type responsesWSFailedResponse struct {
	ID        string                 `json:"id"`
	Object    string                 `json:"object"`
	CreatedAt int64                  `json:"created_at"`
	Status    string                 `json:"status"`
	Error     responsesWSFailedError `json:"error"`
	Model     string                 `json:"model"`
	Output    []dto.ResponsesOutput  `json:"output"`
	Usage     *dto.Usage             `json:"usage"`
}

type responsesWSFailedError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

var responsesWSRequestJSONFields = responseRequestJSONFieldNames()

type responsesWebSocketTurn struct {
	ctx               *gin.Context
	info              *relaycommon.RelayInfo
	accumulator       *openairelay.ResponsesEventAccumulator
	replayMu          sync.Mutex
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
	cancel            context.CancelFunc
	finishOnce        sync.Once
}

func (t *responsesWebSocketTurn) setReplayState(outbound []byte, reconnect func() (*websocket.Conn, *http.Response, error)) {
	if t == nil {
		return
	}
	t.replayMu.Lock()
	t.outbound = outbound
	t.reconnect = reconnect
	t.replayMu.Unlock()
}

func (t *responsesWebSocketTurn) replayStateAvailable() bool {
	if t == nil {
		return false
	}
	t.replayMu.Lock()
	defer t.replayMu.Unlock()
	return len(t.outbound) > 0 && t.reconnect != nil
}

func (t *responsesWebSocketTurn) replaySnapshot() ([]byte, func() (*websocket.Conn, *http.Response, error), bool) {
	if t == nil {
		return nil, nil, false
	}
	t.replayMu.Lock()
	defer t.replayMu.Unlock()
	if len(t.outbound) == 0 || t.reconnect == nil {
		return nil, nil, false
	}
	return t.outbound, t.reconnect, true
}

func (t *responsesWebSocketTurn) clearReplayState() {
	if t == nil {
		return
	}
	t.replayMu.Lock()
	t.outbound = nil
	t.reconnect = nil
	t.replayMu.Unlock()
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
		t.clearReplayState()
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
		if t.cancel != nil {
			t.cancel()
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
			candidateChannel, selectErr := middleware.SelectChannelForModelFiltered(c, request.Model, supportsResponsesWebSocketChannel)
			if selectErr != nil {
				session.sendError(selectErr)
				continue
			}
			selectedChannel = candidateChannel
			session.setChannel(selectedChannel)
		}
		if firstTurn {
			session.lockedModel = request.Model
		}

		upstreamWebSocket := selectedChannel.GetOtherSettings().ResponsesWebSocketV2Enabled
		turn, preparedResponse, adaptor, prepareErr := session.prepareTurn(request, !upstreamWebSocket)
		if prepareErr != nil {
			session.sendError(prepareErr)
			continue
		}
		turn.channel = selectedChannel
		if !upstreamWebSocket {
			if !session.activateSSETurn(turn) {
				turn.finish(false)
				session.sendError(types.NewErrorWithStatusCode(errors.New("Responses SSE turn could not be activated"), types.ErrorCodeDoRequestFailed, http.StatusBadGateway, types.ErrOptionWithSkipRetry()))
				continue
			}
			logger.LogInfo(turn.ctx, fmt.Sprintf("responses websocket client using upstream SSE on channel #%d", selectedChannel.Id))
			session.startSSETurn(turn, preparedResponse, adaptor)
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
		turn.setReplayState(outbound, func() (*websocket.Conn, *http.Response, error) {
			return channel.DoResponsesWssRequest(adaptor, turn.ctx, turn.info)
		})
		turn.replayEnabled = selectedChannel.GetOtherSettings().ResponsesWebSocketV2ReplayEnabled

		upstream := session.getUpstream()
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
				session.sendTurnFailure(turn, apiErr)
				continue
			}
			upstream = session.attachUpstream(upstreamConn)
			if upstream == nil {
				turn.finish(false)
				return
			}
		}

		if !session.activateTurn(upstream, turn) {
			turn.finish(false)
			session.sendTurnFailure(turn, types.NewErrorWithStatusCode(errors.New("upstream Responses WebSocket disconnected before request dispatch"), types.ErrorCodeDoRequestFailed, http.StatusBadGateway, types.ErrOptionWithSkipRetry()))
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
	}
}

func supportsResponsesWebSocketChannel(candidate *model.Channel) bool {
	if candidate == nil {
		return false
	}
	if candidate.GetOtherSettings().ResponsesWebSocketV2Enabled {
		return true
	}
	switch candidate.Type {
	case appconstant.ChannelTypeOpenAI, appconstant.ChannelTypeSub2API:
		return true
	default:
		return false
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

func (s *responsesWebSocketSession) prepareTurn(request *dto.OpenAIResponsesRequest, forceStream bool) (*responsesWebSocketTurn, json.RawMessage, channel.Adaptor, *types.NewAPIError) {
	turnCtx, cancel := newResponsesWSTurnContext(s.baseCtx, s.lockedModel)
	rateFinish, rateErr := middleware.AcquireModelRequestRateLimit(turnCtx)
	if rateErr != nil {
		cancel()
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
		cancel()
		return nil, nil, nil, types.NewErrorWithStatusCode(errors.New("user concurrency limit exceeded"), types.ErrorCodeAccessDenied, http.StatusTooManyRequests, types.ErrOptionWithSkipRetry())
	}
	releaseBusiness := middleware.AcquireRelayConcurrency(turnCtx.Request.Context())

	cleanup := func() {
		rateFinish(false)
		releaseBusiness()
		releaseUser()
		cancel()
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
		cancel:          cancel,
	}
	prepared, adaptor, apiErr := relay.PrepareResponsesWebSocketRequest(turnCtx, relayInfo, request, forceStream)
	if apiErr != nil {
		turn.finish(false)
		return nil, nil, nil, apiErr
	}
	return turn, json.RawMessage(prepared), adaptor, nil
}

func newResponsesWSTurnContext(base *gin.Context, modelName string) (*gin.Context, context.CancelFunc) {
	turnCtx := base.Copy()
	requestContext, cancel := context.WithCancel(base.Request.Context())
	requestCopy := base.Request.Clone(requestContext)
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
	return turnCtx, cancel
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
					}
				}
			}
		}
		if finished != nil {
			finished.finish(successful)
			if successful && finished.channel != nil {
				service.RecordChannelAffinity(s.baseCtx, finished.channel.Id)
			}
			s.activeTurn = nil
		}
		s.mu.Unlock()
		writeErr := s.client.WriteMessage(messageType, data)
		s.clientWriteMu.Unlock()
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

func (s *responsesWebSocketSession) activateSSETurn(turn *responsesWebSocketTurn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.closed:
		return false
	default:
	}
	if s.activeTurn != nil {
		return false
	}
	s.activeTurn = turn
	return true
}

func (s *responsesWebSocketSession) startSSETurn(turn *responsesWebSocketTurn, requestBody []byte, adaptor channel.Adaptor) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runSSETurn(turn, requestBody, adaptor)
	}()
}

func (s *responsesWebSocketSession) runSSETurn(turn *responsesWebSocketTurn, requestBody []byte, adaptor channel.Adaptor) {
	turn.info.DisablePing = true
	turn.info.StreamStatus = relaycommon.NewStreamStatus()
	turn.info.UpstreamRequestBodySize = int64(len(requestBody))
	turn.ctx.Request.Header.Set("Accept", "text/event-stream")
	resp, err := channel.DoApiRequestWithContext(adaptor, turn.ctx, turn.info, bytes.NewReader(requestBody))
	requestBody = nil
	if err != nil {
		turn.info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, err)
		s.failSSETurn(turn, types.NewErrorWithStatusCode(fmt.Errorf("upstream Responses SSE request failed: %w", err), types.ErrorCodeDoRequestFailed, http.StatusBadGateway, types.ErrOptionWithSkipRetry()))
		return
	}
	if resp == nil {
		err = errors.New("upstream Responses SSE returned no HTTP response")
		turn.info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, err)
		s.failSSETurn(turn, types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponse, http.StatusBadGateway, types.ErrOptionWithSkipRetry()))
		return
	}
	if resp.StatusCode != http.StatusOK {
		apiErr := service.RelayErrorHandler(turn.ctx.Request.Context(), resp, false)
		turn.info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, apiErr)
		s.failSSETurn(turn, apiErr)
		return
	}
	defer service.CloseResponseBodyGracefully(resp)

	terminal := false
	idleTimeout := time.Duration(appconstant.StreamingTimeout) * time.Second
	_, scanErr := helper.ScanSSEDataWithIdleTimeout(turn.ctx.Request.Context(), resp.Body, idleTimeout, func(data []byte) (bool, error) {
		var finished bool
		var successful bool

		s.clientWriteMu.Lock()
		s.mu.Lock()
		if s.activeTurn != turn {
			s.mu.Unlock()
			s.clientWriteMu.Unlock()
			return true, nil
		}
		event, consumeErr := turn.accumulator.Consume(turn.ctx, turn.info, data)
		if consumeErr != nil {
			s.mu.Unlock()
			s.clientWriteMu.Unlock()
			return false, consumeErr
		}
		turn.upstreamEvent = true
		turn.info.ReceivedResponseCount++
		if openairelay.IsResponsesFirstOutputEvent(event) {
			turn.info.SetFirstResponseTime()
		}
		if turn.accumulator.Terminal() {
			terminal = true
			finished = true
			successful = turn.accumulator.Successful()
			turn.info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
			turn.finish(successful)
			if successful && turn.channel != nil {
				service.RecordChannelAffinity(s.baseCtx, turn.channel.Id)
			}
			s.activeTurn = nil
		}
		s.mu.Unlock()
		writeErr := s.client.WriteMessage(websocket.TextMessage, data)
		s.clientWriteMu.Unlock()
		if writeErr != nil {
			s.close()
			return true, writeErr
		}
		return finished, nil
	})

	if terminal {
		logger.LogInfo(turn.ctx, fmt.Sprintf("responses websocket upstream SSE ended: %s", turn.info.StreamStatus.Summary()))
		return
	}
	if !s.isActiveTurn(turn) {
		return
	}
	if scanErr != nil {
		status := http.StatusBadGateway
		if errors.Is(scanErr, helper.ErrSSEIdleTimeout) {
			status = http.StatusGatewayTimeout
			turn.info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, scanErr)
		} else {
			turn.info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, scanErr)
		}
		s.failSSETurn(turn, types.NewErrorWithStatusCode(fmt.Errorf("upstream Responses SSE stream failed: %w", scanErr), types.ErrorCodeDoRequestFailed, status, types.ErrOptionWithSkipRetry()))
		return
	}
	err = errors.New("upstream Responses SSE ended before a terminal event")
	turn.info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonEOF, err)
	s.failSSETurn(turn, types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponse, http.StatusBadGateway, types.ErrOptionWithSkipRetry()))
}

func (s *responsesWebSocketSession) isActiveTurn(turn *responsesWebSocketTurn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeTurn == turn
}

func (s *responsesWebSocketSession) failSSETurn(turn *responsesWebSocketTurn, apiErr *types.NewAPIError) {
	s.mu.Lock()
	if s.activeTurn != turn {
		s.mu.Unlock()
		return
	}
	s.activeTurn = nil
	if s.channel == turn.channel {
		s.channel = nil
	}
	s.mu.Unlock()

	turn.finish(false)
	if turn.channel != nil {
		processChannelError(turn.ctx, *types.NewChannelError(turn.channel.Id, turn.channel.Type, turn.channel.Name, turn.channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(turn.ctx, appconstant.ContextKeyChannelKey), turn.channel.GetAutoBan()), apiErr)
	}
	logger.LogWarn(turn.ctx, fmt.Sprintf("responses websocket upstream SSE turn failed without closing downstream websocket: %v", apiErr))
	s.sendTurnFailure(turn, apiErr)
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
		turn.channel != nil && turn.replayStateAvailable()
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
	logger.LogWarn(s.baseCtx, "responses websocket upstream turn failed without closing downstream websocket")
	s.sendTurnFailure(turn, types.NewErrorWithStatusCode(fmt.Errorf("upstream Responses WebSocket disconnected: %w", err), types.ErrorCodeDoRequestFailed, http.StatusBadGateway, types.ErrOptionWithSkipRetry()))
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
	outbound, reconnect, ok := turn.replaySnapshot()
	if !ok {
		return false
	}
	conn, resp, err := reconnect()
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
	err = upstream.conn.WriteMessage(websocket.TextMessage, outbound)
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
	turn.clearReplayState()
	logger.LogInfo(s.baseCtx, fmt.Sprintf("responses websocket internal replay connected on channel #%d", turn.channel.Id))
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

func (s *responsesWebSocketSession) sendTurnFailure(turn *responsesWebSocketTurn, apiErr *types.NewAPIError) {
	if turn == nil || apiErr == nil || s.client == nil {
		return
	}
	responseID := ""
	if turn.accumulator != nil {
		responseID = turn.accumulator.ResponseID()
	}
	if responseID == "" {
		responseID = "resp_starnexus_" + common.GetRandomString(16)
	}
	modelName := ""
	if turn.info != nil {
		modelName = turn.info.OriginModelName
	}
	if modelName == "" {
		modelName = s.lockedModel
	}
	openAIError := apiErr.ToOpenAIError()
	errorCode := strings.TrimSpace(fmt.Sprint(openAIError.Code))
	if errorCode == "" || errorCode == "<nil>" {
		errorCode = "upstream_transport_error"
	}
	payload, err := common.Marshal(responsesWSFailedEvent{
		Type: "response.failed",
		Response: responsesWSFailedResponse{
			ID:        responseID,
			Object:    "response",
			CreatedAt: time.Now().Unix(),
			Status:    "failed",
			Error: responsesWSFailedError{
				Code:    errorCode,
				Message: openAIError.Message,
			},
			Model:  modelName,
			Output: []dto.ResponsesOutput{},
			Usage:  nil,
		},
	})
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
