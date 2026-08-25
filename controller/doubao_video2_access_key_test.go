package controller

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAuthenticateDoubaoVideo2OpenAPIAcceptsOfficialHMACShape(t *testing.T) {
	previous := model.DB
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/doubao-openapi-auth.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.DoubaoVideo2AccessKey{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		_ = sqlDB.Close()
	})

	aesKey := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	t.Setenv("UPSTREAM_ACCOUNT_CREDENTIAL_KEYS", `{"1":"`+aesKey+`"}`)
	t.Setenv("UPSTREAM_ACCOUNT_ACTIVE_KEY_VERSION", "1")
	created, err := service.CreateDoubaoVideo2AccessKey(9, "sdk")
	require.NoError(t, err)

	body := []byte(`{"GroupId":"group-1","URL":"https://example.com/a.png","Name":"a","AssetType":"Image","ProjectName":"default"}`)
	request := httptest.NewRequest(http.MethodPost, "https://gateway.example.com/?Action=CreateAsset&Version=2024-01-01", bytes.NewReader(body))
	signDoubaoVideo2OpenAPIRequest(request, body, created.Key.AccessKeyID, created.SecretAccessKey)

	authenticated, err := authenticateDoubaoVideo2OpenAPI(request, body)
	require.NoError(t, err)
	require.Equal(t, created.Key.ID, authenticated.ID)
	var parsed createDoubaoVideo2UserAssetFromURLRequest
	require.NoError(t, common.Unmarshal(body, &parsed))
	require.Equal(t, "default", parsed.ProjectName)
	_, err = authenticateDoubaoVideo2OpenAPI(request, []byte(strings.ReplaceAll(string(body), "a.png", "b.png")))
	require.ErrorContains(t, err, "does not match")

	// A client may sign the official root endpoint and send the historical
	// /api alias (or vice versa). Both routes are the same operation, so the
	// verifier accepts the other fixed path as a compatibility fallback.
	aliasRequest := httptest.NewRequest(http.MethodPost, "https://gateway.example.com/api/doubao-video/openapi?Action=CreateAsset&Version=2024-01-01", bytes.NewReader(body))
	aliasRequest.Header = request.Header.Clone()
	authenticated, err = authenticateDoubaoVideo2OpenAPI(aliasRequest, body)
	require.NoError(t, err)
	require.Equal(t, created.Key.ID, authenticated.ID)

	aliasSignedRequest := httptest.NewRequest(http.MethodPost, "https://gateway.example.com/api/doubao-video/openapi?Action=CreateAsset&Version=2024-01-01", bytes.NewReader(body))
	signDoubaoVideo2OpenAPIRequest(aliasSignedRequest, body, created.Key.AccessKeyID, created.SecretAccessKey)
	rootWithAliasSignature := httptest.NewRequest(http.MethodPost, "https://gateway.example.com/?Action=CreateAsset&Version=2024-01-01", bytes.NewReader(body))
	rootWithAliasSignature.Header = aliasSignedRequest.Header.Clone()
	authenticated, err = authenticateDoubaoVideo2OpenAPI(rootWithAliasSignature, body)
	require.NoError(t, err)
	require.Equal(t, created.Key.ID, authenticated.ID)

	unknownPathRequest := httptest.NewRequest(http.MethodPost, "https://gateway.example.com/unexpected?Action=CreateAsset&Version=2024-01-01", bytes.NewReader(body))
	unknownPathRequest.Header = request.Header.Clone()
	_, err = authenticateDoubaoVideo2OpenAPI(unknownPathRequest, body)
	require.ErrorContains(t, err, "request signature is invalid")
}

