package controller

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

const paymentMethodUsdt = "usdt"

func epusdtGatewayBase() string {
	return setting.EffectiveEpUSDTGatewayAddress(operation_setting.PayAddress)
}

func epusdtCreateOrderURL() string {
	return epusdtGatewayBase() + "/api/v1/order/create-transaction"
}

func isUsdtTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	if !isEpusdtConfigured() {
		return false
	}
	// Match Stripe: EpUSDT credentials are sufficient to offer USDT top-up.
	// PayMethods can opt out explicitly when usdt is listed with enabled=false.
	if operation_setting.ContainsPayMethod(paymentMethodUsdt) {
		return isPayMethodEnabled(paymentMethodUsdt)
	}
	return true
}

func isEpusdtConfigured() bool {
	return strings.TrimSpace(setting.EpUSDTApiToken) != "" &&
		epusdtGatewayBase() != ""
}

func isEpusdtWebhookEnabled() bool {
	return isUsdtTopUpEnabled()
}

func isPayMethodEnabled(methodType string) bool {
	for _, method := range operation_setting.PayMethods {
		if method["type"] != methodType {
			continue
		}
		if method["enabled"] == "false" {
			return false
		}
		return true
	}
	return false
}

type epusdtCreateOrderResponse struct {
	Code    int    `json:"status_code"`
	CodeAlt int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		PaymentURL string `json:"payment_url"`
	} `json:"data"`
}

func (r epusdtCreateOrderResponse) statusCode() int {
	if r.Code != 0 {
		return r.Code
	}
	return r.CodeAlt
}

const epusdtCreditPerUSDT = 6.8

func epusdtPayMoneyFromCredit(creditAmount int64) float64 {
	if creditAmount <= 0 {
		return 0
	}
	return decimal.NewFromInt(creditAmount).
		Div(decimal.NewFromFloat(epusdtCreditPerUSDT)).
		Round(2).
		InexactFloat64()
}

// epusdtGatewayFiatAmount is the fiat (USD) order amount sent to BEpusdt.
// The gateway converts it to USDT using its configured exchange rate.
func epusdtGatewayFiatAmount(creditAmount int64) float64 {
	if creditAmount <= 0 {
		return 0
	}
	return decimal.NewFromInt(creditAmount).Round(2).InexactFloat64()
}

func epusdtStoredCreditAmount(amount int64) int64 {
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		dAmount := decimal.NewFromInt(amount)
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		return dAmount.Div(dQuotaPerUnit).IntPart()
	}
	return amount
}

// epusdtAmountSignString matches BEpusdt signature rules for numeric amount fields.
func epusdtAmountSignString(amount float64) string {
	rounded := decimal.NewFromFloat(amount).Round(2).InexactFloat64()
	return strconv.FormatFloat(rounded, 'f', -1, 64)
}

func RequestUsdtPay(c *gin.Context) {
	if !operation_setting.IsPaymentComplianceConfirmed() {
		common.ApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
		return
	}

	var req EpayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	if req.PaymentMethod != "" && req.PaymentMethod != paymentMethodUsdt {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "支付方式不存在"})
		return
	}
	if req.Amount < getMinTopup() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", getMinTopup())})
		return
	}
	if !isUsdtTopUpEnabled() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "USDT 支付未配置或未启用"})
		return
	}

	id := c.GetInt("id")
	creditAmount := epusdtStoredCreditAmount(req.Amount)
	payMoney := epusdtPayMoneyFromCredit(creditAmount)
	gatewayFiatAmount := epusdtGatewayFiatAmount(creditAmount)
	if payMoney < 0.01 || gatewayFiatAmount < 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	notifyURL := setting.EffectiveEpUSDTNotifyURL(system_setting.ServerAddress)
	if notifyURL == "" {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "EpUSDT 回调地址未配置"})
		return
	}
	redirectURL := paymentReturnPath("/console/topup?pay=success")
	tradeNo := fmt.Sprintf("USR%dNO%s%d", id, common.GetRandomString(6), time.Now().Unix())

	amountValue := gatewayFiatAmount
	amountSign := epusdtAmountSignString(gatewayFiatAmount)
	signParams := map[string]string{
		"order_id":     tradeNo,
		"amount":       amountSign,
		"notify_url":   notifyURL,
		"redirect_url": redirectURL,
		"fiat":         "USD",
		"trade_type":   "usdt.trc20",
	}
	signature := service.EpusdtSign(signParams, setting.EpUSDTApiToken)

	payload := map[string]any{
		"order_id":     tradeNo,
		"amount":       amountValue,
		"notify_url":   notifyURL,
		"redirect_url": redirectURL,
		"fiat":         "USD",
		"trade_type":   "usdt.trc20",
		"signature":    signature,
	}

	if strings.Contains(notifyURL, "localhost") || strings.Contains(notifyURL, "127.0.0.1") {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("EpUSDT notify_url 不可被公网访问 notify_url=%q", notifyURL))
	}

	paymentURL, err := createEpusdtTransaction(c, payload)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("EpUSDT 拉起支付失败 user_id=%d trade_no=%s fiat_amount=%s pay_usdt=%.2f notify_url=%q error=%q", id, tradeNo, amountSign, payMoney, notifyURL, err.Error()))
		errData := "拉起支付失败"
		if gatewayMsg := epusdtGatewayErrorMessage(err); gatewayMsg != "" {
			errData = gatewayMsg
		}
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": errData})
		return
	}

	amount := creditAmount

	topUp := &model.TopUp{
		UserId:          id,
		Amount:          amount,
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   paymentMethodUsdt,
		PaymentProvider: model.PaymentProviderEpusdt,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err = topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("EpUSDT 创建充值订单失败 user_id=%d trade_no=%s error=%q", id, tradeNo, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("EpUSDT 充值订单创建成功 user_id=%d trade_no=%s credit=%d fiat_amount=%s pay_usdt=%.2f payment_url=%q", id, tradeNo, creditAmount, amountSign, payMoney, paymentURL))
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"pay_link": paymentURL,
		},
	})
}

func epusdtGatewayErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	const prefix = "gateway: "
	msg := err.Error()
	if strings.HasPrefix(msg, prefix) {
		return strings.TrimPrefix(msg, prefix)
	}
	return ""
}

func createEpusdtTransaction(c *gin.Context, payload map[string]any) (string, error) {
	body, err := common.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, epusdtCreateOrderURL(), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gateway unreachable: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("gateway status %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed epusdtCreateOrderResponse
	if err = common.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("invalid gateway response: %s", string(respBody))
	}
	if code := parsed.statusCode(); code != 0 && code != http.StatusOK {
		msg := strings.TrimSpace(parsed.Message)
		if msg == "" {
			msg = string(respBody)
		}
		return "", fmt.Errorf("gateway: %s", msg)
	}
	if parsed.Data.PaymentURL != "" {
		return parsed.Data.PaymentURL, nil
	}

	// Fallback for alternate response shapes.
	var generic map[string]any
	if err = common.Unmarshal(respBody, &generic); err != nil {
		return "", fmt.Errorf("payment_url missing in gateway response: %s", string(respBody))
	}
	if data, ok := generic["data"].(map[string]any); ok {
		if paymentURL, ok := data["payment_url"].(string); ok && paymentURL != "" {
			return paymentURL, nil
		}
	}
	msg := strings.TrimSpace(parsed.Message)
	if msg != "" {
		return "", fmt.Errorf("gateway: %s", msg)
	}
	return "", fmt.Errorf("payment_url missing in gateway response: %s", string(respBody))
}

func EpusdtNotify(c *gin.Context) {
	if !isEpusdtWebhookEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("EpUSDT webhook 被拒绝 reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	params, err := parseEpusdtNotifyParams(c)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("EpUSDT webhook 参数解析失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if len(params) == 0 {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("EpUSDT webhook 收到请求 path=%q client_ip=%s params=%q", c.Request.RequestURI, c.ClientIP(), common.GetJsonString(params)))

	if !service.EpusdtVerifySign(params, setting.EpUSDTApiToken) {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("EpUSDT webhook 验签失败 order_id=%s client_ip=%s", params["order_id"], c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	orderID := strings.TrimSpace(params["order_id"])
	status := strings.TrimSpace(params["status"])
	if orderID == "" {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	switch status {
	case "2":
		LockOrder(orderID)
		defer UnlockOrder(orderID)
		if err := model.RechargeEpusdt(orderID, c.ClientIP()); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("EpUSDT 入账失败 order_id=%s client_ip=%s error=%q", orderID, c.ClientIP(), err.Error()))
			_, _ = c.Writer.Write([]byte("fail"))
			return
		}
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("EpUSDT 入账成功 order_id=%s actual_amount=%s tx=%s client_ip=%s", orderID, params["actual_amount"], params["block_transaction_id"], c.ClientIP()))
	case "3":
		LockOrder(orderID)
		defer UnlockOrder(orderID)
		if err := model.MarkTopUpExpired(orderID, model.PaymentProviderEpusdt); err != nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("EpUSDT 标记超时失败 order_id=%s error=%q", orderID, err.Error()))
		}
	default:
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("EpUSDT webhook 忽略状态 order_id=%s status=%s", orderID, status))
	}

	_, _ = c.Writer.Write([]byte("success"))
}

func parseEpusdtNotifyParams(c *gin.Context) (map[string]string, error) {
	result := make(map[string]string)

	if c.Request.Method == http.MethodPost {
		if err := c.Request.ParseForm(); err == nil && len(c.Request.PostForm) > 0 {
			for key, values := range c.Request.PostForm {
				if len(values) > 0 {
					result[key] = values[0]
				}
			}
			return result, nil
		}
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return result, nil
	}

	var payload map[string]any
	if err = common.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	for key, value := range payload {
		result[key] = fmt.Sprint(value)
	}
	return result, nil
}
