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
import * as React from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Code2, Eye, ShieldAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { RiskAcknowledgementDialog } from '@/components/risk-acknowledgement-dialog'
import { StatusBadge } from '@/components/status-badge'
import { confirmPaymentCompliance } from '../api'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { AmountDiscountVisualEditor } from './amount-discount-visual-editor'
import { AmountOptionsVisualEditor } from './amount-options-visual-editor'
import { PaymentMethodsVisualEditor } from './payment-methods-visual-editor'
import {
  formatJsonForEditor,
  getJsonError,
  isLegacyEpusdtGatewayConflict,
  normalizeJsonForComparison,
  removeTrailingSlash,
  resolveEpusdtGatewayAddress,
} from './utils'
import { normalizePayMethodsJson } from '../utils/normalize-pay-methods'

const paymentSchema = z.object({
  EpUSDTGatewayAddress: z.string().refine((value) => {
    const trimmed = value.trim()
    if (!trimmed) return true
    return /^https?:\/\//.test(trimmed)
  }, 'Provide a valid URL starting with http:// or https://'),
  EpUSDTApiToken: z.string(),
  EpUSDTNotifyURL: z.string().refine((value) => {
    const trimmed = value.trim()
    if (!trimmed) return true
    return /^https?:\/\//.test(trimmed)
  }, 'Provide a valid URL starting with http:// or https://'),
  Price: z.coerce.number().min(0),
  MinTopUp: z.coerce.number().min(0),
  PayMethods: z.string().superRefine((value, ctx) => {
    const error = getJsonError(value)
    if (error) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: error,
      })
    }
  }),
  AmountOptions: z.string().superRefine((value, ctx) => {
    const error = getJsonError(value, (parsed) => Array.isArray(parsed))
    if (error) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: error,
      })
    }
  }),
  AmountDiscount: z.string().superRefine((value, ctx) => {
    const error = getJsonError(
      value,
      (parsed) =>
        !!parsed && typeof parsed === 'object' && !Array.isArray(parsed)
    )
    if (error) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: error,
      })
    }
  }),
  StripeApiSecret: z.string(),
  StripeWebhookSecret: z.string(),
  StripePriceId: z.string(),
  StripeCurrency: z.enum(['usd', 'cny', 'hkd']),
  StripeMinTopUp: z.coerce.number().min(0),
  StripePromotionCodesEnabled: z.boolean(),
})

type PaymentFormValues = z.infer<typeof paymentSchema>

const CURRENT_COMPLIANCE_TERMS_VERSION = 'v1'

type PaymentComplianceDefaults = {
  confirmed: boolean
  termsVersion: string
  confirmedAt: number
  confirmedBy: number
}

type PaymentSecretConfigured = {
  stripeApiSecret: boolean
  stripeWebhookSecret: boolean
  epusdtApiToken: boolean
}

type PaymentSettingsSectionProps = {
  defaultValues: PaymentFormValues
  complianceDefaults: PaymentComplianceDefaults
  legacyPayAddress?: string
  legacyEpayConfigured?: boolean
  legacyPayMethods?: string
  secretConfigured: PaymentSecretConfigured
}

function SecretConfiguredBadge({
  configured,
  value,
}: {
  configured: boolean
  value: string
}) {
  const { t } = useTranslation()

  if (!configured || value.trim()) {
    return null
  }

  return (
    <StatusBadge
      label={t('Key configured')}
      variant='success'
      size='sm'
      showDot
      copyable={false}
    />
  )
}

