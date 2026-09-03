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
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatDateTimeObject } from '@/lib/time'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import type { QuotaDataMeta } from '@/features/dashboard/types'

interface DashboardRefreshStatusProps {
  dataUpdatedAt: number
  isFetching: boolean
  meta?: QuotaDataMeta
  onRefresh: () => void
}

export function DashboardRefreshStatus(props: DashboardRefreshStatusProps) {
  const { t } = useTranslation()
  const updatedAt = (props.meta?.queried_at ?? 0) * 1000 || props.dataUpdatedAt
  const updatedLabel = updatedAt
    ? t('Last updated: {{time}}', {
        time: formatDateTimeObject(new Date(updatedAt)),
      })
    : t('Waiting for statistics')

  return (
    <div className='flex flex-wrap items-center justify-end gap-2'>
      <span className='text-muted-foreground text-xs tabular-nums'>
        {updatedLabel}
      </span>
      {props.meta?.aggregation_enabled === false && (
        <span className='text-warning text-xs font-medium'>
          {t('Data aggregation is disabled')}
        </span>
      )}
      <Button
        variant='outline'
        size='sm'
        onClick={props.onRefresh}
        disabled={props.isFetching}
        aria-label={t('Refresh Stats')}
      >
        <RefreshCw
          data-icon='inline-start'
          className={cn(props.isFetching && 'animate-spin')}
        />
        {props.isFetching ? t('Refreshing...') : t('Refresh Stats')}
      </Button>
    </div>
  )
}
