package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestExportUpstreamAccountsIncludesImportableCredentials(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "export-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeAPIKey,
		Status:         constant.UpstreamStatusActive, SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "export-account", Platform: constant.UpstreamPlatformOpenAI,
			Type: constant.UpstreamAccountTypeAPIKey, Extra: "{}", Concurrency: 2,
			Priority: 10, Weight: 3, Status: constant.UpstreamStatusActive,
			Schedulable: true, AutoPauseOnExpired: true,
			OAuthRefreshOwner: constant.UpstreamOAuthRefreshOwnerExternal,
		},
		Credentials: map[string]any{"api_key": "export-secret"},
		PoolIds:     []int{pool.Id},
	}
	require.NoError(t, CreateUpstreamAccount(&input))

	exported, err := ExportUpstreamAccounts([]int{input.Account.Id})
	require.NoError(t, err)
	require.Equal(t, upstreamAccountExportType, exported.Type)
	require.Equal(t, upstreamAccountExportVersion, exported.Version)
	require.Len(t, exported.Accounts, 1)
	require.Equal(t, "export-secret", exported.Accounts[0].Credentials["api_key"])
	require.Equal(t, []int{pool.Id}, exported.Accounts[0].PoolIds)
}

func TestExportUpstreamAccountsMovesLegacyCredentialBackedOptionsToSub2Locations(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "legacy-options", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey,
			Extra:       `{"intercept_warmup_requests":true,"compact_model_mapping":{"gpt-5.*":"gpt-5.4"},"openai_capabilities":["chat_completions"]}`,
			Concurrency: 1, Priority: 50, Weight: 1, Status: constant.UpstreamStatusActive, Schedulable: true, AutoPauseOnExpired: true,
		},
		Credentials: map[string]any{"api_key": "secret"},
	}
	require.NoError(t, CreateUpstreamAccount(&input))

	exported, err := ExportUpstreamAccounts([]int{input.Account.Id})
	require.NoError(t, err)
	require.Len(t, exported.Accounts, 1)
	credentials := exported.Accounts[0].Credentials
	require.Equal(t, true, credentials["intercept_warmup_requests"])
	require.Equal(t, map[string]any{"gpt-5.*": "gpt-5.4"}, credentials["compact_model_mapping"])
	require.Equal(t, []any{"chat_completions"}, credentials["openai_capabilities"])
	extra := exported.Accounts[0].Extra.(map[string]any)
	require.NotContains(t, extra, "intercept_warmup_requests")
	require.NotContains(t, extra, "compact_model_mapping")
	require.NotContains(t, extra, "openai_capabilities")
}

func TestImportUpstreamAccountsKeepsSub2CredentialBackedOptionsCanonical(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	payload := UpstreamAccountExport{
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Accounts: []UpstreamAccountExportItem{
			{
				Name: "sub2-options", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey,
				Credentials: map[string]any{
					"api_key":                   "secret",
					"intercept_warmup_requests": true,
					"compact_model_mapping":     map[string]any{"gpt-5.*": "gpt-5.4-compact"},
					"openai_capabilities":       []any{"chat_completions"},
				},
				Extra: map[string]any{
					"intercept_warmup_requests": false,
					"compact_model_mapping":     map[string]any{"legacy": "legacy"},
					"openai_capabilities":       []any{"embeddings"},
				},
				Concurrency: 1, Priority: 50,
			},
		},
	}

	result, err := ImportUpstreamData(payload)
	require.NoError(t, err)
	require.Equal(t, 1, result.AccountCreated)
	require.Zero(t, result.AccountFailed)

	var account model.UpstreamAccount
	require.NoError(t, model.DB.Where("name = ?", "sub2-options").First(&account).Error)
	credentials, err := DecryptUpstreamAccountCredentials(&account)
	require.NoError(t, err)
	require.Equal(t, true, credentials["intercept_warmup_requests"])
	require.Equal(t, map[string]any{"gpt-5.*": "gpt-5.4-compact"}, credentials["compact_model_mapping"])
	require.Equal(t, []any{"chat_completions"}, credentials["openai_capabilities"])
	var extra map[string]any
	require.NoError(t, common.UnmarshalJsonStr(account.Extra, &extra))
	require.NotContains(t, extra, "intercept_warmup_requests")
	require.NotContains(t, extra, "compact_model_mapping")
	require.NotContains(t, extra, "openai_capabilities")
}

