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
import {
  PAYMENT_TYPES,
  DEFAULT_PRESET_MULTIPLIERS,
  DEFAULT_PAYMENT_TYPE,
  DEFAULT_MIN_TOPUP,
  EPUSDT_CREDIT_PER_USDT,
} from '../constants'
import type { PresetAmount, TopupInfo } from '../types'
import { formatPaymentGatewayAmount } from '@/lib/currency'
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
 * payUSDT = credit / EPUSDT_CREDIT_PER_USDT, rounded to 2 decimals.
 */
export function calculateEpusdtPayAmount(creditAmount: number): number {
  if (!Number.isFinite(creditAmount) || creditAmount <= 0) {
    return 0
  }

  return (
    Math.round((creditAmount / EPUSDT_CREDIT_PER_USDT) * 100) / 100
  )
}

/**
 * Check if payment method is USDT
 */
export function getDisplayPaymentAmount(
  paymentType: string | undefined,
  topupAmount: number,
  paymentAmount: number
): number {
  if (paymentType && isUsdtPayment(paymentType)) {
    return calculateEpusdtPayAmount(topupAmount)
  }
  return paymentAmount
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
