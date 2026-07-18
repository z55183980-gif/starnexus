package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupRegisterTestRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserAffiliate{}))

	originalDB := model.DB
	originalRegisterEnabled := common.RegisterEnabled
	originalPasswordRegisterEnabled := common.PasswordRegisterEnabled
	originalEmailVerificationEnabled := common.EmailVerificationEnabled
	originalAffiliateEnabled := common.AffiliateEnabled
	originalQuotaForNewUser := common.QuotaForNewUser
	originalGenerateDefaultToken := constant.GenerateDefaultToken
	model.DB = db
	common.RegisterEnabled = true
	common.PasswordRegisterEnabled = true
	common.EmailVerificationEnabled = false
	common.AffiliateEnabled = true
	common.QuotaForNewUser = 0
	constant.GenerateDefaultToken = false
	t.Cleanup(func() {
		model.DB = originalDB
		common.RegisterEnabled = originalRegisterEnabled
		common.PasswordRegisterEnabled = originalPasswordRegisterEnabled
		common.EmailVerificationEnabled = originalEmailVerificationEnabled
		common.AffiliateEnabled = originalAffiliateEnabled
		common.QuotaForNewUser = originalQuotaForNewUser
		constant.GenerateDefaultToken = originalGenerateDefaultToken
	})

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("register-test-secret"))))
	router.POST("/register", Register)
	return router, db
}

func postRegister(t *testing.T, router *gin.Engine, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := common.Marshal(payload)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestRegisterBindsValidAffiliateCode(t *testing.T) {
	router, db := setupRegisterTestRouter(t)
	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "agent",
		Password: "unused-password",
		Role:     common.RoleAgentUser,
		AffCode:  "AGENT1",
	}).Error)
	require.NoError(t, db.Create(&model.UserAffiliate{UserId: 1, AffCode: "AGENT1"}).Error)

	recorder := postRegister(t, router, map[string]any{
		"username": "invitee",
		"password": "password123",
		"aff_code": "agent1",
	})

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)

	var invitee model.User
	require.NoError(t, db.Where("username = ?", "invitee").First(&invitee).Error)
	require.Equal(t, 1, invitee.InviterId)

	var affiliate model.UserAffiliate
	require.NoError(t, db.Where("user_id = ?", invitee.Id).First(&affiliate).Error)
	require.NotNil(t, affiliate.InviterId)
	require.Equal(t, 1, *affiliate.InviterId)
}

func TestRegisterRejectsInvalidAffiliateCode(t *testing.T) {
	router, db := setupRegisterTestRouter(t)
	recorder := postRegister(t, router, map[string]any{
		"username": "invitee",
		"password": "password123",
		"aff_code": "MISSING",
	})

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)

	var count int64
	require.NoError(t, db.Model(&model.User{}).Where("username = ?", "invitee").Count(&count).Error)
	require.Zero(t, count)
}
