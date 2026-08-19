package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	responsesWSCapacityRetryMaxAge = 30 * time.Second
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

type responsesWSBufferedFrame struct {
	messageType int
	data        []byte
}

var responsesWSRequestJSONFields = responseRequestJSONFieldNames()

type responsesWebSocketTurn struct {
	ctx                    *gin.Context
	info                   *relaycommon.RelayInfo
	accumulator            *openairelay.ResponsesEventAccumulator
	replayMu               sync.Mutex
	outbound               []byte
	sseRequestBody         []byte
	sseAdaptor             channel.Adaptor
	originalRequest        *dto.OpenAIResponsesRequest
	forceSSEStream         bool
	upstreamMode           string
	upstreamIdentity       string
	closeUpstreamAfterTurn bool
	channel                *model.Channel
	reconnect              func() (*websocket.Conn, *http.Response, error)
	replayEnabled          bool
	requestDispatched      bool
	upstreamEvent          bool
	upstreamOutputStarted  bool
	upstreamEventCount     int
	capacitySafeEventCount int
	capacityRetryBlocked   bool
	capacityPrelude        []responsesWSBufferedFrame
	replayCount            int
	capacityRetryCount     int
	accountFailovers       int
	failoverStartedAt      time.Time
	rateLimitFinish        func(bool)
	releaseUser            func()
	releaseBusiness        func()
	cancel                 context.CancelFunc
	finishOnce             sync.Once
	replayInput            []json.RawMessage
	replayInputExists      bool
	replayToolContext      []json.RawMessage
	replayToolContextSeen  map[string]struct{}
	accountID              int
	turnState              string
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

func (t *responsesWebSocketTurn) setSSEReplayState(requestBody []byte, adaptor channel.Adaptor) {
	if t == nil {
		return
	}
	t.replayMu.Lock()
	t.sseRequestBody = requestBody
	t.sseAdaptor = adaptor
	t.replayMu.Unlock()
}

func (t *responsesWebSocketTurn) sseReplaySnapshot() ([]byte, channel.Adaptor, bool) {
	if t == nil {
		return nil, nil, false
	}
	t.replayMu.Lock()
	defer t.replayMu.Unlock()
	if len(t.sseRequestBody) == 0 || t.sseAdaptor == nil {
		return nil, nil, false
	}
	return t.sseRequestBody, t.sseAdaptor, true
}

func (t *responsesWebSocketTurn) clearSSEReplayState() {
	if t == nil {
		return
	}
	t.replayMu.Lock()
	t.sseRequestBody = nil
	t.sseAdaptor = nil
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
	t.sseRequestBody = nil
	t.sseAdaptor = nil
	t.replayMu.Unlock()
}

func (t *responsesWebSocketTurn) bufferCapacityPrelude(messageType int, data []byte) {
	if t == nil {
		return
	}
	t.capacityPrelude = append(t.capacityPrelude, responsesWSBufferedFrame{
		messageType: messageType,
		data:        append([]byte(nil), data...),
	})
}

func (t *responsesWebSocketTurn) takeCapacityPrelude() []responsesWSBufferedFrame {
	if t == nil || len(t.capacityPrelude) == 0 {
		return nil
	}
	frames := t.capacityPrelude
	t.capacityPrelude = nil
	return frames
}

func (t *responsesWebSocketTurn) resetCapacityPrelude() {
	if t == nil {
		return
	}
	t.capacityPrelude = nil
	t.capacitySafeEventCount = 0
	t.capacityRetryBlocked = false
}

type responsesWSUpstreamConnection struct {
	conn      *websocket.Conn
	identity  string
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
		t.resetCapacityPrelude()
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
		middleware.ReleaseUpstreamAccountSelection(t.ctx)
	})
}

