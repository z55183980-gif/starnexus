package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stripe/stripe-go/v81"
	"github.com/thanhpk/randstr"
)

var stripeAdaptor = &StripeAdaptor{}

// StripePayRequest represents a payment request for Stripe checkout.
type StripePayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
	SuccessURL    string `json:"success_url,omitempty"`
	CancelURL     string `json:"cancel_url,omitempty"`
}

type StripeAdaptor struct{}

func (*StripeAdaptor) RequestAmount(c *gin.Context, req *StripePayRequest) {
	minTopup := service.StripeMinTopUp()
	if req.Amount < minTopup {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", minTopup)})
		return
	}

	id := c.GetInt("id")
	group, err := model.GetUserGroup(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}

	payMoney := service.CalculateStripePayMoney(float64(req.Amount), group)
	if payMoney <= 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": strconv.FormatFloat(payMoney, 'f', 2, 64)})
}

func (*StripeAdaptor) RequestPay(c *gin.Context, req *StripePayRequest) {
	if !requirePaymentCompliance(c) {
		return
	}
	if !service.IsStripeConfigured() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "Stripe 未配置"})
		return
	}
	if req.PaymentMethod != model.PaymentMethodStripe {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "不支持的支付渠道"})
		return
	}

	minTopup := service.StripeMinTopUp()
	if req.Amount < minTopup {
		c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("充值数量不能小于 %d", minTopup), "data": 10})
		return
	}
	if req.Amount > 10000 {
		c.JSON(http.StatusOK, gin.H{"message": "充值数量不能大于 10000", "data": 10})
		return
	}

	if req.SuccessURL != "" && common.ValidateRedirectURL(req.SuccessURL) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "支付成功重定向URL不在可信任域名列表中", "data": ""})
		return
	}
	if req.CancelURL != "" && common.ValidateRedirectURL(req.CancelURL) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "支付取消重定向URL不在可信任域名列表中", "data": ""})
		return
	}

	id := c.GetInt("id")
	user, err := model.GetUserById(id, false)
	if err != nil || user == nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "用户不存在"})
		return
	}

	group, err := model.GetUserGroup(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}

	payMoney := service.CalculateStripePayMoney(float64(req.Amount), group)
	if payMoney <= 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	reference := fmt.Sprintf("new-api-ref-%d-%d-%s", user.Id, time.Now().UnixMilli(), randstr.String(4))
	referenceId := "ref_" + common.Sha1([]byte(reference))
	chargedMoney := service.CalculateStripeChargedAmount(float64(req.Amount), user.Group)

	topUp := &model.TopUp{
		UserId:          id,
		Amount:          req.Amount,
		Money:           chargedMoney,
		TradeNo:         referenceId,
		PaymentMethod:   model.PaymentMethodStripe,
		PaymentProvider: model.PaymentProviderStripe,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err = topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Stripe 创建充值订单失败 user_id=%d trade_no=%s amount=%d error=%q", id, referenceId, req.Amount, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	checkout, err := service.CreateTopUpCheckoutSession(service.StripeCheckoutSessionInput{
		TradeNo:      referenceId,
		UserID:       id,
		TopUpAmount:  req.Amount,
		PayMoney:     payMoney,
		ChargedMoney: chargedMoney,
		CustomerID:   user.StripeCustomer,
		Email:        user.Email,
		SuccessURL:   req.SuccessURL,
		CancelURL:    req.CancelURL,
	})
	if err != nil {
		_ = model.UpdatePendingTopUpStatus(referenceId, model.PaymentProviderStripe, common.TopUpStatusFailed)
		logger.LogError(c.Request.Context(), fmt.Sprintf("Stripe 创建 Checkout Session 失败 user_id=%d trade_no=%s amount=%d error=%q", id, referenceId, req.Amount, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Stripe 充值订单创建成功 user_id=%d trade_no=%s session_id=%s amount=%d money=%.2f pay=%.2f", id, referenceId, checkout.SessionID, req.Amount, chargedMoney, payMoney))
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"pay_link": checkout.URL,
		},
	})
}

