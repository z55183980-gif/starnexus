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
import { type ColumnDef } from '@tanstack/react-table'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  formatCurrencyFromUSD,
  formatLocalCurrencyAmount,
} from '@/lib/currency'
import { formatTimestampToDate } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { DataTableColumnHeader } from '@/components/data-table'
import { StatusBadge } from '@/components/status-badge'
import { getPaymentMethodName, getStatusConfig } from '../lib/billing'
import type { TopupRecord } from '../types'

const amountFormatOptions = {
  digitsLarge: 4,
  digitsSmall: 6,
  abbreviate: false,
} as const

const paymentFormatOptions = {
  digitsLarge: 2,
  digitsSmall: 2,
  abbreviate: false,
} as const

function isSubscriptionTopup(record: TopupRecord): boolean {
  const tradeNo = (record.trade_no || '').toLowerCase()
  return Number(record.amount || 0) === 0 && tradeNo.startsWith('sub')
}

function AmountBadge({ children }: { children: string }) {
  return (
    <span className='border-border/80 bg-muted/60 inline-flex w-fit items-center rounded-md border px-1.5 py-0.5 font-mono text-xs font-semibold tabular-nums'>
      {children}
    </span>
  )
}

interface UseBillingHistoryColumnsOptions {
  isAdmin: boolean
  completing: boolean
  onCompleteOrder: (tradeNo: string) => void
}

export function useBillingHistoryColumns({
  isAdmin,
  completing,
  onCompleteOrder,
}: UseBillingHistoryColumnsOptions): ColumnDef<TopupRecord>[] {
  const { t } = useTranslation()

  const columns: ColumnDef<TopupRecord>[] = [
    {
      accessorKey: 'create_time',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Time')} />
      ),
      cell: ({ row }) => {
        const record = row.original
        const statusConfig = getStatusConfig(record.status)

        return (
          <div className='flex min-w-[140px] flex-col gap-0.5'>
            <span className='font-mono text-xs tabular-nums'>
              {formatTimestampToDate(record.create_time)}
            </span>
            <StatusBadge
              label={t(statusConfig.label)}
              variant={statusConfig.variant}
              size='sm'
              showDot
              copyable={false}
            />
          </div>
        )
      },
      enableHiding: false,
      meta: { label: t('Time') },
    },
    {
      accessorKey: 'trade_no',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Order No.')} />
      ),
      cell: ({ row }) => {
        const tradeNo = row.getValue('trade_no') as string

        return (
          <StatusBadge
            label={tradeNo}
            variant='neutral'
            copyText={tradeNo}
            size='sm'
            showDot={false}
            className='max-w-[200px] truncate font-mono'
          />
        )
      },
      meta: { label: t('Order No.'), mobileTitle: true },
    },
    {
      accessorKey: 'payment_method',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Payment Method')} />
      ),
      cell: ({ row }) => (
        <span className='text-sm'>
          {getPaymentMethodName(row.getValue('payment_method') as string, t)}
        </span>
      ),
      meta: { label: t('Payment Method') },
    },
    {
      accessorKey: 'amount',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Amount')} />
      ),
      cell: ({ row }) => {
        const record = row.original

        if (isSubscriptionTopup(record)) {
          return (
            <StatusBadge
              label={t('Subscription')}
              variant='neutral'
              size='sm'
              copyable={false}
            />
          )
        }

        const amount = row.getValue('amount') as number
        return (
          <AmountBadge>
            {formatCurrencyFromUSD(amount, amountFormatOptions)}
          </AmountBadge>
        )
      },
      meta: { label: t('Amount') },
    },
    {
      accessorKey: 'money',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Payment')} />
      ),
      cell: ({ row }) => {
        const record = row.original
        // New orders persist the gateway charge separately so processing
        // fees are visible. Fall back to Money for historical rows.
        const paymentAmount = Number(record.payment_amount)
        const money =
          Number.isFinite(paymentAmount) && paymentAmount > 0
            ? paymentAmount
            : (row.getValue('money') as number)

        return (
          <AmountBadge>
            {formatLocalCurrencyAmount(money, paymentFormatOptions)}
          </AmountBadge>
        )
      },
      meta: { label: t('Payment') },
    },
  ]

  if (isAdmin) {
    columns.splice(1, 0, {
      accessorKey: 'username',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('User')} />
      ),
      cell: ({ row }) => {
        const record = row.original
        const userId = record.user_id
        const displayName =
          record.username || (userId != null ? String(userId) : '')

        if (!displayName) {
          return <span className='text-muted-foreground text-xs'>-</span>
        }

        return (
          <StatusBadge
            label={displayName}
            variant='neutral'
            copyText={displayName}
            size='sm'
            showDot={false}
            className={record.username ? '' : 'font-mono'}
          />
        )
      },
      meta: { label: t('User'), mobileHidden: true },
    })

    columns.push({
      id: 'actions',
      header: t('Actions'),
      cell: ({ row }) => {
        const record = row.original

        if (record.status !== 'pending') {
          return null
        }

        return (
          <Button
            size='sm'
            variant='outline'
            onClick={() => onCompleteOrder(record.trade_no)}
            disabled={completing}
          >
            {completing ? <Loader2 className='size-4 animate-spin' /> : null}
            {t('Complete Order')}
          </Button>
        )
      },
      meta: { label: t('Actions'), mobileHidden: true },
    })
  }

  return columns
}
