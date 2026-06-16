// Command affiliate_rebate_verify simulates or retries invitee top-ups to verify
// the affiliate rebate pipeline end-to-end against the local database.
//
// Usage (from repo root):
//
//	go run ./cmd/affiliate_rebate_verify
//	go run ./cmd/affiliate_rebate_verify -invitee 6 -money 10
//	go run ./cmd/affiliate_rebate_verify -topup-id 16
//	go run ./cmd/affiliate_rebate_verify -backfill
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

func main() {
	inviteeID := flag.Int("invitee", 0, "invitee user id (default: first user with inviter_id)")
	money := flag.Float64("money", 10, "simulated recharge USD amount")
	topupID := flag.Int("topup-id", 0, "retry rebate for an existing successful top-up id")
	backfill := flag.Bool("backfill", false, "retry rebate for all successful top-ups without APPLIED audit")
	dryRun := flag.Bool("dry-run", false, "print config only, do not write")
	flag.Parse()

	common.InitEnv()
	if err := model.InitDB(); err != nil {
		fmt.Fprintf(os.Stderr, "init db failed: %v\n", err)
		os.Exit(1)
	}
	model.InitOptionMap()

	printAffiliateConfig()

	switch {
	case *backfill:
		if *dryRun {
			fmt.Println("\n[dry-run] would run BackfillMissedAffiliateRebates()")
			return
		}
		fmt.Println("\n=== backfill missed rebates ===")
		model.BackfillMissedAffiliateRebates()
		printSummary(*inviteeID)
		return
	case *topupID > 0:
		topUp := model.GetTopUpById(*topupID)
		if topUp == nil {
			fmt.Fprintf(os.Stderr, "top-up %d not found\n", *topupID)
			os.Exit(1)
		}
		printTopUp(topUp)
		if *dryRun {
			fmt.Println("\n[dry-run] would call ProcessAffiliateRebateAfterTopUp")
			return
		}
		fmt.Println("\n=== retry rebate for existing top-up ===")
		model.ProcessAffiliateRebateAfterTopUp(topUp)
		printAuditForTopUp(topUp.Id)
		printSummary(topUp.UserId)
		return
	default:
		targetInvitee := *inviteeID
		if targetInvitee <= 0 {
			id, err := firstInviteeUserID()
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}
			targetInvitee = id
		}
		if err := printInviteeContext(targetInvitee); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		if *dryRun {
			fmt.Printf("\n[dry-run] would simulate top-up for invitee=%d money=%.2f USD\n", targetInvitee, *money)
			return
		}
		topUp, err := createSimulatedTopUp(targetInvitee, *money)
		if err != nil {
			fmt.Fprintf(os.Stderr, "create simulated top-up failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("\n=== simulated successful top-up ===")
		printTopUp(topUp)
		fmt.Println("\n=== processing affiliate rebate ===")
		model.ProcessAffiliateRebateAfterTopUp(topUp)
		printAuditForTopUp(topUp.Id)
		printSummary(targetInvitee)
	}
}

func printAffiliateConfig() {
	fmt.Println("=== affiliate config (loaded from DB) ===")
	fmt.Printf("AffiliateEnabled=%v\n", common.AffiliateEnabled)
	fmt.Printf("AffiliateRebateRate=%.4f%%\n", common.AffiliateRebateRate)
	fmt.Printf("AffiliateRebateFreezeHours=%d\n", common.AffiliateRebateFreezeHours)
	fmt.Printf("AffiliateRebateDurationDays=%d\n", common.AffiliateRebateDurationDays)
	fmt.Printf("AffiliateRebatePerInviteeCapUSD=%.4f\n", common.AffiliateRebatePerInviteeCapUSD)
}

func firstInviteeUserID() (int, error) {
	var profile model.UserAffiliate
	err := model.DB.Where("inviter_id IS NOT NULL AND inviter_id > 0").Order("user_id asc").First(&profile).Error
	if err != nil {
		return 0, fmt.Errorf("no invitee found in user_affiliates: %w", err)
	}
	return profile.UserId, nil
}

func printInviteeContext(inviteeID int) error {
	var invitee model.UserAffiliate
	if err := model.DB.Where("user_id = ?", inviteeID).First(&invitee).Error; err != nil {
		return fmt.Errorf("invitee affiliate profile not found for user_id=%d: %w", inviteeID, err)
	}
	if invitee.InviterId == nil || *invitee.InviterId <= 0 {
		return fmt.Errorf("user_id=%d has no inviter_id", inviteeID)
	}

	var inviter model.UserAffiliate
	if err := model.DB.Where("user_id = ?", *invitee.InviterId).First(&inviter).Error; err != nil {
		return fmt.Errorf("inviter affiliate profile not found for user_id=%d: %w", *invitee.InviterId, err)
	}

	fmt.Println("\n=== invite relation ===")
	fmt.Printf("invitee user_id=%d aff_code=%s\n", invitee.UserId, invitee.AffCode)
	fmt.Printf("inviter user_id=%d aff_code=%s effective_rate=%.4f%%\n",
		inviter.UserId,
		inviter.AffCode,
		model.ResolveEffectiveRebateRatePercent(&inviter),
	)
	return nil
}

func createSimulatedTopUp(inviteeID int, money float64) (*model.TopUp, error) {
	now := time.Now().Unix()
	topUp := &model.TopUp{
		UserId:          inviteeID,
		Amount:          int64(money),
		Money:           money / 6.8,
		TradeNo:         fmt.Sprintf("AFFTEST-%d-%d", inviteeID, now),
		PaymentMethod:   model.PaymentMethodUsdt,
		PaymentProvider: model.PaymentProviderEpusdt,
		CreateTime:      now,
		CompleteTime:    now,
		Status:          common.TopUpStatusSuccess,
	}
	if err := topUp.Insert(); err != nil {
		return nil, err
	}
	return model.GetTopUpById(topUp.Id), nil
}

func printTopUp(topUp *model.TopUp) {
	fmt.Printf("topup_id=%d user_id=%d money=%.4f amount=%d status=%s trade_no=%s recharge_usd=%s\n",
		topUp.Id,
		topUp.UserId,
		topUp.Money,
		topUp.Amount,
		topUp.Status,
		topUp.TradeNo,
		model.TopUpRechargeUSD(topUp).StringFixed(4),
	)
}

func printAuditForTopUp(topupID int) {
	var audits []model.TopUpAffiliateAudit
	if err := model.DB.Where("topup_id = ?", topupID).Order("id asc").Find(&audits).Error; err != nil {
		fmt.Printf("audit query failed: %v\n", err)
		return
	}
	fmt.Println("\n=== topup affiliate audits ===")
	if len(audits) == 0 {
		fmt.Println("(none)")
		return
	}
	for _, audit := range audits {
		fmt.Printf("id=%d topup_id=%d action=%s detail=%s created_at=%s\n",
			audit.Id, audit.TopUpId, audit.Action, audit.Detail, audit.CreatedAt)
	}
}

func printSummary(inviteeID int) {
	var invitee model.UserAffiliate
	if err := model.DB.Where("user_id = ?", inviteeID).First(&invitee).Error; err != nil {
		fmt.Printf("\ninvitee profile missing: %v\n", err)
		return
	}
	if invitee.InviterId == nil {
		return
	}
	inviterID := *invitee.InviterId

	var inviter model.UserAffiliate
	if err := model.DB.Where("user_id = ?", inviterID).First(&inviter).Error; err != nil {
		fmt.Printf("\ninviter profile missing: %v\n", err)
		return
	}

	fmt.Println("\n=== inviter balances after processing ===")
	fmt.Printf("inviter user_id=%d aff_quota_usd=%s aff_frozen_quota_usd=%s aff_history_quota_usd=%s\n",
		inviter.UserId,
		inviter.AffQuotaUSD.StringFixed(8),
		inviter.AffFrozenQuotaUSD.StringFixed(8),
		inviter.AffHistoryQuotaUSD.StringFixed(8),
	)

	var ledgerCount int64
	model.DB.Model(&model.UserAffiliateLedger{}).
		Where("user_id = ? AND action = ? AND source_user_id = ?", inviterID, common.AffiliateLedgerActionAccrue, inviteeID).
		Count(&ledgerCount)
	fmt.Printf("accrue ledger rows for invitee=%d -> inviter=%d: %d\n", inviteeID, inviterID, ledgerCount)

	var appliedCount int64
	model.DB.Model(&model.TopUpAffiliateAudit{}).
		Where("action = ?", common.AffiliateTopUpAuditApplied).
		Count(&appliedCount)
	fmt.Printf("total APPLIED affiliate audits: %d\n", appliedCount)
}
