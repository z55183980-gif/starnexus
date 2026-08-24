package doubao

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestDoubaoVideo2UserMaterialClientPreservesCustomUpstreamProtocol(t *testing.T) {
	actions := make([]string, 0, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/support/v1/asset", request.URL.Path)
		require.Equal(t, "upstream-api-key", request.Header.Get("ApiKey"))
		action := request.URL.Query().Get("Action")
		actions = append(actions, action)
		w.Header().Set("Content-Type", "application/json")
		switch action {
		case "CreateAssetGroup":
			_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"group-request"},"Result":{"Id":123,"Status":"Active"}}`))
		case "CreateAsset":
			_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"asset-request"},"Result":{"Id":"asset-456"}}`))
		case "GetAsset":
			_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"get-request"},"Result":{"Status":"Approved"}}`))
		default:
			http.Error(w, "unexpected action", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	channel := &model.Channel{
		Id: 62, Type: constant.ChannelTypeDoubaoVideo2, Status: common.ChannelStatusEnabled,
		BaseURL: common.GetPointer(server.URL), Key: "upstream-api-key",
	}
	client, err := NewDoubaoVideo2UserMaterialClient(channel)
	require.NoError(t, err)
	group, err := client.CreateAssetGroup(t.Context(), "group", "description")
	require.NoError(t, err)
	require.Equal(t, "123", group.ID)
	asset, err := client.CreateAsset(t.Context(), group.ID, "https://example.com/image.png", "image", "Image")
	require.NoError(t, err)
	require.Equal(t, "asset-456", asset.ID)
	require.Equal(t, model.DoubaoVideo2UserMaterialStatusProcessing, asset.Status)
	status, err := client.GetAsset(t.Context(), asset.ID)
	require.NoError(t, err)
	require.Equal(t, model.DoubaoVideo2UserMaterialStatusActive, status.Status)
	require.Equal(t, []string{"CreateAssetGroup", "CreateAsset", "GetAsset"}, actions)
}
