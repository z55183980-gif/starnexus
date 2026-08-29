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
import { formatPaymentGatewayAmount } from '@/lib/currency'
import {
  PAYMENT_TYPES,
  DEFAULT_PRESET_MULTIPLIERS,
  DEFAULT_PAYMENT_TYPE,
  DEFAULT_MIN_TOPUP,
  EPUSDT_CREDIT_PER_USDT,
  QUOTA_PER_DOLLAR,
} from '../constants'
import type { PresetAmount, QuotaDisplayType, TopupInfo } from '../types'
import { formatCurrency } from './format'

// ============================================================================
// Payment Processing Functions
// ============================================================================

/**
 * Check if browser is Safari
 */
function isSafariBrowser(): boolean {
  return (
    navigator.userAgent.indexOf('Safari') > -1 &&
    navigator.userAgent.indexOf('Chrome') < 1
  )
}

/**
 * Submit payment form (for non-Stripe payments)
 */
export function submitPaymentForm(
  url: string,
  params: Record<string, unknown>
): void {
  const form = document.createElement('form')
  form.action = url
  form.method = 'POST'

  // Don't open in new tab for Safari
  if (!isSafariBrowser()) {
    form.target = '_blank'
  }

  // Add form parameters
  Object.entries(params).forEach(([key, value]) => {
    const input = document.createElement('input')
    input.type = 'hidden'
    input.name = key
    input.value = String(value)
    form.appendChild(input)
  })

  document.body.appendChild(form)
  form.submit()
  document.body.removeChild(form)
}

/**
 * Check if payment method is Stripe
 */
export function isStripePayment(paymentType: string): boolean {
  return paymentType === PAYMENT_TYPES.STRIPE
}

/**
 * Calculate EpUSDT payment amount from credit (system USD units).
 * payUSDT = credit / creditPerUsdt × (1 + fee rate), rounded to 2 decimals.
 */
export function calculateEpusdtPayAmount(
  creditAmount: number,
  feeRate = 0,
  creditPerUsdt = EPUSDT_CREDIT_PER_USDT
): number {
  if (!Number.isFinite(creditAmount) || creditAmount <= 0) {
    return 0
  }

  const normalizedCreditPerUsdt =
    Number.isFinite(creditPerUsdt) && creditPerUsdt > 0
      ? creditPerUsdt
      : EPUSDT_CREDIT_PER_USDT
  const normalizedFeeRate =
    Number.isFinite(feeRate) && feeRate > 0 && feeRate <= 1 ? feeRate : 0
  return (
    Math.round(
      (creditAmount / normalizedCreditPerUsdt) * (1 + normalizedFeeRate) * 100
    ) / 100
  )
}

/** Convert the amount sent by the wallet UI into canonical credit USD units. */
export function getTopupCreditAmount(
  amount: number,
  quotaDisplayType?: QuotaDisplayType,
  quotaPerUnit = QUOTA_PER_DOLLAR
): number {
  if (!Number.isFinite(amount) || amount <= 0) return 0
  if (
    quotaDisplayType === 'TOKENS' &&
    Number.isFinite(quotaPerUnit) &&
    quotaPerUnit > 0
  ) {
    return amount / quotaPerUnit
  }
  return amount
}

/** Return the fee rate for an exact recharge amount. */
export function getAmountFeeRate(
  topupInfo: TopupInfo | null,
  amount: number,
  paymentType?: string
): number {
  const feeMap = topupInfo?.fee || {}
  const amountKey = String(amount)
  const channelRuleExistsForAmount = Object.keys(feeMap).some((key) => {
    const separator = key.indexOf(':')
    return separator >= 0 && key.slice(separator + 1) === amountKey
  })
  if (paymentType) {
    const channelKey = `${paymentType}:${amountKey}`
    if (Object.prototype.hasOwnProperty.call(feeMap, channelKey)) {
      const channelRate = Number(feeMap[channelKey])
      if (
        Number.isFinite(channelRate) &&
        channelRate >= 0 &&
        channelRate <= 1
      ) {
        return channelRate
      }
      return 0
    }
    if (channelRuleExistsForAmount) return 0
  } else if (channelRuleExistsForAmount) {
    return 0
  }
  const rate = Number(feeMap[amountKey] ?? 0)
  return Number.isFinite(rate) && rate > 0 && rate <= 1 ? rate : 0
}

