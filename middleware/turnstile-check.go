package middleware

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

type turnstileCheckResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
}

var (
	turnstileVerifyURL  = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	turnstileHTTPClient = &http.Client{
		Timeout: 10 * time.Second,
	}
)

func TurnstileCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !common.TurnstileCheckEnabled {
			c.Next()
			return
		}

		secret := strings.TrimSpace(common.TurnstileSecretKey)
		if secret == "" {
			common.SysLog("Turnstile check enabled but TurnstileSecretKey is empty")
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "Turnstile 校验未正确配置，请联系管理员",
			})
			c.Abort()
			return
		}

		response := strings.TrimSpace(c.Query("turnstile"))
		if response == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "Turnstile token 为空",
			})
			c.Abort()
			return
		}

		// Intentionally omit remoteip: users behind split-tunnel proxies (e.g. rule-based
		// routing where challenges.cloudflare.com exits via proxy while the site connects
		// directly, or dual-stack IPv4/IPv6 selection) legitimately present different IPs.
		rawRes, err := turnstileHTTPClient.PostForm(turnstileVerifyURL, url.Values{
			"secret":   {secret},
			"response": {response},
		})
		if err != nil {
			common.SysLog(fmt.Sprintf("Turnstile siteverify request error: %v", err))
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "人机验证服务暂时不可用，请稍后重试",
			})
			c.Abort()
			return
		}
		defer rawRes.Body.Close()

		var res turnstileCheckResponse
		if err := common.DecodeJson(rawRes.Body, &res); err != nil {
			common.SysLog(fmt.Sprintf("Turnstile siteverify decode error (status=%d): %v", rawRes.StatusCode, err))
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "人机验证响应解析失败，请稍后重试",
			})
			c.Abort()
			return
		}

		if !res.Success {
			common.SysLog(fmt.Sprintf(
				"Turnstile check rejected: path=%s ip=%s error_codes=%v",
				c.Request.URL.Path,
				c.ClientIP(),
				res.ErrorCodes,
			))
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "Turnstile 校验失败，请刷新重试！",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
