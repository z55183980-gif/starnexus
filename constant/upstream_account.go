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
