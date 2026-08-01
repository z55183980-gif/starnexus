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
import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Activity01Icon,
  Calendar03Icon,
  ChartIncreaseIcon,
  CheckmarkCircle02Icon,
  Coins01Icon,
  UserGroupIcon,
  UserMultipleIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { differenceInCalendarDays, format, startOfDay, subDays } from 'date-fns'
import { type DateRange } from 'react-day-picker'
import { enUS, fr, ja, ru, vi, zhCN } from 'react-day-picker/locale'
import { useTranslation } from 'react-i18next'
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from 'recharts'
import {
  formatNumber,
  formatPercent,
  formatQuota,
  formatTimestamp,
} from '@/lib/format'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
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
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from '@/components/ui/chart'
import {
  Item,
  ItemActions,
  ItemContent,
  ItemDescription,
  ItemFooter,
  ItemGroup,
  ItemMedia,
  ItemTitle,
} from '@/components/ui/item'
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from '@/components/ui/popover'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { GroupBadge } from '@/components/group-badge'
import { StatusBadge } from '@/components/status-badge'
import { getUserStatistics } from '../api'
import { USER_ROLES, USER_STATUSES } from '../constants'

type StatisticsRangePreset = 'today' | '7d' | '30d' | 'custom'

const CALENDAR_LOCALES = {
  en: enUS,
  zh: zhCN,
  fr,
  ru,
  ja,
  vi,
} as const

function getInitials(username: string): string {
  return username.trim().slice(0, 2).toUpperCase() || 'U'
}

function getPresetRange(preset: Exclude<StatisticsRangePreset, 'custom'>) {
  const to = startOfDay(new Date())
  const days = preset === 'today' ? 1 : preset === '7d' ? 7 : 30
  return { from: subDays(to, days - 1), to }
}

function normalizeLanguage(language: string): keyof typeof CALENDAR_LOCALES {
  const normalized = language.split('-')[0] as keyof typeof CALENDAR_LOCALES
  return normalized in CALENDAR_LOCALES ? normalized : 'en'
}

function formatChartDate(value: string, language: string): string {
  const date = new Date(`${value}T00:00:00`)
  return date.toLocaleDateString(language, {
    month: 'short',
    day: 'numeric',
  })
}

function formatRangeDate(date: Date, language: string): string {
  return date.toLocaleDateString(language, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  })
}

function MetricCard(props: {
  title: string
  value: string
  description: string
  icon: typeof UserMultipleIcon
  badge?: string
  badgeTitle?: string
  badgeVariant?: 'success' | 'destructive' | 'secondary'
}) {
  return (
    <Card size='sm'>
      <CardHeader>
        <CardTitle>{props.title}</CardTitle>
        <CardAction>
          <div className='bg-muted text-muted-foreground flex size-8 items-center justify-center rounded-lg'>
            <HugeiconsIcon icon={props.icon} strokeWidth={1.8} />
          </div>
        </CardAction>
      </CardHeader>
      <CardContent className='flex flex-col gap-2'>
        <div className='flex flex-wrap items-end gap-2'>
          <span className='font-mono text-2xl font-semibold tracking-tight tabular-nums'>
            {props.value}
          </span>
          {props.badge && (
            <Badge
              variant={props.badgeVariant ?? 'secondary'}
              title={props.badgeTitle}
            >
              {props.badge}
            </Badge>
          )}
        </div>
        <p className='text-muted-foreground text-xs'>{props.description}</p>
      </CardContent>
    </Card>
  )
}

function StatisticsSkeleton() {
  return (
    <div className='flex flex-col gap-4'>
      <Skeleton className='h-16 rounded-xl' />
      <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-6'>
        {Array.from({ length: 6 }).map((_, index) => (
          <Card key={index} size='sm'>
            <CardHeader>
              <Skeleton className='h-4 w-24' />
            </CardHeader>
            <CardContent className='flex flex-col gap-2'>
              <Skeleton className='h-8 w-20' />
              <Skeleton className='h-3 w-40' />
            </CardContent>
          </Card>
        ))}
      </div>
      <Skeleton className='h-[340px] rounded-xl' />
      <Skeleton className='h-64 rounded-xl' />
      <Skeleton className='h-72 rounded-xl' />
    </div>
  )
}

function RecentUserIdentity(props: { username: string; displayName: string }) {
  return (
    <div className='flex min-w-0 items-center gap-2.5'>
      <Avatar className='size-8'>
        <AvatarFallback>{getInitials(props.username)}</AvatarFallback>
      </Avatar>
      <div className='min-w-0'>
        <div className='truncate font-medium'>{props.username}</div>
        {props.displayName && props.displayName !== props.username && (
          <div className='text-muted-foreground truncate text-xs'>
            {props.displayName}
          </div>
        )}
      </div>
    </div>
  )
}

