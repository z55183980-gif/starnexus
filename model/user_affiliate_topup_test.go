package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func TestTopUpRechargeUSDUsesUserRechargeAmount(t *testing.T) {
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	t.Cleanup(func() {
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType
	})

	topUp := &TopUp{
		Amount:          50,
		Money:           7.35,
		PaymentProvider: PaymentProviderEpusdt,
	}
	got := TopUpRechargeUSD(topUp)
	want := decimal.NewFromInt(50)
	if !got.Equal(want) {
		t.Fatalf("epusdt recharge USD = %s, want %s", got, want)
	}

	epayTopUp := &TopUp{
		Amount:          100,
		Money:           730,
		PaymentProvider: PaymentProviderEpay,
	}
	got = TopUpRechargeUSD(epayTopUp)
	want = decimal.NewFromInt(100)
	if !got.Equal(want) {
		t.Fatalf("epay recharge USD = %s, want %s", got, want)
	}

	waffoTopUp := &TopUp{
		Amount:          20,
		Money:           146,
		PaymentProvider: PaymentProviderWaffo,
	}
	got = TopUpRechargeUSD(waffoTopUp)
	want = decimal.NewFromInt(20)
	if !got.Equal(want) {
		t.Fatalf("waffo recharge USD = %s, want %s", got, want)
	}
}

