package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
)

var ErrUserNodeRouterNotConfigured = errors.New("user node router is not configured")

const (
	userNodeRouterVersion              = 2
	userNodeRouterLockTTL              = 2 * time.Minute
	userNodeRouterLockWait             = 10 * time.Second
	userNodeRouterReconcileBatchSize   = 100
	userNodeRouterDefaultReconcileMins = 60
)

var (
	userNodeRouterHTTPClient = &http.Client{Timeout: 15 * time.Second}
	userNodeRoutingLocalLock sync.Map
	userNodeReconcileOnce    sync.Once
	userNodeReconcileRunning atomic.Bool

	userNodeRouterRevisionScript = redis.NewScript(`
local current = tonumber(redis.call('GET', KEYS[1]) or '0')
local candidate = tonumber(ARGV[1])
if candidate <= current then
  candidate = current + 1
end
redis.call('SET', KEYS[1], candidate)
return candidate
`)
	userNodeRouterUnlockScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)
)

type userNodeRouterRequest struct {
	Version     int      `json:"version"`
	Action      string   `json:"action"`
	UserId      int      `json:"user_id"`
	Node        string   `json:"node,omitempty"`
	Origin      string   `json:"origin,omitempty"`
	Revision    int64    `json:"revision,omitempty"`
	TokenHashes []string `json:"token_hashes,omitempty"`
}

type userNodeRouterResponse struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	Version    int    `json:"version"`
	Action     string `json:"action"`
	UserId     int    `json:"user_id"`
	Node       string `json:"node"`
	Revision   int64  `json:"revision"`
	TokenCount int    `json:"token_count"`
}

type userNodeRouterHTTPError struct {
	StatusCode int
	Body       string
}

func (e *userNodeRouterHTTPError) Error() string {
	return fmt.Sprintf("sync user node routing failed with status %d: %s", e.StatusCode, e.Body)
}

func IsUserNodeRouterConfigured() bool {
	return strings.TrimSpace(os.Getenv("NODE_ROUTER_ADMIN_URL")) != "" &&
		strings.TrimSpace(os.Getenv("NODE_ROUTER_ADMIN_SECRET")) != ""
}

func hashUserRoutingToken(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])
}

func NormalizeRoutingOrigin(origin string) (string, error) {
	origin = strings.ToLower(strings.TrimSpace(origin))
	if origin == "" || strings.ContainsAny(origin, "/:\\") {
		return "", errors.New("invalid routing node origin")
	}
	suffix := strings.ToLower(strings.TrimSpace(os.Getenv("NODE_ROUTER_ALLOWED_ORIGIN_SUFFIX")))
	if suffix == "" {
		suffix = ".dkby.com"
	}
	if !strings.HasPrefix(suffix, ".") || !strings.HasSuffix(origin, suffix) {
		return "", errors.New("routing node origin is outside the allowed domain")
	}
	return origin, nil
}

func postUserNodeRouter(ctx context.Context, payload userNodeRouterRequest) (*userNodeRouterResponse, error) {
	adminURL := strings.TrimSpace(os.Getenv("NODE_ROUTER_ADMIN_URL"))
	secret := strings.TrimSpace(os.Getenv("NODE_ROUTER_ADMIN_SECRET"))
	if adminURL == "" || secret == "" {
		return nil, ErrUserNodeRouterNotConfigured
	}
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, adminURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := userNodeRouterHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sync user node routing: %w", err)
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if readErr != nil {
		return nil, fmt.Errorf("read user node router response: %w", readErr)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &userNodeRouterHTTPError{
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(responseBody)),
		}
	}
	var result userNodeRouterResponse
	if err := common.Unmarshal(responseBody, &result); err != nil {
		return nil, fmt.Errorf("invalid user node router response: %w", err)
	}
	if !result.Success {
		return nil, fmt.Errorf("user node router rejected request: %s", result.Message)
	}
	if result.Version != userNodeRouterVersion || result.Action != payload.Action || result.UserId != payload.UserId {
		return nil, errors.New("user node router returned a mismatched response")
	}
	return &result, nil
}