/**
 * Check if payment method is USDT
 */
export function getDisplayPaymentAmount(
  paymentType: string | undefined,
  topupAmount: number,
  paymentAmount: number,
  feeRate = 0,
  creditPerUsdt = EPUSDT_CREDIT_PER_USDT,
  quotaDisplayType?: QuotaDisplayType,
  quotaPerUnit = QUOTA_PER_DOLLAR
): number {
  if (paymentType && isUsdtPayment(paymentType)) {
    return calculateEpusdtPayAmount(
      getTopupCreditAmount(topupAmount, quotaDisplayType, quotaPerUnit),
      feeRate,
      creditPerUsdt
    )
  }
  return paymentAmount
}

/** Calculate the fee component from the final amount and its percentage rate. */
export function calculatePaymentFeeAmount(
  paymentAmount: number,
  feeRate: number
): number {
  if (
    !Number.isFinite(paymentAmount) ||
    paymentAmount <= 0 ||
    !Number.isFinite(feeRate) ||
    feeRate <= 0 ||
    feeRate > 1
  ) {
    return 0
  }
  return paymentAmount - paymentAmount / (1 + feeRate)
}

export function isUsdtPayment(paymentType: string): boolean {
  return paymentType === PAYMENT_TYPES.USDT
}

/**
 * Format the actual charge shown to the user for a selected payment method.
 */
export function formatPaymentChargeAmount(
  amount: number,
  paymentType?: string,
  options?: {
    digitsLarge?: number
    digitsSmall?: number
    abbreviate?: boolean
    stripeCurrency?: string
  }
): string {
  if (paymentType && isUsdtPayment(paymentType)) {
    return `${formatCurrency(amount)} USDT`
  }

  return formatPaymentGatewayAmount(amount, {
    paymentType,
    stripeCurrency: options?.stripeCurrency,
    digitsLarge: options?.digitsLarge ?? 2,
    digitsSmall: options?.digitsSmall ?? 2,
    abbreviate: options?.abbreviate ?? false,
  })
}

/**
 * Get default payment type from topup info
 */
export function getDefaultPaymentType(topupInfo: TopupInfo | null): string {
  if (!topupInfo) {
    return DEFAULT_PAYMENT_TYPE
  }

  // Return first available payment method or default
  const enabledMethods = (topupInfo.pay_methods || []).filter(
    (method) => method.enabled !== false && method.enabled !== 'false'
  )
  if (enabledMethods.length > 0) {
    return enabledMethods[0].type
  }

  if (topupInfo.enable_stripe_topup) {
    return PAYMENT_TYPES.STRIPE
  }

  if (topupInfo.enable_usdt_topup) {
    return PAYMENT_TYPES.USDT
  }

  return DEFAULT_PAYMENT_TYPE
}

/**
 * Get minimum topup amount from topup info
 */
export function getMinTopupAmount(topupInfo: TopupInfo | null): number {
  if (!topupInfo) {
    return DEFAULT_MIN_TOPUP
  }

  if (topupInfo.enable_usdt_topup) {
    return topupInfo.min_topup
  }

  if (topupInfo.enable_stripe_topup) {
    return topupInfo.stripe_min_topup
  }

  return topupInfo.min_topup || topupInfo.stripe_min_topup || DEFAULT_MIN_TOPUP
}

/**
 * Generate preset amounts based on minimum topup
 */
export function generatePresetAmounts(minAmount: number): PresetAmount[] {
  return DEFAULT_PRESET_MULTIPLIERS.map((multiplier) => ({
    value: minAmount * multiplier,
  }))
}

/**
 * Merge custom preset amounts with discounts
 */
export function mergePresetAmounts(
  amountOptions: number[],
  discounts: Record<number, number>
): PresetAmount[] {
  if (!amountOptions || amountOptions.length === 0) {
    return []
  }

  return amountOptions.map((amount) => ({
    value: amount,
    discount: discounts[amount] || 1.0,
  }))
}