func RequestStripeAmount(c *gin.Context) {
	var req StripePayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	stripeAdaptor.RequestAmount(c, &req)
}

func RequestStripePay(c *gin.Context) {
	var req StripePayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	stripeAdaptor.RequestPay(c, &req)
}

func StripeWebhook(c *gin.Context) {
	ctx := c.Request.Context()
	if !isStripeWebhookEnabled() {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe webhook 被拒绝 reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	payload, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Stripe webhook 读取请求体失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}

	signature := c.GetHeader("Stripe-Signature")
	event, err := service.ConstructStripeWebhookEvent(payload, signature)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe webhook 验签失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	callerIp := c.ClientIP()
	logger.LogInfo(ctx, fmt.Sprintf("Stripe webhook 验签成功 event_id=%s event_type=%s client_ip=%s", event.ID, event.Type, callerIp))
	switch event.Type {
	case stripe.EventTypeCheckoutSessionCompleted:
		handleStripeCheckoutCompleted(ctx, event, callerIp)
	case stripe.EventTypeCheckoutSessionExpired:
		handleStripeCheckoutExpired(ctx, event)
	case stripe.EventTypeCheckoutSessionAsyncPaymentSucceeded:
		handleStripeCheckoutAsyncSucceeded(ctx, event, callerIp)
	case stripe.EventTypeCheckoutSessionAsyncPaymentFailed:
		handleStripeCheckoutAsyncFailed(ctx, event, callerIp)
	default:
		logger.LogInfo(ctx, fmt.Sprintf("Stripe webhook 忽略事件 event_type=%s client_ip=%s", event.Type, callerIp))
	}

	c.Status(http.StatusOK)
}

func handleStripeCheckoutCompleted(ctx context.Context, event stripe.Event, callerIp string) {
	session, err := service.ParseCheckoutSessionEvent(event)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe checkout.completed 解析失败 event_id=%s error=%q client_ip=%s", event.ID, err.Error(), callerIp))
		return
	}
	if session.Status != "complete" {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe checkout.completed 状态异常 trade_no=%s status=%s client_ip=%s", session.ClientReferenceID, session.Status, callerIp))
		return
	}
	if session.PaymentStatus != "paid" {
		logger.LogInfo(ctx, fmt.Sprintf("Stripe Checkout 支付未完成，等待异步结果 trade_no=%s payment_status=%s client_ip=%s", session.ClientReferenceID, session.PaymentStatus, callerIp))
		return
	}
	fulfillStripeOrder(ctx, session, string(event.Type), callerIp)
}

func handleStripeCheckoutAsyncSucceeded(ctx context.Context, event stripe.Event, callerIp string) {
	session, err := service.ParseCheckoutSessionEvent(event)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe async_payment_succeeded 解析失败 event_id=%s error=%q client_ip=%s", event.ID, err.Error(), callerIp))
		return
	}
	logger.LogInfo(ctx, fmt.Sprintf("Stripe 异步支付成功 trade_no=%s client_ip=%s", session.ClientReferenceID, callerIp))
	fulfillStripeOrder(ctx, session, string(event.Type), callerIp)
}

func handleStripeCheckoutAsyncFailed(ctx context.Context, event stripe.Event, callerIp string) {
	session, err := service.ParseCheckoutSessionEvent(event)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe async_payment_failed 解析失败 event_id=%s error=%q client_ip=%s", event.ID, err.Error(), callerIp))
		return
	}

	LockOrder(session.ClientReferenceID)
	defer UnlockOrder(session.ClientReferenceID)

	topUp := model.GetTopUpByTradeNo(session.ClientReferenceID)
	if topUp == nil {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe 异步支付失败但本地订单不存在 trade_no=%s client_ip=%s", session.ClientReferenceID, callerIp))
		return
	}
	if topUp.PaymentProvider != model.PaymentProviderStripe {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe 异步支付失败但订单支付网关不匹配 trade_no=%s payment_provider=%s client_ip=%s", session.ClientReferenceID, topUp.PaymentProvider, callerIp))
		return
	}
	if topUp.Status != common.TopUpStatusPending {
		logger.LogInfo(ctx, fmt.Sprintf("Stripe 异步支付失败但订单状态非 pending，忽略处理 trade_no=%s status=%s client_ip=%s", session.ClientReferenceID, topUp.Status, callerIp))
		return
	}

	topUp.Status = common.TopUpStatusFailed
	if err := topUp.Update(); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Stripe 标记充值订单失败状态失败 trade_no=%s client_ip=%s error=%q", session.ClientReferenceID, callerIp, err.Error()))
		return
	}
	logger.LogInfo(ctx, fmt.Sprintf("Stripe 充值订单已标记为失败 trade_no=%s client_ip=%s", session.ClientReferenceID, callerIp))
}

