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
import { useState, useEffect } from 'react'
import { getTopupInfo } from '../api'
import { PAYMENT_TYPES, QUOTA_PER_DOLLAR } from '../constants'
import {
  generatePresetAmounts,
  mergePresetAmounts,
  getMinTopupAmount,
} from '../lib'
import type { TopupInfo, PresetAmount, PaymentMethod } from '../types'

const SUPPORTED_PAYMENT_TYPES = new Set<string>([
  PAYMENT_TYPES.USDT,
  PAYMENT_TYPES.STRIPE,
])

function parseJsonArray(data: unknown): unknown[] {
  if (Array.isArray(data)) {
    return data
  }

  if (typeof data === 'string') {
    try {
      const parsed = JSON.parse(data)
      return Array.isArray(parsed) ? parsed : []
    } catch {
      return []
    }
  }

  return []
}

function parsePaymentMethods(
  data: unknown,
  stripeMinTopup: number
): PaymentMethod[] {
  return parseJsonArray(data)
    .filter(
      (item): item is Record<string, unknown> =>
        !!item && typeof item === 'object'
    )
    .map((item) => {
      const rawMinTopup = Number(item.min_topup)
      const normalizedMinTopup = Number.isFinite(rawMinTopup) ? rawMinTopup : 0
      const type = typeof item.type === 'string' ? item.type : ''

      return {
        name: typeof item.name === 'string' ? item.name : '',
        type,
        color: typeof item.color === 'string' ? item.color : undefined,
        icon: typeof item.icon === 'string' ? item.icon : undefined,
        tag: typeof item.tag === 'string' ? item.tag : undefined,
        enabled:
          typeof item.enabled === 'boolean' || typeof item.enabled === 'string'
            ? item.enabled
            : undefined,
        min_topup:
          type === 'stripe' && normalizedMinTopup <= 0
            ? stripeMinTopup
            : normalizedMinTopup,
      }
    })
    .filter(
      (item) => item.name && item.type && SUPPORTED_PAYMENT_TYPES.has(item.type)
    )
}

function parseAmountOptions(data: unknown): number[] {
  return parseJsonArray(data)
    .map((item) => Number(item))
    .filter((item) => Number.isFinite(item) && item > 0)
}

function parseDiscountMap(data: unknown): Record<number, number> {
  if (!data) {
    return {}
  }

  let parsedData = data

  if (typeof data === 'string') {
    try {
      parsedData = JSON.parse(data)
    } catch {
      return {}
    }
  }

  if (
    !parsedData ||
    typeof parsedData !== 'object' ||
    Array.isArray(parsedData)
  ) {
    return {}
  }

  return Object.entries(parsedData).reduce<Record<number, number>>(
    (result, [key, value]) => {
      const numericKey = Number(key)
      const numericValue = Number(value)

      if (Number.isFinite(numericKey) && Number.isFinite(numericValue)) {
        result[numericKey] = numericValue
      }

      return result
    },
    {}
  )
}

function parseFeeMap(data: unknown): Record<number, number> {
  return parseDiscountMap(data)
}

function isQuotaDisplayType(
  value: unknown
): value is NonNullable<TopupInfo['quota_display_type']> {
  return (
    value === 'USD' ||
    value === 'CNY' ||
    value === 'TOKENS' ||
    value === 'CUSTOM'
  )
}

function normalizeRequestAmount(
  amount: number,
  quotaDisplayType: TopupInfo['quota_display_type'],
  quotaPerUnit: number
): number {
  if (!Number.isFinite(amount) || amount <= 0) return 0
  if (quotaDisplayType !== 'TOKENS') return amount

  // Canonical settings use credit units (for example 100), while older
  // installations may already contain raw token keys (for example 50000000).
  // Preserve the latter instead of multiplying them a second time.
  if (amount >= quotaPerUnit && amount % quotaPerUnit === 0) return amount
  return Math.round(amount * quotaPerUnit)
}

