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
import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getSelf } from '@/lib/api'
import { useStatus } from '@/hooks/use-status'
import { useSystemConfig } from '@/hooks/use-system-config'
import { SectionPageLayout } from '@/components/layout'
import { BillingHistoryDialog } from './components/dialogs/billing-history-dialog'
import { PaymentConfirmDialog } from './components/dialogs/payment-confirm-dialog'
import { RechargeFormCard } from './components/recharge-form-card'
import { SubscriptionPlansCard } from './components/subscription-plans-card'
import { WalletMoreOptionsSection } from './components/wallet-more-options-section'
import { WalletStatsCard } from './components/wallet-stats-card'
import { DEFAULT_DISCOUNT_RATE } from './constants'
import { useTopupInfo, usePayment, useRedemption } from './hooks'
import {
  getAmountFeeRate,
  getDefaultPaymentType,
  getMinTopupAmount,
} from './lib'
import type { UserWalletData, PaymentMethod, PresetAmount } from './types'

interface WalletProps {
  initialShowHistory?: boolean
}

export function Wallet(props: WalletProps) {
  const { t } = useTranslation()
  const [user, setUser] = useState<UserWalletData | null>(null)
  const [userLoading, setUserLoading] = useState(true)
  const [topupAmount, setTopupAmount] = useState(0)
  const [selectedPreset, setSelectedPreset] = useState<number | null>(null)
  const [selectedPaymentMethod, setSelectedPaymentMethod] =
    useState<PaymentMethod>()
  const [paying, setPaying] = useState(false)
  const [confirmDialogOpen, setConfirmDialogOpen] = useState(false)
  const [billingDialogOpen, setBillingDialogOpen] = useState(false)
  const [redemptionCode, setRedemptionCode] = useState('')
  const [showSubscriptionPanel, setShowSubscriptionPanel] = useState(true)

  const { status } = useStatus()
  const { currency } = useSystemConfig()
  const { topupInfo, presetAmounts, loading: topupLoading } = useTopupInfo()
  const epusdtCreditPerUsdt = topupInfo?.epusdt_credit_per_usdt
  const quotaDisplayType = topupInfo?.quota_display_type
  const quotaPerUnit = topupInfo?.quota_per_unit

  const effectiveUsdExchangeRate = useMemo(() => {
    return currency?.quotaDisplayType === 'USD'
      ? 1
      : currency?.usdExchangeRate || 1
  }, [currency?.quotaDisplayType, currency?.usdExchangeRate])
  const {
    amount: paymentAmount,
    calculating,
    processing,
    calculatePaymentAmount,
    processPayment,
  } = usePayment()
  const { redeeming, redeemCode } = useRedemption()
  const topupInitializedRef = useRef(false)

  const fetchUser = useCallback(async () => {
    try {
      setUserLoading(true)
      const response = await getSelf()
      if (response.success && response.data) {
        setUser(response.data as UserWalletData)
      }
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to fetch user data:', error)
    } finally {
      setUserLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchUser()
  }, [fetchUser])

  useEffect(() => {
    if (props.initialShowHistory) {
      setBillingDialogOpen(true)
      window.history.replaceState({}, '', window.location.pathname)
    }
  }, [props.initialShowHistory])

  useEffect(() => {
    if (
      !topupInfo ||
      presetAmounts.length === 0 ||
      topupInitializedRef.current
    ) {
      return
    }

    topupInitializedRef.current = true

    const minTopup = getMinTopupAmount(topupInfo)
    const initialPreset =
      presetAmounts.find((preset) => preset.value >= minTopup) ||
      presetAmounts[0]
    setTopupAmount(initialPreset.value)
    setSelectedPreset(initialPreset.value)

    const enabledMethods = (topupInfo.pay_methods || []).filter(
      (method) => method.enabled !== false && method.enabled !== 'false'
    )
    const defaultMethod = enabledMethods[0]
    if (defaultMethod) {
      setSelectedPaymentMethod(defaultMethod)
    }

    const defaultPaymentType =
      defaultMethod?.type || getDefaultPaymentType(topupInfo)
    calculatePaymentAmount(
      initialPreset.value,
      defaultPaymentType,
      getAmountFeeRate(topupInfo, initialPreset.value, defaultPaymentType),
      epusdtCreditPerUsdt,
      quotaDisplayType,
      quotaPerUnit
    )
  }, [
    topupInfo,
    presetAmounts,
    epusdtCreditPerUsdt,
    quotaDisplayType,
    quotaPerUnit,
    calculatePaymentAmount,
  ])

  const getCurrentPaymentType = useCallback(() => {
    return selectedPaymentMethod?.type || getDefaultPaymentType(topupInfo)
  }, [selectedPaymentMethod, topupInfo])

  const getCurrentFeeRate = useCallback(
    (amount: number) =>
      getAmountFeeRate(topupInfo, amount, getCurrentPaymentType()),
    [getCurrentPaymentType, topupInfo]
  )

  const handleSelectPreset = (preset: PresetAmount) => {
    setTopupAmount(preset.value)
    setSelectedPreset(preset.value)
    calculatePaymentAmount(
      preset.value,
      getCurrentPaymentType(),
      getCurrentFeeRate(preset.value),
      epusdtCreditPerUsdt,
      quotaDisplayType,
      quotaPerUnit
    )
  }

  const handlePaymentMethodChange = (method: PaymentMethod) => {
    setSelectedPaymentMethod(method)
    calculatePaymentAmount(
      topupAmount,
      method.type,
      getAmountFeeRate(topupInfo, topupAmount, method.type),
      epusdtCreditPerUsdt,
      quotaDisplayType,
      quotaPerUnit
    )
  }

  const handlePayNow = async () => {
    const minTopup = getMinTopupAmount(topupInfo)
    if (topupAmount < minTopup) {
      toast.error(t('Minimum topup amount: {{amount}}', { amount: minTopup }))
      return
    }

    if (!selectedPaymentMethod) {
      toast.error(t('Please select a payment method'))
      return
    }

    const methodMinTopup = selectedPaymentMethod.min_topup || 0
    if (topupAmount < methodMinTopup) {
      toast.error(
        t('Minimum topup amount: {{amount}}', { amount: methodMinTopup })
      )
      return
    }

    await calculatePaymentAmount(
      topupAmount,
      selectedPaymentMethod.type,
      getCurrentFeeRate(topupAmount),
      epusdtCreditPerUsdt,
      quotaDisplayType,
      quotaPerUnit
    )
    setConfirmDialogOpen(true)
  }

  const handlePaymentConfirm = async () => {
    if (!selectedPaymentMethod) return

    setPaying(true)
    try {
      const success = await processPayment(
        topupAmount,
        selectedPaymentMethod.type
      )

      if (success) {
        setConfirmDialogOpen(false)
        await fetchUser()
      }
    } finally {
      setPaying(false)
    }
  }

  const handleRedeem = async () => {
    if (!redemptionCode) return

    const success = await redeemCode(redemptionCode)
    if (success) {
      setRedemptionCode('')
      await fetchUser()
    }
  }

  const getDiscountRate = useCallback(() => {
    return topupInfo?.discount?.[topupAmount] || DEFAULT_DISCOUNT_RATE
  }, [topupInfo, topupAmount])

  const handleSubscriptionAvailabilityChange = useCallback(
    (available: boolean) => {
      setShowSubscriptionPanel(available)
    },
    []
  )

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Wallet')}</SectionPageLayout.Title>
        <SectionPageLayout.Description>
          {t('Manage your balance and payment methods')}
        </SectionPageLayout.Description>
        <SectionPageLayout.Content>
          <div className='mx-auto flex w-full max-w-5xl flex-col gap-4 sm:gap-5'>
            <WalletStatsCard user={user} loading={userLoading} />

            <div id='wallet-add-funds' className='scroll-mt-4'>
              <RechargeFormCard
                topupInfo={topupInfo}
                presetAmounts={presetAmounts}
                selectedPreset={selectedPreset}
                onSelectPreset={handleSelectPreset}
                topupAmount={topupAmount}
                paymentAmount={paymentAmount}
                calculating={calculating}
                onPaymentMethodChange={handlePaymentMethodChange}
                selectedPaymentMethod={selectedPaymentMethod}
                onPayNow={handlePayNow}
                paying={paying || processing}
                loading={topupLoading}
                priceRatio={(status?.price as number) || 1}
                usdExchangeRate={effectiveUsdExchangeRate}
                feeRate={getCurrentFeeRate(topupAmount)}
                epusdtCreditPerUsdt={epusdtCreditPerUsdt}
                quotaDisplayType={quotaDisplayType}
                quotaPerUnit={quotaPerUnit}
                onOpenBilling={() => setBillingDialogOpen(true)}
              />
            </div>

            {(topupInfo?.enable_redemption !== false ||
              !!topupInfo?.topup_link) && (
              <WalletMoreOptionsSection
                redemptionCode={redemptionCode}
                onRedemptionCodeChange={setRedemptionCode}
                onRedeem={handleRedeem}
                redeeming={redeeming}
                topupLink={topupInfo?.topup_link}
                redemptionEnabled={topupInfo?.enable_redemption !== false}
              />
            )}

            {showSubscriptionPanel && (
              <SubscriptionPlansCard
                topupInfo={topupInfo}
                onAvailabilityChange={handleSubscriptionAvailabilityChange}
              />
            )}
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <PaymentConfirmDialog
        open={confirmDialogOpen}
        onOpenChange={setConfirmDialogOpen}
        onConfirm={handlePaymentConfirm}
        topupAmount={topupAmount}
        paymentAmount={paymentAmount}
        paymentMethod={selectedPaymentMethod}
        calculating={calculating}
        processing={paying || processing}
        discountRate={getDiscountRate()}
        feeRate={getCurrentFeeRate(topupAmount)}
        epusdtCreditPerUsdt={epusdtCreditPerUsdt}
        quotaDisplayType={quotaDisplayType}
        quotaPerUnit={quotaPerUnit}
        usdExchangeRate={effectiveUsdExchangeRate}
        stripeCurrency={topupInfo?.stripe_currency}
      />

      <BillingHistoryDialog
        open={billingDialogOpen}
        onOpenChange={setBillingDialogOpen}
      />
    </>
  )
}
