import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type KeyboardEvent,
} from 'react'
import {
  type ColumnDef,
  functionalUpdate,
  getCoreRowModel,
  getPaginationRowModel,
  useReactTable,
  type PaginationState,
} from '@tanstack/react-table'
import {
  ArrowReloadHorizontalIcon,
  Download03Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { api } from '@/lib/api'
import { formatBillingCurrencyFromUSD } from '@/lib/currency'
import { formatDateTimeStr } from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { DataTablePage, DataTableToolbar } from '@/components/data-table'
import { SectionPageLayout } from '@/components/layout'

type LedgerAction = 'all' | 'accrue' | 'transfer'

type LedgerRow = {
  id: number
  user_id: number
  username: string
  action: string
  amount_usd: string
  source_user_id?: number | null
  source_username?: string | null
  source_topup_amount?: number | null
  created_at: string
}

const DEFAULT_PAGE_SIZE = 20
const ALL_ACTIONS_VALUE: LedgerAction = 'all'

function formatLedgerAction(
  action: string,
  t: (key: string) => string
): string {
  switch (action) {
    case 'accrue':
      return t('Rebate accrual')
    case 'transfer':
      return t('Transfer to balance')
    default:
      return action || '-'
  }
}

function formatLedgerAmount(value: string | number | undefined): string {
  const amount = Number(value ?? 0)
  if (!Number.isFinite(amount)) return '-'
  return formatBillingCurrencyFromUSD(amount, {
    digitsLarge: 2,
    digitsSmall: 4,
    abbreviate: false,
  })
}

function formatLedgerDate(value?: string | null): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return formatDateTimeStr(date)
}

function formatUserLabel(username?: string | null, userID?: number | null) {
  if (username) return username
  if (userID != null) return `#${userID}`
  return '-'
}

