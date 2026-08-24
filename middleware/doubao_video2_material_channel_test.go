package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestResolveDoubaoVideo2MaterialChannelKeepsRequestBodyReusable(t *testing.T) {
	previous := model.DB
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/doubao-material-route.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.DoubaoVideo2UserAsset{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		_ = sqlDB.Close()
	})
	require.NoError(t, db.Create(&model.DoubaoVideo2UserAsset{
		UserID: 9, AssetGroupID: 1, ChannelID: 620, ProviderAssetID: "approved-asset",
		Name: "approved", AssetType: "Image", Status: model.DoubaoVideo2UserMaterialStatusActive,
	}).Error)

	body := []byte(`{"model":"doubao-seedance-2-0-260128","content":[{"type":"image_url","image_url":{"url":"asset://approved-asset"}}]}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	context.Set("id", 9)
	t.Cleanup(func() { common.CleanupBodyStorage(context) })

	channelID, err := resolveDoubaoVideo2MaterialChannel(context)
	require.NoError(t, err)
	require.Equal(t, 620, channelID)

	var decoded map[string]any
	require.NoError(t, common.UnmarshalBodyReusable(context, &decoded))
	require.Equal(t, "doubao-seedance-2-0-260128", decoded["model"])
}