export function UsersStatistics(props: { onViewAll: () => void }) {
  const { t, i18n } = useTranslation()
  const language = normalizeLanguage(i18n.language)
  const calendarLocale = CALENDAR_LOCALES[language]
  const [rangePreset, setRangePreset] = useState<StatisticsRangePreset>('30d')
  const [customRange, setCustomRange] = useState<DateRange>(() =>
    getPresetRange('30d')
  )
  const [draftRange, setDraftRange] = useState<DateRange | undefined>(() =>
    getPresetRange('30d')
  )
  const [customRangeOpen, setCustomRangeOpen] = useState(false)

  const selectedRange = useMemo(() => {
    if (rangePreset === 'custom') {
      return {
        from: customRange.from ?? startOfDay(new Date()),
        to: customRange.to ?? customRange.from ?? startOfDay(new Date()),
      }
    }
    return getPresetRange(rangePreset)
  }, [customRange, rangePreset])

  const rangeParams = useMemo(
    () => ({
      start_date: format(selectedRange.from, 'yyyy-MM-dd'),
      end_date: format(selectedRange.to, 'yyyy-MM-dd'),
    }),
    [selectedRange]
  )

  const statisticsQuery = useQuery({
    queryKey: ['users', 'statistics', 'overview', rangeParams],
    queryFn: () => getUserStatistics(rangeParams),
    select: (response) => response.data,
    staleTime: 60_000,
    refetchOnMount: 'always',
    retry: 1,
  })

  const statistics = statisticsQuery.data
  const summary = statistics?.summary

  const activeRate = summary?.total_users
    ? (summary.active_users / summary.total_users) * 100
    : 0
  const enabledRate = summary?.total_users
    ? (summary.enabled_users / summary.total_users) * 100
    : 0
  const growthRate = summary?.new_previous_period
    ? ((summary.new_users - summary.new_previous_period) /
        summary.new_previous_period) *
      100
    : null

  const registrationChartConfig = {
    count: {
      label: t('New Users'),
      color: 'var(--chart-1)',
    },
  } satisfies ChartConfig

  const rangeLabel = useMemo(() => {
    if (rangePreset === 'today') return t('Today')
    if (rangePreset === '7d') return t('Last 7 days')
    if (rangePreset === '30d') return t('Last 30 days')
    return `${formatRangeDate(selectedRange.from, language)} – ${formatRangeDate(selectedRange.to, language)}`
  }, [language, rangePreset, selectedRange.from, selectedRange.to, t])
  const customRangeLabel = `${formatRangeDate(customRange.from ?? startOfDay(new Date()), language)} – ${formatRangeDate(customRange.to ?? customRange.from ?? startOfDay(new Date()), language)}`

  const updatedAt = statisticsQuery.dataUpdatedAt
    ? new Intl.DateTimeFormat(language, {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
      }).format(statisticsQuery.dataUpdatedAt)
    : '-'

  const draftDays =
    draftRange?.from && draftRange.to
      ? differenceInCalendarDays(draftRange.to, draftRange.from) + 1
      : 0
  const customRangeIsValid = draftDays >= 1 && draftDays <= 365

  if (statisticsQuery.isLoading) {
    return <StatisticsSkeleton />
  }

  if (statisticsQuery.isError || !statistics || !summary) {
    return (
      <ErrorState
        description={t('Failed to load user statistics')}
        onRetry={() => statisticsQuery.refetch()}
      />
    )
  }

  const growthBadge =
    growthRate === null
      ? summary.new_users > 0
        ? `+${formatNumber(summary.new_users)}`
        : '0%'
      : `${growthRate > 0 ? '+' : ''}${formatPercent(growthRate)}`
  const growthVariant =
    growthRate === null
      ? summary.new_users > 0
        ? 'success'
        : 'secondary'
      : growthRate > 0
        ? 'success'
        : growthRate < 0
          ? 'destructive'
          : 'secondary'
  const trendMaximum = Math.max(
    1,
    ...statistics.registration_trend.map((item) => item.count)
  )
  return (
    <div className='flex flex-col gap-4 pb-2'>
      <div className='border-border bg-muted/30 flex flex-col gap-3 rounded-xl border p-3 lg:flex-row lg:items-center lg:justify-between'>
        <div className='flex min-w-0 flex-col gap-1'>
          <div className='text-sm font-medium'>{t('Statistics range')}</div>
          <div className='text-muted-foreground text-xs'>
            {t(
              'The range applies to logged-in users, new users, trend, and comparison'
            )}
            {' · '}
            {t('Last updated: {{time}}', { time: updatedAt })}
          </div>
        </div>
        <div className='flex flex-wrap items-center gap-2'>
          <ToggleGroup
            value={[customRangeOpen ? 'custom' : rangePreset]}
            onValueChange={(values) => {
              const nextPreset = values.at(-1) as
                | StatisticsRangePreset
                | undefined
              if (!nextPreset) return
              if (nextPreset === 'custom') {
                setDraftRange(customRange)
                setCustomRangeOpen(true)
                return
              }
              setRangePreset(nextPreset)
            }}
            variant='segmented'
            size='sm'
            spacing={1}
            aria-label={t('Statistics range')}
          >
            <ToggleGroupItem value='today'>{t('Today')}</ToggleGroupItem>
            <ToggleGroupItem value='7d'>{t('7 days')}</ToggleGroupItem>
            <ToggleGroupItem value='30d'>{t('30 days')}</ToggleGroupItem>
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

          <Button
            variant='outline'
            size='sm'
            disabled={statisticsQuery.isFetching}
            onClick={() => statisticsQuery.refetch()}
          >
            {statisticsQuery.isFetching
              ? t('Refreshing...')
              : t('Refresh Stats')}
          </Button>
        </div>
      </div>

      <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-6'>
        <MetricCard
          title={t('Total Users')}
          value={formatNumber(summary.total_users)}
          description={t('All current user accounts')}
          icon={UserMultipleIcon}
        />
        <MetricCard
          title={t('Logged-in Users')}
          value={formatNumber(summary.active_users)}
          description={`${formatPercent(activeRate)} ${t('of users logged in during the selected range')}`}
          icon={Activity01Icon}
        />
        <MetricCard
          title={t('New Users')}
          value={formatNumber(summary.new_users)}
          description={t('Registered during the selected range')}
          icon={Calendar03Icon}
          badge={growthBadge}
          badgeTitle={t('Compared with the previous period')}
          badgeVariant={growthVariant}
        />
        <MetricCard
          title={t('Enabled Rate')}
          value={formatPercent(enabledRate)}
          description={`${formatNumber(summary.disabled_users)} ${t('disabled users')}`}
          icon={CheckmarkCircle02Icon}
        />
        <MetricCard
          title={t('Total Balance')}
          value={formatQuota(summary.total_quota)}
          description={t('Remaining quota units')}
          icon={Coins01Icon}
        />
        <MetricCard
          title={t('Total Usage')}
          value={formatQuota(summary.total_used_quota)}
          description={t('Total consumed quota')}
          icon={ChartIncreaseIcon}
        />
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{t('Registration Trend')}</CardTitle>
          <CardDescription>
            {t('Daily new users during the selected range')}
          </CardDescription>
          <CardAction>
            <Badge variant='outline'>{rangeLabel}</Badge>
          </CardAction>
        </CardHeader>
        <CardContent>
          <ChartContainer
            config={registrationChartConfig}
            className='aspect-auto h-[280px] w-full'
            initialDimension={{ width: 760, height: 280 }}
          >
            <BarChart
              accessibilityLayer
              data={statistics.registration_trend}
              margin={{ top: 12, right: 8, bottom: 0, left: 0 }}
            >
              <CartesianGrid vertical={false} strokeDasharray='4 4' />
              <XAxis
                dataKey='date'
                axisLine={false}
                tickLine={false}
                minTickGap={36}
                tickFormatter={(value) =>
                  formatChartDate(String(value), language)
                }
              />
              <YAxis
                allowDecimals={false}
                axisLine={false}
                tickLine={false}
                width={32}
                domain={[0, trendMaximum]}
                tickCount={Math.min(5, trendMaximum + 1)}
              />
              <ChartTooltip
                cursor={{ fill: 'var(--muted)' }}
                content={
                  <ChartTooltipContent
                    labelFormatter={(_label, payload) =>
                      formatChartDate(
                        String(payload?.[0]?.payload?.date ?? ''),
                        language
                      )
                    }
                  />
                }
              />
              <Bar
                dataKey='count'
                fill='var(--color-count)'
                maxBarSize={28}
                radius={[4, 4, 0, 0]}
                isAnimationActive={false}
              />
            </BarChart>
          </ChartContainer>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('Group Distribution')}</CardTitle>
          <CardDescription>{t('Top groups by user count')}</CardDescription>
          <CardAction>
            <HugeiconsIcon icon={UserGroupIcon} strokeWidth={1.8} />
          </CardAction>
        </CardHeader>
        <CardContent className='flex flex-col gap-3'>
          {statistics.group_distribution.map((item) => {
            const percentage = summary.total_users
              ? (item.count / summary.total_users) * 100
              : 0
            const groupLabel =
              item.name === '__other__' ? t('Other') : item.name
            return (
              <div key={item.name} className='flex flex-col gap-1.5'>
                <div className='flex items-center justify-between gap-3 text-xs'>
                  <GroupBadge
                    group={item.name === '__other__' ? undefined : item.name}
                    label={item.name === '__other__' ? t('Other') : undefined}
                  />
                  <span className='text-muted-foreground font-mono tabular-nums'>
                    {formatNumber(item.count)} · {formatPercent(percentage)}
                  </span>
                </div>
                <Progress
                  value={percentage}
                  aria-label={`${groupLabel}: ${formatPercent(percentage)}`}
                />
              </div>
            )
          })}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('Recent Users')}</CardTitle>
          <CardDescription>
            {t('Latest registered user accounts')}
          </CardDescription>
          <CardAction>
            <Button variant='outline' size='sm' onClick={props.onViewAll}>
              {t('View all users')}
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent>
          {statistics.recent_users.length === 0 ? (
            <EmptyState
              className='min-h-48'
              title={t('No recent users')}
              icon={undefined}
            />
          ) : (
            <>
              <ItemGroup className='md:hidden'>
                {statistics.recent_users.map((user) => {
                  const role = USER_ROLES[user.role as keyof typeof USER_ROLES]
                  const status =
                    USER_STATUSES[user.status as keyof typeof USER_STATUSES]
                  return (
                    <Item key={user.id} variant='outline' size='sm'>
                      <ItemMedia>
                        <Avatar className='size-8'>
                          <AvatarFallback>
                            {getInitials(user.username)}
                          </AvatarFallback>
                        </Avatar>
                      </ItemMedia>
                      <ItemContent>
                        <ItemTitle>{user.username}</ItemTitle>
                        {user.display_name &&
                          user.display_name !== user.username && (
                            <ItemDescription>
                              {user.display_name}
                            </ItemDescription>
                          )}
                      </ItemContent>
                      <ItemActions>
                        {status && (
                          <StatusBadge
                            label={t(status.labelKey)}
                            variant={status.variant}
                            showDot={status.showDot}
                            copyable={false}
                          />
                        )}
                      </ItemActions>
                      <ItemFooter>
                        <div className='text-muted-foreground flex min-w-0 items-center gap-2 text-xs'>
                          <span>{role ? t(role.labelKey) : t('Other')}</span>
                          <span>·</span>
                          <span className='truncate'>
                            {formatTimestamp(user.created_at)}
                          </span>
                        </div>
                        <GroupBadge group={user.group} />
                      </ItemFooter>
                    </Item>
                  )
                })}
              </ItemGroup>

              <div className='hidden overflow-x-auto rounded-lg border md:block'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('User')}</TableHead>
                      <TableHead>{t('Status')}</TableHead>
                      <TableHead>{t('Role')}</TableHead>
                      <TableHead>{t('Group')}</TableHead>
                      <TableHead>{t('Created At')}</TableHead>
                      <TableHead>{t('Last Login')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {statistics.recent_users.map((user) => {
                      const role =
                        USER_ROLES[user.role as keyof typeof USER_ROLES]
                      const status =
                        USER_STATUSES[user.status as keyof typeof USER_STATUSES]
                      return (
                        <TableRow key={user.id}>
                          <TableCell>
                            <div className='min-w-44'>
                              <RecentUserIdentity
                                username={user.username}
                                displayName={user.display_name}
                              />
                            </div>
                          </TableCell>
                          <TableCell>
                            {status ? (
                              <StatusBadge
                                label={t(status.labelKey)}
                                variant={status.variant}
                                showDot={status.showDot}
                                copyable={false}
                              />
                            ) : (
                              '-'
                            )}
                          </TableCell>
                          <TableCell>
                            {role ? t(role.labelKey) : t('Other')}
                          </TableCell>
                          <TableCell>
                            <GroupBadge group={user.group} />
                          </TableCell>
                          <TableCell className='text-muted-foreground whitespace-nowrap'>
                            {formatTimestamp(user.created_at)}
                          </TableCell>
                          <TableCell className='text-muted-foreground whitespace-nowrap'>
                            {formatTimestamp(user.last_login_at)}
                          </TableCell>
                        </TableRow>
                      )
                    })}
                  </TableBody>
                </Table>
              </div>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
