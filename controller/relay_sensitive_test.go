package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestGlobalPromptSensitiveCheckCoversAlphaSearchFieldsOutsideAuditExtraction(t *testing.T) {
	originalWords := setting.SensitiveWords
	setting.SensitiveWords = []string{"hidden_sensitive_value"}
	t.Cleanup(func() {
		setting.SensitiveWords = originalWords
	})

	request := &dto.AlphaSearchRequest{
		RawBody: []byte(`{
			"model":"gpt-5.1",
			"commands":{"open":[{"ref_id":"hidden_sensitive_value"}]}
		}`),
	}
	require.Empty(t, service.ExtractPromptAuditUserText(request))

	words, apiErr := checkGlobalPromptSensitive(request.GetTokenCountMeta())
	require.Equal(t, []string{"hidden_sensitive_value"}, words)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeSensitiveWordsDetected, apiErr.ToOpenAIError().Code)
}
