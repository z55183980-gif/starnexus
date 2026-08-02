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

import { useTranslation } from 'react-i18next'
import type { UpstreamAccountWindowStats } from './types'

function formatTodayStatsNumber(value: number): string {
  if (!Number.isFinite(value)) return '0'
  const absolute = Math.abs(value)
  const locale = undefined
  return new Intl.NumberFormat(locale, {
    notation: absolute >= 10_000 ? 'compact' : 'standard',
    maximumFractionDigits: 1,
  }).format(value)
}

function formatTodayStatsTokens(tokens: number): string {
  if (!Number.isFinite(tokens)) return '0'
  if (tokens >= 1_000_000) return `${(tokens / 1_000_000).toFixed(2)}M`
  if (tokens >= 1_000) return `${(tokens / 1_000).toFixed(1)}K`
  return tokens.toString()
}

function formatTodayStatsCurrency(amount: number): string {
  if (!Number.isFinite(amount)) return '$0.00'
  const fractionDigits = amount > 0 && amount < 0.01 ? 6 : 2
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: fractionDigits,
    maximumFractionDigits: fractionDigits,
  }).format(amount)
}

export function AccountTodayStatsCell({
  stats,
  loading = false,
  error = null,
}: {
  stats?: UpstreamAccountWindowStats | null
  loading?: boolean
  error?: string | null
}) {
  const { t } = useTranslation()

  if (loading && !stats) {
    return (
      <div className='space-y-0.5'>
        <div className='h-3 w-12 animate-pulse rounded bg-gray-200 dark:bg-gray-700' />
        <div className='h-3 w-16 animate-pulse rounded bg-gray-200 dark:bg-gray-700' />
        <div className='h-3 w-10 animate-pulse rounded bg-gray-200 dark:bg-gray-700' />
      </div>
    )
  }

  if (error && !stats) {
    return <div className='text-xs text-red-500'>{error}</div>
  }

  if (!stats) {
    return <div className='text-xs text-gray-400'>-</div>
  }

  return (
    <div className='space-y-0.5 text-xs'>
      <div className='flex items-center gap-1'>
        <span className='text-gray-500 dark:text-gray-400'>
          {t('Requests')}:
        </span>
        <span className='font-medium text-gray-700 dark:text-gray-300'>
          {formatTodayStatsNumber(stats.requests)}
        </span>
      </div>
      <div className='flex items-center gap-1'>
        <span className='text-gray-500 dark:text-gray-400'>Token:</span>
        <span className='font-medium text-gray-700 dark:text-gray-300'>
          {formatTodayStatsTokens(stats.tokens)}
        </span>
      </div>
      <div className='flex items-center gap-1'>
        <span className='text-gray-500 dark:text-gray-400'>
          {t('Account billed')}:
        </span>
        <span className='font-medium text-emerald-600 dark:text-emerald-400'>
          {formatTodayStatsCurrency(stats.cost)}
        </span>
      </div>
      <div className='flex items-center gap-1'>
        <span className='text-gray-500 dark:text-gray-400'>
          {t('User billed')}:
        </span>
        <span className='font-medium text-gray-700 dark:text-gray-300'>
          {formatTodayStatsCurrency(stats.user_cost)}
        </span>
      </div>
    </div>
  )
}
