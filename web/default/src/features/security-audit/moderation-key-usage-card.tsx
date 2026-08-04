import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Calendar03Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { differenceInCalendarDays, endOfDay, startOfDay } from 'date-fns'
import { type DateRange } from 'react-day-picker'
import { enUS, fr, ja, ru, vi, zhCN } from 'react-day-picker/locale'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Calendar } from '@/components/ui/calendar'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from '@/components/ui/popover'
import { Skeleton } from '@/components/ui/skeleton'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { getContentModerationKeyUsage } from './api'
import type {
  ContentModerationKeyBalance,
  ContentModerationKeyUsageItem,
} from './types'

type UsageRangePreset = 'today' | 'custom'

const CALENDAR_LOCALES = { en: enUS, zh: zhCN, fr, ru, ja, vi } as const

function normalizeLanguage(language: string): keyof typeof CALENDAR_LOCALES {
  const normalized = language.split('-')[0] as keyof typeof CALENDAR_LOCALES
  return normalized in CALENDAR_LOCALES ? normalized : 'en'
}

function formatRangeDate(date: Date, language: string): string {
  return date.toLocaleDateString(language, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  })
}

function formatTokenCount(value: number, language: string): string {
  return new Intl.NumberFormat(language, { maximumFractionDigits: 0 }).format(
    value
  )
}

function formatBilling(value: number): string {
  if (value > 0 && value < 0.000001) return '<$0.000001'
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 4,
    maximumFractionDigits: 8,
  }).format(value)
}

function formatBalance(
  balances: ContentModerationKeyBalance[],
  language: string
): string {
  if (balances.length === 0) return '—'
  return balances
    .map((balance) => {
      const value = Number(balance.total_balance)
      if (!Number.isFinite(value)) {
        return `${balance.total_balance} ${balance.currency}`
      }
      try {
        return new Intl.NumberFormat(language, {
          style: 'currency',
          currency: balance.currency,
          minimumFractionDigits: 2,
          maximumFractionDigits: 6,
        }).format(value)
      } catch {
        return `${balance.total_balance} ${balance.currency}`
      }
    })
    .join(' · ')
}

function UsageMetric(props: { label: string; value: string; meta?: string }) {
  return (
    <div className='bg-muted/40 rounded-lg p-3'>
      <p className='text-muted-foreground text-xs'>{props.label}</p>
      <p className='mt-1 font-mono text-base font-semibold tabular-nums'>
        {props.value}
      </p>
      {props.meta ? (
        <p className='text-muted-foreground mt-1 text-xs'>{props.meta}</p>
      ) : null}
    </div>
  )
}

function KeyUsageCard(props: {
  item: ContentModerationKeyUsageItem
  language: string
}) {
  const { t } = useTranslation()
  const { item, language } = props
  const tokens = item.token_usage_available
    ? formatTokenCount(item.total_tokens, language)
    : '—'
  const billing = item.billing_available ? formatBilling(item.billing_usd) : '—'
  const balance = item.balance_available
    ? formatBalance(item.balances, language)
    : '—'

  return (
    <Card size='sm'>
      <CardHeader>
        <div className='flex min-w-0 items-center gap-2'>
          <Badge variant='secondary'>#{item.index}</Badge>
          <CardTitle className='truncate font-mono'>{item.key_mask}</CardTitle>
        </div>
        <CardDescription className='truncate'>
          {item.provider} · {item.model_name}
        </CardDescription>
        <CardAction>
          <Badge variant='outline'>
            {t('{{count}} requests', { count: item.request_count })}
          </Badge>
        </CardAction>
      </CardHeader>
      <CardContent className='grid gap-3 sm:grid-cols-3'>
        <UsageMetric
          label={t('Token usage')}
          value={tokens}
          meta={
            item.token_usage_available
              ? t('Input {{input}} · Output {{output}} · Cache hit {{cache}}', {
                  input: formatTokenCount(item.prompt_tokens, language),
                  output: formatTokenCount(item.completion_tokens, language),
                  cache: formatTokenCount(item.cache_hit_tokens, language),
                })
              : t('Not reported by provider')
          }
        />
        <UsageMetric
          label={t('Billing')}
          value={billing}
          meta={
            item.billing_available
              ? t('Calculated at request-time pricing')
              : t('Pricing unavailable for this model')
          }
        />
        <UsageMetric
          label={t('Current balance')}
          value={balance}
          meta={
            item.balance_available
              ? t('Live provider balance')
              : item.balance_error || t('Balance API unavailable')
          }
        />
      </CardContent>
    </Card>
  )
}

