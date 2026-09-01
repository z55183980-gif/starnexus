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
import type { ReactNode } from 'react'
import { Check, Loader2, Receipt } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  formatCurrencyFromUSD,
  formatQuotaWithCurrency,
  getCurrencyLabel,
} from '@/lib/currency'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  calculatePresetPricing,
  formatPaymentChargeAmount,
  getAmountFeeRate,
  getDiscountLabel,
  getDisplayPaymentAmount,
  getPaymentIcon,
  getMinTopupAmount,
  isUsdtPayment,
  calculateEpusdtPayAmount,
  calculatePaymentFeeAmount,
  getTopupCreditAmount,
  StripeAcceptedPaymentMethods,
} from '../lib'
import type {
  PaymentMethod,
  PresetAmount,
  QuotaDisplayType,
  TopupInfo,
} from '../types'

interface RechargeFormCardProps {
  topupInfo: TopupInfo | null
  presetAmounts: PresetAmount[]
  selectedPreset: number | null
  onSelectPreset: (preset: PresetAmount) => void
  topupAmount: number
  paymentAmount: number
  calculating: boolean
  onPaymentMethodChange: (method: PaymentMethod) => void
  selectedPaymentMethod?: PaymentMethod
  onPayNow: () => void
  paying: boolean
  loading?: boolean
  priceRatio?: number
  usdExchangeRate?: number
  feeRate?: number
  epusdtCreditPerUsdt?: number
  quotaDisplayType?: QuotaDisplayType
  quotaPerUnit?: number
  onOpenBilling?: () => void
}

function StepHeader({
  step,
  title,
  description,
}: {
  step: number
  title: string
  description?: string
}) {
  return (
    <div className='flex items-start gap-3'>
      <span className='bg-primary/10 text-primary flex size-7 shrink-0 items-center justify-center rounded-full text-sm font-semibold'>
        {step}
      </span>
      <div className='min-w-0 space-y-0.5'>
        <h3 className='text-sm font-semibold sm:text-base'>{title}</h3>
        {description ? (
          <p className='text-muted-foreground text-xs sm:text-sm'>
            {description}
          </p>
        ) : null}
      </div>
    </div>
  )
}

function SummaryRow({
  label,
  value,
  loading,
  emphasize,
}: {
  label: string
  value: ReactNode
  loading?: boolean
  emphasize?: boolean
}) {
  return (
    <div className='flex items-center justify-between gap-3 text-sm'>
      <span className='text-muted-foreground shrink-0'>{label}</span>
      {loading ? (
        <Skeleton className='h-5 w-20' />
      ) : (
        <span
          className={cn(
            'text-right font-medium',
            emphasize && 'text-base font-semibold sm:text-lg'
          )}
        >
          {value}
        </span>
      )}
    </div>
  )
}