func signDoubaoVideo2OpenAPIRequest(request *http.Request, body []byte, accessKeyID, secretAccessKey string) {
	xDate := time.Now().UTC().Format("20060102T150405Z")
	date := xDate[:8]
	payloadHash := sha256.Sum256(body)
	payloadHashHex := hex.EncodeToString(payloadHash[:])
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Date", xDate)
	request.Header.Set("X-Content-Sha256", payloadHashHex)
	signedHeaders := "content-type;host;x-content-sha256;x-date"
	canonicalURI := request.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalHeaders := "content-type:application/json\n" +
		"host:" + request.Host + "\n" +
		"x-content-sha256:" + payloadHashHex + "\n" +
		"x-date:" + xDate + "\n"
	canonicalRequest := request.Method + "\n" + canonicalURI + "\n" + request.URL.Query().Encode() + "\n" +
		canonicalHeaders + "\n" + signedHeaders + "\n" + payloadHashHex
	canonicalHash := sha256.Sum256([]byte(canonicalRequest))
	scope := date + "/cn-beijing/ark/request"
	stringToSign := "HMAC-SHA256\n" + xDate + "\n" + scope + "\n" + hex.EncodeToString(canonicalHash[:])
	dateKey := doubaoVideo2OpenAPIHMAC([]byte(secretAccessKey), date)
	regionKey := doubaoVideo2OpenAPIHMAC(dateKey, "cn-beijing")
	serviceKey := doubaoVideo2OpenAPIHMAC(regionKey, "ark")
	signingKey := doubaoVideo2OpenAPIHMAC(serviceKey, "request")
	signature := hex.EncodeToString(doubaoVideo2OpenAPIHMAC(signingKey, stringToSign))
	request.Header.Set("Authorization", "HMAC-SHA256 Credential="+accessKeyID+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func TestDoubaoVideo2MaterialPublicURLUsesStableHTTPSEndpoint(t *testing.T) {
	t.Setenv("DOUBAO_VIDEO2_MATERIAL_PUBLIC_BASE_URL", "https://gateway.example.com/base?ignored=1#fragment")
	materialURL, err := doubaoVideo2MaterialPublicURL("stable-token", "image/png")
	require.NoError(t, err)
	require.Equal(t, "https://gateway.example.com/base/api/doubao-video/material-content/stable-token.png", materialURL)

	t.Setenv("DOUBAO_VIDEO2_MATERIAL_PUBLIC_BASE_URL", "http://gateway.example.com")
	_, err = doubaoVideo2MaterialPublicURL("stable-token", "image/png")
	require.ErrorContains(t, err, "public HTTPS address")
}

func TestServeDoubaoVideo2UserAssetContentRequiresOwnership(t *testing.T) {
	previous := model.DB
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/doubao-material-preview.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.DoubaoVideo2UserAsset{}, &model.DoubaoVideo2UserMedia{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		_ = sqlDB.Close()
	})
	t.Setenv("DOUBAO_VIDEO2_R2_ENDPOINT", "https://r2.example.com")
	t.Setenv("DOUBAO_VIDEO2_R2_ACCESS_KEY_ID", "access-key")
	t.Setenv("DOUBAO_VIDEO2_R2_SECRET_ACCESS_KEY", "secret-key")
	t.Setenv("DOUBAO_VIDEO2_R2_BUCKET", "materials")
	require.NoError(t, db.Create(&model.DoubaoVideo2UserMedia{
		UserID: 7, AssetID: 11, TokenHash: strings.Repeat("b", 64),
		ObjectKey: "doubao-video2-material-library/7/object.png", SizeBytes: 10, ContentType: "image/png",
	}).Error)

	serve := func(userID int) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodGet, "/api/doubao-video/materials/11/content", nil)
		context.Params = gin.Params{{Key: "id", Value: "11"}}
		context.Set("id", userID)
		ServeDoubaoVideo2UserAssetContent(context)
		return recorder
	}

	owned := serve(7)
	require.Equal(t, http.StatusTemporaryRedirect, owned.Code)
	require.Contains(t, owned.Header().Get("Location"), "X-Amz-Signature=")
	require.Equal(t, "private, no-store", owned.Header().Get("Cache-Control"))
	require.Equal(t, http.StatusNotFound, serve(8).Code)
}

func TestServeDoubaoVideo2UserAssetContentRedirectsToOwnedSourceURL(t *testing.T) {
	previous := model.DB
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/doubao-material-source-preview.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.DoubaoVideo2UserAsset{}, &model.DoubaoVideo2UserMedia{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		_ = sqlDB.Close()
	})
	asset := &model.DoubaoVideo2UserAsset{
		UserID: 7, AssetGroupID: 3, ChannelID: 5, ProviderAssetID: "video-asset",
		Name: "video", AssetType: "Video", SourceURL: "https://media.example.com/video.mp4", Status: "Active",
	}
	require.NoError(t, db.Create(asset).Error)

	serve := func(userID int) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodGet, "/api/doubao-video/materials/1/content", nil)
		context.Params = gin.Params{{Key: "id", Value: strconv.FormatInt(asset.ID, 10)}}
		context.Set("id", userID)
		ServeDoubaoVideo2UserAssetContent(context)
		return recorder
	}

	owned := serve(7)
	require.Equal(t, http.StatusTemporaryRedirect, owned.Code)
	require.Equal(t, asset.SourceURL, owned.Header().Get("Location"))
	require.Equal(t, "private, no-store", owned.Header().Get("Cache-Control"))
	require.Equal(t, http.StatusNotFound, serve(8).Code)
}