type responsesWebSocketSession struct {
	baseCtx           *gin.Context
	client            *websocket.Conn
	upstream          *responsesWSUpstreamConnection
	channel           *model.Channel
	lockedModel       string
	lockedAccountID   int
	lockedChannelID   int
	lockedWSMode      string
	turnState         string
	replayInput       []json.RawMessage
	replayInputExists bool
	lastResponseID    string
	activeTurn        *responsesWebSocketTurn
	clientGone        bool
	mu                sync.Mutex
	clientWriteMu     sync.Mutex
	upstreamWriteMu   sync.Mutex
	closeOnce         sync.Once
	closed            chan struct{}
	wg                sync.WaitGroup
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
		session.handleClientLoopExit()
		session.wg.Wait()
		session.close()
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
		if err := session.inferToolContinuation(request, envelope); err != nil {
			session.sendError(types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry()))
			continue
		}
		if restoreErr := session.restoreContinuation(request); restoreErr != nil {
			session.sendError(restoreErr)
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
			session.mu.Lock()
			preferredChannelID := session.lockedChannelID
			session.mu.Unlock()
			if preferredChannelID > 0 {
				common.SetContextKey(c, appconstant.ContextKeyResponsesWebSocketPreferredChannelId, preferredChannelID)
			}
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

		turn, preparedResponse, adaptor, prepareErr := session.prepareTurn(request, selectedChannel)
		if prepareErr != nil {
			session.sendError(prepareErr)
			continue
		}
		preparedResponse, prepareErr = session.prepareReplayPayload(turn, preparedResponse)
		if prepareErr != nil {
			turn.finish(false)
			session.sendError(prepareErr)
			continue
		}
		turn.channel = selectedChannel
		turn.upstreamIdentity = responsesWSUpstreamIdentity(turn.ctx, selectedChannel)
		turn.closeUpstreamAfterTurn = false
		turn.ctx.Set("use_channel", []string{fmt.Sprintf("%d", selectedChannel.Id)})
		upstreamWebSocket := responsesWSModeUsesUpstreamWebSocket(turn.upstreamMode)
		upstreamTransport := "sse"
		upstreamMode := "responses_sse_bridge_" + turn.upstreamMode
		if upstreamWebSocket {
			upstreamTransport = "websocket"
			upstreamMode = "responses_websocket_v2_" + turn.upstreamMode
		}
		service.SetStreamTransportInfo(turn.ctx, "websocket", upstreamTransport, upstreamMode)
		if !upstreamWebSocket {
			if !session.activateSSETurn(turn) {
				turn.finish(false)
				session.sendError(types.NewErrorWithStatusCode(errors.New("Responses SSE turn could not be activated"), types.ErrorCodeDoRequestFailed, http.StatusBadGateway, types.ErrOptionWithSkipRetry()))
				continue
			}
			logger.LogInfo(turn.ctx, fmt.Sprintf("responses websocket client using upstream SSE on channel #%d", selectedChannel.Id))
			turn.setSSEReplayState(preparedResponse, adaptor)
			session.startSSETurn(turn)
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
		// Context-pool mode promises SUB2API-compatible continuity. A request that
		// failed before any upstream event is therefore replayable once even when
		// the legacy per-channel replay switch is disabled. The switch remains the
		// opt-in for other future modes.
		turn.replayEnabled = turn.upstreamMode == model.UpstreamOpenAIWSModeContextPool || selectedChannel.GetOtherSettings().ResponsesWebSocketV2ReplayEnabled

		upstream := session.getUpstreamForIdentity(turn.upstreamIdentity)
		if upstream == nil {
			upstreamConn, resp, dialErr := channel.DoResponsesWssRequest(adaptor, turn.ctx, turn.info)
			if dialErr != nil {
				status := http.StatusBadGateway
				if resp != nil {
					status = resp.StatusCode
					if resp.Body != nil {
						_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
						_ = resp.Body.Close()
					}
				}
				apiErr := types.NewErrorWithStatusCode(dialErr, types.ErrorCodeDoRequestFailed, status, types.ErrOptionWithSkipRetry())
				if status == 529 && session.retryInitialResponsesWSCapacityDial(turn, selectedChannel) {
					if replacementOutbound, _, ok := turn.replaySnapshot(); ok {
						outbound = replacementOutbound
					}
					continue
				}
				turn.info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, dialErr)
				turn.finish(false)
				processChannelError(turn.ctx, *types.NewChannelError(selectedChannel.Id, selectedChannel.Type, selectedChannel.Name, selectedChannel.ChannelInfo.IsMultiKey, common.GetContextKeyString(turn.ctx, appconstant.ContextKeyChannelKey), selectedChannel.GetAutoBan()), apiErr)
				session.clearChannel(selectedChannel)
				session.sendTurnFailure(turn, apiErr)
				continue
			}
			upstream = session.attachUpstream(upstreamConn, turn.upstreamIdentity)
			if upstream == nil {
				turn.finish(false)
				return
			}
		}

		if !session.activateTurn(upstream, turn) {
			err := errors.New("upstream Responses WebSocket disconnected before request dispatch")
			turn.info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, err)
			turn.finish(false)
			session.sendTurnFailure(turn, types.NewErrorWithStatusCode(err, types.ErrorCodeDoRequestFailed, http.StatusBadGateway, types.ErrOptionWithSkipRetry()))
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
	otherSettings := candidate.GetOtherSettings()
	if otherSettings.ResponsesWebSocketV2Mode == model.UpstreamOpenAIWSModeOff {
		return false
	}
	if otherSettings.ResponsesWebSocketV2Enabled {
		return true
	}
	if candidate.Type == appconstant.ChannelTypeCodex &&
		candidate.CredentialSource == appconstant.ChannelCredentialSourceAccountPool {
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

func (s *responsesWebSocketSession) prepareTurn(request *dto.OpenAIResponsesRequest, selectedChannel *model.Channel) (*responsesWebSocketTurn, json.RawMessage, channel.Adaptor, *types.NewAPIError) {
	turnCtx, cancel := newResponsesWSTurnContext(s.baseCtx, s.lockedModel)
	common.SetContextKey(turnCtx, appconstant.ContextKeyResponsesWebSocketIngress, true)
	s.mu.Lock()
	preferredAccountID := s.lockedAccountID
	preferredWSMode := s.lockedWSMode
	turnState := s.turnState
	s.mu.Unlock()
	if preferredAccountID > 0 {
		common.SetContextKey(turnCtx, appconstant.ContextKeyUpstreamAccountPreferredId, preferredAccountID)
		common.SetContextKey(turnCtx, appconstant.ContextKeyUpstreamAccountPreferredRequired, true)
		if preferredWSMode != "" {
			common.SetContextKey(turnCtx, appconstant.ContextKeyUpstreamAccountRequiredWSMode, preferredWSMode)
		}
	}
	if turnState != "" {
		turnCtx.Request.Header.Set("X-Codex-Turn-State", turnState)
	}
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
		middleware.ReleaseUpstreamAccountSelection(turnCtx)
	}
	if apiErr := middleware.SetupContextForSelectedChannel(turnCtx, selectedChannel, request.Model); apiErr != nil {
		cleanup()
		return nil, nil, nil, apiErr
	}
	upstreamMode := responsesWSUpstreamMode(turnCtx, selectedChannel)
	if upstreamMode == model.UpstreamOpenAIWSModeOff {
		cleanup()
		return nil, nil, nil, types.NewErrorWithStatusCode(errors.New("Responses WebSocket mode is disabled for the selected upstream account"), types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	}
	forceStream := !responsesWSModeUsesUpstreamWebSocket(upstreamMode)
	accountID := common.GetContextKeyInt(turnCtx, appconstant.ContextKeyUpstreamAccountId)
	if accountID > 0 {
		s.mu.Lock()
		lockedAccountID := s.lockedAccountID
		s.mu.Unlock()
		if lockedAccountID > 0 && lockedAccountID != accountID {
			cleanup()
			return nil, nil, nil, types.NewErrorWithStatusCode(errors.New("Responses WebSocket continuation account is unavailable"), types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
		}
	}
	relayInfo, err := relaycommon.GenRelayInfo(turnCtx, types.RelayFormatOpenAIResponses, request, nil)
	if err != nil {
		cleanup()
		return nil, nil, nil, types.NewError(err, types.ErrorCodeGenRelayInfoFailed, types.ErrOptionWithSkipRetry())
	}
	relayInfo.IsStream = true
	relayInfo.StreamStatus = relaycommon.NewStreamStatus()
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
		ctx:               turnCtx,
		info:              relayInfo,
		accumulator:       openairelay.NewResponsesEventAccumulator(),
		originalRequest:   request,
		forceSSEStream:    forceStream,
		upstreamMode:      upstreamMode,
		accountID:         accountID,
		failoverStartedAt: time.Now(),
		rateLimitFinish:   rateFinish,
		releaseUser:       releaseUser,
		releaseBusiness:   releaseBusiness,
		cancel:            cancel,
	}
	prepared, adaptor, apiErr := relay.PrepareResponsesWebSocketRequest(turnCtx, relayInfo, request, forceStream)
	if apiErr != nil {
		turn.finish(false)
		return nil, nil, nil, apiErr
	}
	return turn, json.RawMessage(prepared), adaptor, nil
}

func responsesWSUpstreamMode(c *gin.Context, selectedChannel *model.Channel) string {
	if c != nil {
		if settings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](c, appconstant.ContextKeyChannelOtherSetting); ok {
			switch settings.ResponsesWebSocketV2Mode {
			case model.UpstreamOpenAIWSModeOff, model.UpstreamOpenAIWSModeContextPool,
				model.UpstreamOpenAIWSModePassthrough, model.UpstreamOpenAIWSModeHTTPBridge:
				return settings.ResponsesWebSocketV2Mode
			}
			if settings.ResponsesWebSocketV2Enabled {
				return model.UpstreamOpenAIWSModeContextPool
			}
		}
	}
	if selectedChannel != nil && selectedChannel.GetOtherSettings().ResponsesWebSocketV2Enabled {
		return model.UpstreamOpenAIWSModeContextPool
	}
	return model.UpstreamOpenAIWSModeHTTPBridge
}

func responsesWSModeUsesUpstreamWebSocket(mode string) bool {
	return mode == model.UpstreamOpenAIWSModeContextPool || mode == model.UpstreamOpenAIWSModePassthrough
}

func responsesWSUpstreamIdentity(c *gin.Context, selectedChannel *model.Channel) string {
	if c == nil || selectedChannel == nil {
		return ""
	}
	accountID := common.GetContextKeyInt(c, appconstant.ContextKeyUpstreamAccountId)
	credentialIdentity := common.GetContextKeyString(c, appconstant.ContextKeyChannelKey)
	if accountID > 0 {
		// OAuth access tokens can rotate between leases. The account is the stable
		// continuation boundary; changing its short-lived token must not discard a
		// healthy native WebSocket carrying previous_response_id state.
		credentialIdentity = ""
	}
	identity := fmt.Sprintf("%d|%d|%d|%s|%s",
		selectedChannel.Id,
		common.GetContextKeyInt(c, appconstant.ContextKeyChannelType),
		accountID,
		common.GetContextKeyString(c, appconstant.ContextKeyChannelBaseUrl),
		credentialIdentity,
	)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
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
		var clientGone bool
		var recoverPreviousResponse bool
		var recoveryAPIError *types.NewAPIError
		var retryCapacity bool
		var capacityAPIError *types.NewAPIError
		var capacityDecision *responsesWSCapacityRetryDecision
		var holdCurrentFrame bool
		var pendingPrelude []responsesWSBufferedFrame
		var retryPrelude []responsesWSBufferedFrame
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
				turn.collectReplayContext(data)
				event, consumeErr := turn.accumulator.Consume(turn.ctx, turn.info, data)
				if consumeErr != nil {
					logger.LogError(turn.ctx, "failed to parse Responses WebSocket event: "+consumeErr.Error())
				} else {
					turn.upstreamEvent = true
					turn.upstreamEventCount++
					if openairelay.ResponsesStreamDataStartsClientOutput(string(data), event) {
						turn.upstreamOutputStarted = true
					}
					safePrelude := isResponsesWSCapacitySafePrelude(event, data)
					if safePrelude {
						turn.capacitySafeEventCount++
						if shouldHoldResponsesWSCapacityPrelude(turn, event, data) {
							turn.bufferCapacityPrelude(messageType, data)
							holdCurrentFrame = true
						}
					}
					var terminalErr *types.NewAPIError
					capacityRetryEligible := false
					if event != nil && turn.accumulator.Terminal() && !turn.accumulator.Successful() {
						terminalErr = responsesWSTerminalAPIError(turn)
						decision := evaluateResponsesWSCapacityRetry(turn, terminalErr)
						capacityDecision = &decision
						capacityRetryEligible = decision.Eligible
					}
					if event != nil && !safePrelude {
						if !turn.accumulator.Terminal() {
							turn.capacityRetryBlocked = true
						}
						if capacityRetryEligible {
							retryPrelude = turn.takeCapacityPrelude()
						} else {
							pendingPrelude = turn.takeCapacityPrelude()
						}
					}
					if openairelay.IsResponsesFirstFrameEvent(event) && !capacityRetryEligible {
						turn.info.SetFirstResponseTime()
					}
					if event != nil && turn.accumulator.Terminal() {
						successful = turn.accumulator.Successful()
						if !successful && shouldRecoverResponsesWSPreviousResponse(turn) {
							recoveryAPIError = responsesWSTerminalAPIError(turn)
							turn.replayCount++
							turn.accumulator = openairelay.NewResponsesEventAccumulator()
							turn.upstreamEvent = false
							turn.upstreamOutputStarted = false
							turn.upstreamEventCount = 0
							turn.resetCapacityPrelude()
							s.upstream = nil
							s.channel = nil
							recoverPreviousResponse = true
						} else if !successful {
							if capacityRetryEligible {
								recordResponsesWSNativeFailure(turn, terminalErr)
								recordResponsesWSCapacityRetryDecision(turn, *capacityDecision)
								turn.capacityRetryCount++
								turn.replayCount++
								s.upstream = nil
								s.channel = nil
								s.lockedAccountID = 0
								s.lockedWSMode = ""
								retryCapacity = true
								capacityAPIError = terminalErr
							} else {
								finished = turn
							}
						} else {
							finished = turn
						}
					}
					if terminalErr != nil && !capacityRetryEligible &&
						service.IsUpstreamCapacityError(terminalErr) {
						if sanitized, changed := openairelay.SanitizeResponsesCapacityErrorForClient(string(data)); changed {
							data = []byte(sanitized)
						}
					}
				}
			}
		} else if turn != nil {
			turn.capacityRetryBlocked = true
			pendingPrelude = turn.takeCapacityPrelude()
		}
		if finished != nil {
			if successful && finished.channel != nil {
				s.commitReplayState(finished)
				finished.info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				recordResponsesWSNativeSuccess(finished)
				service.RecordChannelAffinity(finished.ctx, finished.channel.Id)
			} else {
				terminalErr := responsesWSTerminalAPIError(finished)
				finished.info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, terminalErr)
				recordResponsesWSNativeFailure(finished, terminalErr)
				if capacityDecision != nil {
					recordResponsesWSCapacityRetryDecision(finished, *capacityDecision)
				}
			}
			finished.finish(successful)
			s.activeTurn = nil
		}
		clientGone = s.clientGone
		s.mu.Unlock()
		if retryCapacity {
			s.clientWriteMu.Unlock()
			s.upstreamWriteMu.Lock()
			upstream.close()
			s.upstreamWriteMu.Unlock()
			if s.retryResponsesWSCapacityTurn(turn) {
				return
			}
			turn.info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, capacityAPIError)
			turn.finish(false)
			s.mu.Lock()
			if s.activeTurn == turn {
				s.activeTurn = nil
			}
			clientGone = s.clientGone
			s.mu.Unlock()
			if !clientGone && s.client != nil {
				s.clientWriteMu.Lock()
				for _, frame := range retryPrelude {
					if err := s.client.WriteMessage(frame.messageType, frame.data); err != nil {
						break
					}
				}
				_ = s.client.WriteMessage(messageType, data)
				s.clientWriteMu.Unlock()
			}
			if clientGone {
				s.close()
			}
			return
		}
		if recoverPreviousResponse {
			s.clientWriteMu.Unlock()
			s.upstreamWriteMu.Lock()
			upstream.close()
			s.upstreamWriteMu.Unlock()
			logger.LogWarn(turn.ctx, "Responses WebSocket previous_response_id was rejected; replaying once as a full create")
			if s.replayTurn(turn) {
				return
			}
			if recoveryAPIError == nil {
				recoveryAPIError = types.NewErrorWithStatusCode(errors.New("previous_response_id recovery failed"), types.ErrorCodeDoRequestFailed, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
			}
			turn.info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, recoveryAPIError)
			recordResponsesWSNativeFailure(turn, recoveryAPIError)
			turn.finish(false)
			s.mu.Lock()
			if s.activeTurn == turn {
				s.activeTurn = nil
			}
			s.mu.Unlock()
			s.sendTurnFailure(turn, recoveryAPIError)
			if clientGone {
				s.close()
			}
			return
		}
		var writeErr error
		if !clientGone && s.client != nil {
			for _, frame := range pendingPrelude {
				if writeErr = s.client.WriteMessage(frame.messageType, frame.data); writeErr != nil {
					break
				}
			}
			if writeErr == nil && !holdCurrentFrame {
				writeErr = s.client.WriteMessage(messageType, data)
			}
		}
		s.clientWriteMu.Unlock()
		if writeErr != nil {
			s.handleClientLoopExit()
			return
		}
		if finished != nil && clientGone {
			s.close()
			return
		}
		if finished != nil && finished.closeUpstreamAfterTurn {
			s.closeIdleUpstream(upstream)
		}
	}
}

