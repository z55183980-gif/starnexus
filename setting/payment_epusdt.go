package setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/operation_setting"
)

var EpUSDTGatewayAddress = DefaultEpUSDTGatewayAddress
var EpUSDTApiToken = ""
var EpUSDTNotifyURL = ""

const DefaultEpUSDTGatewayAddress = "https://pay.xingyuapi.com"

func storedEpUSDTGatewayAddress() string {
	if gateway := normalizeGatewayURL(EpUSDTGatewayAddress); gateway != "" {
		return gateway
	}
	return normalizeGatewayURL(DefaultEpUSDTGatewayAddress)
}

func normalizeGatewayURL(url string) string {
	return strings.TrimRight(strings.TrimSpace(url), "/")
}

// EffectiveEpUSDTNotifyURL returns the webhook URL sent to BEpusdt when creating
// orders. When EpUSDTNotifyURL is set, it is used as-is; otherwise the default
// is derived from the API server address.
func EffectiveEpUSDTNotifyURL(serverAddress string) string {
	if custom := normalizeGatewayURL(EpUSDTNotifyURL); custom != "" {
		return custom
	}

	base := normalizeGatewayURL(serverAddress)
	if base == "" {
		return ""
	}
	return base + "/api/epusdt/notify"
}

// IsEpayConfigured reports whether legacy Epay credentials are still present.
func IsEpayConfigured() bool {
	return strings.TrimSpace(operation_setting.PayAddress) != "" &&
		strings.TrimSpace(operation_setting.EpayId) != "" &&
		strings.TrimSpace(operation_setting.EpayKey) != ""
}

// IsLegacyEpUSDTGatewayAddress reports whether the saved gateway address was
// mistakenly copied from the legacy Epay PayAddress setting.
func IsLegacyEpUSDTGatewayAddress(gatewayAddress, legacyPayAddress string) bool {
	gateway := normalizeGatewayURL(gatewayAddress)
	payAddress := normalizeGatewayURL(legacyPayAddress)
	return gateway != "" && payAddress != "" && gateway == payAddress
}

// IsLegacyEpUSDTGatewayConflict returns true only when legacy Epay is still offered
// and the EpUSDT gateway matches the Epay PayAddress.
func IsLegacyEpUSDTGatewayConflict(gatewayAddress, legacyPayAddress string) bool {
	if !IsEpayConfigured() || !operation_setting.IsLegacyEpayOfferedInPayMethods() {
		return false
	}
	return IsLegacyEpUSDTGatewayAddress(gatewayAddress, legacyPayAddress)
}

// EffectiveEpUSDTGatewayAddress returns the BEpusdt gateway base URL.
// When Epay is still configured, values that match PayAddress are treated as unconfigured.
func EffectiveEpUSDTGatewayAddress(legacyPayAddress string) string {
	gateway := storedEpUSDTGatewayAddress()
	if IsLegacyEpUSDTGatewayConflict(gateway, legacyPayAddress) {
		return ""
	}
	return gateway
}