func TestServeDoubaoVideo2MaterialContentStreamsObjectWithoutRedirect(t *testing.T) {
	payload := []byte("persistent-material")
	r2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(payload)
	}))
	defer r2.Close()
	t.Setenv("DOUBAO_VIDEO2_R2_ENDPOINT", r2.URL)
	t.Setenv("DOUBAO_VIDEO2_R2_ACCESS_KEY_ID", "access-key")
	t.Setenv("DOUBAO_VIDEO2_R2_SECRET_ACCESS_KEY", "secret-key")
	t.Setenv("DOUBAO_VIDEO2_R2_BUCKET", "material-bucket")

	previous := model.DB
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/doubao-material-content.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.DoubaoVideo2UserMedia{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		_ = sqlDB.Close()
	})
	token := strings.Repeat("stable-token-", 4)
	require.NoError(t, db.Create(&model.DoubaoVideo2UserMedia{
		UserID: 1, TokenHash: doubaoVideo2MaterialTokenHash(token),
		ObjectKey: "doubao-video2-material-library/1/object.png", SizeBytes: int64(len(payload)), ContentType: "image/png",
	}).Error)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/doubao-video/material-content/:token", ServeDoubaoVideo2MaterialContent)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/doubao-video/material-content/"+token+".png", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "image/png", recorder.Header().Get("Content-Type"))
	require.Equal(t, payload, recorder.Body.Bytes())
}