func TestTopUpRechargeUSDStripeUsesCreditedMoney(t *testing.T) {
	topUp := &TopUp{
		Amount:          100,
		Money:           120,
		PaymentProvider: PaymentProviderStripe,
	}
	got := TopUpRechargeUSD(topUp)
	want := decimal.NewFromInt(120)
	if !got.Equal(want) {
		t.Fatalf("stripe recharge USD = %s, want %s", got, want)
	}
}
func TestAccrueAffiliateRebateSkipsAlreadyAuditedTopUp(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	originalDB := DB
	DB = db
	t.Cleanup(func() {
		DB = originalDB
	})

	if err := db.AutoMigrate(&TopUpAffiliateAudit{}, &UserAffiliate{}, &UserAffiliateLedger{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	originalAffiliateEnabled := common.AffiliateEnabled
	common.AffiliateEnabled = true
	t.Cleanup(func() {
		common.AffiliateEnabled = originalAffiliateEnabled
	})

	topUp := &TopUp{Id: 18, UserId: 100}
	if err := db.Create(&TopUpAffiliateAudit{
		TopUpId: topUp.Id,
		Action:  common.AffiliateTopUpAuditSkipped,
		Detail:  "zero rebate",
	}).Error; err != nil {
		t.Fatalf("seed skipped audit: %v", err)
	}

	applied, err := AccrueAffiliateRebateForTopUp(topUp, decimal.NewFromInt(10))
	if err != nil {
		t.Fatalf("accrue already audited topup: %v", err)
	}
	if !applied.IsZero() {
		t.Fatalf("applied rebate = %s, want zero", applied)
	}

	var count int64
	if err := db.Model(&TopUpAffiliateAudit{}).Where("topup_id = ?", topUp.Id).Count(&count).Error; err != nil {
		t.Fatalf("count audits: %v", err)
	}
	if count != 1 {
		t.Fatalf("audit count = %d, want 1", count)
	}
}

func TestResolveAffiliateRebateRateUsesAgentDefault(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	originalDB := DB
	DB = db
	originalGlobalRate := common.AffiliateRebateRate
	originalAgentRate := common.AgentAffiliateRebateRate
	common.AffiliateRebateRate = 10
	common.AgentAffiliateRebateRate = 25
	t.Cleanup(func() {
		DB = originalDB
		common.AffiliateRebateRate = originalGlobalRate
		common.AgentAffiliateRebateRate = originalAgentRate
	})

	if err := db.AutoMigrate(&User{}, &UserAffiliate{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	if err := db.Create(&User{Id: 1, Username: "agent", Role: common.RoleAgentUser, AffCode: "AGENT1"}).Error; err != nil {
		t.Fatalf("seed agent user: %v", err)
	}
	if err := db.Create(&User{Id: 2, Username: "common", Role: common.RoleCommonUser, AffCode: "COMMON2"}).Error; err != nil {
		t.Fatalf("seed common user: %v", err)
	}

	if got := resolveAffiliateRebateRatePercent(&UserAffiliate{UserId: 1}); got != 25 {
		t.Fatalf("agent rebate rate = %v, want 25", got)
	}
	if got := resolveAffiliateRebateRatePercent(&UserAffiliate{UserId: 2}); got != 10 {
		t.Fatalf("common rebate rate = %v, want 10", got)
	}
	overrideRate := 40.0
	if got := resolveAffiliateRebateRatePercent(&UserAffiliate{UserId: 1, AffRebateRatePercent: &overrideRate}); got != 40 {
		t.Fatalf("agent override rebate rate = %v, want 40", got)
	}
}
func TestUpdateUserInviterSyncsUserAndAffiliateRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	originalDB := DB
	DB = db
	t.Cleanup(func() { DB = originalDB })

	if err := db.AutoMigrate(&User{}, &UserAffiliate{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	users := []User{
		{Id: 1, Username: "invitee", AffCode: "AAAA"},
		{Id: 2, Username: "old-agent", AffCode: "BBBB", AffCount: 1},
		{Id: 3, Username: "new-agent", AffCode: "CCCC"},
	}
	for _, user := range users {
		if err := db.Create(&user).Error; err != nil {
			t.Fatalf("seed user %d: %v", user.Id, err)
		}
	}
	oldInviterID := 2
	affiliates := []UserAffiliate{
		{UserId: 1, AffCode: "AAAA", InviterId: &oldInviterID},
		{UserId: 2, AffCode: "BBBB", AffCount: 1},
		{UserId: 3, AffCode: "CCCC"},
	}
	for _, affiliate := range affiliates {
		if err := db.Create(&affiliate).Error; err != nil {
			t.Fatalf("seed affiliate %d: %v", affiliate.UserId, err)
		}
	}

	if err := UpdateUserInviter(1, 3); err != nil {
		t.Fatalf("update inviter: %v", err)
	}

	var invitee User
	if err := db.First(&invitee, 1).Error; err != nil {
		t.Fatalf("load invitee: %v", err)
	}
	if invitee.InviterId != 3 {
		t.Fatalf("user inviter_id = %d, want 3", invitee.InviterId)
	}

	var inviteeAffiliate UserAffiliate
	if err := db.First(&inviteeAffiliate, "user_id = ?", 1).Error; err != nil {
		t.Fatalf("load invitee affiliate: %v", err)
	}
	if inviteeAffiliate.InviterId == nil || *inviteeAffiliate.InviterId != 3 {
		t.Fatalf("affiliate inviter_id = %v, want 3", inviteeAffiliate.InviterId)
	}

	var oldAgent, newAgent User
	_ = db.First(&oldAgent, 2).Error
	_ = db.First(&newAgent, 3).Error
	if oldAgent.AffCount != 0 || newAgent.AffCount != 1 {
		t.Fatalf("aff_count old=%d new=%d, want old=0 new=1", oldAgent.AffCount, newAgent.AffCount)
	}

	pageInfo := &common.PageInfo{Page: 1, PageSize: 20}
	invitees, total, err := GetUsersByInviterId(3, pageInfo)
	if err != nil {
		t.Fatalf("get users by inviter: %v", err)
	}
	if total != 1 || len(invitees) != 1 || invitees[0].Id != 1 {
		t.Fatalf("agent invitee list total=%d users=%v, want only user 1", total, invitees)
	}
}

func TestTransferUserQuotaMovesQuotaAtomically(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	originalDB := DB
	DB = db
	t.Cleanup(func() {
		DB = originalDB
	})

	if err := db.AutoMigrate(&User{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	agent := User{Id: 1, Username: "agent", Password: "password123", Quota: 1000, AffCode: "agent-code"}
	invitee := User{Id: 2, Username: "invitee", Password: "password123", Quota: 100, AffCode: "invitee-code"}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := db.Create(&invitee).Error; err != nil {
		t.Fatalf("create invitee: %v", err)
	}

	if err := TransferUserQuota(agent.Id, invitee.Id, 300); err != nil {
		t.Fatalf("transfer quota: %v", err)
	}

	var updatedAgent User
	var updatedInvitee User
	if err := db.First(&updatedAgent, agent.Id).Error; err != nil {
		t.Fatalf("load agent: %v", err)
	}
	if err := db.First(&updatedInvitee, invitee.Id).Error; err != nil {
		t.Fatalf("load invitee: %v", err)
	}
	if updatedAgent.Quota != 700 {
		t.Fatalf("agent quota = %d, want 700", updatedAgent.Quota)
	}
	if updatedInvitee.Quota != 400 {
		t.Fatalf("invitee quota = %d, want 400", updatedInvitee.Quota)
	}
}

func TestTransferUserQuotaRejectsInsufficientQuota(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	originalDB := DB
	DB = db
	t.Cleanup(func() {
		DB = originalDB
	})

	if err := db.AutoMigrate(&User{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	agent := User{Id: 1, Username: "agent", Password: "password123", Quota: 100, AffCode: "agent-code"}
	invitee := User{Id: 2, Username: "invitee", Password: "password123", Quota: 50, AffCode: "invitee-code"}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := db.Create(&invitee).Error; err != nil {
		t.Fatalf("create invitee: %v", err)
	}

	if err := TransferUserQuota(agent.Id, invitee.Id, 300); err != ErrUserQuotaInsufficient {
		t.Fatalf("transfer error = %v, want ErrUserQuotaInsufficient", err)
	}

	var updatedAgent User
	var updatedInvitee User
	if err := db.First(&updatedAgent, agent.Id).Error; err != nil {
		t.Fatalf("load agent: %v", err)
	}
	if err := db.First(&updatedInvitee, invitee.Id).Error; err != nil {
		t.Fatalf("load invitee: %v", err)
	}
	if updatedAgent.Quota != 100 {
		t.Fatalf("agent quota = %d, want 100", updatedAgent.Quota)
	}
	if updatedInvitee.Quota != 50 {
		t.Fatalf("invitee quota = %d, want 50", updatedInvitee.Quota)
	}
}