export function ModerationKeyUsageCard(props: {
  enabled: boolean
  keyCount: number
  refreshNonce: number
}) {
  const { t, i18n } = useTranslation()
  const language = normalizeLanguage(i18n.language)
  const calendarLocale = CALENDAR_LOCALES[language]
  const today = useMemo(() => startOfDay(new Date()), [])
  const [rangePreset, setRangePreset] = useState<UsageRangePreset>('today')
  const [customRange, setCustomRange] = useState<DateRange>({
    from: today,
    to: today,
  })
  const [draftRange, setDraftRange] = useState<DateRange | undefined>({
    from: today,
    to: today,
  })
  const [customRangeOpen, setCustomRangeOpen] = useState(false)

  const selectedRange = useMemo(() => {
    if (rangePreset === 'custom') {
      const from = customRange.from ?? today
      return { from, to: customRange.to ?? from }
    }
    return { from: today, to: today }
  }, [customRange, rangePreset, today])
  const rangeParams = useMemo(
    () => ({
      start_timestamp: Math.floor(
        startOfDay(selectedRange.from).getTime() / 1000
      ),
      end_timestamp: Math.floor(endOfDay(selectedRange.to).getTime() / 1000),
    }),
    [selectedRange]
  )

  const usageQuery = useQuery({
    queryKey: ['security-audit', 'moderation-key-usage', rangeParams],
    queryFn: () => getContentModerationKeyUsage(rangeParams),
    enabled: props.enabled,
    select: (response) => response.data,
    staleTime: 30_000,
    refetchOnMount: 'always',
  })
  const refetchUsage = usageQuery.refetch

  useEffect(() => {
    if (props.refreshNonce <= 0 || !props.enabled) return
    void refetchUsage()
  }, [props.enabled, props.refreshNonce, refetchUsage])

  const customRangeLabel = `${formatRangeDate(selectedRange.from, language)} – ${formatRangeDate(selectedRange.to, language)}`
  const customRangeIsValid = Boolean(
    draftRange?.from &&
    draftRange.to &&
    differenceInCalendarDays(draftRange.to, draftRange.from) >= 0 &&
    differenceInCalendarDays(draftRange.to, draftRange.from) <= 364
  )

  return (
    <Card className='gap-0 py-0 shadow-sm'>
      <CardHeader className='gap-3 border-b py-4 sm:grid-cols-[minmax(0,1fr)_auto]'>
        <CardTitle>{t('Audit API key load')}</CardTitle>
        <CardDescription>
          {t('Per-key token usage, billing, and current provider balance.')}
        </CardDescription>
        <CardAction className='flex flex-wrap items-center gap-2'>
          <Badge variant='secondary'>
            {props.enabled
              ? t('{{count}} keys', { count: props.keyCount })
              : t('Not configured')}
          </Badge>
          <ToggleGroup
            value={[customRangeOpen ? 'custom' : rangePreset]}
            onValueChange={(values) => {
              const nextPreset = values.at(-1) as UsageRangePreset | undefined
              if (!nextPreset) return
              if (nextPreset === 'custom') {
                setDraftRange(customRange)
                setCustomRangeOpen(true)
                return
              }
              setRangePreset('today')
              setCustomRangeOpen(false)
            }}
            variant='segmented'
            size='sm'
            spacing={1}
            aria-label={t('Usage range')}
          >
            <ToggleGroupItem value='today'>{t('Today')}</ToggleGroupItem>
            <ToggleGroupItem value='custom'>{t('Custom')}</ToggleGroupItem>
          </ToggleGroup>
          {(rangePreset === 'custom' || customRangeOpen) && (
            <Popover
              open={customRangeOpen}
              onOpenChange={(open) => {
                setCustomRangeOpen(open)
                if (open) setDraftRange(customRange)
              }}
            >
              <PopoverTrigger render={<Button variant='outline' size='sm' />}>
                <HugeiconsIcon
                  icon={Calendar03Icon}
                  data-icon='inline-start'
                  strokeWidth={2}
                />
                {customRangeLabel}
              </PopoverTrigger>
              <PopoverContent className='w-auto p-0' align='end'>
                <PopoverHeader className='p-3 pb-0'>
                  <PopoverTitle>{t('Custom date range')}</PopoverTitle>
                  <PopoverDescription>
                    {t('Select a date range of up to 365 days')}
                  </PopoverDescription>
                </PopoverHeader>
                <Calendar
                  mode='range'
                  defaultMonth={draftRange?.from}
                  selected={draftRange}
                  onSelect={setDraftRange}
                  max={364}
                  locale={calendarLocale}
                  disabled={(date) => date > startOfDay(new Date())}
                />
                <div className='flex items-center justify-end gap-2 border-t p-2'>
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={() => setCustomRangeOpen(false)}
                  >
                    {t('Cancel')}
                  </Button>
                  <Button
                    size='sm'
                    disabled={!customRangeIsValid}
                    onClick={() => {
                      if (!draftRange?.from || !draftRange.to) return
                      setCustomRange({
                        from: startOfDay(draftRange.from),
                        to: startOfDay(draftRange.to),
                      })
                      setRangePreset('custom')
                      setCustomRangeOpen(false)
                    }}
                  >
                    {t('Apply')}
                  </Button>
                </div>
              </PopoverContent>
            </Popover>
          )}
        </CardAction>
      </CardHeader>
      <CardContent className='flex max-h-[420px] flex-col gap-3 overflow-y-auto p-5'>
        {!props.enabled ? (
          <Alert>
            <AlertDescription>
              {t('No audit API key load data')}
            </AlertDescription>
          </Alert>
        ) : usageQuery.isLoading ? (
          Array.from({ length: Math.max(1, props.keyCount) }).map(
            (_, index) => <Skeleton key={index} className='h-40 rounded-xl' />
          )
        ) : usageQuery.isError ? (
          <Alert variant='destructive'>
            <AlertDescription>{t('Failed to load')}</AlertDescription>
          </Alert>
        ) : (
          (usageQuery.data?.items ?? []).map((item) => (
            <KeyUsageCard
              key={`${item.key_mask}-${item.index}`}
              item={item}
              language={language}
            />
          ))
        )}
      </CardContent>
    </Card>
  )
}
