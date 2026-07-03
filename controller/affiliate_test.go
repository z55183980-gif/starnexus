package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupAffiliateLedgerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = originalDB
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserAffiliateLedger{}))
	return db
}

func newAffiliateLedgerContext(method, target string, userID int) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, target, nil)
	ctx.Set("id", userID)
	return ctx, recorder
}

func TestAdminListAffiliateLedgersReturnsUsernames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAffiliateLedgerTestDB(t)

	require.NoError(t, db.Create(&model.User{Id: 1, Username: "agent", Role: common.RoleAgentUser, AffCode: "AGENT"}).Error)
	require.NoError(t, db.Create(&model.User{Id: 2, Username: "invitee", Role: common.RoleCommonUser, AffCode: "INVITEE"}).Error)
	sourceUserID := 2
	sourceTopUpID := 91
	require.NoError(t, db.Create(&model.UserAffiliateLedger{
		UserId:        1,
		Action:        common.AffiliateLedgerActionAccrue,
		AmountUSD:     decimal.NewFromFloat(2.5),
		SourceUserId:  &sourceUserID,
		SourceTopUpId: &sourceTopUpID,
	}).Error)

	ctx, recorder := newAffiliateLedgerContext(http.MethodGet, "/api/affiliate/admin/ledger?page=1&page_size=20&keyword=agent", 10)
	AdminListAffiliateLedgers(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Items []affiliateLedgerItem `json:"items"`
			Total int64                 `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Equal(t, int64(1), response.Data.Total)
	require.Len(t, response.Data.Items, 1)
	require.Equal(t, "agent", response.Data.Items[0].Username)
	require.NotNil(t, response.Data.Items[0].SourceUsername)
	require.Equal(t, "invitee", *response.Data.Items[0].SourceUsername)
}

func TestAgentListAffiliateLedgersIsScopedToCurrentUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAffiliateLedgerTestDB(t)

	require.NoError(t, db.Create(&model.User{Id: 1, Username: "agent-one", Role: common.RoleAgentUser, AffCode: "AGENT1"}).Error)
	require.NoError(t, db.Create(&model.User{Id: 2, Username: "agent-two", Role: common.RoleAgentUser, AffCode: "AGENT2"}).Error)
	require.NoError(t, db.Create(&model.UserAffiliateLedger{
		UserId:    1,
		Action:    common.AffiliateLedgerActionTransfer,
		AmountUSD: decimal.NewFromFloat(1),
	}).Error)
	require.NoError(t, db.Create(&model.UserAffiliateLedger{
		UserId:    2,
		Action:    common.AffiliateLedgerActionTransfer,
		AmountUSD: decimal.NewFromFloat(9),
	}).Error)

	ctx, recorder := newAffiliateLedgerContext(http.MethodGet, "/api/affiliate/agent/ledger?page=1&page_size=20&user_id=2", 1)
	AgentListAffiliateLedgers(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Items []affiliateLedgerItem `json:"items"`
			Total int64                 `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Equal(t, int64(1), response.Data.Total)
	require.Len(t, response.Data.Items, 1)
	require.Equal(t, 1, response.Data.Items[0].UserId)
	require.Equal(t, "agent-one", response.Data.Items[0].Username)
}
