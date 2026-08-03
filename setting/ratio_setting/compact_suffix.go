package ratio_setting

import "strings"

const CompactModelSuffix = "-openai-compact"
const CompactWildcardModelKey = "*" + CompactModelSuffix

func WithCompactModelSuffix(modelName string) string {
	modelName = strings.TrimSpace(modelName)
	if IsCompactVirtualModel(modelName) {
		return modelName
	}
	return modelName + CompactModelSuffix
}

// IsCompactVirtualModel reports whether modelName is the internal model key
// used to select a channel for the Responses compaction endpoint. The suffix
// is not part of the upstream model name and must not be accepted by regular
// generation endpoints.
func IsCompactVirtualModel(modelName string) bool {
	return strings.HasSuffix(strings.TrimSpace(modelName), CompactModelSuffix)
}

// BaseModelFromCompactVirtualModel removes the internal compaction suffix.
// Non-compaction model names are returned unchanged.
func BaseModelFromCompactVirtualModel(modelName string) string {
	modelName = strings.TrimSpace(modelName)
	return strings.TrimSuffix(modelName, CompactModelSuffix)
}
