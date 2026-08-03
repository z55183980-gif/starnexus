package codex

import (
	"encoding/json"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// RepairAccountPassthroughResponsesBody applies the same narrow Codex input
// repair to account-owned passthrough without decoding and rebuilding the full
// request. Unknown top-level fields and their explicit zero values are kept.
func RepairAccountPassthroughResponsesBody(c *gin.Context, body []byte) ([]byte, error) {
	input := gjson.GetBytes(body, "input")
	if !input.Exists() {
		return body, nil
	}
	repairedInput, err := applyCodexInputRepair(c, json.RawMessage(input.Raw))
	if err != nil {
		return nil, err
	}
	if string(repairedInput) == input.Raw {
		return body, nil
	}
	repairedBody, err := sjson.SetRawBytes(body, "input", repairedInput)
	if err != nil {
		return nil, fmt.Errorf("repair Codex account passthrough input: %w", err)
	}
	return repairedBody, nil
}