type responsesWSCapacityRetryDecision struct {
	Eligible     bool
	Reason       string
	TerminalType string
}

func shouldRetryResponsesWSCapacity(turn *responsesWebSocketTurn, apiErr *types.NewAPIError) bool {
	return evaluateResponsesWSCapacityRetry(turn, apiErr).Eligible
}

func evaluateResponsesWSCapacityRetry(turn *responsesWebSocketTurn, apiErr *types.NewAPIError) responsesWSCapacityRetryDecision {
	decision := responsesWSCapacityRetryDecision{Reason: "invalid_context"}
	if turn == nil || apiErr == nil || turn.accumulator == nil || turn.channel == nil || turn.originalRequest == nil {
		return decision
	}
	terminalType := turn.accumulator.TerminalEventType()
	decision.TerminalType = terminalType
	if terminalType != "response.failed" && terminalType != "error" {
		decision.Reason = "terminal_type_unsupported"
		return decision
	}
	if apiErr.StatusCode != 529 {
		decision.Reason = "status_not_capacity"
		return decision
	}
	failure := turn.accumulator.FailureError()
	if failure == nil {
		decision.Reason = "missing_failure"
		return decision
	}
	capacitySignature := strings.ToLower(strings.TrimSpace(fmt.Sprint(failure.Code)) + " " + failure.Type + " " + failure.Message)
	if !strings.Contains(capacitySignature, "capacity") && !strings.Contains(capacitySignature, "overload") &&
		!strings.Contains(capacitySignature, "slow_down") {
		decision.Reason = "capacity_signature_mismatch"
		return decision
	}
	if turn.channel.CredentialSource != appconstant.ChannelCredentialSourceAccountPool {
		decision.Reason = "credential_source_mismatch"
		return decision
	}
	if !responsesWSModeUsesUpstreamWebSocket(turn.upstreamMode) {
		decision.Reason = "upstream_mode_mismatch"
		return decision
	}
	// Capacity retry is safe while the upstream has emitted only transport or
	// response lifecycle prelude frames. Response-specific lifecycle frames are
	// held from the client until real output arrives, so a failed attempt can be
	// replaced without exposing conflicting response IDs.
	if turn.capacityRetryBlocked {
		decision.Reason = "semantic_output_started"
		return decision
	}
	if turn.upstreamEventCount != turn.capacitySafeEventCount+1 {
		decision.Reason = "unsafe_event_sequence"
		return decision
	}
	if turn.replayCount != turn.capacityRetryCount {
		decision.Reason = "replay_state_conflict"
		return decision
	}
	if !service.ShouldRetryUpstreamAccount(turn.accountFailovers, time.Time{}) {
		decision.Reason = "retry_budget_exhausted"
		return decision
	}
	if turn.accumulator.UsageReported() {
		decision.Reason = "usage_reported"
		return decision
	}
	if !turn.replayStateAvailable() {
		decision.Reason = "replay_state_missing"
		return decision
	}
	if !turn.failoverStartedAt.IsZero() && time.Since(turn.failoverStartedAt) > responsesWSCapacityRetryMaxAge {
		decision.Reason = "retry_window_exhausted"
		return decision
	}
	decision.Eligible = true
	decision.Reason = "allowed"
	return decision
}