func shouldCompensateUserRoute(err error) bool {
	var httpErr *userNodeRouterHTTPError
	if !errors.As(err, &httpErr) {
		return true
	}
	switch httpErr.StatusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict:
		return false
	default:
		return true
	}
}

func syncUserRoutingTokensUnlocked(ctx context.Context, userId int) error {
	keys, err := model.GetUserTokenRoutingKeys(userId)
	if err != nil {
		return err
	}
	hashes := make([]string, 0, len(keys))
	for _, key := range keys {
		hashes = append(hashes, hashUserRoutingToken(key))
	}
	result, err := postUserNodeRouter(ctx, userNodeRouterRequest{
		Version:     userNodeRouterVersion,
		Action:      "sync_tokens",
		UserId:      userId,
		TokenHashes: hashes,
	})
	if err != nil {
		return err
	}
	if result.TokenCount != len(hashes) {
		return errors.New("user node router returned an unexpected token count")
	}
	return nil
}

func setUserRoutingRouteUnlocked(ctx context.Context, userId int, node string, origin string, revision int64) error {
	result, err := postUserNodeRouter(ctx, userNodeRouterRequest{
		Version:  userNodeRouterVersion,
		Action:   "set_route",
		UserId:   userId,
		Node:     node,
		Origin:   origin,
		Revision: revision,
	})
	if err != nil {
		return err
	}
	if result.Node != node || result.Revision != revision {
		return errors.New("user node router returned a mismatched route")
	}
	return nil
}

func nextUserNodeRoutingRevision(ctx context.Context, minimum int64) (int64, error) {
	candidate := time.Now().UnixMilli()
	if candidate <= minimum {
		candidate = minimum + 1
	}
	if !common.RedisEnabled || common.RDB == nil {
		return candidate, nil
	}
	return userNodeRouterRevisionScript.Run(
		ctx,
		common.RDB,
		[]string{"node-routing:revision"},
		candidate,
	).Int64()
}