function escapeCSVCell(value: unknown): string {
  if (value == null) return ''
  const text = String(value)
  if (/[",\r\n]/.test(text)) {
    return `"${text.replace(/"/g, '""')}"`
  }
  return text
}

function buildAffiliateLedgerCSV(
  rows: LedgerRow[],
  t: (key: string) => string
): string {
  const headers = [
    t('ID'),
    t('User'),
    t('User ID'),
    t('Action'),
    t('Rebate Amount'),
    t('Source user'),
    t('Source user ID'),
    t('Recharge Amount'),
    t('Time'),
  ]
  const lines = rows.map((row) =>
    [
      row.id,
      formatUserLabel(row.username, row.user_id),
      row.user_id,
      formatLedgerAction(row.action, t),
      formatLedgerAmount(row.amount_usd),
      formatUserLabel(row.source_username, row.source_user_id),
      row.source_user_id ?? '',
      row.source_topup_amount == null
        ? ''
        : formatLedgerAmount(row.source_topup_amount),
      formatLedgerDate(row.created_at),
    ]
      .map(escapeCSVCell)
      .join(',')
  )
  return [headers.map(escapeCSVCell).join(','), ...lines].join('\r\n')
}

function downloadCSV(content: string) {
  const blob = new Blob(['\ufeff', content], {
    type: 'text/csv;charset=utf-8',
  })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = `affiliate-ledgers-${new Date()
    .toISOString()
    .slice(0, 19)
    .replace(/[:T]/g, '-')}.csv`
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

export function AdminAffiliateRecords() {
  const { t } = useTranslation()
  const [rows, setRows] = useState<LedgerRow[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [keyword, setKeyword] = useState('')
  const [searchDraft, setSearchDraft] = useState('')
  const [action, setAction] = useState<LedgerAction>(ALL_ACTIONS_VALUE)
  const [userID, setUserID] = useState('')
  const [startDate, setStartDate] = useState('')
  const [endDate, setEndDate] = useState('')
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: DEFAULT_PAGE_SIZE,
  })
  const userRole = useAuthStore((state) => state.auth.user?.role ?? ROLE.GUEST)
  const ledgerEndpoint =
    userRole === ROLE.AGENT
      ? '/api/affiliate/agent/ledger'
      : '/api/affiliate/admin/ledger'
  const canFilterUser = userRole !== ROLE.AGENT

  const load = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)
      const params = new URLSearchParams({
        page: String(pagination.pageIndex + 1),
        page_size: String(pagination.pageSize),
      })
      if (keyword) params.append('keyword', keyword)
      if (action !== ALL_ACTIONS_VALUE) params.append('action', action)
      if (canFilterUser && userID.trim()) {
        params.append('user_id', userID.trim())
      }
      if (startDate) {
        params.append(
          'start_time',
          new Date(`${startDate}T00:00:00`).toISOString()
        )
      }
      if (endDate) {
        params.append('end_time', new Date(`${endDate}T23:59:59`).toISOString())
      }
      const res = await api.get<{
        success: boolean
        message?: string
        data: {
          items: LedgerRow[]
          total: number
          page: number
          page_size: number
        }
      }>(`${ledgerEndpoint}?${params.toString()}`)
      if (res.data.success) {
        setRows(res.data.data.items ?? [])
        setTotal(res.data.data.total ?? 0)
      } else {
        const message =
          res.data.message || t('Failed to load affiliate records')
        setError(message)
        setRows([])
        setTotal(0)
      }
    } catch (err) {
      const message =
        err instanceof Error
          ? err.message
          : t('Failed to load affiliate records')
      setError(message)
      setRows([])
      setTotal(0)
    } finally {
      setLoading(false)
    }
  }, [
    action,
    canFilterUser,
    endDate,
    keyword,
    ledgerEndpoint,
    pagination.pageIndex,
    pagination.pageSize,
    startDate,
    t,
    userID,
  ])

  const handleApplySearch = useCallback(() => {
    setPagination((current) => ({ ...current, pageIndex: 0 }))
    setKeyword(searchDraft.trim())
  }, [searchDraft])

  const handleResetSearch = useCallback(() => {
    setSearchDraft('')
    setKeyword('')
    setAction(ALL_ACTIONS_VALUE)
    setUserID('')
    setStartDate('')
    setEndDate('')
    setPagination((current) => ({ ...current, pageIndex: 0 }))
  }, [])

  const handleSearchKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      handleApplySearch()
    }
  }

  const resetToFirstPage = useCallback(() => {
    setPagination((current) => ({ ...current, pageIndex: 0 }))
  }, [])

  const handleExport = useCallback(() => {
    if (!rows.length) {
      toast.info(t('No records to export'))
      return
    }
    downloadCSV(buildAffiliateLedgerCSV(rows, t))
    toast.success(t('Exported current page'))
  }, [rows, t])

  useEffect(() => {
    void load()
  }, [load])

  const pageCount = Math.max(1, Math.ceil(total / pagination.pageSize))
  useEffect(() => {
    if (pagination.pageIndex + 1 > pageCount) {
      setPagination((current) => ({
        ...current,
        pageIndex: Math.max(0, pageCount - 1),
      }))
    }
  }, [pageCount, pagination.pageIndex])

  const columns = useMemo<ColumnDef<LedgerRow>[]>(
    () => [
      {
        accessorKey: 'id',
        header: t('ID'),
        cell: ({ row }) => (
          <span className='font-mono tabular-nums'>{row.original.id}</span>
        ),
        size: 72,
        meta: { label: t('ID'), mobileHidden: true },
      },
      {
        accessorKey: 'username',
        header: t('User'),
        cell: ({ row }) => (
          <div className='min-w-0'>
            <div className='truncate font-medium'>
              {formatUserLabel(row.original.username, row.original.user_id)}
            </div>
            <div className='text-muted-foreground font-mono text-xs'>
              #{row.original.user_id}
            </div>
          </div>
        ),
        size: 160,
        meta: { label: t('User'), mobileTitle: true },
      },
      {
        accessorKey: 'action',
        header: t('Action'),
        cell: ({ row }) => (
          <Badge variant='secondary'>
            {formatLedgerAction(row.original.action, t)}
          </Badge>
        ),
        size: 128,
        meta: { label: t('Action'), mobileBadge: true },
      },
      {
        accessorKey: 'amount_usd',
        header: t('Rebate Amount'),
        cell: ({ row }) => (
          <span className='font-mono tabular-nums'>
            {formatLedgerAmount(row.original.amount_usd)}
          </span>
        ),
        size: 120,
        meta: { label: t('Rebate Amount') },
      },
      {
        accessorKey: 'source_username',
        header: t('Source user'),
        cell: ({ row }) => (
          <div className='min-w-0'>
            <div className='truncate'>
              {formatUserLabel(
                row.original.source_username,
                row.original.source_user_id
              )}
            </div>
            {row.original.source_user_id != null && (
              <div className='text-muted-foreground font-mono text-xs'>
                #{row.original.source_user_id}
              </div>
            )}
          </div>
        ),
        size: 160,
        meta: { label: t('Source user') },
      },
      {
        accessorKey: 'source_topup_amount',
        header: t('Recharge Amount'),
        cell: ({ row }) =>
          row.original.source_topup_amount == null ? (
            '-'
          ) : (
            <span className='font-mono tabular-nums'>
              {formatLedgerAmount(row.original.source_topup_amount)}
            </span>
          ),
        size: 120,
        meta: { label: t('Recharge Amount') },
      },
      {
        accessorKey: 'created_at',
        header: t('Time'),
        cell: ({ row }) => formatLedgerDate(row.original.created_at),
        size: 172,
        meta: { label: t('Time') },
      },
    ],
    [t]
  )

  const table = useReactTable({
    data: rows,
    columns,
    state: { pagination },
    onPaginationChange: (updater) => {
      const next = functionalUpdate(updater, pagination)
      setPagination(next)
    },
    getCoreRowModel: getCoreRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    manualPagination: true,
    pageCount,
  })

  const hasFilters =
    !!keyword ||
    !!searchDraft ||
    action !== ALL_ACTIONS_VALUE ||
    !!userID ||
    !!startDate ||
    !!endDate

  const toolbar = (
    <DataTableToolbar
      table={table}
      hideViewOptions
      hasAdditionalFilters={hasFilters}
      onReset={handleResetSearch}
      onSearch={handleApplySearch}
      searchLoading={loading}
      customSearch={
        <Input
          placeholder={t(
            'Search by record ID, user ID, username, source user, or action...'
          )}
          value={searchDraft}
          onChange={(e) => setSearchDraft(e.target.value)}
          onKeyDown={handleSearchKeyDown}
          className='w-full sm:w-[280px] lg:w-[360px]'
        />
      }
      additionalSearch={
        <>
          <Select
            value={action}
            onValueChange={(value) => {
              setAction((value || ALL_ACTIONS_VALUE) as LedgerAction)
              resetToFirstPage()
            }}
          >
            <SelectTrigger className='w-full sm:w-[148px]'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                <SelectItem value={ALL_ACTIONS_VALUE}>
                  {t('All actions')}
                </SelectItem>
                <SelectItem value='accrue'>{t('Rebate accrual')}</SelectItem>
                <SelectItem value='transfer'>
                  {t('Transfer to balance')}
                </SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
          {canFilterUser && (
            <Input
              inputMode='numeric'
              placeholder={t('User ID')}
              value={userID}
              onChange={(e) => {
                const value = e.target.value.replace(/[^\d]/g, '')
                setUserID(value)
                resetToFirstPage()
              }}
              onKeyDown={handleSearchKeyDown}
              className='w-full sm:w-[112px]'
            />
          )}
        </>
      }
      expandable={
        <>
          <Input
            type='date'
            aria-label={t('Start date')}
            value={startDate}
            onChange={(e) => {
              setStartDate(e.target.value)
              resetToFirstPage()
            }}
            className='w-full sm:w-[148px]'
          />
          <Input
            type='date'
            aria-label={t('End date')}
            value={endDate}
            onChange={(e) => {
              setEndDate(e.target.value)
              resetToFirstPage()
            }}
            className='w-full sm:w-[148px]'
          />
        </>
      }
      hasExpandedActiveFilters={!!startDate || !!endDate}
      preActions={
        <>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant='outline'
                  onClick={() => void load()}
                  disabled={loading}
                  className='size-8 p-0'
                >
                  <span className='sr-only'>{t('Refresh')}</span>
                  <HugeiconsIcon icon={ArrowReloadHorizontalIcon} />
                </Button>
              }
            />
            <TooltipContent>{t('Refresh')}</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant='outline'
                  onClick={handleExport}
                  disabled={loading || rows.length === 0}
                  className='size-8 p-0'
                >
                  <span className='sr-only'>{t('Export current page')}</span>
                  <HugeiconsIcon icon={Download03Icon} />
                </Button>
              }
            />
            <TooltipContent>{t('Export current page')}</TooltipContent>
          </Tooltip>
        </>
      }
    />
  )

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Affiliate Rebate Records')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t('Audit trail for USD referral rebates and transfers.')}
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        <DataTablePage
          table={table}
          columns={columns}
          isLoading={loading}
          emptyTitle={
            error
              ? t('Failed to load affiliate records')
              : t('No affiliate rebate records yet')
          }
          emptyDescription={
            error
              ? error
              : hasFilters
                ? t('Try adjusting your search')
                : t('Affiliate rebate activity will appear here')
          }
          toolbar={toolbar}
          skeletonKeyPrefix='affiliate-ledgers-skeleton'
          tableClassName='max-h-[calc(100dvh-16rem)] overflow-auto sm:max-h-[calc(100dvh-14rem)]'
          tableHeaderClassName='bg-muted/30 sticky top-0 z-10'
          applyHeaderSize
        />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
