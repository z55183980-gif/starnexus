/**
此文件为旧版支付设置文件，如需增加新的参数、变量等，请在 payment_setting.go 中添加
This file is the old version of the payment settings file. If you need to add new parameters, variables, etc., please add them in payment_setting.go
*/

package operation_setting

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

var PayAddress = ""
var CustomCallbackAddress = ""
var EpayId = ""
var EpayKey = ""
var Price = 7.3
var MinTopUp = 1
var USDExchangeRate = 7.3

var PayMethods = []map[string]string{
	{
		"name":    "USDT",
		"color":   "#26A17B",
		"type":    "usdt",
		"enabled": "true",
		"tag":     "Crypto",
	},
	{
		"name":    "Stripe",
		"color":   "#635BFF",
		"type":    "stripe",
		"enabled": "true",
	},
}

func UpdatePayMethodsByJsonString(jsonString string) error {
	var raw []map[string]any
	if err := common.Unmarshal([]byte(jsonString), &raw); err != nil {
		return err
	}

	PayMethods = make([]map[string]string, 0, len(raw))
	for _, item := range raw {
		row := make(map[string]string, len(item))
		for key, value := range item {
			row[key] = payMethodFieldToString(value)
		}
		PayMethods = append(PayMethods, row)
	}
	return nil
}

func payMethodFieldToString(value any) string {
	if value == nil {
		return ""
	}
	switch val := value.(type) {
	case string:
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	default:
		return fmt.Sprint(val)
	}
}

func PayMethods2JsonString() string {
	jsonBytes, err := common.Marshal(PayMethods)
	if err != nil {
		return "[]"
	}
	return string(jsonBytes)
}

func ContainsPayMethod(method string) bool {
	for _, payMethod := range PayMethods {
		if payMethod["type"] == method {
			return true
		}
	}
	return false
}

// IsLegacyEpayOfferedInPayMethods reports whether PayMethods still exposes a legacy
// Epay channel such as alipay. Stored credentials alone do not count as active Epay.
func IsLegacyEpayOfferedInPayMethods() bool {
	for _, method := range PayMethods {
		if !isLegacyEpayPayMethodType(method["type"]) {
			continue
		}
		if method["enabled"] == "false" {
			continue
		}
		return true
	}
	return false
}

func isLegacyEpayPayMethodType(methodType string) bool {
	methodType = strings.TrimSpace(methodType)
	if methodType == "" {
		return false
	}
	switch methodType {
	case "stripe", "usdt", "waffo", "waffo_pancake", "creem":
		return false
	default:
		return true
	}
}
