package operation_setting

import "testing"

func TestUpdatePayMethodsByJsonString_NormalizesBoolEnabled(t *testing.T) {
	jsonString := `[
  {"name":"USDT","type":"usdt","color":"#26A17B","enabled":"true","tag":"Crypto"},
  {"name":"Stripe","type":"stripe","color":"#635BFF","enabled":true}
]`

	if err := UpdatePayMethodsByJsonString(jsonString); err != nil {
		t.Fatalf("UpdatePayMethodsByJsonString() error = %v", err)
	}

	if len(PayMethods) != 2 {
		t.Fatalf("len(PayMethods) = %d, want 2", len(PayMethods))
	}
	if PayMethods[0]["enabled"] != "true" {
		t.Fatalf("PayMethods[0][enabled] = %q, want %q", PayMethods[0]["enabled"], "true")
	}
	if PayMethods[0]["tag"] != "Crypto" {
		t.Fatalf("PayMethods[0][tag] = %q, want Crypto", PayMethods[0]["tag"])
	}
	if PayMethods[1]["type"] != "stripe" {
		t.Fatalf("PayMethods[1][type] = %q, want stripe", PayMethods[1]["type"])
	}
}
