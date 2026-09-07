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
import { useEffect, useMemo, useState, useRef, useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import { VChart } from '@visactor/react-vchart'
import { Users, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { type TimeGranularity } from '@/lib/time'
import { VCHART_OPTION } from '@/lib/vchart'
import { useThemeCustomization } from '@/context/theme-customization-provider'
import { useTheme } from '@/context/theme-provider'
import { Skeleton } from '@/components/ui/skeleton'
import { getUserQuotaDataByUsers } from '@/features/dashboard/api'
import { DashboardRefreshStatus } from '@/features/dashboard/components/ui/dashboard-refresh-status'
import { DashboardTimeRangeToggle } from '@/features/dashboard/components/ui/dashboard-time-range-toggle'
import { DASHBOARD_USER_REFRESH_INTERVAL } from '@/features/dashboard/hooks/use-dashboard-refresh'
import {
  getDashboardDateRange,
  getSavedChartPreferences,
  processUserChartData,
} from '@/features/dashboard/lib'
import type { ProcessedUserChartData } from '@/features/dashboard/types'

let themeManagerPromise: Promise<
  (typeof import('@visactor/vchart'))['ThemeManager']
> | null = null

const USER_CHARTS: {
  value: string
  labelKey: string
  specKey: keyof ProcessedUserChartData
}[] = [
  {
    value: 'rank',
    labelKey: 'User Consumption Ranking',
    specKey: 'spec_user_rank',
  },
  {
    value: 'trend',
    labelKey: 'User Consumption Trend',
    specKey: 'spec_user_trend',
  },
]

const TOP_USER_LIMIT_OPTIONS = [5, 10, 20, 50]
const EMPTY_USER_DATA: NonNullable<
  Awaited<ReturnType<typeof getUserQuotaDataByUsers>>['data']
> = []

export function UserCharts() {
  const { t } = useTranslation()
  const { resolvedTheme } = useTheme()
  const { customization } = useThemeCustomization()
  const [themeReady, setThemeReady] = useState(false)
  const themeManagerRef = useRef<
    (typeof import('@visactor/vchart'))['ThemeManager'] | null
  >(null)

  const [selectedRange, setSelectedRange] = useState<number>(
    () => getSavedChartPreferences().defaultTimeRangeDays
  )
  const timeGranularity: TimeGranularity =
    selectedRange <= 1 ? 'hour' : selectedRange <= 7 ? 'day' : 'week'
  const [topUserLimit, setTopUserLimit] = useState(10)
  const [rangeRevision, setRangeRevision] = useState(0)

  const handleRangeChange = useCallback((days: number) => {
    setSelectedRange(days)
    setRangeRevision((current) => current + 1)
  }, [])

  useEffect(() => {
    const updateTheme = async () => {
      setThemeReady(false)
      if (!themeManagerPromise) {
        themeManagerPromise = import('@visactor/vchart').then(
          (m) => m.ThemeManager
        )
      }
      const ThemeManager = await themeManagerPromise
      themeManagerRef.current = ThemeManager
      ThemeManager.setCurrentTheme(resolvedTheme === 'dark' ? 'dark' : 'light')
      setThemeReady(true)
    }
    updateTheme()
  }, [resolvedTheme])

  const userQuotaQuery = useQuery({
    queryKey: ['dashboard', 'user-quota', selectedRange, rangeRevision],
    queryFn: ({ queryKey }) => {
      const rangeDays = Number(queryKey[2])
      const { start, end } = getDashboardDateRange(rangeDays)
      return getUserQuotaDataByUsers({
        start_timestamp: Math.floor(start.getTime() / 1000),
        end_timestamp: Math.floor(end.getTime() / 1000),
      })
    },
    select: (response) => ({
      data: response.success ? response.data : [],
      meta: response.meta,
    }),
    staleTime: 30_000,
    refetchInterval: DASHBOARD_USER_REFRESH_INTERVAL,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: true,
  })
  const userData = userQuotaQuery.data?.data ?? EMPTY_USER_DATA
  const isLoading = userQuotaQuery.isLoading

  const chartData = useMemo(
    () =>
      processUserChartData(
        isLoading ? [] : userData,
        timeGranularity,
        t,
        topUserLimit,
        customization.preset
      ),
    [
      userData,
      isLoading,
      timeGranularity,
      t,
      topUserLimit,
      customization.preset,
    ]
  )

  return (
    <div className='space-y-3'>
      <div className='flex items-center gap-1.5 overflow-x-auto pb-1 sm:gap-2'>
        <DashboardTimeRangeToggle
          value={selectedRange}
          onValueChange={handleRangeChange}
        />

        <div className='flex shrink-0 items-center gap-1.5 rounded-lg border p-0.5'>
          <span className='text-muted-foreground px-2 text-xs font-medium'>
            {t('Top Users')}
          </span>
          {TOP_USER_LIMIT_OPTIONS.map((limit) => (
            <button
              key={limit}
              type='button'
              onClick={() => setTopUserLimit(limit)}
              className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                topUserLimit === limit
                  ? 'bg-primary text-primary-foreground shadow-sm'
                  : 'text-muted-foreground hover:bg-muted hover:text-foreground'
              }`}
            >
              {t('Top {{count}}', { count: limit })}
            </button>
          ))}
        </div>

        {isLoading && (
          <Loader2 className='text-muted-foreground size-4 animate-spin' />
        )}
      </div>

      <DashboardRefreshStatus
        dataUpdatedAt={userQuotaQuery.dataUpdatedAt}
        isFetching={userQuotaQuery.isFetching}
        meta={userQuotaQuery.data?.meta}
        onRefresh={() => void userQuotaQuery.refetch()}
      />

      <div className='grid gap-3'>
        {USER_CHARTS.map((chart) => {
          const spec = chartData[chart.specKey]

          return (
            <div
              key={chart.value}
              className='overflow-hidden rounded-lg border'
            >
              <div className='flex w-full items-center gap-2 border-b px-3 py-2 sm:px-5 sm:py-3'>
                <Users className='text-muted-foreground/60 size-4' />
                <div className='text-sm font-semibold'>{t(chart.labelKey)}</div>
              </div>

              <div className='h-[300px] p-1.5 sm:h-96 sm:p-2'>
                {isLoading ? (
                  <Skeleton className='h-full w-full' />
                ) : (
                  themeReady &&
                  spec && (
                    <VChart
                      key={`user-${chart.value}-${topUserLimit}-${resolvedTheme}-${customization.preset}`}
                      spec={{
                        ...spec,
                        theme: resolvedTheme === 'dark' ? 'dark' : 'light',
                        background: 'transparent',
                      }}
                      option={VCHART_OPTION}
                    />
                  )
                )}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
