package constant

const (
	ChannelCredentialSourceKey         = "channel_key"
	ChannelCredentialSourceMultiKey    = "channel_multi_key"
	ChannelCredentialSourceAccountPool = "local_account_pool"
)

const (
	UpstreamPlatformOpenAI = "openai"
)

const (
	UpstreamAccountTypeOAuth  = "oauth"
	UpstreamAccountTypeAPIKey = "apikey"
)

const (
	UpstreamStatusActive   = "active"
	UpstreamStatusInactive = "inactive"
	UpstreamStatusError    = "error"
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
