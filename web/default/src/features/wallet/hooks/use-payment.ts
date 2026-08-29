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
import { useState, useCallback } from 'react'
import i18next from 'i18next'
import { toast } from 'sonner'
import {
  calculateAmount,
  calculateStripeAmount,
  requestStripePayment,
  requestUsdtPayment,
  isApiSuccess,
  getApiErrorMessage,
} from '../api'
import { EPUSDT_CREDIT_PER_USDT } from '../constants'
import {
  isUsdtPayment,
  isStripePayment,
  calculateEpusdtPayAmount,
  getTopupCreditAmount,
} from '../lib'
import type { QuotaDisplayType } from '../types'

// ============================================================================
// Payment Hook
// ============================================================================

export function usePayment() {
  const [amount, setAmount] = useState<number>(0)
  const [calculating, setCalculating] = useState(false)
  const [processing, setProcessing] = useState(false)

  // Calculate payment amount
  const calculatePaymentAmount = useCallback(
    async (
      topupAmount: number,
      paymentType: string,
      feeRate = 0,
      creditPerUsdt = EPUSDT_CREDIT_PER_USDT,
      quotaDisplayType?: QuotaDisplayType,
      quotaPerUnit?: number
    ) => {
      if (isUsdtPayment(paymentType)) {
        const calculated = calculateEpusdtPayAmount(
          getTopupCreditAmount(topupAmount, quotaDisplayType, quotaPerUnit),
          feeRate,
          creditPerUsdt
        )
        setAmount(calculated)
        return calculated
      }

      try {
        setCalculating(true)

        const isStripe = isStripePayment(paymentType)
        const response = isStripe
          ? await calculateStripeAmount({
              amount: topupAmount,
              payment_method: paymentType,
            })
          : await calculateAmount({
              amount: topupAmount,
              payment_method: paymentType,
            })

        if (isApiSuccess(response) && response.data) {
          const calculatedAmount = parseFloat(response.data)
          setAmount(calculatedAmount)
          return calculatedAmount
        }

        // Don't show error for calculation, just set to 0
        setAmount(0)
        return 0
      } catch (_error) {
        setAmount(0)
        return 0
      } finally {
        setCalculating(false)
      }
    },
    []
  )

  // Process payment
  const processPayment = useCallback(
    async (topupAmount: number, paymentType: string) => {
      try {
        setProcessing(true)

        const isStripe = isStripePayment(paymentType)
        const isUsdt = isUsdtPayment(paymentType)
        const amount = Math.floor(topupAmount)

        if (!isStripe && !isUsdt) {
          toast.error(i18next.t('Payment request failed'))
          return false
        }

        const response = isStripe
          ? await requestStripePayment({
              amount,
              payment_method: 'stripe',
            })
          : await requestUsdtPayment({
              amount,
              payment_method: 'usdt',
            })

        if (!isApiSuccess(response)) {
          toast.error(
            getApiErrorMessage(response, i18next.t('Payment request failed'))
          )
          return false
        }

        if (isStripe && response.data?.pay_link) {
          window.open(response.data.pay_link as string, '_blank')
          toast.success(i18next.t('Redirecting to payment page...'))
          return true
        }

        if (isUsdt && response.data) {
          const usdtPayLink =
            typeof response.data === 'object'
              ? (response.data as { pay_link?: string }).pay_link
              : undefined
          if (usdtPayLink) {
            window.open(usdtPayLink, '_blank')
            toast.success(i18next.t('Redirecting to payment page...'))
            return true
          }
        }

        return false
      } catch (_error) {
        toast.error(i18next.t('Payment request failed'))
        return false
      } finally {
        setProcessing(false)
      }
    },
    []
  )

  return {
    amount,
    calculating,
    processing,
    calculatePaymentAmount,
    processPayment,
    setAmount,
  }
}
