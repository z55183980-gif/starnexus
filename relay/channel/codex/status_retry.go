package codex

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const codexRemovedStatusCandidatesKey = "codex_removed_status_candidates"
const codexMissingStatusRetryIndexKey = "codex_missing_status_retry_index"
const codexOutboundInputTypesKey = "codex_outbound_input_types"

type codexRemovedStatusCandidate struct {
	itemType     string
	status       json.RawMessage
	outboundHash [32]byte
}

// captureCodexInputStatusCandidates remembers only top-level status values
// that the Codex scrubber will remove. It runs after length-changing replay
// repair, so candidate indexes match the input passed to the normalizer.
func captureCodexInputStatusCandidates(c *gin.Context, raw json.RawMessage) {
	if c == nil {
		return
	}
	candidates := make(map[int]codexRemovedStatusCandidate)
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		c.Set(codexRemovedStatusCandidatesKey, candidates)
		return
	}
	var items []json.RawMessage
	if trimmed[0] == '[' {
		if common.Unmarshal(trimmed, &items) != nil {
			return
		}
	} else if trimmed[0] == '{' {
		items = []json.RawMessage{trimmed}
	} else {
		c.Set(codexRemovedStatusCandidatesKey, candidates)
		return
	}
	for index, item := range items {
		var envelope map[string]json.RawMessage
		if common.Unmarshal(item, &envelope) != nil {
			continue
		}
		itemType := codexJSONRawString(envelope["type"])
		if itemType == "reasoning" {
			continue
		}
		status, exists := envelope["status"]
		if !exists || len(status) > 64 {
			continue
		}
		var value string
		if common.Unmarshal(status, &value) != nil || value == "" {
			continue
		}
		candidates[index] = codexRemovedStatusCandidate{itemType: itemType, status: append(json.RawMessage(nil), status...)}
	}
	if c.GetInt(codexMissingStatusRetryIndexKey) > 0 {
		return
	}
	c.Set(codexRemovedStatusCandidatesKey, candidates)
}

// finalizeCodexMissingStatusRetry binds each candidate to the exact outbound
// item. On the one permitted retry, only that same item gets its original
// status restored; index shifts or overrides fail closed.
func finalizeCodexMissingStatusRetry(c *gin.Context, body []byte) ([]byte, error) {
	if c == nil {
		return body, nil
	}
	value, ok := c.Get(codexRemovedStatusCandidatesKey)
	if !ok {
		return body, nil
	}
	candidates, ok := value.(map[int]codexRemovedStatusCandidate)
	if !ok || len(candidates) == 0 {
		return body, nil
	}
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return body, nil
	}
	items := input.Array()
	itemTypes := make([]string, len(items))
	for index, item := range items {
		itemTypes[index] = item.Get("type").String()
	}
	c.Set(codexOutboundInputTypesKey, itemTypes)
	if retryIndex := c.GetInt(codexMissingStatusRetryIndexKey); retryIndex > 0 {
		index := retryIndex - 1
		candidate, exists := candidates[index]
		if !exists || index >= len(items) || items[index].Get("type").String() != candidate.itemType || items[index].Get("status").Exists() || sha256.Sum256([]byte(items[index].Raw)) != candidate.outboundHash {
			return body, nil
		}
		patched, err := sjson.SetRawBytes(body, fmt.Sprintf("input.%d.status", index), candidate.status)
		if err != nil {
			return nil, err
		}
		return patched, nil
	}
	for index, candidate := range candidates {
		if index < len(items) && items[index].Get("type").String() == candidate.itemType && !items[index].Get("status").Exists() {
			candidate.outboundHash = sha256.Sum256([]byte(items[index].Raw))
			candidates[index] = candidate
		} else {
			delete(candidates, index)
		}
	}
	return body, nil
}

// DescribeCodexMissingStatusItem returns only the item type and whether this
// request had an explicit status removed by the gateway. No payload is logged.
func DescribeCodexMissingStatusItem(c *gin.Context, index int) (string, bool, bool) {
	if c == nil || index < 0 {
		return "", false, false
	}
	value, exists := c.Get(codexOutboundInputTypesKey)
	if !exists {
		return "", false, false
	}
	itemTypes, ok := value.([]string)
	if !ok || index >= len(itemTypes) {
		return "", false, false
	}
	candidateValue, exists := c.Get(codexRemovedStatusCandidatesKey)
	if !exists {
		return itemTypes[index], false, true
	}
	candidates, ok := candidateValue.(map[int]codexRemovedStatusCandidate)
	if !ok {
		return itemTypes[index], false, true
	}
	_, removed := candidates[index]
	return itemTypes[index], removed, true
}

// ArmCodexMissingStatusRetry succeeds only when the gateway deleted status
// from the exact item named by the upstream error.
func ArmCodexMissingStatusRetry(c *gin.Context, index int) (string, bool) {
	if c == nil || index < 0 || c.GetInt(codexMissingStatusRetryIndexKey) > 0 {
		return "", false
	}
	value, exists := c.Get(codexRemovedStatusCandidatesKey)
	if !exists {
		return "", false
	}
	candidates, ok := value.(map[int]codexRemovedStatusCandidate)
	if !ok {
		return "", false
	}
	candidate, exists := candidates[index]
	if !exists || candidate.outboundHash == ([32]byte{}) {
		return "", false
	}
	c.Set(codexMissingStatusRetryIndexKey, index+1)
	return candidate.itemType, true
}