func TestImportUpstreamAccountsAppliesPoolMemberships(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "import-default-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType:  constant.UpstreamAccountTypeAPIKey,
		Status:          constant.UpstreamStatusActive,
		SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))

	payload := UpstreamAccountExport{Accounts: []UpstreamAccountExportItem{{
		Name: "pooled-import", Platform: constant.UpstreamPlatformOpenAI,
		Type:        constant.UpstreamAccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "pooled-secret"},
		PoolIds:     []int{pool.Id},
	}}}
	result, err := ImportUpstreamData(payload)
	require.NoError(t, err)
	require.Equal(t, 1, result.AccountCreated)

	var account model.UpstreamAccount
	require.NoError(t, model.DB.Where("name = ?", "pooled-import").First(&account).Error)
	poolIds, err := listUpstreamAccountPoolIds(model.DB, account.Id)
	require.NoError(t, err)
	require.Equal(t, []int{pool.Id}, poolIds)
}

func TestImportUpstreamAccountsAppliesTeamDefaultConfiguration(t *testing.T) {
	setupUpstreamAdminTestDB(t)

	payload := UpstreamAccountExport{Accounts: []UpstreamAccountExportItem{{
		Name: "team-import", Platform: constant.UpstreamPlatformOpenAI,
		Type:        constant.UpstreamAccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "team-secret"},
		Concurrency: 2, Priority: 50, Weight: 3,
	}}}
	result, err := ImportUpstreamData(payload, "team")
	require.NoError(t, err)
	require.Equal(t, 1, result.AccountCreated)

	var account model.UpstreamAccount
	require.NoError(t, model.DB.Where("name = ?", "team-import").First(&account).Error)
	require.Equal(t, 10, account.Concurrency)
	require.Equal(t, 1, account.Priority)
	require.Equal(t, 1000, account.Weight)
	require.NotNil(t, account.LoadFactor)
	require.Equal(t, 1000, *account.LoadFactor)
	require.NotNil(t, account.RateMultiplier)
	require.Equal(t, 1.0, *account.RateMultiplier)

	var pool model.UpstreamAccountPool
	require.NoError(t, model.DB.Where("name = ?", upstreamAccountImportTeamPoolName).First(&pool).Error)
	require.Equal(t, constant.UpstreamPlatformOpenAI, pool.Platform)
	require.Equal(t, upstreamAccountImportTeamSchedulerConfig, pool.SchedulerConfig)
	poolIds, err := listUpstreamAccountPoolIds(model.DB, account.Id)
	require.NoError(t, err)
	require.Equal(t, []int{pool.Id}, poolIds)
}

func TestImportUpstreamAccountsCopiesTeamCredentialOptions(t *testing.T) {
	setupUpstreamAdminTestDB(t)

	payload := UpstreamAccountExport{Accounts: []UpstreamAccountExportItem{{
		Name: "team-oauth-import", Platform: constant.UpstreamPlatformOpenAI,
		Type: constant.UpstreamAccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "import-access-token", "account_id": "import-account-id",
			"model_mapping": map[string]any{"gpt-5.6": "wrong-model"},
			"plan_type":     "plus",
		},
		Extra: map[string]any{
			"openai_oauth_responses_websockets_v2_mode": "off",
			"codex_fingerprint_mode":                    "off",
			"codex_7d_used_percent":                     99,
		},
	}}}
	result, err := ImportUpstreamData(payload, "team")
	require.NoError(t, err)
	require.Equal(t, 1, result.AccountCreated)

	var account model.UpstreamAccount
	require.NoError(t, model.DB.Where("name = ?", "team-oauth-import").First(&account).Error)
	credentials, err := DecryptUpstreamAccountCredentials(&account)
	require.NoError(t, err)
	require.Equal(t, "team", credentials["plan_type"])
	require.Equal(t, upstreamAccountImportTeamModelMapping, upstreamCredentialStringMap(credentials["model_mapping"]))
	var extra map[string]any
	require.NoError(t, common.UnmarshalJsonStr(account.Extra, &extra))
	require.Equal(t, "ctx_pool", extra["openai_oauth_responses_websockets_v2_mode"])
	require.Equal(t, "device", extra["codex_fingerprint_mode"])
	require.Equal(t, false, extra["openai_long_context_billing_enabled"])
	require.NotContains(t, extra, "codex_7d_used_percent")
}

