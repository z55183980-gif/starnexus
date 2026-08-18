package constant

type ContextKey string

const (
	ContextKeyTokenCountMeta  ContextKey = "token_count_meta"
	ContextKeyPromptTokens    ContextKey = "prompt_tokens"
	ContextKeyEstimatedTokens ContextKey = "estimated_tokens"

	ContextKeyOriginalModel    ContextKey = "original_model"
	ContextKeyRequestStartTime ContextKey = "request_start_time"

	/* token related keys */
	ContextKeyTokenUnlimited         ContextKey = "token_unlimited_quota"
	ContextKeyTokenKey               ContextKey = "token_key"
	ContextKeyTokenId                ContextKey = "token_id"
	ContextKeyTokenGroup             ContextKey = "token_group"
	ContextKeyTokenSpecificChannelId ContextKey = "specific_channel_id"
	ContextKeyTokenModelLimitEnabled ContextKey = "token_model_limit_enabled"
	ContextKeyTokenModelLimit        ContextKey = "token_model_limit"
	ContextKeyTokenCrossGroupRetry   ContextKey = "token_cross_group_retry"

	/* channel related keys */
	ContextKeyChannelId                ContextKey = "channel_id"
	ContextKeyChannelName              ContextKey = "channel_name"
	ContextKeyChannelCreateTime        ContextKey = "channel_create_time"
	ContextKeyChannelBaseUrl           ContextKey = "base_url"
	ContextKeyChannelType              ContextKey = "channel_type"
	ContextKeyChannelSetting           ContextKey = "channel_setting"
	ContextKeyChannelOtherSetting      ContextKey = "channel_other_setting"
	ContextKeyChannelParamOverride     ContextKey = "param_override"
	ContextKeyChannelHeaderOverride    ContextKey = "header_override"
	ContextKeyChannelOrganization      ContextKey = "channel_organization"
	ContextKeyChannelAutoBan           ContextKey = "auto_ban"
	ContextKeyChannelModelMapping      ContextKey = "model_mapping"
	ContextKeyChannelStatusCodeMapping ContextKey = "status_code_mapping"
	ContextKeyChannelIsMultiKey        ContextKey = "channel_is_multi_key"
	ContextKeyChannelMultiKeyIndex     ContextKey = "channel_multi_key_index"
	ContextKeyChannelKey               ContextKey = "channel_key"
	ContextKeyChannelCredentialSource  ContextKey = "channel_credential_source"

	/* local upstream account related keys */
	ContextKeyUpstreamAccountPoolId                ContextKey = "upstream_account_pool_id"
	ContextKeyUpstreamAccountId                    ContextKey = "upstream_account_id"
	ContextKeyUpstreamAccountName                  ContextKey = "upstream_account_name"
	ContextKeyUpstreamAccountPlatform              ContextKey = "upstream_account_platform"
	ContextKeyUpstreamAccountType                  ContextKey = "upstream_account_type"
	ContextKeyUpstreamAccountPlanType              ContextKey = "upstream_account_plan_type"
	ContextKeyUpstreamProxyId                      ContextKey = "upstream_proxy_id"
	ContextKeyUpstreamAccountLeaseId               ContextKey = "upstream_account_lease_id"
	ContextKeyUpstreamAccountSelection             ContextKey = "upstream_account_selection"
	ContextKeyUpstreamAccountExcluded              ContextKey = "upstream_account_excluded_ids"
	ContextKeyUpstreamAccountPreferredId           ContextKey = "upstream_account_preferred_id"
	ContextKeyUpstreamAccountPreferredRequired     ContextKey = "upstream_account_preferred_required"
	ContextKeyUpstreamAccountRequiredWSMode        ContextKey = "upstream_account_required_ws_mode"
	ContextKeyUpstreamAccountMappedModel           ContextKey = "upstream_account_mapped_model"
	ContextKeyUpstreamAccountRateMultiplier        ContextKey = "upstream_account_rate_multiplier"
	ContextKeyUpstreamAnthropicAuthScheme          ContextKey = "upstream_anthropic_auth_scheme"
	ContextKeyUpstreamOpenAIResponsesMode          ContextKey = "upstream_openai_responses_mode"
	ContextKeyUpstreamOpenAILongContextBilling     ContextKey = "upstream_openai_long_context_billing"
	ContextKeyUpstreamInterceptWarmup              ContextKey = "upstream_intercept_warmup"
	ContextKeyResponsesWebSocketIngress            ContextKey = "responses_websocket_ingress"
	ContextKeyResponsesWebSocketPreferredChannelId ContextKey = "responses_websocket_preferred_channel_id"
	ContextKeyCodexReasoningContentRetryArmed      ContextKey = "codex_reasoning_content_retry_armed"
	ContextKeyCodexReasoningContentRetryIndex      ContextKey = "codex_reasoning_content_retry_index"
	ContextKeyCodexReasoningContentRetryLength     ContextKey = "codex_reasoning_content_retry_length"

	ContextKeyAutoGroup           ContextKey = "auto_group"
	ContextKeyAutoGroupIndex      ContextKey = "auto_group_index"
	ContextKeyAutoGroupRetryIndex ContextKey = "auto_group_retry_index"

	/* user related keys */
	ContextKeyUserId          ContextKey = "id"
	ContextKeyUserSetting     ContextKey = "user_setting"
	ContextKeyUserQuota       ContextKey = "user_quota"
	ContextKeyUserStatus      ContextKey = "user_status"
	ContextKeyUserEmail       ContextKey = "user_email"
	ContextKeyUserGroup       ContextKey = "user_group"
	ContextKeyUsingGroup      ContextKey = "group"
	ContextKeyUserName        ContextKey = "username"
	ContextKeyUserConcurrency ContextKey = "user_concurrency"

	ContextKeyLocalCountTokens ContextKey = "local_count_tokens"

	ContextKeySystemPromptOverride ContextKey = "system_prompt_override"

	// ContextKeyFileSourcesToCleanup stores file sources that need cleanup when request ends
	ContextKeyFileSourcesToCleanup ContextKey = "file_sources_to_cleanup"
	// ContextKeyZQBAPIRetryPayload stores the sanitized upstream video request
	// needed for one automatic real-person material retry.
	ContextKeyZQBAPIRetryPayload ContextKey = "zqbapi_retry_payload"
	// ContextKeyDoubaoVideo2RetryPayload stores a type-62-only provider request
	// for one material-library recovery attempt.
	ContextKeyDoubaoVideo2RetryPayload ContextKey = "doubao_video2_retry_payload"
	// ContextKeyZQBAPIOpenAIVideoResponse stores a successful compatibility
	// response until the controller has durably inserted the local task.
	ContextKeyZQBAPIOpenAIVideoResponse ContextKey = "zqbapi_openai_video_response"
	// ContextKeyZQBAPIOpenAIVideoRequest marks only channel type 61 requests
	// that entered the OpenAI Videos compatibility path.
	ContextKeyZQBAPIOpenAIVideoRequest ContextKey = "zqbapi_openai_video_request"
	// ContextKeyOpenAIVideoResponse stores a successful provider-independent
	// compatibility response until the local task row is durable.
	ContextKeyOpenAIVideoResponse ContextKey = "openai_video_response"
	// ContextKeyOpenAIVideoRequest marks a request that entered /v1/videos.
	ContextKeyOpenAIVideoRequest ContextKey = "openai_video_request"

	// ContextKeyAdminRejectReason stores an admin-only reject/block reason extracted from upstream responses.
	// It is not returned to end users, but can be persisted into consume/error logs for debugging.
	ContextKeyAdminRejectReason ContextKey = "admin_reject_reason"

	// ContextKeyLanguage stores the user's language preference for i18n
	ContextKeyLanguage ContextKey = "language"
	ContextKeyIsStream ContextKey = "is_stream"
)
