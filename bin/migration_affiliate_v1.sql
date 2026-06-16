-- Affiliate rebate module (USD decimal) for starnexus / new-api
-- Applied automatically via GORM AutoMigrate; this script documents the schema.

CREATE TABLE IF NOT EXISTS user_affiliates (
    user_id INTEGER PRIMARY KEY,
    aff_code VARCHAR(32) NOT NULL UNIQUE,
    inviter_id INTEGER NULL,
    aff_count INTEGER NOT NULL DEFAULT 0,
    aff_quota_usd DECIMAL(20,8) NOT NULL DEFAULT 0,
    aff_frozen_quota_usd DECIMAL(20,8) NOT NULL DEFAULT 0,
    aff_history_quota_usd DECIMAL(20,8) NOT NULL DEFAULT 0,
    aff_rebate_rate_percent DECIMAL(5,2) NULL,
    aff_code_custom BOOLEAN NOT NULL DEFAULT FALSE,
    created_at DATETIME,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS user_affiliate_ledger (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    action VARCHAR(32) NOT NULL,
    amount_usd DECIMAL(20,8) NOT NULL,
    source_user_id INTEGER NULL,
    source_topup_id INTEGER NULL,
    frozen_until DATETIME NULL,
    quota_after INTEGER NULL,
    aff_quota_usd_after DECIMAL(20,8) NULL,
    aff_frozen_quota_usd_after DECIMAL(20,8) NULL,
    aff_history_quota_usd_after DECIMAL(20,8) NULL,
    created_at DATETIME,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS topup_affiliate_audits (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    topup_id INTEGER NOT NULL,
    action VARCHAR(64) NOT NULL,
    detail TEXT,
    created_at DATETIME,
    UNIQUE(topup_id, action)
);

-- Suggested options (insert via admin UI):
-- AffiliateEnabled=true
-- AffiliateRebateRate=10
-- AffiliateRebateFreezeHours=0
-- AffiliateRebateDurationDays=0
-- AffiliateRebatePerInviteeCapUSD=0