func fulfillStripeOrder(ctx context.Context, session *service.StripeCheckoutSessionEvent, eventType string, callerIp string) {
	LockOrder(session.ClientReferenceID)
	defer UnlockOrder(session.ClientReferenceID)

	payload := service.BuildStripeFulfillmentPayload(session, eventType)
	if err := model.CompleteSubscriptionOrder(session.ClientReferenceID, payload, model.PaymentProviderStripe, ""); err == nil {
		logger.LogInfo(ctx, fmt.Sprintf("Stripe 订阅订单处理成功 trade_no=%s event_type=%s client_ip=%s", session.ClientReferenceID, eventType, callerIp))
		return
	} else if err != nil && !errors.Is(err, model.ErrSubscriptionOrderNotFound) {
		logger.LogError(ctx, fmt.Sprintf("Stripe 订阅订单处理失败 trade_no=%s event_type=%s client_ip=%s error=%q", session.ClientReferenceID, eventType, callerIp, err.Error()))
		return
	}

	if err := model.Recharge(session.ClientReferenceID, session.Customer, callerIp); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Stripe 充值处理失败 trade_no=%s event_type=%s client_ip=%s error=%q", session.ClientReferenceID, eventType, callerIp, err.Error()))
		return
	}

	logger.LogInfo(ctx, fmt.Sprintf("Stripe 充值成功 trade_no=%s amount_total=%.2f currency=%s event_type=%s client_ip=%s", session.ClientReferenceID, float64(session.AmountTotal)/100, session.Currency, eventType, callerIp))
}

func handleStripeCheckoutExpired(ctx context.Context, event stripe.Event) {
	session, err := service.ParseCheckoutSessionEvent(event)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe checkout.expired 解析失败 event_id=%s error=%q", event.ID, err.Error()))
		return
	}
	if session.Status != "expired" {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe checkout.expired 状态异常 trade_no=%s status=%s", session.ClientReferenceID, session.Status))
		return
	}

	LockOrder(session.ClientReferenceID)
	defer UnlockOrder(session.ClientReferenceID)

	if err := model.ExpireSubscriptionOrder(session.ClientReferenceID, model.PaymentProviderStripe); err == nil {
		logger.LogInfo(ctx, fmt.Sprintf("Stripe 订阅订单已过期 trade_no=%s", session.ClientReferenceID))
		return
	} else if err != nil && !errors.Is(err, model.ErrSubscriptionOrderNotFound) {
		logger.LogError(ctx, fmt.Sprintf("Stripe 订阅订单过期处理失败 trade_no=%s error=%q", session.ClientReferenceID, err.Error()))
		return
	}

	err = model.UpdatePendingTopUpStatus(session.ClientReferenceID, model.PaymentProviderStripe, common.TopUpStatusExpired)
	if errors.Is(err, model.ErrTopUpNotFound) {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe 充值订单不存在，无法标记过期 trade_no=%s", session.ClientReferenceID))
		return
	}
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Stripe 充值订单过期处理失败 trade_no=%s error=%q", session.ClientReferenceID, err.Error()))
		return
	}

	logger.LogInfo(ctx, fmt.Sprintf("Stripe 充值订单已过期 trade_no=%s", session.ClientReferenceID))
}