function normalizeRequestMap(
  values: Record<number, number>,
  quotaDisplayType: TopupInfo['quota_display_type'],
  quotaPerUnit: number
): Record<number, number> {
  if (quotaDisplayType !== 'TOKENS') return values

  return Object.entries(values).reduce<Record<number, number>>(
    (result, [key, value]) => {
      const normalizedKey = normalizeRequestAmount(
        Number(key),
        quotaDisplayType,
        quotaPerUnit
      )
      if (normalizedKey > 0) result[normalizedKey] = value
      return result
    },
    {}
  )
}

export function useTopupInfo() {
  const [topupInfo, setTopupInfo] = useState<TopupInfo | null>(null)
  const [presetAmounts, setPresetAmounts] = useState<PresetAmount[]>([])
  const [loading, setLoading] = useState(true)

  const fetchTopupInfo = async () => {
    try {
      setLoading(true)

      const response = await getTopupInfo()

      if (!response.success || !response.data) {
        // eslint-disable-next-line no-console
        console.error('Failed to fetch topup info:', response.message)
        return
      }

      const payMethods = parsePaymentMethods(
        response.data.pay_methods,
        response.data.stripe_min_topup
      )
      const quotaDisplayType = isQuotaDisplayType(
        response.data.quota_display_type
      )
        ? response.data.quota_display_type
        : 'USD'
      const parsedQuotaPerUnit = Number(response.data.quota_per_unit)
      const quotaPerUnit =
        Number.isFinite(parsedQuotaPerUnit) && parsedQuotaPerUnit > 0
          ? parsedQuotaPerUnit
          : QUOTA_PER_DOLLAR
      const normalizeAmount = (amount: number) =>
        normalizeRequestAmount(amount, quotaDisplayType, quotaPerUnit)
      const parsedDiscount = parseDiscountMap(response.data.discount)
      const parsedFee = parseFeeMap(response.data.fee)

      const processedData: TopupInfo = {
        ...response.data,
        quota_display_type: quotaDisplayType,
        quota_per_unit: quotaPerUnit,
        topup_group_ratio:
          Number.isFinite(Number(response.data.topup_group_ratio)) &&
          Number(response.data.topup_group_ratio) > 0
            ? Number(response.data.topup_group_ratio)
            : 1,
        min_topup: normalizeAmount(Number(response.data.min_topup)),
        stripe_min_topup: normalizeAmount(
          Number(response.data.stripe_min_topup)
        ),
        waffo_min_topup: response.data.waffo_min_topup
          ? normalizeAmount(Number(response.data.waffo_min_topup))
          : response.data.waffo_min_topup,
        waffo_pancake_min_topup: response.data.waffo_pancake_min_topup
          ? normalizeAmount(Number(response.data.waffo_pancake_min_topup))
          : response.data.waffo_pancake_min_topup,
        pay_methods: payMethods.map((method) => ({
          ...method,
          min_topup:
            method.min_topup && method.min_topup > 0
              ? normalizeAmount(method.min_topup)
              : method.min_topup,
        })),
        amount_options: parseAmountOptions(response.data.amount_options).map(
          normalizeAmount
        ),
        discount: normalizeRequestMap(
          parsedDiscount,
          quotaDisplayType,
          quotaPerUnit
        ),
        fee: normalizeRequestMap(parsedFee, quotaDisplayType, quotaPerUnit),
      }

      setTopupInfo(processedData)

      if (processedData.amount_options.length > 0) {
        const customPresets = mergePresetAmounts(
          processedData.amount_options,
          processedData.discount || {}
        )
        setPresetAmounts(customPresets)
      } else {
        const minTopup = getMinTopupAmount(processedData)
        const defaultPresets = generatePresetAmounts(minTopup)
        setPresetAmounts(defaultPresets)
      }
    } catch (err) {
      // eslint-disable-next-line no-console
      console.error('Failed to fetch topup info:', err)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchTopupInfo()
  }, [])

  return {
    topupInfo,
    presetAmounts,
    loading,
    refetch: fetchTopupInfo,
  }
}