func isResponsesWSCapacitySafePrelude(event *dto.ResponsesStreamResponse, data []byte) bool {
	if event == nil {
		return false
	}
	switch strings.TrimSpace(event.Type) {
	case "responsesapi.websocket_timing":
		return true
	case "response.failed", "error":
		// Both terminal envelopes must remain outside the safe-prelude count.
		// OpenAI may close a capacity-rejected WebSocket turn after only an
		// `error` event, without a following `response.failed`. Treating that
		// error as buffered prelude prevented the request from ever entering
		// the account failover path.
		return false
	default:
		return !openairelay.ResponsesStreamDataStartsClientOutput(string(data), event)
	}
}

func shouldHoldResponsesWSCapacityPrelude(turn *responsesWebSocketTurn, event *dto.ResponsesStreamResponse, data []byte) bool {
	if turn == nil || event == nil || turn.channel == nil || turn.originalRequest == nil {
		return false
	}
	eventType := strings.TrimSpace(event.Type)
	if eventType == "response.failed" ||
		openairelay.ResponsesStreamDataStartsClientOutput(string(data), event) {
		return false
	}
	return turn.channel.CredentialSource == appconstant.ChannelCredentialSourceAccountPool &&
		responsesWSModeUsesUpstreamWebSocket(turn.upstreamMode) &&
		!turn.capacityRetryBlocked &&
		turn.replayCount == turn.capacityRetryCount &&
		service.ShouldRetryUpstreamAccount(turn.accountFailovers, time.Time{}) &&
		turn.replayStateAvailable()
}