export function RechargeFormCard({
  topupInfo,
  presetAmounts,
  selectedPreset,
  onSelectPreset,
  topupAmount,
  paymentAmount,
  calculating,
  onPaymentMethodChange,
  selectedPaymentMethod,
  onPayNow,
  paying,
  loading,
  priceRatio = 1,
  usdExchangeRate = 1,
  feeRate = 0,
  epusdtCreditPerUsdt,
  quotaDisplayType,
  quotaPerUnit,
  onOpenBilling,
}: RechargeFormCardProps) {
  const { t } = useTranslation()
  const quotaCurrencyLabel =
    quotaDisplayType === 'TOKENS' ? t('Tokens') : getCurrencyLabel()
  const paymentFormatOptions = {
    digitsLarge: 2,
    digitsSmall: 2,
    abbreviate: false,
    stripeCurrency: topupInfo?.stripe_currency,
  } as const

  const formatQuotaAmount = (amount: number) =>
    quotaDisplayType === 'TOKENS'
      ? formatQuotaWithCurrency(amount, paymentFormatOptions)
      : formatCurrencyFromUSD(amount, paymentFormatOptions)

  const formatPayAmountLabel = (amount: number, paymentType?: string) =>
    formatPaymentChargeAmount(amount, paymentType, paymentFormatOptions)
  const isMethodEnabled = (method: PaymentMethod) =>
    method.enabled !== false && method.enabled !== 'false'

  const hasStandardPaymentMethods =
    Array.isArray(topupInfo?.pay_methods) &&
    topupInfo.pay_methods.some((method) => isMethodEnabled(method))
  const visiblePaymentMethods = (topupInfo?.pay_methods || []).filter(
    (method) => isMethodEnabled(method)
  )
  const hasAnyTopup = hasStandardPaymentMethods
  const minTopup = getMinTopupAmount(topupInfo)
  const selectedPaymentType = selectedPaymentMethod?.type
  const isUsdtSelected = isUsdtPayment(selectedPaymentType || '')
  const displayPaymentAmount = getDisplayPaymentAmount(
    selectedPaymentType,
    topupAmount,
    paymentAmount,
    feeRate,
    epusdtCreditPerUsdt,
    quotaDisplayType,
    quotaPerUnit
  )
  const paymentFeeAmount = calculatePaymentFeeAmount(
    displayPaymentAmount,
    feeRate
  )
  const feePercentage = Math.round(feeRate * 10000) / 100
  const hasPaymentSelection = !!selectedPaymentMethod
  const selectedMethodLabel = selectedPaymentMethod?.name
  const amountBelowMin = topupAmount < minTopup
  const canPay =
    hasPaymentSelection && !amountBelowMin && !calculating && !paying

  const renderStandardPaymentMethods = () =>
    hasStandardPaymentMethods ? (
      <div className='grid grid-cols-1 gap-2 sm:grid-cols-2'>
        {visiblePaymentMethods.map((method) => {
          const methodMinTopup = method.min_topup || 0
          const disabled = methodMinTopup > topupAmount
          const isSelected = selectedPaymentMethod?.type === method.type

          const card = (
            <button
              key={method.type}
              type='button'
              onClick={() => onPaymentMethodChange(method)}
              disabled={disabled || paying}
              className={cn(
                'relative flex min-h-14 w-full items-center gap-3 rounded-xl border px-3 py-3 text-left transition-colors sm:min-h-16 sm:px-4',
                isSelected
                  ? 'border-primary bg-primary/5 ring-primary/20 ring-1'
                  : 'border-border hover:border-foreground/30 hover:bg-muted/40',
                disabled && 'cursor-not-allowed opacity-50'
              )}
            >
              <span className='bg-background flex size-10 shrink-0 items-center justify-center rounded-lg border'>
                {getPaymentIcon(
                  method.type,
                  'h-5 w-5',
                  method.icon,
                  method.name
                )}
              </span>
              <span className='min-w-0 flex-1'>
                <span className='block truncate text-sm font-medium'>
                  {method.name}
                </span>
                {method.tag ? (
                  <span className='text-primary mt-0.5 inline-block text-[11px]'>
                    {method.tag}
                  </span>
                ) : null}
              </span>
              {method.type === 'stripe' ? (
                <StripeAcceptedPaymentMethods />
              ) : null}
              {isSelected ? (
                <span className='bg-primary text-primary-foreground flex size-5 shrink-0 items-center justify-center rounded-full'>
                  <Check className='size-3' />
                </span>
              ) : (
                <span className='border-muted-foreground/40 size-5 shrink-0 rounded-full border' />
              )}
            </button>
          )

          return disabled ? (
            <TooltipProvider key={method.type}>
              <Tooltip>
                <TooltipTrigger render={card} />
                <TooltipContent>
                  {t('Minimum topup amount: {{amount}}', {
                    amount: methodMinTopup,
                  })}
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          ) : (
            card
          )
        })}
      </div>
    ) : null

  if (loading) {
    return (
      <Card className='gap-0 overflow-hidden py-0'>
        <CardHeader className='border-b p-3 !pb-3 sm:p-5 sm:!pb-5'>
          <Skeleton className='h-6 w-32' />
          <Skeleton className='mt-2 h-4 w-48' />
        </CardHeader>
        <CardContent className='space-y-4 p-3 sm:space-y-6 sm:p-5'>
          <div className='space-y-4 sm:space-y-6'>
            <div className='space-y-3'>
              <Skeleton className='h-3 w-32' />
              <div className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
                {Array.from({ length: 8 }).map((_, i) => (
                  <Skeleton key={i} className='h-[72px] rounded-lg' />
                ))}
              </div>
            </div>
            <div className='space-y-3'>
              <Skeleton className='h-3 w-28' />
              <Skeleton className='h-[42px] w-full' />
            </div>
            <div className='space-y-3'>
              <Skeleton className='h-3 w-32' />
              <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className='h-16 rounded-xl' />
                ))}
              </div>
            </div>
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <TitledCard
      title={t('Add Funds')}
      description={t('Select amount and payment method, then confirm to pay')}
      headerClassName='bg-muted/70 border-b'
      titleClassName='font-bold'
      contentClassName='space-y-5 bg-background sm:space-y-6'
      action={
        onOpenBilling ? (
          <Button
            variant='outline'
            size='sm'
            onClick={onOpenBilling}
            className='w-full gap-2 sm:w-auto'
          >
            <Receipt className='h-4 w-4' />
            {t('Order History')}
          </Button>
        ) : null
      }
    >
      {hasAnyTopup ? (
        <>
          <section className='space-y-3 sm:space-y-4'>
            <StepHeader step={1} title={t('Select top-up amount')} />

            {presetAmounts.length > 0 ? (
              <div className='space-y-2'>
                <Label className='text-muted-foreground text-xs font-medium tracking-wide'>
                  {t('Top-up quota ({{currency}})', {
                    currency: quotaCurrencyLabel,
                  })}
                </Label>
                <div className='grid grid-cols-2 gap-2 sm:gap-3 md:grid-cols-4'>
                  {presetAmounts.map((preset, index) => {
                    const discount =
                      preset.discount ||
                      topupInfo?.discount?.[preset.value] ||
                      1.0
                    const creditAmount = getTopupCreditAmount(
                      preset.value,
                      quotaDisplayType,
                      quotaPerUnit
                    )
                    const pricing = calculatePresetPricing(
                      creditAmount,
                      priceRatio,
                      discount,
                      usdExchangeRate
                    )
                    const presetFeeRate = getAmountFeeRate(
                      topupInfo,
                      preset.value,
                      selectedPaymentType
                    )
                    const actualPrice = isUsdtSelected
                      ? calculateEpusdtPayAmount(
                          creditAmount,
                          presetFeeRate,
                          epusdtCreditPerUsdt
                        )
                      : (selectedPaymentType === 'stripe'
                          ? creditAmount *
                            (topupInfo?.topup_group_ratio || 1) *
                            discount
                          : pricing.actualPrice *
                            (topupInfo?.topup_group_ratio || 1)) *
                        (1 + presetFeeRate)
                    const hasDiscount = isUsdtSelected
                      ? false
                      : pricing.hasDiscount

                    return (
                      <Button
                        key={index}
                        variant='outline'
                        className={cn(
                          'hover:border-primary/50 flex min-h-[4.75rem] flex-col items-center justify-center rounded-xl px-2 py-2.5 text-center whitespace-normal sm:min-h-20',
                          selectedPreset === preset.value
                            ? 'border-primary bg-primary/5 ring-primary/20 ring-1'
                            : 'border-muted'
                        )}
                        onClick={() => onSelectPreset(preset)}
                      >
                        <div className='text-lg font-semibold sm:text-xl'>
                          {formatQuotaAmount(preset.value)}
                        </div>
                        <div className='text-muted-foreground mt-1 text-[11px] sm:text-xs'>
                          {hasDiscount ? (
                            <>
                              <span className='text-green-600'>
                                {getDiscountLabel(discount)}
                              </span>
                              {' · '}
                            </>
                          ) : null}
                          {presetFeeRate > 0 ? (
                            <>
                              {t('Fee {{percentage}}%', {
                                percentage:
                                  Math.round(presetFeeRate * 10000) / 100,
                              })}
                              {' · '}
                            </>
                          ) : null}
                          {t('Pay {{amount}}', {
                            amount: formatPayAmountLabel(
                              actualPrice,
                              selectedPaymentType
                            ),
                          })}
                        </div>
                      </Button>
                    )
                  })}
                </div>
              </div>
            ) : null}
          </section>

          <section className='space-y-3 border-t pt-5 sm:space-y-4 sm:pt-6'>
            <StepHeader
              step={2}
              title={t('Select payment method')}
              description={t(
                'Tap a method below — payment starts only after you confirm'
              )}
            />

            {renderStandardPaymentMethods()}

            {!hasStandardPaymentMethods ? (
              <Alert>
                <AlertDescription>
                  {t(
                    'No payment methods available. Please contact administrator.'
                  )}
                </AlertDescription>
              </Alert>
            ) : null}
          </section>

          <section className='bg-muted/30 space-y-4 rounded-xl border p-4 sm:p-5'>
            <h3 className='text-sm font-semibold'>{t('Order summary')}</h3>
            {feeRate > 0 ? (
              <Alert className='bg-warning/5'>
                <AlertDescription>
                  {t(
                    'A {{percentage}}% processing fee applies to this recharge amount. The final amount is shown below.',
                    { percentage: feePercentage }
                  )}
                </AlertDescription>
              </Alert>
            ) : null}
            <div className='space-y-2.5'>
              <SummaryRow
                label={t('Topup Amount ({{currency}})', {
                  currency: quotaCurrencyLabel,
                })}
                value={formatQuotaAmount(topupAmount)}
              />
              {feeRate > 0 && paymentFeeAmount > 0 ? (
                <SummaryRow
                  label={t('Processing fee ({{percentage}}%)', {
                    percentage: feePercentage,
                  })}
                  value={formatPaymentChargeAmount(
                    paymentFeeAmount,
                    selectedPaymentType,
                    paymentFormatOptions
                  )}
                />
              ) : null}
              <SummaryRow
                label={
                  isUsdtSelected
                    ? t('Amount to pay (USDT):')
                    : t('Amount to pay:')
                }
                value={formatPaymentChargeAmount(
                  displayPaymentAmount,
                  selectedPaymentType,
                  paymentFormatOptions
                )}
                loading={calculating}
                emphasize
              />
              <SummaryRow
                label={t('Payment Method')}
                value={
                  selectedMethodLabel || (
                    <span className='text-muted-foreground font-normal'>
                      {t('Not selected')}
                    </span>
                  )
                }
              />
            </div>

            <Button
              size='lg'
              className='h-12 w-full text-base font-semibold'
              onClick={onPayNow}
              disabled={!canPay}
            >
              {paying ? (
                <Loader2 className='mr-2 h-5 w-5 animate-spin' />
              ) : null}
              {t('Pay now')}
              {!calculating && hasPaymentSelection ? (
                <span className='ml-2 opacity-90'>
                  ·{' '}
                  {formatPaymentChargeAmount(
                    displayPaymentAmount,
                    selectedPaymentType,
                    paymentFormatOptions
                  )}
                </span>
              ) : null}
            </Button>

            <p className='text-muted-foreground text-center text-sm'>
              {t('You will be redirected to complete payment after confirming')}
            </p>
          </section>
        </>
      ) : (
        <Alert>
          <AlertDescription>
            {t(
              'Online topup is not enabled. Please use redemption code or contact administrator.'
            )}
          </AlertDescription>
        </Alert>
      )}
    </TitledCard>
  )
}