export function PaymentSettingsSection({
  defaultValues,
  complianceDefaults,
  legacyPayAddress = '',
  legacyEpayConfigured = false,
  legacyPayMethods = '',
  secretConfigured,
}: PaymentSettingsSectionProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const updateOption = useUpdateOption()
  const initialRef = React.useRef(defaultValues)
  const defaultsSignature = React.useMemo(
    () => JSON.stringify(defaultValues),
    [defaultValues]
  )

  const [payMethodsVisualMode, setPayMethodsVisualMode] = React.useState(true)
  const [amountOptionsVisualMode, setAmountOptionsVisualMode] =
    React.useState(true)
  const [amountDiscountVisualMode, setAmountDiscountVisualMode] =
    React.useState(true)
  const [showComplianceDialog, setShowComplianceDialog] = React.useState(false)

  const resolvedLegacyPayMethods =
    legacyPayMethods.trim() || defaultValues.PayMethods

  const hasLegacyEpusdtGateway = React.useMemo(
    () =>
      isLegacyEpusdtGatewayConflict(
        defaultValues.EpUSDTGatewayAddress,
        legacyPayAddress,
        legacyEpayConfigured,
        resolvedLegacyPayMethods
      ),
    [
      defaultValues.EpUSDTGatewayAddress,
      defaultValues.PayMethods,
      legacyPayAddress,
      legacyEpayConfigured,
      legacyPayMethods,
      resolvedLegacyPayMethods,
    ]
  )

  const complianceStatements = React.useMemo(
    () => [
      t(
        'You have legally obtained authorization for the connected model APIs, accounts, keys, and quotas.'
      ),
      t(
        'You commit to using upstream APIs, accounts, keys, quotas, and service capabilities only within the scope of lawful authorization obtained from upstream service providers, model service providers, or relevant rights holders, and will not conduct unauthorized resale, trafficking, distribution, or other non-compliant commercialization.'
      ),
      t(
        'If you provide generative AI services to the public in mainland China, you will fulfill legal obligations including filing, security assessment, content safety, complaint handling, generated content labeling, log retention, and personal information protection.'
      ),
      t(
        'You commit not to use this system to implement, assist with, or indirectly implement acts that violate applicable laws and regulations, regulatory requirements, platform rules, public interests, or the lawful rights and interests of third parties.'
      ),
      t(
        'You understand and independently bear legal responsibility arising from deployment, operation, and charging behavior.'
      ),
      t(
        'You understand this compliance reminder is only for risk notice and does not constitute legal advice, a compliance review conclusion, or a guarantee of the legality of your use of this system; you should consult professional legal or compliance advisors based on your actual business scenario.'
      ),
    ],
    [t]
  )

  const complianceRequiredText = t(
    'I have read and understood the above compliance reminder, acknowledge the related legal risks, and confirm that I bear legal responsibility arising from deployment, operation, and charging behavior.'
  )
  const complianceRequiredTextParts = React.useMemo(
    () => [
      {
        type: 'input' as const,
        text: t('I have read and understood the above compliance reminder'),
      },
      { type: 'static' as const, text: t('，') },
      {
        type: 'input' as const,
        text: t('acknowledge the related legal risks'),
      },
      { type: 'static' as const, text: t('，and ') },
      {
        type: 'input' as const,
        text: t(
          'confirm that I bear legal responsibility arising from deployment'
        ),
      },
      { type: 'static' as const, text: t('、') },
      {
        type: 'input' as const,
        text: t('operation and charging behavior'),
      },
    ],
    [t]
  )

  const complianceConfirmed =
    complianceDefaults.confirmed &&
    complianceDefaults.termsVersion === CURRENT_COMPLIANCE_TERMS_VERSION

  const confirmComplianceMutation = useMutation({
    mutationFn: confirmPaymentCompliance,
    onSuccess: (data) => {
      if (data.success) {
        toast.success(t('Compliance confirmed successfully'))
        setShowComplianceDialog(false)
        queryClient.invalidateQueries({ queryKey: ['system-options'] })
      } else {
        toast.error(data.message || t('Failed to confirm compliance'))
      }
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to confirm compliance'))
    },
  })

  const form = useForm({
    resolver: zodResolver(paymentSchema),
    mode: 'onChange', // Enable real-time validation
    defaultValues: {
      ...defaultValues,
      PayMethods: formatJsonForEditor(defaultValues.PayMethods),
      AmountOptions: formatJsonForEditor(defaultValues.AmountOptions),
      AmountDiscount: formatJsonForEditor(defaultValues.AmountDiscount),
    },
  })

  React.useEffect(() => {
    const parsedDefaults = JSON.parse(defaultsSignature) as PaymentFormValues
    initialRef.current = parsedDefaults
    form.reset({
      ...parsedDefaults,
      PayMethods: formatJsonForEditor(parsedDefaults.PayMethods),
      AmountOptions: formatJsonForEditor(parsedDefaults.AmountOptions),
      AmountDiscount: formatJsonForEditor(parsedDefaults.AmountDiscount),
    })
  }, [defaultsSignature, form])

  const saveGeneralSettings = async () => {
    const values = form.getValues()
    const sanitized = {
      Price: values.Price as number,
      MinTopUp: values.MinTopUp as number,
      PayMethods: normalizePayMethodsJson(values.PayMethods.trim()),
      AmountOptions: values.AmountOptions.trim(),
      AmountDiscount: values.AmountDiscount.trim(),
    }

    const initial = {
      Price: initialRef.current.Price,
      MinTopUp: initialRef.current.MinTopUp,
      PayMethods: initialRef.current.PayMethods.trim(),
      AmountOptions: initialRef.current.AmountOptions.trim(),
      AmountDiscount: initialRef.current.AmountDiscount.trim(),
    }

    const updates: Array<{ key: string; value: string | number }> = []

    if (sanitized.Price !== initial.Price) {
      updates.push({ key: 'Price', value: sanitized.Price })
    }

    if (sanitized.MinTopUp !== initial.MinTopUp) {
      updates.push({ key: 'MinTopUp', value: sanitized.MinTopUp })
    }

    if (
      normalizeJsonForComparison(sanitized.PayMethods) !==
      normalizeJsonForComparison(initial.PayMethods)
    ) {
      updates.push({ key: 'PayMethods', value: sanitized.PayMethods })
    }

    if (
      normalizeJsonForComparison(sanitized.AmountOptions) !==
      normalizeJsonForComparison(initial.AmountOptions)
    ) {
      updates.push({
        key: 'payment_setting.amount_options',
        value: sanitized.AmountOptions,
      })
    }

    if (
      normalizeJsonForComparison(sanitized.AmountDiscount) !==
      normalizeJsonForComparison(initial.AmountDiscount)
    ) {
      updates.push({
        key: 'payment_setting.amount_discount',
        value: sanitized.AmountDiscount,
      })
    }

    if (updates.length === 0) {
      return
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
  }

  const rejectLegacyEpusdtGateway = (gatewayAddress: string) => {
    if (
      isLegacyEpusdtGatewayConflict(
        gatewayAddress,
        legacyPayAddress,
        legacyEpayConfigured,
        form.getValues('PayMethods') || resolvedLegacyPayMethods
      )
    ) {
      toast.error(
        t(
          'EpUSDT gateway address cannot be the same as the legacy Epay endpoint. Please enter your BEpusdt gateway URL.'
        )
      )
      return true
    }
    return false
  }

  const saveEpusdtSettings = async () => {
    const values = form.getValues()
    const sanitized = {
      EpUSDTGatewayAddress: resolveEpusdtGatewayAddress(
        values.EpUSDTGatewayAddress
      ),
      EpUSDTApiToken: values.EpUSDTApiToken.trim(),
      EpUSDTNotifyURL: removeTrailingSlash(values.EpUSDTNotifyURL),
    }

    if (rejectLegacyEpusdtGateway(sanitized.EpUSDTGatewayAddress)) {
      return
    }

    const initial = {
      EpUSDTGatewayAddress: removeTrailingSlash(
        initialRef.current.EpUSDTGatewayAddress
      ),
      EpUSDTApiToken: initialRef.current.EpUSDTApiToken.trim(),
      EpUSDTNotifyURL: removeTrailingSlash(initialRef.current.EpUSDTNotifyURL),
    }

    const updates: Array<{ key: string; value: string }> = []

    if (sanitized.EpUSDTGatewayAddress !== initial.EpUSDTGatewayAddress) {
      updates.push({
        key: 'EpUSDTGatewayAddress',
        value: sanitized.EpUSDTGatewayAddress,
      })
    }

    if (
      sanitized.EpUSDTApiToken &&
      sanitized.EpUSDTApiToken !== initial.EpUSDTApiToken
    ) {
      updates.push({
        key: 'EpUSDTApiToken',
        value: sanitized.EpUSDTApiToken,
      })
    }

    if (sanitized.EpUSDTNotifyURL !== initial.EpUSDTNotifyURL) {
      updates.push({
        key: 'EpUSDTNotifyURL',
        value: sanitized.EpUSDTNotifyURL,
      })
    }

    if (updates.length === 0) {
      return
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }

    initialRef.current = {
      ...initialRef.current,
      EpUSDTGatewayAddress: sanitized.EpUSDTGatewayAddress,
      EpUSDTApiToken: sanitized.EpUSDTApiToken,
      EpUSDTNotifyURL: sanitized.EpUSDTNotifyURL,
    }
  }

  const saveStripeSettings = async () => {
    const values = form.getValues()
    const sanitized = {
      StripeApiSecret: values.StripeApiSecret.trim(),
      StripeWebhookSecret: values.StripeWebhookSecret.trim(),
      StripePriceId: values.StripePriceId.trim(),
      StripeCurrency: values.StripeCurrency,
      StripeMinTopUp: values.StripeMinTopUp as number,
      StripePromotionCodesEnabled:
        values.StripePromotionCodesEnabled as boolean,
    }

    const initial = {
      StripeApiSecret: initialRef.current.StripeApiSecret.trim(),
      StripeWebhookSecret: initialRef.current.StripeWebhookSecret.trim(),
      StripePriceId: initialRef.current.StripePriceId.trim(),
      StripeCurrency: initialRef.current.StripeCurrency,
      StripeMinTopUp: initialRef.current.StripeMinTopUp,
      StripePromotionCodesEnabled:
        initialRef.current.StripePromotionCodesEnabled,
    }

    const updates: Array<{ key: string; value: string | number | boolean }> = []

    if (
      sanitized.StripeApiSecret &&
      sanitized.StripeApiSecret !== initial.StripeApiSecret
    ) {
      updates.push({ key: 'StripeApiSecret', value: sanitized.StripeApiSecret })
    }

    if (
      sanitized.StripeWebhookSecret &&
      sanitized.StripeWebhookSecret !== initial.StripeWebhookSecret
    ) {
      updates.push({
        key: 'StripeWebhookSecret',
        value: sanitized.StripeWebhookSecret,
      })
    }

    if (sanitized.StripePriceId !== initial.StripePriceId) {
      updates.push({ key: 'StripePriceId', value: sanitized.StripePriceId })
    }

    if (sanitized.StripeCurrency !== initial.StripeCurrency) {
      updates.push({ key: 'StripeCurrency', value: sanitized.StripeCurrency })
    }

    if (sanitized.StripeMinTopUp !== initial.StripeMinTopUp) {
      updates.push({ key: 'StripeMinTopUp', value: sanitized.StripeMinTopUp })
    }

    if (
      sanitized.StripePromotionCodesEnabled !==
      initial.StripePromotionCodesEnabled
    ) {
      updates.push({
        key: 'StripePromotionCodesEnabled',
        value: sanitized.StripePromotionCodesEnabled,
      })
    }

    if (updates.length === 0) {
      return
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
  }

  const onSubmit = async (values: PaymentFormValues) => {
    const epusdtGatewayAddress = resolveEpusdtGatewayAddress(
      values.EpUSDTGatewayAddress
    )
    if (rejectLegacyEpusdtGateway(epusdtGatewayAddress)) {
      return
    }

    const sanitized = {
      EpUSDTGatewayAddress: epusdtGatewayAddress,
      EpUSDTApiToken: values.EpUSDTApiToken.trim(),
      EpUSDTNotifyURL: removeTrailingSlash(values.EpUSDTNotifyURL),
      Price: values.Price,
      MinTopUp: values.MinTopUp,
      PayMethods: normalizePayMethodsJson(values.PayMethods.trim()),
      AmountOptions: values.AmountOptions.trim(),
      AmountDiscount: values.AmountDiscount.trim(),
      StripeApiSecret: values.StripeApiSecret.trim(),
      StripeWebhookSecret: values.StripeWebhookSecret.trim(),
      StripePriceId: values.StripePriceId.trim(),
      StripeCurrency: values.StripeCurrency,
      StripeMinTopUp: values.StripeMinTopUp,
      StripePromotionCodesEnabled: values.StripePromotionCodesEnabled,
    }

    const initial = {
      EpUSDTGatewayAddress: removeTrailingSlash(
        initialRef.current.EpUSDTGatewayAddress
      ),
      EpUSDTApiToken: initialRef.current.EpUSDTApiToken.trim(),
      EpUSDTNotifyURL: removeTrailingSlash(initialRef.current.EpUSDTNotifyURL),
      Price: initialRef.current.Price,
      MinTopUp: initialRef.current.MinTopUp,
      PayMethods: initialRef.current.PayMethods.trim(),
      AmountOptions: initialRef.current.AmountOptions.trim(),
      AmountDiscount: initialRef.current.AmountDiscount.trim(),
      StripeApiSecret: initialRef.current.StripeApiSecret.trim(),
      StripeWebhookSecret: initialRef.current.StripeWebhookSecret.trim(),
      StripePriceId: initialRef.current.StripePriceId.trim(),
      StripeCurrency: initialRef.current.StripeCurrency,
      StripeMinTopUp: initialRef.current.StripeMinTopUp,
      StripePromotionCodesEnabled:
        initialRef.current.StripePromotionCodesEnabled,
    }

    const updates: Array<{ key: string; value: string | number | boolean }> = []

    if (sanitized.EpUSDTGatewayAddress !== initial.EpUSDTGatewayAddress) {
      updates.push({
        key: 'EpUSDTGatewayAddress',
        value: sanitized.EpUSDTGatewayAddress,
      })
    }

    if (
      sanitized.EpUSDTApiToken &&
      sanitized.EpUSDTApiToken !== initial.EpUSDTApiToken
    ) {
      updates.push({
        key: 'EpUSDTApiToken',
        value: sanitized.EpUSDTApiToken,
      })
    }

    if (sanitized.EpUSDTNotifyURL !== initial.EpUSDTNotifyURL) {
      updates.push({
        key: 'EpUSDTNotifyURL',
        value: sanitized.EpUSDTNotifyURL,
      })
    }

    if (sanitized.Price !== initial.Price) {
      updates.push({ key: 'Price', value: sanitized.Price })
    }

    if (sanitized.MinTopUp !== initial.MinTopUp) {
      updates.push({ key: 'MinTopUp', value: sanitized.MinTopUp })
    }

    if (
      normalizeJsonForComparison(sanitized.PayMethods) !==
      normalizeJsonForComparison(initial.PayMethods)
    ) {
      updates.push({ key: 'PayMethods', value: sanitized.PayMethods })
    }

    if (
      normalizeJsonForComparison(sanitized.AmountOptions) !==
      normalizeJsonForComparison(initial.AmountOptions)
    ) {
      updates.push({
        key: 'payment_setting.amount_options',
        value: sanitized.AmountOptions,
      })
    }

    if (
      normalizeJsonForComparison(sanitized.AmountDiscount) !==
      normalizeJsonForComparison(initial.AmountDiscount)
    ) {
      updates.push({
        key: 'payment_setting.amount_discount',
        value: sanitized.AmountDiscount,
      })
    }

    if (
      sanitized.StripeApiSecret &&
      sanitized.StripeApiSecret !== initial.StripeApiSecret
    ) {
      updates.push({ key: 'StripeApiSecret', value: sanitized.StripeApiSecret })
    }

    if (
      sanitized.StripeWebhookSecret &&
      sanitized.StripeWebhookSecret !== initial.StripeWebhookSecret
    ) {
      updates.push({
        key: 'StripeWebhookSecret',
        value: sanitized.StripeWebhookSecret,
      })
    }

    if (sanitized.StripePriceId !== initial.StripePriceId) {
      updates.push({ key: 'StripePriceId', value: sanitized.StripePriceId })
    }

    if (sanitized.StripeCurrency !== initial.StripeCurrency) {
      updates.push({ key: 'StripeCurrency', value: sanitized.StripeCurrency })
    }

    if (sanitized.StripeMinTopUp !== initial.StripeMinTopUp) {
      updates.push({ key: 'StripeMinTopUp', value: sanitized.StripeMinTopUp })
    }

    if (
      sanitized.StripePromotionCodesEnabled !==
      initial.StripePromotionCodesEnabled
    ) {
      updates.push({
        key: 'StripePromotionCodesEnabled',
        value: sanitized.StripePromotionCodesEnabled,
      })
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
  }

  return (
    <SettingsSection
      title={t('Payment Gateway')}
      description={t(
        'Configure recharge pricing and payment gateway integrations'
      )}
    >
      {!complianceConfirmed ? (
        <Alert variant='destructive' className='mb-6'>
          <ShieldAlert className='h-4 w-4' />
          <AlertTitle>{t('Compliance confirmation required')}</AlertTitle>
          <AlertDescription>
            <div className='space-y-3'>
              <p>
                {t(
                  'Payment, redemption codes, subscription plans, and invitation rewards are locked until the root administrator confirms the compliance terms.'
                )}
              </p>
              <ol className='list-decimal space-y-1 pl-5'>
                {complianceStatements.map((statement) => (
                  <li key={statement}>{statement}</li>
                ))}
              </ol>
            </div>
          </AlertDescription>
          <AlertAction>
            <Button
              type='button'
              size='sm'
              variant='destructive'
              onClick={() => setShowComplianceDialog(true)}
            >
              {t('Confirm compliance')}
            </Button>
          </AlertAction>
        </Alert>
      ) : (
        <Alert className='mb-6'>
          <AlertTitle>{t('Compliance confirmed')}</AlertTitle>
          <AlertDescription>
            {t('Confirmed at {{time}} by user #{{userId}}', {
              time: complianceDefaults.confirmedAt
                ? new Date(
                    complianceDefaults.confirmedAt * 1000
                  ).toLocaleString()
                : '-',
              userId: complianceDefaults.confirmedBy || '-',
            })}
          </AlertDescription>
        </Alert>
      )}

      <RiskAcknowledgementDialog
        open={showComplianceDialog}
        onOpenChange={setShowComplianceDialog}
        title={t('Confirm compliance terms')}
        description={t(
          'This confirmation unlocks payment, redemption code, subscription plan, and invitation reward features. Please read the statements carefully.'
        )}
        items={complianceStatements}
        requiredText={complianceRequiredText}
        requiredTextParts={complianceRequiredTextParts}
        inputPrompt={t('Please type the following text to confirm:')}
        inputPlaceholder={t('Type the confirmation text here')}
        mismatchHint={t('The entered text does not match the required text.')}
        confirmText={t('Confirm and enable')}
        isLoading={confirmComplianceMutation.isPending}
        onConfirm={() => confirmComplianceMutation.mutate()}
      />

      {/* eslint-disable react-hooks/refs */}
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit(onSubmit)}
          className={cn(
            'space-y-8',
            !complianceConfirmed && 'pointer-events-none opacity-40'
          )}
          data-no-autosubmit='true'
        >
          <div className='space-y-4'>
            <div>
              <h3 className='text-lg font-medium'>{t('General Settings')}</h3>
              <p className='text-muted-foreground text-sm'>
                {t('Shared configuration for all payment gateways')}
              </p>
            </div>

            <div className='grid gap-6 md:grid-cols-2'>
              <FormField
                control={form.control}
                name='Price'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Price (local currency / USD)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        step='0.01'
                        min={0}
                        value={(field.value ?? 0) as number}
                        onChange={(event) =>
                          field.onChange(event.target.valueAsNumber)
                        }
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'How much to charge for each US dollar of balance (USDT)'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='MinTopUp'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Minimum top-up (USD)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        step='0.01'
                        min={0}
                        value={(field.value ?? 0) as number}
                        onChange={(event) =>
                          field.onChange(event.target.valueAsNumber)
                        }
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Smallest USD amount users can recharge (USDT)')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <FormField
              control={form.control}
              name='PayMethods'
              render={({ field }) => (
                <FormItem>
                  <div className='mb-2 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
                    <FormLabel>{t('Payment methods')}</FormLabel>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      onClick={() =>
                        setPayMethodsVisualMode(!payMethodsVisualMode)
                      }
                      className='w-full sm:w-auto'
                    >
                      {payMethodsVisualMode ? (
                        <>
                          <Code2 className='mr-2 h-3 w-3' />
                          {t('JSON Editor')}
                        </>
                      ) : (
                        <>
                          <Eye className='mr-2 h-3 w-3' />
                          {t('Visual Editor')}
                        </>
                      )}
                    </Button>
                  </div>
                  <FormControl>
                    {payMethodsVisualMode ? (
                      <PaymentMethodsVisualEditor
                        value={field.value}
                        onChange={field.onChange}
                      />
                    ) : (
                      <Textarea
                        rows={4}
                        placeholder={t(
                          '[{"name":"USDT","type":"usdt","color":"#26A17B","enabled":"true"},{"name":"Stripe","type":"stripe","color":"#635BFF","enabled":"true"}]'
                        )}
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    )}
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Configure available payment methods. Provide a JSON array.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <div className='grid gap-6 md:grid-cols-2 md:items-start'>
              <FormField
                control={form.control}
                name='AmountOptions'
                render={({ field }) => (
                  <FormItem>
                    <div className='mb-2 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
                      <FormLabel>{t('Top-up amount options')}</FormLabel>
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        onClick={() =>
                          setAmountOptionsVisualMode(!amountOptionsVisualMode)
                        }
                        className='w-full sm:w-auto'
                      >
                        {amountOptionsVisualMode ? (
                          <>
                            <Code2 className='mr-2 h-3 w-3' />
                            {t('JSON Editor')}
                          </>
                        ) : (
                          <>
                            <Eye className='mr-2 h-3 w-3' />
                            {t('Visual Editor')}
                          </>
                        )}
                      </Button>
                    </div>
                    <FormControl>
                      {amountOptionsVisualMode ? (
                        <AmountOptionsVisualEditor
                          value={field.value}
                          onChange={field.onChange}
                        />
                      ) : (
                        <Textarea
                          rows={4}
                          placeholder='[10, 20, 50, 100]'
                          {...field}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                        />
                      )}
                    </FormControl>
                    <FormDescription>
                      {t('Preset recharge amounts (JSON array)')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='AmountDiscount'
                render={({ field }) => (
                  <FormItem>
                    <div className='mb-2 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
                      <FormLabel>{t('Amount discount')}</FormLabel>
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        onClick={() =>
                          setAmountDiscountVisualMode(!amountDiscountVisualMode)
                        }
                        className='w-full sm:w-auto'
                      >
                        {amountDiscountVisualMode ? (
                          <>
                            <Code2 className='mr-2 h-3 w-3' />
                            {t('JSON Editor')}
                          </>
                        ) : (
                          <>
                            <Eye className='mr-2 h-3 w-3' />
                            {t('Visual Editor')}
                          </>
                        )}
                      </Button>
                    </div>
                    <FormControl>
                      {amountDiscountVisualMode ? (
                        <AmountDiscountVisualEditor
                          value={field.value}
                          onChange={field.onChange}
                        />
                      ) : (
                        <Textarea
                          rows={4}
                          placeholder='{"100":0.95,"200":0.9}'
                          {...field}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                        />
                      )}
                    </FormControl>
                    <FormDescription>
                      {t('Discount map by recharge amount (JSON object)')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <Button
              type='button'
              onClick={(e) => {
                e.preventDefault()
                e.stopPropagation()
                saveGeneralSettings()
              }}
              disabled={updateOption.isPending}
            >
              {updateOption.isPending
                ? t('Saving...')
                : t('Save general settings')}
            </Button>
          </div>

          <Separator />

          <div className='space-y-4'>
            <div>
              <h3 className='text-lg font-medium'>{t('EpUSDT Gateway')}</h3>
              <p className='text-muted-foreground text-sm'>
                {t('Configuration for EpUSDT cryptocurrency payment integration')}
              </p>
            </div>

            {hasLegacyEpusdtGateway ? (
              <Alert variant='destructive'>
                <AlertTitle>
                  {t('Legacy EpUSDT gateway configuration detected')}
                </AlertTitle>
                <AlertDescription>
                  {t(
                    'The saved EpUSDT gateway address matches the legacy Epay endpoint and is invalid. Re-enter your BEpusdt gateway URL. EpUSDT webhooks always use your server address, not the Epay callback address.'
                  )}
                </AlertDescription>
              </Alert>
            ) : null}

            <div className='rounded-md bg-blue-50 p-4 text-sm text-blue-900 dark:bg-blue-950 dark:text-blue-100'>
              <p className='mb-2 font-medium'>{t('Webhook Configuration:')}</p>
              <ul className='list-inside list-disc space-y-1'>
                <li>
                  {t(
                    'Configure the notify URL below and paste the same value into your BEpusdt gateway.'
                  )}
                </li>
                <li>
                  {t('Default when blank:')}{' '}
                  <code className='rounded bg-blue-100 px-1 py-0.5 text-xs dark:bg-blue-900'>
                    {'<ServerAddress>/api/epusdt/notify'}
                  </code>
                </li>
                <li>
                  {t(
                    'Add a PayMethods entry with type "usdt" and enabled "true" to show USDT in the wallet'
                  )}
                </li>
              </ul>
            </div>

            <div className='grid gap-6 md:grid-cols-2'>
              <FormField
                control={form.control}
                name='EpUSDTGatewayAddress'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Gateway address')}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder='https://pay.xingyuapi.com'
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('EpUSDT gateway base URL')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='EpUSDTApiToken'
                render={({ field }) => (
                  <FormItem>
                    <div className='flex items-center gap-2'>
                      <FormLabel>{t('API token')}</FormLabel>
                      <SecretConfiguredBadge
                        configured={secretConfigured.epusdtApiToken}
                        value={field.value}
                      />
                    </div>
                    <FormControl>
                      <Input
                        type='password'
                        placeholder={t('Enter new token to update')}
                        autoComplete='new-password'
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Leave blank unless rotating the token')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <FormField
              control={form.control}
              name='EpUSDTNotifyURL'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('EpUSDT notify URL')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder='https://your-api.example.com/api/epusdt/notify'
                      {...field}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Async notify URL sent to BEpusdt when creating orders. Leave blank to use <ServerAddress>/api/epusdt/notify.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <Button
              type='button'
              onClick={(e) => {
                e.preventDefault()
                e.stopPropagation()
                saveEpusdtSettings()
              }}
              disabled={updateOption.isPending}
            >
              {updateOption.isPending
                ? t('Saving...')
                : t('Save EpUSDT settings')}
            </Button>
          </div>

          <Separator />

          <div className='space-y-4'>
            <div>
              <h3 className='text-lg font-medium'>{t('Stripe Gateway')}</h3>
              <p className='text-muted-foreground text-sm'>
                {t('Configuration for Stripe payment integration')}
              </p>
            </div>

            <div className='rounded-md bg-blue-50 p-4 text-sm text-blue-900 dark:bg-blue-950 dark:text-blue-100'>
              <p className='mb-2 font-medium'>{t('Webhook Configuration:')}</p>
              <ul className='list-inside list-disc space-y-1'>
                <li>
                  {t('Webhook URL:')}{' '}
                  <code className='rounded bg-blue-100 px-1 py-0.5 text-xs dark:bg-blue-900'>
                    {'<ServerAddress>/api/stripe/webhook'}
                  </code>
                </li>
                <li>
                  {t('Required events:')}{' '}
                  <code className='rounded bg-blue-100 px-1 py-0.5 text-xs dark:bg-blue-900'>
                    {t('checkout.session.completed')}
                  </code>{' '}
                  {t('and')}{' '}
                  <code className='rounded bg-blue-100 px-1 py-0.5 text-xs dark:bg-blue-900'>
                    {t('checkout.session.expired')}
                  </code>
                </li>
                <li>
                  {t('Configure at:')}{' '}
                  <a
                    href='https://dashboard.stripe.com/developers'
                    target='_blank'
                    rel='noreferrer'
                    className='underline hover:no-underline'
                  >
                    {t('Stripe Dashboard')}
                  </a>
                </li>
              </ul>
            </div>

            <div className='grid gap-6 md:grid-cols-3'>
              <FormField
                control={form.control}
                name='StripeApiSecret'
                render={({ field }) => (
                  <FormItem>
                    <div className='flex items-center gap-2'>
                      <FormLabel>{t('API secret')}</FormLabel>
                      <SecretConfiguredBadge
                        configured={secretConfigured.stripeApiSecret}
                        value={field.value}
                      />
                    </div>
                    <FormControl>
                      <Input
                        type='password'
                        placeholder={t('sk_xxx or rk_xxx')}
                        autoComplete='new-password'
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Stripe API key (leave blank unless updating)')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='StripeWebhookSecret'
                render={({ field }) => (
                  <FormItem>
                    <div className='flex items-center gap-2'>
                      <FormLabel>{t('Webhook secret')}</FormLabel>
                      <SecretConfiguredBadge
                        configured={secretConfigured.stripeWebhookSecret}
                        value={field.value}
                      />
                    </div>
                    <FormControl>
                      <Input
                        type='password'
                        placeholder={t('whsec_xxx')}
                        autoComplete='new-password'
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Webhook signing secret (leave blank unless updating)'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='StripePriceId'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Stripe Product ID (optional)')}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder={t('prod_xxx')}
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Optional Stripe catalog product ID. Top-up charges use server-side price_data; subscription plans still use their own price IDs.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <div className='grid gap-6 md:grid-cols-3'>
              <FormField
                control={form.control}
                name='StripeCurrency'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Stripe settlement currency')}</FormLabel>
                    <Select
                      value={field.value}
                      onValueChange={field.onChange}
                    >
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue
                            placeholder={t('Select settlement currency')}
                          />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent>
                        <SelectItem value='usd'>USD</SelectItem>
                        <SelectItem value='cny'>CNY</SelectItem>
                        <SelectItem value='hkd'>HKD</SelectItem>
                      </SelectContent>
                    </Select>
                    <FormDescription>
                      {t(
                        'Currency charged by Stripe Checkout. Amounts are billed 1:1 with no exchange conversion.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='StripeMinTopUp'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Minimum top-up amount')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        step='0.01'
                        min={0}
                        value={(field.value ?? 0) as number}
                        onChange={(event) =>
                          field.onChange(event.target.valueAsNumber)
                        }
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Minimum recharge amount in the selected Stripe settlement currency'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='StripePromotionCodesEnabled'
                render={({ field }) => (
                  <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                    <div className='space-y-0.5'>
                      <FormLabel className='text-base'>
                        {t('Promotion codes')}
                      </FormLabel>
                      <FormDescription>
                        {t('Allow users to enter promo codes')}
                      </FormDescription>
                    </div>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </FormItem>
                )}
              />
            </div>

            <Button
              type='button'
              onClick={(e) => {
                e.preventDefault()
                e.stopPropagation()
                saveStripeSettings()
              }}
              disabled={updateOption.isPending}
            >
              {updateOption.isPending
                ? t('Saving...')
                : t('Save Stripe settings')}
            </Button>
          </div>

          <Button type='submit' disabled={updateOption.isPending}>
            {updateOption.isPending ? t('Saving...') : t('Save all settings')}
          </Button>
        </form>
      </Form>
      {/* eslint-enable react-hooks/refs */}
    </SettingsSection>
  )
}