func (s *responsesWebSocketSession) retryResponsesWSCapacityTurn(turn *responsesWebSocketTurn) bool {
	if s == nil || turn == nil || turn.ctx == nil || turn.info == nil || turn.channel == nil {
		return false
	}
	failedAccountID := common.GetContextKeyInt(turn.ctx, appconstant.ContextKeyUpstreamAccountId)
	if failedAccountID <= 0 {
		return false
	}
	service.RecordUpstreamAccountModelTransientFailure(failedAccountID, turn.info.UpstreamModelName)
	excludedIDs, _ := common.GetContextKeyType[map[int]struct{}](turn.ctx, appconstant.ContextKeyUpstreamAccountExcluded)
	if excludedIDs == nil {
		excludedIDs = make(map[int]struct{})
	}
	excludedIDs[failedAccountID] = struct{}{}
	common.SetContextKey(turn.ctx, appconstant.ContextKeyUpstreamAccountExcluded, excludedIDs)
	common.SetContextKey(turn.ctx, appconstant.ContextKeyUpstreamAccountPreferredId, 0)
	common.SetContextKey(turn.ctx, appconstant.ContextKeyUpstreamAccountPreferredRequired, false)
	common.SetContextKey(turn.ctx, appconstant.ContextKeyUpstreamAccountRequiredWSMode, turn.upstreamMode)
	if setupErr := middleware.SetupContextForSelectedChannel(turn.ctx, turn.channel, turn.info.OriginModelName); setupErr != nil {
		return false
	}
	replacementAccountID := common.GetContextKeyInt(turn.ctx, appconstant.ContextKeyUpstreamAccountId)
	if replacementAccountID <= 0 || replacementAccountID == failedAccountID {
		return false
	}
	replacementMode := responsesWSUpstreamMode(turn.ctx, turn.channel)
	if replacementMode != turn.upstreamMode || !responsesWSModeUsesUpstreamWebSocket(replacementMode) {
		return false
	}

	outbound, adaptor, prepareErr := relay.PrepareResponsesWebSocketRequest(turn.ctx, turn.info, turn.originalRequest, false)
	if prepareErr != nil {
		return false
	}
	if turn.originalRequest != nil && strings.TrimSpace(turn.originalRequest.PreviousResponseID) != "" {
		var err error
		outbound, err = prepareResponsesWSStatelessReplayOutbound(turn, outbound)
		if err != nil {
			return false
		}
	}
	turn.accountID = replacementAccountID
	turn.upstreamIdentity = responsesWSUpstreamIdentity(turn.ctx, turn.channel)
	turn.accumulator = openairelay.NewResponsesEventAccumulator()
	turn.upstreamEvent = false
	turn.upstreamOutputStarted = false
	turn.upstreamEventCount = 0
	turn.resetCapacityPrelude()
	turn.requestDispatched = false
	turn.accountFailovers++
	turn.info.StreamStatus = relaycommon.NewStreamStatus()
	reconnect := func() (*websocket.Conn, *http.Response, error) {
		return channel.DoResponsesWssRequest(adaptor, turn.ctx, turn.info)
	}
	turn.setReplayState(outbound, reconnect)
	recordUpstreamRequestEvent(turn.ctx, "request_retry", "retry", fmt.Sprintf("capacity failover from account #%d", failedAccountID))

	conn, resp, err := reconnect()
	if resp != nil && resp.Body != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
		_ = resp.Body.Close()
	}
	if err != nil || conn == nil {
		if conn != nil {
			_ = conn.Close()
		}
		return false
	}
	if !s.isActiveTurn(turn) {
		_ = conn.Close()
		return false
	}
	s.setChannel(turn.channel)
	replacement := s.attachUpstream(conn, turn.upstreamIdentity)
	if replacement == nil {
		return false
	}
	s.mu.Lock()
	if s.activeTurn == turn {
		turn.requestDispatched = true
		s.lockedAccountID = replacementAccountID
		s.lockedWSMode = turn.upstreamMode
	}
	s.mu.Unlock()
	s.upstreamWriteMu.Lock()
	err = replacement.conn.WriteMessage(websocket.TextMessage, outbound)
	s.upstreamWriteMu.Unlock()
	if err != nil {
		s.handleUpstreamFailure(replacement, err)
		return true
	}
	logger.LogWarn(turn.ctx, fmt.Sprintf("responses websocket capacity failure retry #%d on account #%d -> #%d", turn.capacityRetryCount, failedAccountID, replacementAccountID))
	return true
}

