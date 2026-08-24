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
