/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { parseCurrencyDisplayType } from '@/lib/currency'
import { CheckinSettingsSection } from '../general/checkin-settings-section'
import { PricingSection } from '../general/pricing-section'
import { QuotaSettingsSection } from '../general/quota-settings-section'
import { PaymentSettingsSection } from '../integrations/payment-settings-section'
import {
  isEpayConfigured,
  isLegacyEpusdtGatewayConflict,
} from '../integrations/utils'
import { RatioSettingsCard } from '../models/ratio-settings-card'
import type { BillingSettings } from '../types'
import { createSectionRegistry } from '../utils/section-registry'
import { AffiliateRebateSettingsSection } from './affiliate-rebate-settings-section'
import {
  parseTokenPricingDefaults,
  TokenPricingSettingsSection,
} from './token-pricing-settings-section'

const getModelDefaults = (settings: BillingSettings) => ({
  ModelPrice: settings.ModelPrice,
  ModelRatio: settings.ModelRatio,
  CacheRatio: settings.CacheRatio,
  CreateCacheRatio: settings.CreateCacheRatio,
  CompletionRatio: settings.CompletionRatio,
  ImageRatio: settings.ImageRatio,
  AudioRatio: settings.AudioRatio,
  AudioCompletionRatio: settings.AudioCompletionRatio,
  VideoTokenPrice: settings.VideoTokenPrice,
  ExposeRatioEnabled: settings.ExposeRatioEnabled,
  BillingMode: settings['billing_setting.billing_mode'],
  BillingExpr: settings['billing_setting.billing_expr'],
  CacheBillingOffsetBps: settings['billing_setting.cache_billing_offset_bps'],
})

const getGroupDefaults = (settings: BillingSettings) => ({
  TopupGroupRatio: settings.TopupGroupRatio,
  GroupRatio: settings.GroupRatio,
  UserUsableGroups: settings.UserUsableGroups,
  GroupGroupRatio: settings.GroupGroupRatio,
  AutoGroups: settings.AutoGroups,
  DefaultUseAutoGroup: settings.DefaultUseAutoGroup,
  GroupSpecialUsableGroup:
    settings['group_ratio_setting.group_special_usable_group'],
})

function parseJsonString(raw: string | undefined): unknown {
  if (!raw) {
    return undefined
  }
  try {
    return JSON.parse(raw)
  } catch {
    return undefined
  }
}

function collectGroupNames(value: unknown, names: Set<string>) {
  if (Array.isArray(value)) {
    value.forEach((item) => {
      if (typeof item === 'string' && item.trim()) {
        names.add(item.trim())
      } else {
        collectGroupNames(item, names)
      }
    })
    return
  }
  if (value && typeof value === 'object') {
    Object.entries(value as Record<string, unknown>).forEach(([key, item]) => {
      const groupName = key.replace(/^[+-]:/, '').trim()
      if (groupName) {
        names.add(groupName)
      }
      collectGroupNames(item, names)
    })
  }
}

const getTokenPricingGroupOptions = (settings: BillingSettings) => {
  const names = new Set<string>()
  ;[
    settings.GroupRatio,
    settings.UserUsableGroups,
    settings.GroupGroupRatio,
    settings.AutoGroups,
    settings['group_ratio_setting.group_special_usable_group'],
  ].forEach((raw) => collectGroupNames(parseJsonString(raw), names))
  return Array.from(names).sort()
}

