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
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

var ErrUserNodeRouterNotConfigured = errors.New("user node router is not configured")

type userNodeRouterSyncRequest struct {
	UserId      int      `json:"user_id"`
	Node        string   `json:"node"`
	TokenHashes []string `json:"token_hashes"`
}

func IsUserNodeRouterConfigured() bool {
	return strings.TrimSpace(os.Getenv("NODE_ROUTER_ADMIN_URL")) != "" &&
		strings.TrimSpace(os.Getenv("NODE_ROUTER_ADMIN_SECRET")) != ""
}

func hashUserRoutingToken(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])
}

func SyncUserNodeBinding(ctx context.Context, userId int, node string) error {
	node, err := model.NormalizeUserNode(node)
	if err != nil {
		return err
	}
	adminURL := strings.TrimSpace(os.Getenv("NODE_ROUTER_ADMIN_URL"))
	secret := strings.TrimSpace(os.Getenv("NODE_ROUTER_ADMIN_SECRET"))
	if adminURL == "" || secret == "" {
		return ErrUserNodeRouterNotConfigured
	}

	keys, err := model.GetUserTokenRoutingKeys(userId)
	if err != nil {
		return err
	}
	hashes := make([]string, 0, len(keys))
	for _, key := range keys {
		hashes = append(hashes, hashUserRoutingToken(key))
	}
	payload, err := common.Marshal(userNodeRouterSyncRequest{
		UserId:      userId,
		Node:        node,
		TokenHashes: hashes,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, adminURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sync user node binding: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("sync user node binding failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func RefreshStoredUserNodeBinding(ctx context.Context, userId int) error {
	node, err := model.GetUserNodeBinding(userId)
	if err != nil || node == model.UserNodeAuto || !IsUserNodeRouterConfigured() {
		return err
	}
	return SyncUserNodeBinding(ctx, userId, node)
}
