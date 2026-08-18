package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
)

const (
	openAITeamLinkedErrorDedupTTL      = 60 * time.Second
	openAITeamLinkedErrorFanoutTimeout = 30 * time.Second
	openAITeamLinkedErrorBlockReason   = "team_linked_error"
	openAITeamLinkedRuntimeBlockTTL    = 2 * time.Minute
)

var openAITeamLinkedErrorState = struct {
	sync.Mutex
	recent       map[string]time.Time
	blockedUntil map[int]time.Time
}{}

type openAITeamLinkedErrorPayload struct {
	Detail struct {
		Code string `json:"code"`
	} `json:"detail"`
}

// maybeHandleOpenAITeamLinkedError mirrors Sub2API's ChatGPT Team behavior:
// a deactivated workspace invalidates the other active OAuth accounts that
// carry the same ChatGPT account/team identity.
func maybeHandleOpenAITeamLinkedError(account *model.UpstreamAccount, apiErr *types.NewAPIError) {
	if account == nil || apiErr == nil || model.DB == nil ||
		account.Platform != constant.UpstreamPlatformOpenAI ||
		account.Type != constant.UpstreamAccountTypeOAuth ||
		apiErr.StatusCode != http.StatusPaymentRequired {
		return
	}
	_, body := apiErr.UpstreamResponse()
	var payload openAITeamLinkedErrorPayload
	if common.Unmarshal(body, &payload) != nil || strings.TrimSpace(payload.Detail.Code) != "deactivated_workspace" {
		return
	}
	teamID := upstreamOpenAIChatGPTAccountID(account)
	if teamID == "" || !markOpenAITeamLinkedErrorFired(teamID) {
		return
	}

	opCtx, cancel := context.WithTimeout(context.Background(), openAITeamLinkedErrorFanoutTimeout)
	defer cancel()

	var accounts []model.UpstreamAccount
	if err := model.DB.WithContext(opCtx).
		Where("platform = ? AND status = ?", constant.UpstreamPlatformOpenAI, constant.UpstreamStatusActive).
		Find(&accounts).Error; err != nil {
		common.SysLog(fmt.Sprintf("OpenAI Team 联动熔断查询账号失败(account=%d): %v", account.Id, err))
		return
	}

	targets := make([]*model.UpstreamAccount, 0)
	for i := range accounts {
		candidate := &accounts[i]
		if candidate.Id == account.Id || upstreamOpenAIChatGPTAccountID(candidate) != teamID {
			continue
		}
		targets = append(targets, candidate)
	}
	if len(targets) == 0 {
		return
	}

	// Block scheduling before the database fan-out so concurrent requests stop
	// selecting the siblings immediately.
	for _, target := range targets {
		blockUpstreamAccountRuntime(target.Id)
	}

	errorMessage := fmt.Sprintf("Workspace deactivated (402): team-linked error triggered by account #%d", account.Id)
	for _, target := range targets {
		updates := map[string]any{
			"status":          constant.UpstreamStatusError,
			"schedulable":     false,
			"error_message":   errorMessage,
			"updated_at":      common.GetTimestamp(),
		}
		if err := model.DB.WithContext(opCtx).Model(&model.UpstreamAccount{}).
			Where("id = ?", target.Id).
			Updates(updates).Error; err != nil {
			common.SysLog(fmt.Sprintf("OpenAI Team 联动熔断更新账号失败(account=%d): %v", target.Id, err))
		}
	}
}

// upstreamOpenAIChatGPTAccountID keeps the upstream key as the primary
// identity and accepts StarNexus' canonical account_id for migrated OAuth
// credentials that retain no legacy chatgpt_account_id field.
func upstreamOpenAIChatGPTAccountID(account *model.UpstreamAccount) string {
	if account == nil || account.Platform != constant.UpstreamPlatformOpenAI || account.Type != constant.UpstreamAccountTypeOAuth {
		return ""
	}
	credentials, err := DecryptUpstreamAccountCredentials(account)
	if err != nil {
		return ""
	}
	if id := upstreamCredentialMapString(credentials, "chatgpt_account_id"); id != "" {
		return id
	}
	return upstreamCredentialMapString(credentials, "account_id")
}

func markOpenAITeamLinkedErrorFired(teamID string) bool {
	now := time.Now()
	openAITeamLinkedErrorState.Lock()
	defer openAITeamLinkedErrorState.Unlock()
	if expiry, ok := openAITeamLinkedErrorState.recent[teamID]; ok && expiry.After(now) {
		return false
	}
	if openAITeamLinkedErrorState.recent == nil {
		openAITeamLinkedErrorState.recent = make(map[string]time.Time)
	}
	for key, expiry := range openAITeamLinkedErrorState.recent {
		if !expiry.After(now) {
			delete(openAITeamLinkedErrorState.recent, key)
		}
	}
	openAITeamLinkedErrorState.recent[teamID] = now.Add(openAITeamLinkedErrorDedupTTL)
	return true
}

func blockUpstreamAccountRuntime(accountID int) {
	if accountID <= 0 {
		return
	}
	now := time.Now()
	openAITeamLinkedErrorState.Lock()
	defer openAITeamLinkedErrorState.Unlock()
	if openAITeamLinkedErrorState.blockedUntil == nil {
		openAITeamLinkedErrorState.blockedUntil = make(map[int]time.Time)
	}
	until := now.Add(openAITeamLinkedRuntimeBlockTTL)
	if current, ok := openAITeamLinkedErrorState.blockedUntil[accountID]; !ok || until.After(current) {
		openAITeamLinkedErrorState.blockedUntil[accountID] = until
	}
}

func isUpstreamAccountRuntimeBlocked(accountID int, now time.Time) bool {
	if accountID <= 0 {
		return false
	}
	openAITeamLinkedErrorState.Lock()
	defer openAITeamLinkedErrorState.Unlock()
	until, ok := openAITeamLinkedErrorState.blockedUntil[accountID]
	if !ok {
		return false
	}
	if until.After(now) {
		return true
	}
	delete(openAITeamLinkedErrorState.blockedUntil, accountID)
	return false
}

func clearUpstreamAccountRuntimeBlock(accountID int) {
	if accountID <= 0 {
		return
	}
	openAITeamLinkedErrorState.Lock()
	defer openAITeamLinkedErrorState.Unlock()
	delete(openAITeamLinkedErrorState.blockedUntil, accountID)
}