// retryInitialResponsesWSCapacityDial handles a 529 returned during the
// initial upstream WebSocket handshake. No Responses event exists yet, so the
// normal terminal-event capacity retry path cannot run. Exclude the rejected
// account and establish the same request on another account serving the same
// model before exposing the failure to the downstream client.
func (s *responsesWebSocketSession) retryInitialResponsesWSCapacityDial(turn *responsesWebSocketTurn, selectedChannel *model.Channel) bool {
	if s == nil || turn == nil || turn.ctx == nil || turn.info == nil || selectedChannel == nil ||
		selectedChannel.CredentialSource != appconstant.ChannelCredentialSourceAccountPool || turn.originalRequest == nil {
		return false
	}
	failedAccountID := common.GetContextKeyInt(turn.ctx, appconstant.ContextKeyUpstreamAccountId)
	if failedAccountID <= 0 {
		return false
	}
	service.RecordUpstreamAccountModelTransientFailure(failedAccountID, turn.info.UpstreamModelName)
	excludedIDs, _ := common.GetContextKeyType[map[int]struct{}](turn.ctx, appconstant.ContextKeyUpstreamAccountExcluded)
	if excludedIDs == nil {
		excludedIDs = make(map[int]struct{})
	}
	excludedIDs[failedAccountID] = struct{}{}
	common.SetContextKey(turn.ctx, appconstant.ContextKeyUpstreamAccountExcluded, excludedIDs)
	common.SetContextKey(turn.ctx, appconstant.ContextKeyUpstreamAccountPreferredId, 0)
	common.SetContextKey(turn.ctx, appconstant.ContextKeyUpstreamAccountPreferredRequired, false)
	common.SetContextKey(turn.ctx, appconstant.ContextKeyUpstreamAccountRequiredWSMode, turn.upstreamMode)

	for service.ShouldRetryUpstreamAccount(turn.accountFailovers, turn.failoverStartedAt) {
		if setupErr := middleware.SetupContextForSelectedChannel(turn.ctx, selectedChannel, turn.info.OriginModelName); setupErr != nil {
			return false
		}
		replacementAccountID := common.GetContextKeyInt(turn.ctx, appconstant.ContextKeyUpstreamAccountId)
		replacementMode := responsesWSUpstreamMode(turn.ctx, selectedChannel)
		if replacementAccountID <= 0 || replacementAccountID == failedAccountID || replacementMode != turn.upstreamMode ||
			!responsesWSModeUsesUpstreamWebSocket(replacementMode) {
			middleware.ReleaseUpstreamAccountSelection(turn.ctx)
			return false
		}
		turn.info.InitChannelMeta(turn.ctx)
		outbound, adaptor, prepareErr := relay.PrepareResponsesWebSocketRequest(turn.ctx, turn.info, turn.originalRequest, false)
		if prepareErr != nil {
			middleware.ReleaseUpstreamAccountSelection(turn.ctx)
			return false
		}
		// Count every replacement account selected, including a handshake that
		// is rejected again with 529. Otherwise repeated capacity responses would
		// bypass the account failover cap and spin through the pool until the time
		// budget expires.
		turn.accountFailovers++
		turn.channel = selectedChannel
		turn.accountID = replacementAccountID
		turn.upstreamIdentity = responsesWSUpstreamIdentity(turn.ctx, selectedChannel)
		turn.accumulator = openairelay.NewResponsesEventAccumulator()
		turn.upstreamEvent = false
		turn.upstreamOutputStarted = false
		turn.upstreamEventCount = 0
		turn.resetCapacityPrelude()
		turn.info.StreamStatus = relaycommon.NewStreamStatus()
		reconnect := func() (*websocket.Conn, *http.Response, error) {
			return channel.DoResponsesWssRequest(adaptor, turn.ctx, turn.info)
		}
		conn, resp, dialErr := reconnect()
		if resp != nil && resp.Body != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
			_ = resp.Body.Close()
		}
		if dialErr != nil || conn == nil {
			if conn != nil {
				_ = conn.Close()
			}
			if resp == nil || resp.StatusCode != 529 {
				return false
			}
			service.RecordUpstreamAccountModelTransientFailure(replacementAccountID, turn.info.UpstreamModelName)
			excludedIDs[replacementAccountID] = struct{}{}
			common.SetContextKey(turn.ctx, appconstant.ContextKeyUpstreamAccountExcluded, excludedIDs)
			failedAccountID = replacementAccountID
			continue
		}
		upstream := s.attachUpstream(conn, turn.upstreamIdentity)
		if upstream == nil {
			_ = conn.Close()
			return false
		}
		s.mu.Lock()
		s.lockedAccountID = replacementAccountID
		s.lockedWSMode = turn.upstreamMode
		s.mu.Unlock()
		turn.setReplayState(outbound, reconnect)
		s.setChannel(selectedChannel)
		recordUpstreamRequestEvent(turn.ctx, "request_retry", "retry", fmt.Sprintf("initial capacity failover from account #%d", failedAccountID))
		return true
	}
	return false
}

