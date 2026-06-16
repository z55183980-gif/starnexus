package main

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

func main() {
	common.InitEnv()
	if err := model.InitDB(); err != nil {
		panic(err)
	}
	model.InitOptionMap()

	var inviter model.UserAffiliate
	if err := model.DB.Where("user_id = ?", 2).First(&inviter).Error; err != nil {
		panic(err)
	}
	fmt.Printf("inviter aff_quota_usd=%s aff_history=%s\n",
		inviter.AffQuotaUSD.StringFixed(8), inviter.AffHistoryQuotaUSD.StringFixed(8))

	var auditCount int64
	model.DB.Model(&model.TopUpAffiliateAudit{}).Count(&auditCount)
	fmt.Printf("audit rows=%d\n", auditCount)
}
