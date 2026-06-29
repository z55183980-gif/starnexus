import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  functionalUpdate,
  getCoreRowModel,
  getPaginationRowModel,
  useReactTable,
  type ColumnDef,
  type PaginationState,
} from '@tanstack/react-table'
import { Share2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { CopyButton } from '@/components/copy-button'
import { DataTablePagination } from '@/components/data-table/pagination'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatQuota } from '@/lib/format'
import { getSelf } from '@/lib/api'
import { cn } from '@/lib/utils'
import {
  generateAffiliateLink,
  getAffiliateDetail,
  transferAffiliateUSD,
  type AffiliateDetail,
  type AffiliateInvitee,
} from './api'

const DEFAULT_INVITEE_PAGE_SIZE = 10

function formatUSD(value: string | number | undefined): string {
  const n = typeof value === 'string' ? parseFloat(value) : (value ?? 0)
  if (!Number.isFinite(n)) return '$0.00'
  return `$${n.toFixed(2)}`
}

export function AffiliateProgram() {
  const { t } = useTranslation()
  const [detail, setDetail] = useState<AffiliateDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [inviteesRefreshing, setInviteesRefreshing] = useState(false)
  const [transferring, setTransferring] = useState(false)
  const [inviteePage, setInviteePage] = useState(1)
  const [inviteePageSize, setInviteePageSize] = useState(
    DEFAULT_INVITEE_PAGE_SIZE
  )
  const loadedRef = useRef(false)

  const load = useCallback(async () => {
    try {
      if (loadedRef.current) {
        setInviteesRefreshing(true)
      } else {
        setLoading(true)
      }
      const res = await getAffiliateDetail({
        page: inviteePage,
        pageSize: inviteePageSize,
      })
      if (res.data.success) {
        setDetail(res.data.data)
      }
    } catch {
      toast.error(t('Failed to load affiliate data'))
    } finally {
      loadedRef.current = true
      setLoading(false)
      setInviteesRefreshing(false)
    }
  }, [inviteePage, inviteePageSize, t])

  useEffect(() => {
    void load()
  }, [load])

  const inviteLink = detail ? generateAffiliateLink(detail.aff_code) : ''
  const inviteeTotal =
    detail?.invitees_total ?? detail?.aff_count ?? detail?.invitees?.length ?? 0
  const inviteePageCount = Math.max(1, Math.ceil(inviteeTotal / inviteePageSize))
  const inviteePagination = useMemo<PaginationState>(
    () => ({
      pageIndex: inviteePage - 1,
      pageSize: inviteePageSize,
    }),
    [inviteePage, inviteePageSize]
  )
  const inviteeColumns = useMemo<ColumnDef<AffiliateInvitee>[]>(() => [], [])
  const inviteeTable = useReactTable({
    data: detail?.invitees ?? [],
    columns: inviteeColumns,
    state: { pagination: inviteePagination },
    onPaginationChange: (updater) => {
      const next = functionalUpdate(updater, inviteePagination)
      if (next.pageSize !== inviteePagination.pageSize) {
        setInviteePage(1)
        setInviteePageSize(next.pageSize)
        return
      }
      if (next.pageIndex !== inviteePagination.pageIndex) {
        setInviteePage(next.pageIndex + 1)
      }
    },
    getCoreRowModel: getCoreRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    manualPagination: true,
    pageCount: inviteePageCount,
  })

  useEffect(() => {
    if (detail && inviteePage > inviteePageCount) {
      setInviteePage(inviteePageCount)
    }
  }, [detail, inviteePage, inviteePageCount])

  const handleTransferUSD = async () => {
    try {
      setTransferring(true)
      const res = await transferAffiliateUSD()
      if (res.data.success) {
        toast.success(
          t('Transferred {{usd}} to balance (+{{quota}} quota)', {
            usd: formatUSD(res.data.data.transferred_usd),
            quota: formatQuota(res.data.data.quota_added),
          })
        )
        await getSelf()
        await load()
      }
    } catch {
      toast.error(t('Transfer failed'))
    } finally {
      setTransferring(false)
    }
  }

  if (loading) {
    return (
      <div className='space-y-4 p-4'>
        <Skeleton className='h-10 w-64' />
        <Skeleton className='h-40 w-full' />
      </div>
    )
  }

  if (!detail) {
    return (
      <div className='text-muted-foreground p-4 text-sm'>
        {t('Failed to load affiliate data')}
      </div>
    )
  }

  const availableUSD = parseFloat(String(detail.aff_quota_usd)) || 0
  const frozenUSD = parseFloat(String(detail.aff_frozen_quota_usd)) || 0

  return (
    <div className='mx-auto flex w-full max-w-5xl flex-col gap-4 p-4'>
      <div className='flex items-center gap-2'>
        <Share2 className='text-muted-foreground size-5' />
        <h1 className='text-xl font-semibold'>{t('Referral Program')}</h1>
      </div>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Share your link. When invitees top up, you earn {{rate}}% rebate.',
          { rate: detail.effective_rebate_rate_percent }
        )}
      </p>

      <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
        {[
          [t('Rebate Rate'), `${detail.effective_rebate_rate_percent}%`],
          [t('Invites'), String(detail.aff_count)],
          [t('Available (USD)'), formatUSD(availableUSD)],
          [t('Total Earned (USD)'), formatUSD(detail.aff_history_quota_usd)],
        ].map(([label, value]) => (
          <Card key={label}>
            <CardHeader className='pb-2'>
              <CardTitle className='text-muted-foreground text-xs font-medium uppercase'>
                {label}
              </CardTitle>
            </CardHeader>
            <CardContent className='text-lg font-semibold tabular-nums'>
              {value}
            </CardContent>
          </Card>
        ))}
      </div>

      {frozenUSD > 0 && (
        <p className='text-amber-600 text-sm dark:text-amber-400'>
          {t('Frozen rebate (USD): {{amount}}', { amount: formatUSD(frozenUSD) })}
        </p>
      )}

      <Card>
        <CardContent className='grid gap-4 p-4 md:grid-cols-2'>
          <div>
            <div className='mb-1 text-sm font-medium'>{t('Your invite code')}</div>
            <div className='flex items-center gap-2'>
              <Input readOnly value={detail.aff_code} />
              <CopyButton value={detail.aff_code} />
            </div>
          </div>
          <div>
            <div className='mb-1 text-sm font-medium'>{t('Invite link')}</div>
            <div className='flex items-center gap-2'>
              <Input readOnly value={inviteLink} />
              <CopyButton value={inviteLink} />
            </div>
          </div>
        </CardContent>
      </Card>

      <div className='flex gap-2'>
        <Button
          disabled={!detail.affiliate_enabled || availableUSD <= 0 || transferring}
          onClick={() => void handleTransferUSD()}
        >
          {transferring ? t('Transferring...') : t('Transfer to Balance')}
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className='text-base'>{t('Invitees')}</CardTitle>
        </CardHeader>
        <CardContent className='p-0'>
          <Table
            className={cn(
              'transition-opacity',
              inviteesRefreshing && 'opacity-60'
            )}
          >
            <TableHeader>
              <TableRow>
                <TableHead>{t('User')}</TableHead>
                <TableHead>{t('Email')}</TableHead>
                <TableHead>{t('Rebate (USD)')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {detail.invitees?.length ? (
                detail.invitees.map((row) => (
                  <TableRow key={row.user_id}>
                    <TableCell>{row.username || `#${row.user_id}`}</TableCell>
                    <TableCell>{row.email || '—'}</TableCell>
                    <TableCell>{formatUSD(row.total_rebate_usd)}</TableCell>
                  </TableRow>
                ))
              ) : (
                <TableRow>
                  <TableCell colSpan={3} className='text-muted-foreground text-center'>
                    {t('No invitees yet')}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
          {inviteeTotal > 0 && (
            <div className='border-t px-4 py-3'>
              <DataTablePagination table={inviteeTable} />
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