func TestDeleteDoubaoVideo2UserAssetDeletesR2BeforeReleasingStorage(t *testing.T) {
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "DeleteAsset", request.URL.Query().Get("Action"))
		require.Equal(t, "upstream-api-key", request.Header.Get("ApiKey"))
		upstreamCalls++
		w.Header().Set("Content-Type", "application/json")
		if upstreamCalls == 1 {
			_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"delete-request"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"delete-retry","Error":{"Code":"ResourceNotFound","Message":"asset does not exist"}}}`))
	}))
	defer upstream.Close()

	r2Calls := 0
	r2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodDelete, request.Method)
		require.Contains(t, request.URL.EscapedPath(), "doubao-video2-material-library")
		r2Calls++
		if r2Calls == 1 {
			http.Error(w, "temporary failure", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer r2.Close()
	t.Setenv("DOUBAO_VIDEO2_R2_ENDPOINT", r2.URL)
	t.Setenv("DOUBAO_VIDEO2_R2_ACCESS_KEY_ID", "access-key")
	t.Setenv("DOUBAO_VIDEO2_R2_SECRET_ACCESS_KEY", "secret-key")
	t.Setenv("DOUBAO_VIDEO2_R2_BUCKET", "materials")

	previous := model.DB
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/doubao-material-delete.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Channel{},
		&model.DoubaoVideo2UserAssetGroup{},
		&model.DoubaoVideo2UserAsset{},
		&model.DoubaoVideo2UserMedia{},
	))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		_ = sqlDB.Close()
	})

	user := &model.User{Username: "material-delete-user", Password: "not-a-real-password", DoubaoVideo2MaterialBytes: 10}
	require.NoError(t, db.Create(user).Error)
	channel := &model.Channel{
		Type: constant.ChannelTypeDoubaoVideo2, Status: common.ChannelStatusEnabled,
		Name: "doubao-material-delete", Key: "upstream-api-key", BaseURL: common.GetPointer(upstream.URL),
	}
	require.NoError(t, db.Create(channel).Error)
	group := &model.DoubaoVideo2UserAssetGroup{
		UserID: user.Id, ChannelID: channel.Id, ProviderGroupID: "group-delete", Name: "group",
		GroupType: "AIGC", Status: model.DoubaoVideo2UserMaterialStatusActive, AssetCount: 1,
	}
	require.NoError(t, db.Create(group).Error)
	asset := &model.DoubaoVideo2UserAsset{
		UserID: user.Id, AssetGroupID: group.ID, ChannelID: channel.Id,
		ProviderAssetID: "asset-delete", Name: "asset", AssetType: "Image", Status: model.DoubaoVideo2UserMaterialStatusActive,
	}
	require.NoError(t, db.Create(asset).Error)
	media := &model.DoubaoVideo2UserMedia{
		UserID: user.Id, AssetID: asset.ID, TokenHash: strings.Repeat("d", 64),
		ObjectKey: "doubao-video2-material-library/1/object.png", SizeBytes: 10, ContentType: "image/png",
	}
	require.NoError(t, db.Create(media).Error)

	err = deleteDoubaoVideo2UserAsset(t.Context(), user.Id, asset)
	require.ErrorContains(t, err, "delete R2 object returned HTTP 503")
	_, err = model.GetDoubaoVideo2UserAsset(asset.ID, user.Id)
	require.NoError(t, err, "database record must remain while R2 deletion is incomplete")
	usage, err := model.GetDoubaoVideo2StorageUsage(user.Id)
	require.NoError(t, err)
	require.Equal(t, int64(10), usage.UsedBytes, "storage must not be released before R2 deletion succeeds")

	require.NoError(t, deleteDoubaoVideo2UserAsset(t.Context(), user.Id, asset))
	require.Equal(t, 2, upstreamCalls)
	require.Equal(t, 2, r2Calls)
	_, err = model.GetDoubaoVideo2UserAsset(asset.ID, user.Id)
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound))
	_, err = model.GetDoubaoVideo2UserMediaByAssetID(asset.ID, user.Id)
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound))
	usage, err = model.GetDoubaoVideo2StorageUsage(user.Id)
	require.NoError(t, err)
	require.Zero(t, usage.UsedBytes)
	group, err = model.GetDoubaoVideo2UserAssetGroup(group.ID, user.Id)
	require.NoError(t, err)
	require.Zero(t, group.AssetCount)
}

func TestDeleteDoubaoVideo2UserAssetGroupDeletesEveryUpstreamAsset(t *testing.T) {
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "DeleteAsset", request.URL.Query().Get("Action"))
		require.Equal(t, "upstream-api-key", request.Header.Get("ApiKey"))
		upstreamCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"delete-group-request"}}`))
	}))
	defer upstream.Close()
	r2Calls := 0
	r2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodDelete, request.Method)
		require.Contains(t, request.URL.EscapedPath(), "doubao-video2-material-library")
		r2Calls++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer r2.Close()
	t.Setenv("DOUBAO_VIDEO2_R2_ENDPOINT", r2.URL)
	t.Setenv("DOUBAO_VIDEO2_R2_ACCESS_KEY_ID", "access-key")
	t.Setenv("DOUBAO_VIDEO2_R2_SECRET_ACCESS_KEY", "secret-key")
	t.Setenv("DOUBAO_VIDEO2_R2_BUCKET", "materials")

	previous := model.DB
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/doubao-material-group-delete.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Channel{},
		&model.DoubaoVideo2UserAssetGroup{},
		&model.DoubaoVideo2UserAsset{},
		&model.DoubaoVideo2UserMedia{},
	))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		_ = sqlDB.Close()
	})

	user := &model.User{
		Username: "material-group-delete-user", Password: "not-a-real-password",
		DoubaoVideo2MaterialBytes: 20,
	}
	require.NoError(t, db.Create(user).Error)
	channel := &model.Channel{
		Type: constant.ChannelTypeDoubaoVideo2, Status: common.ChannelStatusEnabled,
		Name: "doubao-material-group-delete", Key: "upstream-api-key", BaseURL: common.GetPointer(upstream.URL),
	}
	require.NoError(t, db.Create(channel).Error)
	group := &model.DoubaoVideo2UserAssetGroup{
		UserID: user.Id, ChannelID: channel.Id, ProviderGroupID: "group-delete", Name: "group",
		GroupType: "AIGC", Status: model.DoubaoVideo2UserMaterialStatusActive, AssetCount: 2,
	}
	require.NoError(t, db.Create(group).Error)
	for index, providerAssetID := range []string{"asset-delete-1", "asset-delete-2"} {
		asset := &model.DoubaoVideo2UserAsset{
			UserID: user.Id, AssetGroupID: group.ID, ChannelID: channel.Id,
			ProviderAssetID: providerAssetID, Name: providerAssetID,
			AssetType: "Image", Status: model.DoubaoVideo2UserMaterialStatusActive,
		}
		require.NoError(t, db.Create(asset).Error)
		require.NoError(t, db.Create(&model.DoubaoVideo2UserMedia{
			UserID: user.Id, AssetID: asset.ID, TokenHash: strings.Repeat(strconv.Itoa(index+1), 64),
			ObjectKey: "doubao-video2-material-library/group-delete/" + providerAssetID,
			SizeBytes: 10, ContentType: "image/png",
		}).Error)
	}

	deletedAssets, err := deleteDoubaoVideo2UserAssetGroup(t.Context(), user.Id, group.ID)
	require.NoError(t, err)
	require.Equal(t, 2, deletedAssets)
	require.Equal(t, 2, upstreamCalls)
	require.Equal(t, 2, r2Calls)
	_, err = model.GetDoubaoVideo2UserAssetGroup(group.ID, user.Id)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	assets, err := model.ListDoubaoVideo2UserAssetsByGroup(user.Id, group.ID)
	require.NoError(t, err)
	require.Empty(t, assets)
	usage, err := model.GetDoubaoVideo2StorageUsage(user.Id)
	require.NoError(t, err)
	require.Zero(t, usage.UsedBytes)
}
