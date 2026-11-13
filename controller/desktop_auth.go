package controller

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const desktopCallback = "com.dkby.starnexus.keytool://auth/callback"
const desktopGrantTTL = 2 * time.Minute

var desktopTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var desktopVerifierPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
var errDesktopGrant = errors.New("授权无效或已过期，请重新登录")

//go:embed desktop_login.html
var desktopLoginHTML []byte

type desktopGrant struct {
	UserID    int    `json:"user_id"`
	Challenge string `json:"challenge"`
}
type desktopGrantStore interface {
	Save(context.Context, string, string) error
	Get(context.Context, string) (string, error)
	Consume(context.Context, string, string) (bool, error)
}
type redisDesktopGrants struct{}

func (redisDesktopGrants) Save(ctx context.Context, key, value string) error {
	if !common.RedisEnabled || common.RDB == nil {
		return errors.New("桌面登录暂不可用")
	}
	return common.RDB.Set(ctx, key, value, desktopGrantTTL).Err()
}
func (redisDesktopGrants) Get(ctx context.Context, key string) (string, error) {
	if !common.RedisEnabled || common.RDB == nil {
		return "", errDesktopGrant
	}
	return common.RDB.Get(ctx, key).Result()
}
func (redisDesktopGrants) Consume(ctx context.Context, key, expected string) (bool, error) {
	// Compare-and-delete makes a successful exchange single-use across all nodes.
	n, err := common.RDB.Eval(ctx, `if redis.call('GET', KEYS[1]) == ARGV[1] then return redis.call('DEL', KEYS[1]) else return 0 end`, []string{key}, expected).Int()
	return n == 1, err
}
func desktopHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
func desktopGrantKey(code string) string { return "starnexus:desktop-login:" + desktopHash(code) }
func issueDesktopGrant(ctx context.Context, store desktopGrantStore, uid int, challenge string) (string, error) {
	if uid <= 0 || !desktopTokenPattern.MatchString(challenge) {
		return "", errDesktopGrant
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	value, err := common.Marshal(desktopGrant{UserID: uid, Challenge: challenge})
	if err != nil {
		return "", err
	}
	if err := store.Save(ctx, desktopGrantKey(code), string(value)); err != nil {
		return "", err
	}
	return code, nil
}
func consumeDesktopGrant(ctx context.Context, store desktopGrantStore, code, verifier string) (int, error) {
	if !desktopTokenPattern.MatchString(code) || !desktopVerifierPattern.MatchString(verifier) {
		return 0, errDesktopGrant
	}
	key := desktopGrantKey(code)
	raw, err := store.Get(ctx, key)
	if err != nil {
		return 0, errDesktopGrant
	}
	var grant desktopGrant
	if common.UnmarshalJsonStr(raw, &grant) != nil || grant.UserID <= 0 || subtle.ConstantTimeCompare([]byte(grant.Challenge), []byte(desktopHash(verifier))) != 1 {
		return 0, errDesktopGrant
	}
	ok, err := store.Consume(ctx, key, raw)
	if err != nil || !ok {
		return 0, errDesktopGrant
	}
	return grant.UserID, nil
}
func desktopNoCache(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Robots-Tag", "noindex, nofollow")
}
func DesktopLoginPage(c *gin.Context) {
	desktopNoCache(c)
	c.Header("X-Frame-Options", "DENY")
	c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'")
	c.Data(http.StatusOK, "text/html; charset=utf-8", desktopLoginHTML)
}
func DesktopLoginConfig(c *gin.Context) {
	desktopNoCache(c)
	common.ApiSuccess(c, gin.H{"callback": desktopCallback, "pkce": "S256"})
}
func DesktopLoginAuthorize(c *gin.Context) {
	desktopNoCache(c)
	if c.GetHeader("Origin") != "https://dkby.com" {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	var req struct {
		Challenge string `json:"code_challenge"`
		State     string `json:"state"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if c.ShouldBindJSON(&req) != nil || !desktopTokenPattern.MatchString(req.State) {
		common.ApiErrorMsg(c, "登录请求无效，请从工具重新发起")
		return
	}
	code, err := issueDesktopGrant(c.Request.Context(), redisDesktopGrants{}, c.GetInt("id"), req.Challenge)
	if err != nil {
		common.ApiErrorMsg(c, "无法建立授权，请稍后重新登录")
		return
	}
	callback := desktopCallback + "?" + url.Values{"code": {code}, "state": {req.State}}.Encode()
	common.ApiSuccess(c, gin.H{"callback": callback})
}
func DesktopLoginExchange(c *gin.Context) {
	desktopNoCache(c)
	// Native backend only. Website JS never receives the new session cookie.
	if c.GetHeader("Origin") != "" {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	var req struct {
		Code     string `json:"code"`
		Verifier string `json:"code_verifier"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if c.ShouldBindJSON(&req) != nil {
		common.ApiErrorMsg(c, "登录请求无效")
		return
	}
	uid, err := consumeDesktopGrant(c.Request.Context(), redisDesktopGrants{}, req.Code, req.Verifier)
	if err != nil {
		common.ApiErrorMsg(c, errDesktopGrant.Error())
		return
	}
	user, err := model.GetUserById(uid, false)
	if err != nil || user == nil || user.Status != common.UserStatusEnabled {
		common.ApiErrorMsg(c, "账号不可用，请重新登录")
		return
	}
	setupLogin(user, c)
}
