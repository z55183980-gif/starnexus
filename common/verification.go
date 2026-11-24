package common

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

type verificationValue struct {
	code     string
	time     time.Time
	attempts int
}

const (
	EmailVerificationPurpose = "v"
	PasswordResetPurpose     = "r"
	MaxVerificationAttempts  = 5
)

var verificationMutex sync.Mutex
var verificationMap map[string]verificationValue
var verificationMapMaxSize = 10
var VerificationValidMinutes = 10

var verifyCodeLuaScript = redis.NewScript(`
local code = redis.call('HGET', KEYS[1], 'code')
if not code then
	return 0
end
if code == ARGV[1] then
	return 1
end
local attempts = redis.call('HINCRBY', KEYS[1], 'attempts', 1)
if attempts >= tonumber(ARGV[2]) then
	redis.call('DEL', KEYS[1])
end
return 0
`)

func normalizeVerificationKey(key string, purpose string) string {
	return purpose + ":" + strings.ToLower(strings.TrimSpace(key))
}

func redisVerificationKey(key string, purpose string) string {
	return "verification:" + normalizeVerificationKey(key, purpose)
}

func GenerateVerificationCode(length int) string {
	code := uuid.New().String()
	code = strings.Replace(code, "-", "", -1)
	if length == 0 {
		return code
	}
	if length > len(code) {
		return code
	}
	return code[:length]
}

// GenerateNumericVerificationCode generates a cryptographically random numeric
// verification code (0-9 only) so email codes cannot collide with HTML/CSS hex tokens.
func GenerateNumericVerificationCode(length int) string {
	if length <= 0 {
		length = 6
	}
	digits := make([]byte, length)
	ten := big.NewInt(10)
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, ten)
		if err != nil {
			// Fallback to UUID digit extraction only if OS CSPRNG fails
			fallback := GenerateVerificationCode(length)
			return fallback
		}
		digits[i] = byte('0' + n.Int64())
	}
	return string(digits)
}

func RegisterVerificationCodeWithKey(key string, code string, purpose string) {
	code = strings.TrimSpace(code)
	ttl := time.Duration(VerificationValidMinutes) * time.Minute

	if RedisEnabled && RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		rKey := redisVerificationKey(key, purpose)
		pipe := RDB.TxPipeline()
		pipe.Del(ctx, rKey)
		pipe.HSet(ctx, rKey, "code", code, "attempts", 0)
		pipe.Expire(ctx, rKey, ttl)
		if _, err := pipe.Exec(ctx); err == nil {
			return
		}
	}

	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap[normalizeVerificationKey(key, purpose)] = verificationValue{
		code:     code,
		time:     time.Now(),
		attempts: 0,
	}
	if len(verificationMap) > verificationMapMaxSize {
		removeExpiredPairs()
	}
}

func VerifyCodeWithKey(key string, code string, purpose string) bool {
	code = strings.TrimSpace(code)
	if code == "" {
		return false
	}

	if RedisEnabled && RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		rKey := redisVerificationKey(key, purpose)
		matched, err := verifyCodeLuaScript.Run(ctx, RDB, []string{rKey}, code, MaxVerificationAttempts).Int()
		if err == nil {
			return matched == 1
		}
	}

	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	mapKey := normalizeVerificationKey(key, purpose)
	value, okay := verificationMap[mapKey]
	now := time.Now()
	if !okay || int(now.Sub(value.time).Seconds()) >= VerificationValidMinutes*60 {
		delete(verificationMap, mapKey)
		return false
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(value.code)) == 1 {
		return true
	}
	value.attempts++
	if value.attempts >= MaxVerificationAttempts {
		delete(verificationMap, mapKey)
	} else {
		verificationMap[mapKey] = value
	}
	return false
}

func DeleteKey(key string, purpose string) {
	if RedisEnabled && RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = RDB.Del(ctx, redisVerificationKey(key, purpose)).Err()
	}
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	delete(verificationMap, normalizeVerificationKey(key, purpose))
}

// no lock inside, so the caller must lock the verificationMap before calling!
func removeExpiredPairs() {
	now := time.Now()
	for key := range verificationMap {
		if int(now.Sub(verificationMap[key].time).Seconds()) >= VerificationValidMinutes*60 {
			delete(verificationMap, key)
		}
	}
}

func init() {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap = make(map[string]verificationValue)
}