func userNodeLocalMutex(userId int) *sync.Mutex {
	value, _ := userNodeRoutingLocalLock.LoadOrStore(userId, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func withUserNodeRoutingLock(ctx context.Context, userId int, fn func(context.Context) error) error {
	if userId <= 0 {
		return errors.New("invalid user id")
	}
	if !common.RedisEnabled || common.RDB == nil {
		localLock := userNodeLocalMutex(userId)
		localLock.Lock()
		defer localLock.Unlock()

		lockCtx, cancel := context.WithTimeout(ctx, userNodeRouterLockWait)
		defer cancel()
		owner := common.GetUUID()
		for {
			acquired, err := model.TryAcquireUserNodeRoutingLock(
				userId,
				owner,
				common.GetTimestamp()+int64(userNodeRouterLockTTL/time.Second),
			)
			if err != nil {
				return fmt.Errorf("acquire database user node routing lock: %w", err)
			}
			if acquired {
				break
			}
			select {
			case <-lockCtx.Done():
				return errors.New("timed out waiting for database user node routing lock")
			case <-time.After(50 * time.Millisecond):
			}
		}
		defer func() {
			if err := model.ReleaseUserNodeRoutingLock(userId, owner); err != nil {
				common.SysLog(fmt.Sprintf("release database user node routing lock failed for user %d: %s", userId, err.Error()))
			}
		}()
		return fn(ctx)
	}

	lockCtx, cancel := context.WithTimeout(ctx, userNodeRouterLockWait)
	defer cancel()
	key := "node-routing:lock:user:" + strconv.Itoa(userId)
	owner := common.GetUUID()
	for {
		acquired, err := common.RDB.SetNX(lockCtx, key, owner, userNodeRouterLockTTL).Result()
		if err != nil {
			return fmt.Errorf("acquire user node routing lock: %w", err)
		}
		if acquired {
			break
		}
		select {
		case <-lockCtx.Done():
			return errors.New("timed out waiting for user node routing lock")
		case <-time.After(50 * time.Millisecond):
		}
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer releaseCancel()
		if _, err := userNodeRouterUnlockScript.Run(releaseCtx, common.RDB, []string{key}, owner).Result(); err != nil {
			common.SysLog(fmt.Sprintf("release user node routing lock failed for user %d: %s", userId, err.Error()))
		}
	}()
	return fn(ctx)
}

func resolveRoutingNode(node string) (string, string, error) {
	normalized, err := model.NormalizeUserNode(node)
	if err != nil {
		return "", "", err
	}
	if normalized == model.UserNodeAuto {
		return normalized, "", nil
	}
	routingNode, err := model.GetRoutingNodeByKey(normalized, true)
	if err != nil {
		return "", "", err
	}
	origin, err := NormalizeRoutingOrigin(routingNode.Origin)
	if err != nil {
		return "", "", err
	}
	return routingNode.Key, origin, nil
}

func compensateUserRoute(ctx context.Context, userId int, previous *model.UserNodeBinding, minimumRevision int64) {
	node := model.UserNodeAuto
	origin := ""
	if previous != nil && previous.Node != model.UserNodeAuto {
		if resolvedNode, resolvedOrigin, err := resolveRoutingNode(previous.Node); err == nil {
			node = resolvedNode
			origin = resolvedOrigin
		}
	}
	revision, err := nextUserNodeRoutingRevision(ctx, minimumRevision)
	if err != nil {
		common.SysLog(fmt.Sprintf("generate rollback revision failed for user %d: %s", userId, err.Error()))
		return
	}
	if err := setUserRoutingRouteUnlocked(ctx, userId, node, origin, revision); err != nil {
		common.SysLog(fmt.Sprintf("rollback user node route failed for user %d: %s", userId, err.Error()))
		return
	}
	tokensSynced := previous != nil && previous.TokensSynced
	if err := model.SaveUserNodeBindingState(userId, node, revision, tokensSynced); err != nil {
		common.SysLog(fmt.Sprintf("persist rolled back user node route failed for user %d: %s", userId, err.Error()))
	}
}

func UpdateUserNodeRoute(ctx context.Context, userId int, requestedNode string) (*model.UserNodeBinding, error) {
	node, origin, err := resolveRoutingNode(requestedNode)
	if err != nil {
		return nil, err
	}
	var result *model.UserNodeBinding
	err = withUserNodeRoutingLock(ctx, userId, func(lockCtx context.Context) error {
		previous, err := model.GetUserNodeBindingRecord(userId)
		if err != nil {
			return err
		}
		revision, err := nextUserNodeRoutingRevision(lockCtx, previous.Revision)
		if err != nil {
			return err
		}
		if err := setUserRoutingRouteUnlocked(lockCtx, userId, node, origin, revision); err != nil {
			if shouldCompensateUserRoute(err) {
				compensateUserRoute(context.Background(), userId, previous, revision)
			}
			return err
		}
		tokensSynced := previous.TokensSynced
		if !tokensSynced {
			if err := syncUserRoutingTokensUnlocked(lockCtx, userId); err != nil {
				compensateUserRoute(context.Background(), userId, previous, revision)
				return err
			}
			tokensSynced = true
		}
		if err := model.SaveUserNodeBindingState(userId, node, revision, tokensSynced); err != nil {
			compensateUserRoute(context.Background(), userId, previous, revision)
			return err
		}
		result = &model.UserNodeBinding{
			UserId:       userId,
			Node:         node,
			Revision:     revision,
			TokensSynced: tokensSynced,
			UpdatedAt:    common.GetTimestamp(),
		}
		return nil
	})
	return result, err
}

func SyncUserRoutingTokens(ctx context.Context, userId int) error {
	return withUserNodeRoutingLock(ctx, userId, func(lockCtx context.Context) error {
		if err := syncUserRoutingTokensUnlocked(lockCtx, userId); err != nil {
			return err
		}
		return model.UpdateUserNodeTokensSynced(userId, true)
	})
}

func DeleteUserNodeRouting(ctx context.Context, userId int) error {
	return withUserNodeRoutingLock(ctx, userId, func(lockCtx context.Context) error {
		_, err := postUserNodeRouter(lockCtx, userNodeRouterRequest{
			Version: userNodeRouterVersion,
			Action:  "delete_user",
			UserId:  userId,
		})
		return err
	})
}

func ReconcileUserNodeRouting(ctx context.Context, userId int) error {
	return withUserNodeRoutingLock(ctx, userId, func(lockCtx context.Context) error {
		binding, err := model.GetUserNodeBindingRecord(userId)
		if err != nil {
			return err
		}
		if binding.Node == model.UserNodeAuto {
			return nil
		}
		node, origin, err := resolveRoutingNode(binding.Node)
		if err != nil {
			return err
		}
		needsTokenSync := !binding.TokensSynced
		revision, err := nextUserNodeRoutingRevision(lockCtx, binding.Revision)
		if err != nil {
			return err
		}
		if err := setUserRoutingRouteUnlocked(lockCtx, userId, node, origin, revision); err != nil {
			return err
		}
		if needsTokenSync {
			if err := syncUserRoutingTokensUnlocked(lockCtx, userId); err != nil {
				return err
			}
		}
		return model.SaveUserNodeBindingState(userId, node, revision, true)
	})
}

func RefreshStoredUserNodeBinding(ctx context.Context, userId int) error {
	if !IsUserNodeRouterConfigured() {
		return nil
	}
	return withUserNodeRoutingLock(ctx, userId, func(lockCtx context.Context) error {
		binding, err := model.GetUserNodeBindingRecord(userId)
		if err != nil {
			return err
		}
		if err := model.UpdateUserNodeTokensSynced(userId, false); err != nil {
			return err
		}
		if binding.Node == model.UserNodeAuto {
			return nil
		}
		if err := syncUserRoutingTokensUnlocked(lockCtx, userId); err != nil {
			return err
		}
		return model.UpdateUserNodeTokensSynced(userId, true)
	})
}

func SyncUserNodeBinding(ctx context.Context, userId int, node string) error {
	_, err := UpdateUserNodeRoute(ctx, userId, node)
	return err
}

func ReconcileAllUserNodeRouting(ctx context.Context) error {
	lastUserId := 0
	for {
		bindings, err := model.ListUserNodeBindingsBatch(lastUserId, userNodeRouterReconcileBatchSize)
		if err != nil {
			return err
		}
		for _, binding := range bindings {
			if err := ReconcileUserNodeRouting(ctx, binding.UserId); err != nil {
				common.SysLog(fmt.Sprintf("reconcile user node routing failed for user %d: %s", binding.UserId, err.Error()))
			}
			lastUserId = binding.UserId
		}
		if len(bindings) < userNodeRouterReconcileBatchSize {
			return nil
		}
	}
}

func TriggerUserNodeRoutingReconcile() bool {
	if !userNodeReconcileRunning.CompareAndSwap(false, true) {
		return false
	}
	go func() {
		defer userNodeReconcileRunning.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if err := ReconcileAllUserNodeRouting(ctx); err != nil {
			common.SysLog("user node routing reconciliation failed: " + err.Error())
		}
	}()
	return true
}

func StartUserNodeRoutingReconcileTask() {
	userNodeReconcileOnce.Do(func() {
		if !common.IsMasterNode || !IsUserNodeRouterConfigured() {
			return
		}
		intervalMinutes := common.GetEnvOrDefault(
			"NODE_ROUTER_RECONCILE_INTERVAL_MINUTES",
			userNodeRouterDefaultReconcileMins,
		)
		if intervalMinutes == 0 {
			go func() {
				time.Sleep(30 * time.Second)
				TriggerUserNodeRoutingReconcile()
			}()
			return
		}
		if intervalMinutes < 0 {
			intervalMinutes = userNodeRouterDefaultReconcileMins
		}
		interval := time.Duration(intervalMinutes) * time.Minute
		go func() {
			time.Sleep(30 * time.Second)
			TriggerUserNodeRoutingReconcile()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for range ticker.C {
				TriggerUserNodeRoutingReconcile()
			}
		}()
	})
}