func TestSub2APICompatibleExportImportRoundTripWithoutKeyring(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	t.Setenv(upstreamCredentialKeysEnv, "")
	t.Setenv(upstreamCredentialActiveVersionEnv, "")

	proxyInput := UpstreamProxyCreateInput{
		Proxy: model.UpstreamProxy{
			Name: "roundtrip-proxy", Protocol: constant.UpstreamProxyProtocolSOCKS5,
			Host: "127.0.0.1", Port: 1080, Status: constant.UpstreamStatusActive,
			FallbackMode: constant.UpstreamProxyFallbackNone,
		},
		Auth: UpstreamProxyAuthInput{Username: "proxy-user", Password: "proxy-secret"},
	}
	require.NoError(t, CreateUpstreamProxy(&proxyInput))
	accountInput := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "roundtrip-account", Platform: constant.UpstreamPlatformOpenAI,
			Type: constant.UpstreamAccountTypeAPIKey, Extra: `{"responses_mode":"auto"}`,
			ProxyId: &proxyInput.Proxy.Id, Concurrency: 2, Priority: 10, Weight: 1,
			Status: constant.UpstreamStatusActive, Schedulable: true, AutoPauseOnExpired: true,
		},
		Credentials: map[string]any{"api_key": "roundtrip-secret"},
	}
	require.NoError(t, CreateUpstreamAccount(&accountInput))

	exported, err := ExportUpstreamAccounts([]int{accountInput.Account.Id})
	require.NoError(t, err)
	require.Equal(t, "sub2api-data", exported.Type)
	require.Equal(t, 1, exported.Version)
	require.Len(t, exported.Proxies, 1)
	require.Len(t, exported.Accounts, 1)
	require.NotEmpty(t, exported.Proxies[0].ProxyKey)
	require.NotNil(t, exported.Accounts[0].ProxyKey)
	require.Equal(t, exported.Proxies[0].ProxyKey, *exported.Accounts[0].ProxyKey)
	require.Equal(t, "auto", exported.Accounts[0].Extra.(map[string]any)["responses_mode"])

	raw, err := common.Marshal(exported)
	require.NoError(t, err)
	var decoded UpstreamAccountExport
	require.NoError(t, common.Unmarshal(raw, &decoded))
	require.NotNil(t, decoded.Proxies)
	require.NotNil(t, decoded.Accounts)

	result, err := ImportUpstreamData(decoded)
	require.NoError(t, err)
	require.Equal(t, 1, result.ProxyReused)
	require.Equal(t, 0, result.ProxyFailed)
	require.Equal(t, 1, result.AccountCreated)
	require.Equal(t, 0, result.AccountFailed)

	var imported model.UpstreamAccount
	require.NoError(t, model.DB.Where("name = ? AND id <> ?", "roundtrip-account", accountInput.Account.Id).First(&imported).Error)
	require.Equal(t, proxyInput.Proxy.Id, *imported.ProxyId)
	require.Zero(t, imported.CredentialKeyVersion)
	require.Empty(t, imported.CredentialNonce)
	credentials, err := DecryptUpstreamAccountCredentials(&imported)
	require.NoError(t, err)
	require.Equal(t, "roundtrip-secret", credentials["api_key"])
}

func TestImportLegacySub2APIOAuthAccountWithoutFrontendNormalization(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	t.Setenv(upstreamCredentialKeysEnv, "")
	t.Setenv(upstreamCredentialActiveVersionEnv, "")

	payload := UpstreamAccountExport{
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Proxies:    []UpstreamProxyExportItem{},
		Accounts: []UpstreamAccountExportItem{
			{
				Name: "legacy-sub2-oauth", Platform: constant.UpstreamPlatformOpenAI,
				Type: constant.UpstreamAccountTypeOAuth,
				Credentials: map[string]any{
					"access_token": "legacy-access-token", "refresh_token": "legacy-refresh-token",
					"chatgpt_account_id": "legacy-account-id",
				},
				Extra: map[string]any{"privacy_mode": "training_off"}, Concurrency: 1, Priority: 50,
			},
		},
	}

	result, err := ImportUpstreamData(payload)
	require.NoError(t, err)
	require.Equal(t, 1, result.AccountCreated)
	require.Zero(t, result.AccountFailed)

	var account model.UpstreamAccount
	require.NoError(t, model.DB.Where("name = ?", "legacy-sub2-oauth").First(&account).Error)
	require.Zero(t, account.CredentialKeyVersion)
	require.JSONEq(t, `{"privacy_mode":"training_off"}`, account.Extra)
	credentials, err := DecryptUpstreamAccountCredentials(&account)
	require.NoError(t, err)
	require.Equal(t, "legacy-account-id", credentials["account_id"])
	require.Equal(t, "legacy-account-id", credentials["chatgpt_account_id"])
}

func TestCRSSourceIDIsStableAndPositive(t *testing.T) {
	first := crsSourceID("crs-account-42")
	require.Positive(t, first)
	require.Equal(t, first, crsSourceID("crs-account-42"))
	require.NotEqual(t, first, crsSourceID("crs-account-43"))
}
