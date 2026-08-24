package controller

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

	body := []byte(`{"GroupId":"group-1","URL":"https://example.com/a.png","Name":"a","AssetType":"Image"}`)
	request := httptest.NewRequest(http.MethodPost, "https://gateway.example.com/api/doubao-video/openapi?Action=CreateAsset&Version=2024-01-01", bytes.NewReader(body))
	xDate := time.Now().UTC().Format("20060102T150405Z")
	date := xDate[:8]
	payloadHash := sha256.Sum256(body)
	payloadHashHex := hex.EncodeToString(payloadHash[:])
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Date", xDate)
	request.Header.Set("X-Content-Sha256", payloadHashHex)
	signedHeaders := "content-type;host;x-content-sha256;x-date"
	canonicalHeaders := "content-type:application/json\n" +
		"host:" + request.Host + "\n" +
		"x-content-sha256:" + payloadHashHex + "\n" +
		"x-date:" + xDate + "\n"
	canonicalRequest := request.Method + "\n" + request.URL.EscapedPath() + "\n" + request.URL.Query().Encode() + "\n" +
		canonicalHeaders + "\n" + signedHeaders + "\n" + payloadHashHex
	canonicalHash := sha256.Sum256([]byte(canonicalRequest))
	scope := date + "/cn-beijing/ark/request"
	stringToSign := "HMAC-SHA256\n" + xDate + "\n" + scope + "\n" + hex.EncodeToString(canonicalHash[:])
	dateKey := doubaoVideo2OpenAPIHMAC([]byte(created.SecretAccessKey), date)
	regionKey := doubaoVideo2OpenAPIHMAC(dateKey, "cn-beijing")
	serviceKey := doubaoVideo2OpenAPIHMAC(regionKey, "ark")
	signingKey := doubaoVideo2OpenAPIHMAC(serviceKey, "request")
	signature := hex.EncodeToString(doubaoVideo2OpenAPIHMAC(signingKey, stringToSign))
	request.Header.Set("Authorization", "HMAC-SHA256 Credential="+created.Key.AccessKeyID+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)

	authenticated, err := authenticateDoubaoVideo2OpenAPI(request, body)
	require.NoError(t, err)
	require.Equal(t, created.Key.ID, authenticated.ID)
	_, err = authenticateDoubaoVideo2OpenAPI(request, []byte(strings.ReplaceAll(string(body), "a.png", "b.png")))
	require.ErrorContains(t, err, "does not match")
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
	require.NoError(t, db.AutoMigrate(&model.DoubaoVideo2UserMedia{}))
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
