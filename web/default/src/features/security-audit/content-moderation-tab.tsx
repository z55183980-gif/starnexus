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
import { useEffect, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, FileText, KeyRound, Shield, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import {
  getOptionValue,
  useSystemOptions,
} from '@/features/system-settings/hooks/use-system-options'
import { ContentModerationSection } from '@/features/system-settings/request-limits/content-moderation-section'
import {
  DEFAULT_CONTENT_MODERATION_CONFIG,
  parseContentModerationConfig,
  stringifyContentModerationConfig,
} from '@/features/system-settings/request-limits/content-moderation-utils'
import { listContentModerationLogs } from './api'
import { ModerationAuditRecords } from './moderation-audit-records'
import { ModerationKeyUsageCard } from './moderation-key-usage-card'

const defaultOptions = {
  ContentModerationConfig: stringifyContentModerationConfig(
    DEFAULT_CONTENT_MODERATION_CONFIG
  ),
}

type ContentModerationTabProps = {
  settingsOpen: boolean
  onSettingsOpenChange: (open: boolean) => void
  refreshNonce: number
}

type SummaryCardProps = {
  icon: ReactNode
  iconClassName: string
  label: string
  value: string
  meta?: string
  badge?: string
  badgeClassName?: string
}

function SummaryCard({
  icon,
  iconClassName,
  label,
  value,
  meta,
  badge,
  badgeClassName,
}: SummaryCardProps) {
  return (
    <Card size='sm' className='gap-0 py-0 shadow-sm'>
      <CardContent className='flex items-center gap-3 px-4 py-3'>
        <div
          className={cn(
            'flex size-9 shrink-0 items-center justify-center rounded-lg',
            iconClassName
          )}
        >
          {icon}
        </div>
        <div className='min-w-0 flex-1'>
          <div className='flex min-w-0 items-center justify-between gap-2'>
            <p className='text-muted-foreground truncate text-xs font-medium'>
              {label}
            </p>
            {badge ? (
              <Badge
                variant='secondary'
                className={cn(
                  'h-5 shrink-0 rounded-full px-2 text-[11px] font-medium',
                  badgeClassName
                )}
              >
                {badge}
              </Badge>
            ) : null}
          </div>
          <div className='mt-1 flex min-w-0 items-baseline gap-2'>
            <p className='truncate text-xl leading-7 font-semibold'>{value}</p>
            {meta ? (
              <p className='text-muted-foreground truncate text-xs'>{meta}</p>
            ) : null}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

export function ContentModerationTab({
  settingsOpen,
  onSettingsOpenChange,
  refreshNonce,
}: ContentModerationTabProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { data, isLoading, isError, refetch } = useSystemOptions()
  const logsCountQuery = useQuery({
    queryKey: ['security-audit', 'moderation-logs-count'],
    queryFn: async () => {
      const [passedRes, hitRes, blockedRes] = await Promise.all([
        listContentModerationLogs({ p: 1, page_size: 1, action: 'recorded' }),
        listContentModerationLogs({ p: 1, page_size: 1, action: 'hit' }),
        listContentModerationLogs({ p: 1, page_size: 1, action: 'blocked' }),
      ])
      return {
        passed: passedRes.data?.total ?? 0,
        observedHits: hitRes.data?.total ?? 0,
        blockedHits: blockedRes.data?.total ?? 0,
      }
    },
    staleTime: 30_000,
  })
  const refetchLogsCount = logsCountQuery.refetch

  useEffect(() => {
    if (refreshNonce <= 0) return
    void refetch()
    void refetchLogsCount()
  }, [refreshNonce, refetch, refetchLogsCount])

  const handleSettingsOpenChange = (open: boolean) => {
    onSettingsOpenChange(open)
    if (!open) {
      void refetch()
      void queryClient.invalidateQueries({
        queryKey: ['security-audit', 'moderation-logs-count'],
      })
      void queryClient.invalidateQueries({
        queryKey: ['security-audit', 'moderation-logs'],
      })
    }
  }

  if (isLoading) {
    return (
      <div className='space-y-6 py-2'>
        <div className='grid gap-3 md:grid-cols-2 xl:grid-cols-4'>
          {Array.from({ length: 4 }).map((_, index) => (
            <Skeleton key={index} className='h-[4.5rem] w-full rounded-lg' />
          ))}
        </div>
        <Skeleton className='h-64 w-full rounded-xl' />
      </div>
    )
  }

  if (isError) {
    return (
      <div className='text-muted-foreground flex flex-col items-start gap-3 py-6 text-sm'>
        <span>{t('Failed to load')}</span>
        <button
          type='button'
          className='text-foreground underline'
          onClick={() => void refetch()}
        >
          {t('Retry')}
        </button>
      </div>
    )
  }

  const settings = getOptionValue(data?.data, defaultOptions)
  const config = parseContentModerationConfig(settings.ContentModerationConfig)
  const keysConfigured = Boolean(
    config.keys_configured || config.api_keys.length > 0
  )
  const keyCount = config.api_key_count ?? config.api_keys.length
  const observedHits = logsCountQuery.data?.observedHits ?? 0
  const blockedHits = logsCountQuery.data?.blockedHits ?? 0
  const passedCount = logsCountQuery.data?.passed ?? 0
  const recordsTotal = passedCount + observedHits + blockedHits
  const isObserve = config.mode === 'observe'
  const modeLabel = isObserve ? t('Observe only') : t('Front interception')
  const showRuntimePanels = config.enabled
  let scopeValue = t('All Groups')
  if (!config.all_groups) {
    if (config.groups.length > 0) {
      scopeValue = t('{{count}} groups', { count: config.groups.length })
    } else {
      scopeValue = t('No groups')
    }
  }
  let scopeMeta = modeLabel
  if (config.model_filter.type === 'include') {
    scopeMeta = t('{{count}} models included', {
      count: config.model_filter.models.length,
    })
  } else if (config.model_filter.type === 'exclude') {
    scopeMeta = t('{{count}} models excluded', {
      count: config.model_filter.models.length,
    })
  }

  return (
    <div className='space-y-6'>
      <div className='grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4'>
        <SummaryCard
          icon={
            config.enabled ? (
              <CheckCircle2 className='size-4 text-emerald-600' />
            ) : (
              <XCircle className='text-muted-foreground size-4' />
            )
          }
          iconClassName={config.enabled ? 'bg-emerald-500/10' : 'bg-muted'}
          label={t('Running status')}
          value={config.enabled ? t('Enabled') : t('Disabled')}
          badge={config.enabled ? t('Active') : t('Off')}
          badgeClassName={
            config.enabled
              ? 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-300'
              : undefined
          }
        />
        <SummaryCard
          icon={<KeyRound className='size-4 text-sky-600' />}
          iconClassName='bg-sky-500/10'
          label={t('API Key')}
          value={
            keysConfigured
              ? t('{{count}} configured', { count: keyCount })
              : t('Not configured')
          }
          meta={
            keysConfigured
              ? t('Ready for audit API calls')
              : t('Add keys in content audit settings')
          }
        />
        <SummaryCard
          icon={<Shield className='size-4 text-violet-600' />}
          iconClassName='bg-violet-500/10'
          label={t('Audit scope')}
          value={scopeValue}
          meta={scopeMeta}
        />
        <SummaryCard
          icon={<FileText className='size-4 text-amber-600' />}
          iconClassName='bg-amber-500/10'
          label={t('Audit overview')}
          value={
            logsCountQuery.isLoading
              ? '—'
              : `${passedCount} / ${observedHits} / ${blockedHits} / ${recordsTotal}`
          }
          meta={t('Passed · Observed hits · Blocked hits · Total audits')}
        />
      </div>

      {showRuntimePanels ? (
        <div className='grid grid-cols-1 gap-4 xl:grid-cols-2'>
          <Card className='gap-0 py-0 shadow-sm'>
            <div className='flex flex-col gap-2 border-b px-5 py-4 sm:flex-row sm:items-center sm:justify-between'>
              <div>
                <h3 className='text-base font-semibold'>
                  {t('Front interception runtime')}
                </h3>
                <p className='text-muted-foreground mt-1 text-sm'>
                  {t(
                    'Runtime settings used for content audit checks. Sync queue metrics are not available in this build.'
                  )}
                </p>
              </div>
              <Badge variant='secondary' className='w-fit rounded-full'>
                {modeLabel}
              </Badge>
            </div>
            <CardContent className='grid grid-cols-2 gap-3 p-5 md:grid-cols-3'>
              <div className='rounded-lg bg-sky-500/5 p-4'>
                <p className='text-muted-foreground text-xs'>
                  {t('Model Name')}
                </p>
                <p className='mt-2 truncate text-lg font-semibold'>
                  {config.model}
                </p>
              </div>
              <div className='bg-muted/50 rounded-lg p-4'>
                <p className='text-muted-foreground text-xs'>
                  {t('HTTP Timeout (ms)')}
                </p>
                <p className='mt-2 text-lg font-semibold'>
                  {config.timeout_ms}
                </p>
              </div>
              <div className='rounded-lg bg-emerald-500/5 p-4'>
                <p className='text-muted-foreground text-xs'>
                  {isObserve ? t('Fail-open') : t('Fail-closed')}
                </p>
                <p className='mt-2 text-lg font-semibold'>
                  {isObserve
                    ? t('API failures allow requests')
                    : t('API failures deny requests')}
                </p>
              </div>
              <div className='col-span-2 rounded-lg bg-violet-500/5 p-4 md:col-span-3'>
                <p className='text-muted-foreground text-xs'>
                  {t('API Base URL')}
                </p>
                <p className='mt-2 truncate font-mono text-sm font-semibold'>
                  {config.base_url}
                </p>
              </div>
            </CardContent>
          </Card>

          <ModerationKeyUsageCard
            enabled={keysConfigured}
            keyCount={keyCount}
            refreshNonce={refreshNonce}
          />
        </div>
      ) : null}

      <ModerationAuditRecords refreshNonce={refreshNonce} />

      <Sheet open={settingsOpen} onOpenChange={handleSettingsOpenChange}>
        <SheetContent className='w-full overflow-y-auto sm:max-w-xl'>
          <SheetHeader className='border-b'>
            <SheetTitle>{t('Content audit settings')}</SheetTitle>
            <SheetDescription>
              {t(
                'Audit all client-supplied prompt text before it reaches upstream GPT/OpenAI accounts. Pre-block mode denies requests when the audit service is unavailable.'
              )}
            </SheetDescription>
          </SheetHeader>
          <div className='p-4'>
            <ContentModerationSection
              defaultValue={settings.ContentModerationConfig}
              embedded
            />
          </div>
        </SheetContent>
      </Sheet>
    </div>
  )
}
