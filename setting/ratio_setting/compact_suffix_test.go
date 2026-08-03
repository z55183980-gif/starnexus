package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompactVirtualModelHelpers(t *testing.T) {
	require.True(t, IsCompactVirtualModel("gpt-5.6-luna-openai-compact"))
	require.True(t, IsCompactVirtualModel(" gpt-5.6-luna-openai-compact "))
	require.False(t, IsCompactVirtualModel("gpt-5.6-luna"))

	require.Equal(t, "gpt-5.6-luna", BaseModelFromCompactVirtualModel("gpt-5.6-luna-openai-compact"))
	require.Equal(t, "gpt-5.6-luna", BaseModelFromCompactVirtualModel("gpt-5.6-luna"))
	require.Equal(t, "gpt-5.6-luna-openai-compact", WithCompactModelSuffix("gpt-5.6-luna-openai-compact"))
}
