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
import { Fragment, useEffect, useState, type ReactNode } from 'react'
import {
  CircleAlert,
  CircleCheck,
  CircleDollarSign,
  Clock3,
  Gauge,
  Check,
  CheckCircle2,
  Timer,
  Users,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { SectionPageLayout } from '@/components/layout'
import { getCommonHeaders } from '@/lib/api'
import { cn } from '@/lib/utils'
import { getUserQuotaDates } from '@/features/dashboard/api'
import { getUsers } from '@/features/users/api'
import {
  acknowledgeBusinessMonitorAlert,
  getBusinessMonitorConcurrency,
  getBusinessMonitorAlerts,
  resolveBusinessMonitorAlert,
} from './api'
import {
  formatLogQuota,
  formatTimeStr,
  formatTokens,
  formatUseTime,
} from '@/lib/format'
import { getAllLogs, getLogStats } from '@/features/usage-logs/api'
import { LOG_TYPE_ENUM } from '@/features/usage-logs/constants'
import type { UsageLog } from '@/features/usage-logs/data/schema'
import {
  getFirstResponseTimeColor,
  getResponseTimeColor,
  parseLogOther,
} from '@/features/usage-logs/lib/format'

const NODE_STYLES: Record<number, string> = {
  1: 'bg-sky-100 text-sky-700 dark:bg-sky-950 dark:text-sky-300',
  2: 'bg-violet-100 text-violet-700 dark:bg-violet-950 dark:text-violet-300',
  3: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300',
  4: 'bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300',
}

function getTodayStartTimestamp() {
  const start = new Date()
  start.setHours(0, 0, 0, 0)
  return Math.floor(start.getTime() / 1000)
}

function parseNodeNumber(log: UsageLog, index: number) {
  try {
    const other = JSON.parse(log.other || '{}') as {
      admin_info?: { node_name?: string }
    }
    const nodeName = other.admin_info?.node_name || ''
    const match = nodeName.match(/(?:^|[-_])(\d+)$/)
    const parsed = match ? Number(match[1]) : 0
    if (parsed >= 1 && parsed <= 4) return parsed
  } catch {
    // Ignore malformed optional log metadata.
  }
  return (index % 4) + 1
}

function getLogTokens(log: UsageLog) {
  return Math.max(0, log.prompt_tokens + log.completion_tokens)
}

const TIMING_PILL_BG: Record<string, string> = {
  success:
    'border border-emerald-200/40 bg-emerald-50/35 dark:border-emerald-900/40 dark:bg-emerald-950/15',
  warning:
    'border border-amber-200/45 bg-amber-50/35 dark:border-amber-900/40 dark:bg-amber-950/15',
  danger:
    'border border-rose-200/50 bg-rose-50/35 dark:border-rose-900/40 dark:bg-rose-950/15',
}

const TIMING_PILL_TEXT: Record<string, string> = {
  success: 'text-emerald-700/85 dark:text-emerald-400/85',
  warning: 'text-amber-700/85 dark:text-amber-400/85',
  danger: 'text-rose-700/85 dark:text-rose-400/85',
}

const TIMING_PILL_DOT: Record<string, string> = {
  success: 'bg-emerald-500/80',
  warning: 'bg-amber-500/80',
  danger: 'bg-rose-500/80',
}

const ALERT_SEVERITY_STYLES: Record<string, string> = {
  critical: 'text-rose-500',
  warning: 'text-amber-500',
  info: 'text-sky-500',
}

const ALERT_STATUS_STYLES: Record<string, string> = {
  unhandled: 'border-rose-200 text-rose-600 dark:border-rose-900 dark:text-rose-400',
  acknowledged:
    'border-amber-200 text-amber-600 dark:border-amber-900 dark:text-amber-400',
  resolved:
    'border-emerald-200 text-emerald-600 dark:border-emerald-900 dark:text-emerald-400',
}

function LogTiming({ log }: { log: UsageLog }) {
  const { t } = useTranslation()
  const other = parseLogOther(log.other)
  const frt = other?.frt
  const tokensPerSecond =
    log.use_time > 0 && log.completion_tokens > 0
      ? log.completion_tokens / log.use_time
      : null
  const timeVariant = getResponseTimeColor(log.use_time, log.completion_tokens)
  const frtVariant = frt ? getFirstResponseTimeColor(frt / 1000) : null

  return (
    <div className='flex flex-col gap-1'>
      <div className='flex items-center gap-1.5'>
        <span
          className={cn(
            'inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 font-mono text-xs font-medium',
            TIMING_PILL_BG[timeVariant],
            TIMING_PILL_TEXT[timeVariant]
          )}
        >
          <span
            className={cn(
              'size-1.5 shrink-0 rounded-full',
              TIMING_PILL_DOT[timeVariant]
            )}
            aria-hidden='true'
          />
          {formatUseTime(log.use_time)}
        </span>
        {log.is_stream &&
          (frt != null && frt > 0 ? (
            <span
              className={cn(
                'inline-flex items-center rounded-md px-1.5 py-0.5 font-mono text-xs font-medium',
                TIMING_PILL_BG[frtVariant!],
                TIMING_PILL_TEXT[frtVariant!]
              )}
            >
              {formatUseTime(frt / 1000)}
            </span>
          ) : (
            <span className='border-border/60 text-muted-foreground/50 inline-flex items-center rounded-md border px-1.5 py-0.5 text-[11px]'>
              N/A
            </span>
          ))}
      </div>
      <span className='text-muted-foreground/60 text-[11px]'>
        {log.is_stream ? t('Stream') : t('Non-stream')}
        {tokensPerSecond != null && (
          <>
            {' · '}
            <span className='font-mono tabular-nums'>
              {Math.round(tokensPerSecond)}
            </span>
            {' t/s'}
          </>
        )}
      </span>
    </div>
  )
}

function getPercentileLatency(logs: UsageLog[], percentile: number) {
  const values = logs
    .map((log) => Number(log.use_time))
    .filter((value) => Number.isFinite(value) && value >= 0)
    .sort((a, b) => a - b)
  if (values.length === 0) return 0
  return values[Math.min(values.length - 1, Math.ceil(values.length * percentile) - 1)]
}


type MetricProps = {
  icon: typeof Gauge
  title: string
  children: ReactNode
}

function Metric({ icon: Icon, title, children }: MetricProps) {
  return (
    <div className='bg-card min-w-0 px-4 py-3.5 sm:px-5 sm:py-4'>
      <div className='text-muted-foreground flex items-center gap-2 text-xs font-medium'>
        <Icon className='size-3.5 shrink-0' />
        <span className='truncate'>{title}</span>
      </div>
      {children}
    </div>
  )
}

function DualMetricValues(props: {
  leftValue: string
  leftLabel: string
  rightValue: string
  rightLabel: string
}) {
  return (
    <div className='mt-2.5 grid grid-cols-2 gap-4'>
      <div className='min-w-0'>
        <div className='truncate font-mono text-lg font-semibold tabular-nums sm:text-xl'>
          {props.leftValue}
        </div>
        <div className='text-muted-foreground mt-0.5 truncate text-xs'>
          {props.leftLabel}
        </div>
      </div>
      <div className='min-w-0'>
        <div className='truncate font-mono text-lg font-semibold tabular-nums sm:text-xl'>
          {props.rightValue}
        </div>
        <div className='text-muted-foreground mt-0.5 truncate text-xs'>
          {props.rightLabel}
        </div>
      </div>
    </div>
  )
}

function TripleMetricValues(props: {
  values: Array<{ value: string; label: string }>
}) {
  return (
    <div className='mt-2.5 grid grid-cols-3 gap-2'>
      {props.values.map((item) => (
        <div className='min-w-0' key={item.label}>
          <div className='truncate font-mono text-lg font-semibold tabular-nums sm:text-xl'>
            {item.value}
          </div>
          <div className='text-muted-foreground mt-0.5 truncate text-xs'>
            {item.label}
          </div>
        </div>
      ))}
    </div>
  )
}

export function BusinessMonitor() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [streamHealthy, setStreamHealthy] = useState(false)
  const alertMutation = useMutation({
    mutationFn: async (input: { id: number; action: 'acknowledge' | 'resolve' }) =>
      input.action === 'acknowledge'
        ? acknowledgeBusinessMonitorAlert(input.id)
        : resolveBusinessMonitorAlert(input.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['business-monitor'] })
    },
  })

  useEffect(() => {
    const controller = new AbortController()
    let lastEventId = ''
    let reconnectTimer: ReturnType<typeof setTimeout> | undefined
    let refreshTimer: ReturnType<typeof setTimeout> | undefined

    const scheduleRefresh = () => {
      if (refreshTimer) return
      refreshTimer = setTimeout(() => {
        refreshTimer = undefined
        queryClient.invalidateQueries({ queryKey: ['business-monitor'] })
      }, 500)
    }

    const scheduleReconnect = () => {
      if (controller.signal.aborted || reconnectTimer) return
      reconnectTimer = setTimeout(() => {
        reconnectTimer = undefined
        consumeStream()
      }, 3000)
    }

    const consumeStream = async () => {
      try {
        const streamUrl = lastEventId
          ? `/api/business-monitor/stream?last_id=${encodeURIComponent(lastEventId)}`
          : '/api/business-monitor/stream'
        const response = await fetch(streamUrl, {
          headers: getCommonHeaders(),
          credentials: 'include',
          signal: controller.signal,
        })
        if (!response.ok || !response.body) {
          throw new Error(`business monitor stream failed: ${response.status}`)
        }
        setStreamHealthy(true)

        const reader = response.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ''

        while (!controller.signal.aborted) {
          const { value, done } = await reader.read()
          if (done) break
          buffer += decoder.decode(value, { stream: true })
          const blocks = buffer.split(/\r?\n\r?\n/)
          buffer = blocks.pop() || ''

          for (const block of blocks) {
            const idLine = block.match(/^id:\s*(.+)$/m)
            if (idLine?.[1]) lastEventId = idLine[1].trim()
            if (
              /^event:\s*business-monitor-(log|alert)$/m.test(block)
            ) {
              scheduleRefresh()
            }
          }
        }
        if (!controller.signal.aborted) {
          setStreamHealthy(false)
          scheduleReconnect()
        }
      } catch {
        if (!controller.signal.aborted) {
          setStreamHealthy(false)
          scheduleReconnect()
        }
      }
    }

    consumeStream()

    return () => {
      controller.abort()
      if (reconnectTimer) clearTimeout(reconnectTimer)
      if (refreshTimer) clearTimeout(refreshTimer)
    }
  }, [queryClient])

  const { data: monitorData } = useQuery({
    queryKey: ['business-monitor'],
    queryFn: async () => {
      const now = Math.floor(Date.now() / 1000)
      const todayStart = getTodayStartTimestamp()
      const fetchUserSummary = async () => {
        let page = 1
        let total = 0
        let todayNew = 0
        while (page <= 100) {
          const response = await getUsers({ p: page, page_size: 100 })
          if (!response.success || !response.data) break
          if (page === 1) total = response.data.total
          const users = response.data.items
          todayNew += users.filter(
            (user) => (user.created_at ?? 0) >= todayStart
          ).length
          if (
            users.length === 0 ||
            (users[users.length - 1]?.created_at ?? 0) < todayStart
          ) {
            break
          }
          page += 1
        }
        return { total, todayNew }
      }
      const [
        logsResponse,
        statsResponse,
        quotaResponse,
        alertsResponse,
        concurrencyResponse,
        userSummary,
      ] =
        await Promise.all([
          getAllLogs({
            p: 1,
            page_size: 100,
            type: LOG_TYPE_ENUM.CONSUME,
            start_timestamp: todayStart,
            end_timestamp: now,
          }),
          getLogStats({
            type: LOG_TYPE_ENUM.CONSUME,
            start_timestamp: todayStart,
            end_timestamp: now,
          }),
          getUserQuotaDates(
            {
              start_timestamp: todayStart,
              end_timestamp: now,
            },
            true
          ),
          getBusinessMonitorAlerts(),
          getBusinessMonitorConcurrency(),
          fetchUserSummary(),
        ])

      const logs = (
        logsResponse.success ? (logsResponse.data?.items ?? []) : []
      ) as UsageLog[]
      const quotaData = quotaResponse.success ? (quotaResponse.data ?? []) : []
      const totalTokens = quotaData.reduce(
        (total, item) => total + Number(item.token_used || 0),
        0
      )

      return {
        logs,
        alerts: alertsResponse.success ? (alertsResponse.data ?? []) : [],
        concurrency:
          concurrencyResponse.success &&
          concurrencyResponse.data?.available !== false
          ? concurrencyResponse.data?.concurrency
          : undefined,
        stats: statsResponse.success ? statsResponse.data : undefined,
        totalTokens,
        userSummary,
        updatedAt: now,
      }
    },
    refetchInterval: 15000,
    staleTime: 10000,
  })

  const logs = monitorData?.logs ?? []
  const alerts = monitorData?.alerts ?? []
  const recentLogs = logs.filter(
    (log) => log.created_at >= (monitorData?.updatedAt ?? 0) - 60
  )
  const activeUsers = new Set(recentLogs.map((log) => log.user_id)).size
  const p50Latency = getPercentileLatency(logs, 0.5)
  const p95Latency = getPercentileLatency(logs, 0.95)
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Business Monitor')}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t('Real-time business activity and user health overview')}
      </SectionPageLayout.Description>
      <SectionPageLayout.Actions>
        <Badge variant='outline'>
          <span
            className={cn(
              'size-1.5 rounded-full',
              streamHealthy ? 'bg-emerald-500' : 'bg-amber-500'
            )}
            aria-hidden='true'
          />
          {t('Real-time Monitoring')}
        </Badge>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-3 sm:gap-4'>
          <Card size='sm' className='gap-0 py-0'>
            <CardContent className='p-0'>
              <div className='bg-border grid grid-cols-1 gap-px sm:grid-cols-2 xl:grid-cols-5'>
                <Metric icon={Gauge} title={t('Concurrency')}>
                  <DualMetricValues
                    leftValue={
                      monitorData?.concurrency == null
                        ? '--'
                        : String(monitorData.concurrency)
                    }
                    leftLabel={t('Real-time concurrency')}
                    rightValue={String(monitorData?.stats?.rpm ?? 0)}
                    rightLabel={t('RPM')}
                  />
                </Metric>
                <Metric icon={Timer} title={t('Token Throughput')}>
                  <DualMetricValues
                    leftValue={formatTokens(monitorData?.stats?.tpm ?? 0)}
                    leftLabel={t('Last minute')}
                    rightValue={formatTokens(monitorData?.totalTokens ?? 0)}
                    rightLabel={t("Today's total")}
                  />
                </Metric>
                <Metric icon={Clock3} title={t('Request Latency')}>
                  <DualMetricValues
                    leftValue={formatUseTime(p50Latency)}
                    leftLabel='P50'
                    rightValue={formatUseTime(p95Latency)}
                    rightLabel='P95'
                  />
                </Metric>
                <Metric
                  icon={CircleDollarSign}
                  title={t("Today's Consumption")}
                >
                  <div className='mt-2.5 truncate font-mono text-lg font-semibold tabular-nums sm:text-xl'>
                    {formatLogQuota(monitorData?.stats?.quota ?? 0)}
                  </div>
                </Metric>
                <Metric icon={Users} title={t('User Data')}>
                  <TripleMetricValues
                    values={[
                      {
                        value: String(activeUsers),
                        label: t('Real-time active'),
                      },
                      {
                        value: String(monitorData?.userSummary.todayNew ?? 0),
                        label: t("Today's new"),
                      },
                      {
                        value: String(monitorData?.userSummary.total ?? 0),
                        label: t('Total users'),
                      },
                    ]}
                  />
                </Metric>
              </div>
            </CardContent>
          </Card>

          <div className='grid min-w-0 grid-cols-1 items-stretch gap-3 sm:gap-4 xl:grid-cols-[minmax(0,2.15fr)_minmax(260px,0.95fr)]'>
            <Card size='sm' className='h-full min-w-0 gap-0 py-0'>
              <CardContent className='p-0'>
                <Table className='min-w-[720px] [&_th]:h-8'>
                  <TableHeader className='bg-muted/30'>
                    <TableRow>
                      <TableHead>{t('Time')}</TableHead>
                      <TableHead>{t('User / Model')}</TableHead>
                      <TableHead>{t('Channel / Latency')}</TableHead>
                      <TableHead>{t('Tokens')}</TableHead>
                      <TableHead>{t('Consumption')}</TableHead>
                      <TableHead>{t('Status')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {logs.map((log, index) => (
                      <TableRow key={log.id}>
                        <TableCell className='py-1.5 tabular-nums'>
                          <div className='flex items-center gap-2'>
                            <span
                              className={`inline-flex size-5 shrink-0 items-center justify-center rounded-full text-[11px] font-semibold ${NODE_STYLES[parseNodeNumber(log, index)]}`}
                            >
                              {parseNodeNumber(log, index)}
                            </span>
                            <span>{formatTimeStr(new Date(log.created_at * 1000))}</span>
                          </div>
                        </TableCell>
                        <TableCell className='py-1.5'>
                          <div className='flex min-w-0 flex-col gap-0.5'>
                            <span className='truncate font-medium'>
                              {log.username || log.user_id}
                            </span>
                            <span className='text-muted-foreground truncate text-xs'>
                              {log.model_name || '-'}
                            </span>
                          </div>
                        </TableCell>
                        <TableCell className='py-1.5'>
                          <div className='flex min-w-0 flex-col gap-1.5'>
                            <span className='truncate font-medium'>
                              {log.channel_name || `#${log.channel}`}
                            </span>
                            <LogTiming log={log} />
                          </div>
                        </TableCell>
                        <TableCell className='py-1.5 tabular-nums'>
                          {formatTokens(getLogTokens(log))}
                        </TableCell>
                        <TableCell className='py-1.5 tabular-nums'>
                          {formatLogQuota(log.quota)}
                        </TableCell>
                        <TableCell className='py-1.5'>
                          <Badge
                            variant='outline'
                          >
                            <CircleCheck data-icon='inline-start' />
                            {t('Success')}
                          </Badge>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>

            <Card size='sm' className='h-full min-w-0 gap-0 py-0'>
              <CardHeader className='border-b py-2.5'>
                <CardTitle>{t('Error Alerts')}</CardTitle>
                <CardAction>
                  <Badge variant='outline'>
                    {t('{{count}} active', { count: alerts.length })}
                  </Badge>
                </CardAction>
              </CardHeader>
              <CardContent className='px-4 py-1'>
                {alerts.map((alert, index) => (
                  <Fragment key={alert.id}>
                    <div className='flex min-w-0 items-start gap-2 py-2.5'>
                      <CircleAlert
                        className={cn(
                          'mt-0.5 size-3.5 shrink-0',
                          ALERT_SEVERITY_STYLES[alert.severity]
                        )}
                      />
                      <div className='min-w-0 flex-1'>
                        <div className='flex min-w-0 items-center gap-1.5'>
                          <span className='truncate text-sm font-medium'>
                            {alert.title}
                          </span>
                          <Badge
                            variant='outline'
                            className={cn(
                              'h-5 shrink-0 px-1.5 text-[10px]',
                              ALERT_STATUS_STYLES[alert.status]
                            )}
                          >
                            {t(
                              alert.status === 'unhandled'
                                ? 'Unhandled'
                                : alert.status === 'acknowledged'
                                  ? 'Acknowledged'
                                  : 'Resolved'
                            )}
                          </Badge>
                        </div>
                        <div className='text-muted-foreground mt-1 flex min-w-0 items-center gap-1 text-[11px]'>
                          <span className='truncate'>
                            {alert.channel_name || `#${alert.channel_id}`}
                            {alert.node_name ? ` · ${alert.node_name}` : ''}
                          </span>
                          <span className='shrink-0'>·</span>
                          <span className='shrink-0 tabular-nums'>
                            {t('{{count}} occurrences', {
                              count: alert.occurrence_count,
                            })}
                          </span>
                          <span className='shrink-0'>·</span>
                          <span className='shrink-0 tabular-nums'>
                            {formatTimeStr(new Date(alert.last_seen_at * 1000))}
                          </span>
                        </div>
                      </div>
                      <div className='flex shrink-0 items-center gap-0.5'>
                        {alert.status === 'unhandled' && (
                          <button
                            type='button'
                            className='text-muted-foreground hover:bg-muted hover:text-foreground inline-flex size-7 items-center justify-center rounded-md'
                            title={t('Acknowledge')}
                            aria-label={t('Acknowledge')}
                            disabled={alertMutation.isPending}
                            onClick={() =>
                              alertMutation.mutate({
                                id: alert.id,
                                action: 'acknowledge',
                              })
                            }
                          >
                            <Check className='size-3.5' />
                          </button>
                        )}
                        {alert.status !== 'resolved' && (
                          <button
                            type='button'
                            className='text-muted-foreground hover:bg-muted hover:text-foreground inline-flex size-7 items-center justify-center rounded-md'
                            title={t('Resolve')}
                            aria-label={t('Resolve')}
                            disabled={alertMutation.isPending}
                            onClick={() =>
                              alertMutation.mutate({
                                id: alert.id,
                                action: 'resolve',
                              })
                            }
                          >
                            <CheckCircle2 className='size-3.5' />
                          </button>
                        )}
                      </div>
                    </div>
                    {index < alerts.length - 1 && <Separator />}
                  </Fragment>
                ))}
              </CardContent>
            </Card>
          </div>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