const BILLING_SECTIONS = [
  {
    id: 'quota',
    titleKey: 'Quota Settings',
    descriptionKey: 'Configure user quota allocation and rewards',
    build: (settings: BillingSettings) => (
      <QuotaSettingsSection
        defaultValues={{
          QuotaForNewUser: settings.QuotaForNewUser,
          PreConsumedQuota: settings.PreConsumedQuota,
          TopUpLink: settings.TopUpLink,
          general_setting: {
            docs_link: settings['general_setting.docs_link'],
          },
          quota_setting: {
            enable_free_model_pre_consume:
              settings['quota_setting.enable_free_model_pre_consume'],
          },
        }}
      />
    ),
  },
  {
    id: 'affiliate-rebate',
    titleKey: 'Affiliate Rebate (USD)',
    descriptionKey: 'Configure recharge-based referral rebates in USD.',
    build: (settings: BillingSettings) => (
      <AffiliateRebateSettingsSection
        defaultValues={{
          AffiliateEnabled: settings.AffiliateEnabled ?? true,
          AffiliateRebateRate: settings.AffiliateRebateRate ?? 0,
          AgentAffiliateRebateRate: settings.AgentAffiliateRebateRate ?? 0,
          AffiliateRebateFreezeHours: settings.AffiliateRebateFreezeHours ?? 0,
          AffiliateRebateDurationDays:
            settings.AffiliateRebateDurationDays ?? 0,
          AffiliateRebatePerInviteeCapUSD:
            settings.AffiliateRebatePerInviteeCapUSD ?? 0,
        }}
      />
    ),
  },
  {
    id: 'currency',
    titleKey: 'Currency & Display',
    descriptionKey: 'Configure currency conversion and quota display options',
    build: (settings: BillingSettings) => (
      <PricingSection
        defaultValues={{
          QuotaPerUnit: settings.QuotaPerUnit,
          USDExchangeRate: settings.USDExchangeRate,
          DisplayInCurrencyEnabled: settings.DisplayInCurrencyEnabled,
          DisplayTokenStatEnabled: settings.DisplayTokenStatEnabled,
          general_setting: {
            quota_display_type: parseCurrencyDisplayType(
              settings['general_setting.quota_display_type']
            ),
            custom_currency_symbol:
              settings['general_setting.custom_currency_symbol'] ?? '¤',
            custom_currency_exchange_rate:
              settings['general_setting.custom_currency_exchange_rate'] ?? 1,
          },
        }}
      />
    ),
  },
  {
    id: 'token-pricing',
    titleKey: 'Token Pricing',
    descriptionKey:
      'Adjust billable token counts while preserving raw token audit data for administrators',
    build: (settings: BillingSettings) => (
      <TokenPricingSettingsSection
        defaultValues={parseTokenPricingDefaults(
          settings['billing_setting.token_pricing']
        )}
        groupOptions={getTokenPricingGroupOptions(settings)}
      />
    ),
  },
  {
    id: 'model-pricing',
    titleKey: 'Model Pricing',
    descriptionKey: 'Configure model pricing ratios and tool prices',
    build: (settings: BillingSettings) => (
      <RatioSettingsCard
        titleKey='Model Pricing'
        descriptionKey='Configure model pricing ratios and tool prices'
        modelDefaults={getModelDefaults(settings)}
        groupDefaults={getGroupDefaults(settings)}
        toolPricesDefault={settings['tool_price_setting.prices']}
        gptImagePricesDefault={settings.GPTImagePrice}
        visibleTabs={[
          'models',
          'gpt-image-prices',
          'tool-prices',
          'upstream-sync',
        ]}
      />
    ),
  },
  {
    id: 'group-pricing',
    titleKey: 'Group Pricing',
    descriptionKey: 'Configure group ratios and group-specific pricing rules',
    build: (settings: BillingSettings) => (
      <RatioSettingsCard
        titleKey='Group Pricing'
        descriptionKey='Configure group ratios and group-specific pricing rules'
        modelDefaults={getModelDefaults(settings)}
        groupDefaults={getGroupDefaults(settings)}
        toolPricesDefault={settings['tool_price_setting.prices']}
        gptImagePricesDefault={settings.GPTImagePrice}
        visibleTabs={['groups']}
      />
    ),
  },
  {
    id: 'payment',
    titleKey: 'Payment Gateway',
    descriptionKey: 'Configure payment gateway integrations',
    build: (settings: BillingSettings) => {
      const epayConfigured = isEpayConfigured(settings)
      const legacyPayAddress = settings.PayAddress ?? ''
      const storedGateway = settings.EpUSDTGatewayAddress ?? ''
      const payMethods = settings.PayMethods ?? ''

      return (
        <PaymentSettingsSection
          legacyPayAddress={legacyPayAddress}
          legacyEpayConfigured={epayConfigured}
          legacyPayMethods={payMethods}
          defaultValues={{
            EpUSDTGatewayAddress: isLegacyEpusdtGatewayConflict(
              storedGateway,
              legacyPayAddress,
              epayConfigured,
              payMethods
            )
              ? ''
              : storedGateway,
            EpUSDTApiToken: settings.EpUSDTApiToken ?? '',
            EpUSDTNotifyURL: settings.EpUSDTNotifyURL ?? '',
            EpUSDTCreditPerUSDT: settings.EpUSDTCreditPerUSDT ?? 6.8,
            Price: settings.Price,
            MinTopUp: settings.MinTopUp,
            PayMethods: settings.PayMethods,
            AmountOptions: settings['payment_setting.amount_options'],
            AmountDiscount: settings['payment_setting.amount_discount'],
            StripeApiSecret: settings.StripeApiSecret,
            StripeWebhookSecret: settings.StripeWebhookSecret,
            StripePriceId: settings.StripePriceId,
            StripeCurrency: (['usd', 'cny', 'hkd'].includes(
              (settings.StripeCurrency || 'usd').toLowerCase()
            )
              ? (settings.StripeCurrency || 'usd').toLowerCase()
              : 'usd') as 'usd' | 'cny' | 'hkd',
            StripeMinTopUp: settings.StripeMinTopUp,
            StripePromotionCodesEnabled: settings.StripePromotionCodesEnabled,
          }}
          secretConfigured={{
            stripeApiSecret: settings.StripeApiSecretConfigured ?? false,
            stripeWebhookSecret:
              settings.StripeWebhookSecretConfigured ?? false,
            epusdtApiToken: settings.EpUSDTApiTokenConfigured ?? false,
          }}
          complianceDefaults={{
            confirmed:
              settings['payment_setting.compliance_confirmed'] ?? false,
            termsVersion:
              settings['payment_setting.compliance_terms_version'] ?? '',
            confirmedAt:
              settings['payment_setting.compliance_confirmed_at'] ?? 0,
            confirmedBy:
              settings['payment_setting.compliance_confirmed_by'] ?? 0,
          }}
        />
      )
    },
  },
  {
    id: 'checkin',
    titleKey: 'Check-in Rewards',
    descriptionKey: 'Configure daily check-in rewards for users',
    build: (settings: BillingSettings) => (
      <CheckinSettingsSection
        defaultValues={{
          enabled: settings['checkin_setting.enabled'],
          minQuota: settings['checkin_setting.min_quota'],
          maxQuota: settings['checkin_setting.max_quota'],
        }}
      />
    ),
  },
] as const

export type BillingSectionId = (typeof BILLING_SECTIONS)[number]['id']

const billingRegistry = createSectionRegistry<
  BillingSectionId,
  BillingSettings
>({
  sections: BILLING_SECTIONS,
  defaultSection: 'quota',
  basePath: '/system-settings/billing',
  urlStyle: 'path',
})

export const BILLING_SECTION_IDS = billingRegistry.sectionIds
export const BILLING_DEFAULT_SECTION = billingRegistry.defaultSection
export const getBillingSectionNavItems = billingRegistry.getSectionNavItems
export const getBillingSectionContent = billingRegistry.getSectionContent
