package constant

const (
	ChannelCredentialSourceKey         = "channel_key"
	ChannelCredentialSourceMultiKey    = "channel_multi_key"
	ChannelCredentialSourceAccountPool = "local_account_pool"
)

const (
	UpstreamPlatformOpenAI    = "openai"
	UpstreamPlatformAnthropic = "anthropic"
)

const (
	UpstreamAccountTypeOAuth          = "oauth"
	UpstreamAccountTypeSetupToken     = "setup_token"
	UpstreamAccountTypeAPIKey         = "apikey"
	UpstreamAccountTypeBedrock        = "bedrock"
	UpstreamAccountTypeServiceAccount = "service_account"
)

const (
	UpstreamOAuthRefreshOwnerExternal  = "external"
	UpstreamOAuthRefreshOwnerStarNexus = "starnexus"
)

const (
	UpstreamStatusActive   = "active"
	UpstreamStatusInactive = "inactive"
	UpstreamStatusError    = "error"
	UpstreamStatusExpired  = "expired"
)

const (
	UpstreamAccountReasonAuthenticationFailed  = "authentication_failed"
	UpstreamAccountReasonOAuthRefreshPending   = "oauth_refresh_pending"
	UpstreamAccountReasonOAuthRefreshFailed    = "oauth_refresh_failed"
	UpstreamAccountReasonOAuthRefreshPermanent = "oauth_refresh_permanent"
)

func UpstreamAccountOAuthRefreshBlocksScheduling(reason string) bool {
	switch reason {
	case UpstreamAccountReasonOAuthRefreshPending,
		UpstreamAccountReasonOAuthRefreshFailed,
		UpstreamAccountReasonOAuthRefreshPermanent:
		return true
	default:
		return false
	}
}

const (
	UpstreamProxyProtocolHTTP    = "http"
	UpstreamProxyProtocolHTTPS   = "https"
	UpstreamProxyProtocolSOCKS5  = "socks5"
	UpstreamProxyProtocolSOCKS5H = "socks5h"
)

const (
	UpstreamProxyFallbackNone   = "none"
	UpstreamProxyFallbackProxy  = "proxy"
	UpstreamProxyFallbackDirect = "direct"
)