func (s *responsesWebSocketSession) clientHeartbeat() {
	ticker := time.NewTicker(responsesWSHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			if s.isClientGone() {
				return
			}
			deadline := now.Add(10 * time.Second)
			s.clientWriteMu.Lock()
			clientErr := s.client.WriteControl(websocket.PingMessage, nil, deadline)
			s.clientWriteMu.Unlock()
			if clientErr != nil {
				s.handleClientLoopExit()
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

func (s *responsesWebSocketSession) getUpstreamForIdentity(identity string) *responsesWSUpstreamConnection {
	s.mu.Lock()
	upstream := s.upstream
	if upstream == nil || upstream.identity == identity {
		s.mu.Unlock()
		return upstream
	}
	s.upstream = nil
	s.mu.Unlock()

	s.upstreamWriteMu.Lock()
	upstream.close()
	s.upstreamWriteMu.Unlock()
	return nil
}

func (s *responsesWebSocketSession) closeIdleUpstream(upstream *responsesWSUpstreamConnection) {
	if upstream == nil {
		return
	}
	s.mu.Lock()
	if s.upstream != upstream || s.activeTurn != nil {
		s.mu.Unlock()
		return
	}
	s.upstream = nil
	s.mu.Unlock()

	s.upstreamWriteMu.Lock()
	upstream.close()
	s.upstreamWriteMu.Unlock()
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

func (s *responsesWebSocketSession) attachUpstream(conn *websocket.Conn, identity string) *responsesWSUpstreamConnection {
	if conn == nil {
		return nil
	}
	upstream := newResponsesWSUpstreamConnection(conn)
	upstream.identity = identity
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

func (s *responsesWebSocketSession) startSSETurn(turn *responsesWebSocketTurn) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runSSETurn(turn)
	}()
}

func (s *responsesWebSocketSession) runSSETurn(turn *responsesWebSocketTurn) {
	requestBody, adaptor, ok := turn.sseReplaySnapshot()
	if !ok {
		s.failSSETurn(turn, types.NewErrorWithStatusCode(errors.New("upstream Responses SSE replay state is unavailable"), types.ErrorCodeDoRequestFailed, http.StatusBadGateway, types.ErrOptionWithSkipRetry()))
		return
	}
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
	turn.turnState = strings.TrimSpace(resp.Header.Get("X-Codex-Turn-State"))
	defer service.CloseResponseBodyGracefully(resp)

	terminal := false
	idleTimeout := time.Duration(appconstant.StreamingTimeout) * time.Second
	_, scanErr := helper.ScanSSEDataWithIdleTimeout(turn.ctx.Request.Context(), resp.Body, idleTimeout, func(data []byte) (bool, error) {
		var finished bool
		var successful bool
		var clientGone bool

		s.clientWriteMu.Lock()
		s.mu.Lock()
		if s.activeTurn != turn {
			s.mu.Unlock()
			s.clientWriteMu.Unlock()
			return true, nil
		}
		turn.collectReplayContext(data)
		event, consumeErr := turn.accumulator.Consume(turn.ctx, turn.info, data)
		if consumeErr != nil {
			s.mu.Unlock()
			s.clientWriteMu.Unlock()
			return false, consumeErr
		}
		turn.upstreamEvent = true
		if openairelay.ResponsesStreamDataStartsClientOutput(string(data), event) {
			turn.upstreamOutputStarted = true
		}
		turn.clearSSEReplayState()
		turn.info.ReceivedResponseCount++
		if openairelay.IsResponsesFirstFrameEvent(event) {
			turn.info.SetFirstResponseTime()
		}
		if turn.accumulator.Terminal() {
			terminal = true
			finished = true
			successful = turn.accumulator.Successful()
			if successful && turn.channel != nil {
				s.commitReplayState(turn)
				turn.info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				service.RecordUpstreamAccountSuccess(common.GetContextKeyInt(turn.ctx, appconstant.ContextKeyUpstreamAccountId))
				recordUpstreamRequestEvent(turn.ctx, "request_success", "success", "")
				service.RecordChannelAffinity(turn.ctx, turn.channel.Id)
			} else {
				terminalErr := responsesWSTerminalAPIError(turn)
				turn.info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, terminalErr)
				recordResponsesWSNativeFailure(turn, terminalErr)
			}
			turn.finish(successful)
			s.activeTurn = nil
		}
		clientGone = s.clientGone
		s.mu.Unlock()
		var writeErr error
		if !clientGone && s.client != nil {
			writeErr = s.client.WriteMessage(websocket.TextMessage, data)
		}
		s.clientWriteMu.Unlock()
		if writeErr != nil {
			s.handleClientLoopExit()
			return true, writeErr
		}
		if finished && clientGone {
			s.close()
			return true, nil
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
	if retried, accountOwned := s.retrySSETurnWithAnotherAccount(turn, apiErr); retried {
		return
	} else if accountOwned {
		s.finishFailedSSETurn(turn, apiErr, false)
		return
	}
	s.finishFailedSSETurn(turn, apiErr, true)
}

func (s *responsesWebSocketSession) retrySSETurnWithAnotherAccount(turn *responsesWebSocketTurn, apiErr *types.NewAPIError) (bool, bool) {
	if turn == nil || apiErr == nil || turn.upstreamOutputStarted || turn.channel == nil ||
		turn.channel.CredentialSource != appconstant.ChannelCredentialSourceAccountPool {
		return false, false
	}
	// Lifecycle-only Responses events (created/in_progress/empty reasoning
	// metadata) do not mean that client-visible output has started. Capacity
	// failures may still fail over after those events; preserve the stricter
	// acknowledgement rule for other SSE errors.
	if turn.upstreamEvent && !service.IsUpstreamCapacityError(apiErr) {
		return false, false
	}
	accountId := common.GetContextKeyInt(turn.ctx, appconstant.ContextKeyUpstreamAccountId)
	proxyId := common.GetContextKeyInt(turn.ctx, appconstant.ContextKeyUpstreamProxyId)
	disposition := service.ApplyUpstreamAccountError(accountId, proxyId, apiErr)
	if !disposition.Handled() {
		return false, false
	}
	recordUpstreamRequestEvent(turn.ctx, "request_error", "error", service.UpstreamAccountErrorSummary(apiErr))
	if service.IsUpstreamCapacityError(apiErr) {
		service.RecordUpstreamAccountModelTransientFailure(accountId, turn.info.UpstreamModelName)
	}
	if !disposition.RetryWithinPool() || !service.ShouldRetryUpstreamAccount(turn.accountFailovers, turn.failoverStartedAt) {
		return false, true
	}
	excludedIds, _ := common.GetContextKeyType[map[int]struct{}](turn.ctx, appconstant.ContextKeyUpstreamAccountExcluded)
	if excludedIds == nil {
		excludedIds = make(map[int]struct{})
	}
	excludedIds[accountId] = struct{}{}
	common.SetContextKey(turn.ctx, appconstant.ContextKeyUpstreamAccountExcluded, excludedIds)
	common.SetContextKey(turn.ctx, appconstant.ContextKeyUpstreamAccountRequiredWSMode, turn.upstreamMode)
	if setupErr := middleware.SetupContextForSelectedChannel(turn.ctx, turn.channel, turn.info.OriginModelName); setupErr != nil {
		return false, true
	}
	turn.accountFailovers++
	if !s.isActiveTurn(turn) {
		middleware.ReleaseUpstreamAccountSelection(turn.ctx)
		return false, true
	}
	if turn.originalRequest != nil {
		prepared, adaptor, prepareErr := relay.PrepareResponsesWebSocketRequest(turn.ctx, turn.info, turn.originalRequest, turn.forceSSEStream)
		if prepareErr != nil {
			return false, true
		}
		prepared, prepareErr = s.prepareReplayPayload(turn, prepared)
		if prepareErr != nil {
			return false, true
		}
		turn.setSSEReplayState(prepared, adaptor)
	} else {
		turn.info.InitChannelMeta(turn.ctx)
	}
	turn.accumulator = openairelay.NewResponsesEventAccumulator()
	turn.upstreamEvent = false
	turn.upstreamOutputStarted = false
	turn.upstreamEventCount = 0
	turn.info.StreamStatus = relaycommon.NewStreamStatus()
	logger.LogWarn(turn.ctx, fmt.Sprintf("responses websocket upstream SSE retrying with another local account on channel #%d", turn.channel.Id))
	s.startSSETurn(turn)
	return true, true
}

func (s *responsesWebSocketSession) finishFailedSSETurn(turn *responsesWebSocketTurn, apiErr *types.NewAPIError, processChannel bool) {
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
	if processChannel && turn.channel != nil {
		processChannelError(turn.ctx, *types.NewChannelError(turn.channel.Id, turn.channel.Type, turn.channel.Name, turn.channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(turn.ctx, appconstant.ContextKeyChannelKey), turn.channel.GetAutoBan()), apiErr)
	}
	logger.LogWarn(turn.ctx, fmt.Sprintf("responses websocket upstream SSE turn failed without closing downstream websocket: %v", apiErr))
	s.sendTurnFailure(turn, apiErr)
	if s.isClientGone() {
		s.close()
	}
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
		if s.isClientGone() {
			s.close()
		}
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
	turn.info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, err)
	apiErr := types.NewErrorWithStatusCode(fmt.Errorf("upstream Responses WebSocket disconnected: %w", err), types.ErrorCodeDoRequestFailed, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	recordResponsesWSNativeFailure(turn, apiErr)
	turn.finish(false)
	logger.LogWarn(s.baseCtx, "responses websocket upstream turn failed without closing downstream websocket")
	s.sendTurnFailure(turn, apiErr)
	if s.isClientGone() {
		s.close()
	}
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
	if turn.originalRequest != nil && strings.TrimSpace(turn.originalRequest.PreviousResponseID) != "" {
		var err error
		outbound, err = prepareResponsesWSStatelessReplayOutbound(turn, outbound)
		if err != nil {
			logger.LogWarn(s.baseCtx, "responses websocket stateless replay preparation failed: "+err.Error())
			s.abandonReplay(turn)
			return false
		}
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
	upstream := s.attachUpstream(conn, turn.upstreamIdentity)
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

func shouldRecoverResponsesWSPreviousResponse(turn *responsesWebSocketTurn) bool {
	if turn == nil || !turn.replayEnabled || turn.replayCount != 0 || !turn.replayInputExists || turn.originalRequest == nil ||
		strings.TrimSpace(turn.originalRequest.PreviousResponseID) == "" || turn.accumulator == nil {
		return false
	}
	failure := turn.accumulator.FailureError()
	if failure == nil {
		return false
	}
	text := strings.ToLower(strings.Join([]string{
		failure.Message, failure.Type, failure.Param, fmt.Sprint(failure.Code),
	}, " "))
	if !strings.Contains(text, "previous_response") {
		return false
	}
	return strings.Contains(text, "not_found") || strings.Contains(text, "not found") ||
		strings.Contains(text, "unsupported parameter") || strings.Contains(text, "only supported")
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
	if apiErr == nil || s.client == nil || s.isClientGone() {
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
	if turn == nil || apiErr == nil || s.client == nil || s.isClientGone() {
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

func (s *responsesWebSocketSession) isClientGone() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clientGone
}

func (s *responsesWebSocketSession) handleClientLoopExit() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.clientGone {
		drain := s.activeTurn != nil
		s.mu.Unlock()
		if !drain {
			s.close()
		}
		return
	}
	s.clientGone = true
	drain := s.activeTurn != nil
	s.mu.Unlock()

	if s.client != nil {
		s.clientWriteMu.Lock()
		_ = s.client.Close()
		s.clientWriteMu.Unlock()
	}
	if !drain {
		s.close()
	}
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
			turn.info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, errors.New("downstream Responses WebSocket closed"))
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
