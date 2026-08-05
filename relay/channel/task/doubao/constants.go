package doubao

import (
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

var ModelList = []string{
	"doubao-seedance-1-0-pro-250528",
	"doubao-seedance-1-0-lite-t2v",
	"doubao-seedance-1-0-lite-i2v",
	"doubao-seedance-1-5-pro-251215",
	"doubao-seedance-2-0-260128",
	"doubao-seedance-2-0-fast-260128",
	// Dreamina-branded aliases used by channel model lists.
	"dreamina-seedance-2-0-260128",
	"dreamina-seedance-2-0-ep",
	"dreamina-seedance-2-0-hc",
	"dreamina-seedance-2-0-fast-hc",
	"dreamina-seedance-2-0-mini-hc",
}

var ChannelName = "doubao-video"

// videoPriceKey 价格表的键：输出分辨率档（is1080p/is4k 均为 false 即 480p/720p 基准档）、输入是否含视频。
type videoPriceKey struct {
	is1080p  bool
	is4k     bool
	hasVideo bool
}

// Official Seedance 2.0 relative unit cards (vendor list prices).
// Zero-value key {480p/720p, no video} is the base administrators should
// align ModelRatio with; billing multiplies OtherRatio = actual / base.
// Absolute currency in the vendor list may be CNY — only ratios are used here.
var (
	videoPriceTablePro = map[videoPriceKey]float64{
		{hasVideo: false}:                46.0,
		{hasVideo: true}:                 28.0,
		{is1080p: true, hasVideo: false}: 51.0,
		{is1080p: true, hasVideo: true}:  31.0,
		{is4k: true, hasVideo: false}:    26.0,
		{is4k: true, hasVideo: true}:     16.0,
	}
	videoPriceTableFast = map[videoPriceKey]float64{
		{hasVideo: false}: 37.0,
		{hasVideo: true}:  22.0,
	}
)

// videoPriceTable maps model name → (resolution × hasVideo) unit prices.
// Dreamina aliases share the same cards as the matching Doubao Seedance tier:
//   pro: 260128 / ep / hc
//   fast: fast-260128 / fast-hc / mini-hc
var videoPriceTable = map[string]map[videoPriceKey]float64{
	"doubao-seedance-2-0-260128":      videoPriceTablePro,
	"doubao-seedance-2-0-fast-260128": videoPriceTableFast,
	"dreamina-seedance-2-0-260128":    videoPriceTablePro,
	"dreamina-seedance-2-0-ep":        videoPriceTablePro,
	"dreamina-seedance-2-0-hc":        videoPriceTablePro,
	"dreamina-seedance-2-0-fast-hc":   videoPriceTableFast,
	"dreamina-seedance-2-0-mini-hc":   videoPriceTableFast,
}

const contextKeySeedanceHasVideo = "doubao_seedance_has_video"

// GetVideoInputRatio 返回指定模型在给定输出分辨率/是否含视频输入下，相对基准价的计费倍率。
// 优先使用管理端 VideoTokenPrice 配置；缺档或不完整时回退硬编码价表。
func GetVideoInputRatio(modelName, resolution string, hasVideo bool) (float64, bool) {
	res := ratio_setting.NormalizeVideoResolution(resolution)
	if ratio, ok := ratio_setting.GetConfiguredVideoTokenRatio(modelName, res, hasVideo); ok {
		return ratio, true
	}
	prices, ok := videoPriceTable[modelName]
	if !ok {
		return 0, false
	}
	base := prices[videoPriceKey{}] // zero key = {480p/720p, no video}
	if base <= 0 {
		return 0, false
	}
	price, ok := prices[videoPriceKey{is1080p: res == "1080p", is4k: res == "4k", hasVideo: hasVideo}]
	if !ok {
		// Unconfigured combo (e.g. fast has no 1080p/4k) bills at base.
		return 1.0, true
	}
	return price / base, true
}
