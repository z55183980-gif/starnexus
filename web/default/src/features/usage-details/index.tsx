/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or (at your
option) any later version.
*/
import { useMemo, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import {
  type ColumnDef,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  type PaginationState,
  type SortingState,
  useReactTable,
} from '@tanstack/react-table'
import {
  ChartNoAxesCombined,
  Download,
  FileText,
  RefreshCw,
  TriangleAlert,
  Trash2,
  type LucideIcon,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import {
  formatLogQuota,
  formatTimestampToDate,
  formatUseTime,
} from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { useIsAdmin } from '@/hooks/use-admin'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
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
import { TableCell, TableRow } from '@/components/ui/table'
import {
  DataTableColumnHeader,
  DataTablePage,
  DataTableViewOptions,
} from '@/components/data-table'
import { SectionPageLayout } from '@/components/layout'
import { deleteLogsBefore } from '@/features/system-settings/api'
import {
  getAllLogs,
  getAgentLogs,
  getUserLogs,
} from '@/features/usage-logs/api'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import { DEFAULT_LOGS_DATA } from '@/features/usage-logs/constants'
import type { UsageLog } from '@/features/usage-logs/data/schema'
import {
  parseLogOther,
  getLogUseTimeSeconds,
} from '@/features/usage-logs/lib/format'
import { getDefaultTimeRange } from '@/features/usage-logs/lib/utils'
import type { GetLogsParams, LogAccessScope } from '@/features/usage-logs/types'
import { UsersRankingCard } from '@/features/users/components/users-ranking-card'

type UsageDetailsTab = 'usage' | 'errors' | 'ranking'

type UsageDetailsFilters = {
  username: string
  token: string
  model: string
  account: string
  group: string
  stream: '' | 'true' | 'false'
  billingType: '' | '0' | '1'
  billingMode: '' | 'token' | 'per_request' | 'image' | 'video' | 'tiered_expr'
  startTime?: Date
  endTime?: Date
}

const defaultFilters = (): UsageDetailsFilters => {
  const { start, end } = getDefaultTimeRange()
  return {
    username: '',
    token: '',
    model: '',
    account: '',
    group: '',
    stream: '',
    billingType: '',
    billingMode: '',
    startTime: start,
    endTime: end,
  }
}

function buildQueryParams(
  filters: UsageDetailsFilters,
  page: PaginationState,
  isManagedScope: boolean,
  logType: number
): GetLogsParams {
  const params: GetLogsParams = {
    p: page.pageIndex + 1,
    page_size: page.pageSize,
    // Usage details intentionally excludes top-up/manage/audit rows.
    type: logType,
  }
  const addFuzzyFilter = (value: string, assign: (value: string) => void) => {
    const trimmed = value.trim()
    // The shared backend fuzzy-search contract requires at least two
    // characters; wait until that threshold instead of issuing an error.
    if (trimmed.length >= 2) assign(trimmed)
  }
  if (isManagedScope)
    addFuzzyFilter(filters.username, (value) => {
      params.username = value
    })
  addFuzzyFilter(filters.token, (value) => {
    params.token_name = value
  })
  addFuzzyFilter(filters.model, (value) => {
    params.model_name = value
  })
  addFuzzyFilter(filters.group, (value) => {
    params.group = value
  })
  if (isManagedScope)
    addFuzzyFilter(filters.account, (value) => {
      params.account = value
    })
  if (filters.stream) params.stream = filters.stream === 'true'
  if (filters.billingMode && logType === 2)
    params.billing_mode = filters.billingMode
  if (filters.billingType && logType === 2)
    params.billing_type = Number(filters.billingType)
  if (filters.startTime)
    params.start_timestamp = Math.floor(filters.startTime.getTime() / 1000)
  if (filters.endTime)
    params.end_timestamp = Math.floor(filters.endTime.getTime() / 1000)
  return params
}

function csvCell(value: unknown): string {
  const text = value == null ? '' : String(value)
  return `"${text.replaceAll('"', '""')}"`
}

function getBillingMode(log: UsageLog): string {
  const other = parseLogOther(log.other)
  return (
    other?.billing_mode ||
    (other?.video_enabled ? 'video' : other?.image ? 'image' : 'token')
  )
}

function exportLogs(logs: UsageLog[], t: (key: string) => string) {
  const header = [
    t('User'),
    t('API Key'),
    t('Account'),
    t('Model'),
    t('Endpoint'),
    t('Group'),
    t('Type'),
    t('Billing Mode'),
    t('Tokens'),
    t('Cost'),
    t('Latency'),
    t('Time'),
    t('IP'),
  ]
  const rows = logs.map((log) => {
    const other = parseLogOther(log.other)
    const endpoint = other?.request_path || log.channel_name || ''
    return [
      log.username,
      log.token_name,
      log.upstream_account_name || '',
      log.model_name,
      endpoint,
      log.group,
      log.is_stream ? t('Stream') : t('Sync'),
      getBillingMode(log),
      log.prompt_tokens + log.completion_tokens,
      formatLogQuota(log.quota),
      formatUseTime(getLogUseTimeSeconds(log)),
      formatTimestampToDate(log.created_at),
      log.ip,
    ]
  })
  const content = [header, ...rows]
    .map((row) => row.map(csvCell).join(','))
    .join('\r\n')
  const blob = new Blob(['\ufeff', content], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = `usage-details-${new Date().toISOString().slice(0, 10)}.csv`
  anchor.click()
  URL.revokeObjectURL(url)
}

function useUsageDetailsColumns(isManagedScope: boolean, isAdmin: boolean) {
  const { t } = useTranslation()
  return useMemo<ColumnDef<UsageLog>[]>(() => {
    const columns: ColumnDef<UsageLog>[] = []
    if (isManagedScope) {
      columns.push({
        accessorKey: 'username',
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('User')} />
        ),
        cell: ({ row }) => (
          <span className='max-w-[160px] truncate'>
            {row.original.username || '-'}
          </span>
        ),
        meta: { label: t('User') },
      })
    }
    columns.push(
      {
        accessorKey: 'token_name',
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('API Key')} />
        ),
        cell: ({ row }) => (
          <span className='max-w-[180px] truncate font-mono text-xs'>
            {row.original.token_name || '-'}
          </span>
        ),
        meta: { label: t('API Key') },
      },
      {
        accessorKey: 'model_name',
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Model')} />
        ),
        cell: ({ row }) => (
          <span className='max-w-[190px] truncate font-medium'>
            {row.original.model_name || '-'}
          </span>
        ),
        meta: { label: t('Model') },
      },
      {
        id: 'endpoint',
        accessorFn: (log) =>
          parseLogOther(log.other)?.request_path || log.channel_name || '',
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Endpoint')} />
        ),
        cell: ({ row }) => {
          const other = parseLogOther(row.original.other)
          return (
            <span className='max-w-[220px] truncate text-xs'>
              {other?.request_path || row.original.channel_name || '-'}
            </span>
          )
        },
        meta: { label: t('Endpoint') },
      },
      {
        accessorKey: 'group',
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Group')} />
        ),
        cell: ({ row }) => (
          <span className='text-muted-foreground'>
            {row.original.group || '-'}
          </span>
        ),
        meta: { label: t('Group') },
      },
      {
        id: 'request_type',
        accessorFn: (log) => (log.is_stream ? 'stream' : 'sync'),
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Type')} />
        ),
        cell: ({ row }) => (
          <span className='text-info'>
            {row.original.is_stream ? t('Stream') : t('Sync')}
          </span>
        ),
        meta: { label: t('Type') },
      },
      {
        id: 'billing_mode',
        accessorFn: (log) => getBillingMode(log),
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Billing Mode')} />
        ),
        cell: ({ row }) => {
          const mode = getBillingMode(row.original)
          return (
            <span className='text-chart-2'>
              {t(
                mode === 'per_request'
                  ? 'Per Request'
                  : mode === 'tiered_expr'
                    ? 'Tiered'
                    : mode === 'image'
                      ? 'Image'
                      : mode === 'video'
                        ? 'Video'
                        : 'Token-based'
              )}
            </span>
          )
        },
        meta: { label: t('Billing Mode') },
      },
      {
        id: 'tokens',
        accessorFn: (log) => log.prompt_tokens + log.completion_tokens,
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Tokens')} />
        ),
        cell: ({ row }) => (
          <span className='font-mono text-xs tabular-nums'>
            {(
              row.original.prompt_tokens + row.original.completion_tokens
            ).toLocaleString()}
          </span>
        ),
        meta: { label: t('Tokens') },
      },
      {
        accessorKey: 'quota',
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Cost')} />
        ),
        cell: ({ row }) => (
          <span className='font-mono text-xs tabular-nums'>
            {formatLogQuota(row.original.quota)}
          </span>
        ),
        meta: { label: t('Cost') },
      },
      {
        id: 'latency',
        accessorFn: (log) => getLogUseTimeSeconds(log),
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Latency')} />
        ),
        cell: ({ row }) => (
          <span className='font-mono text-xs tabular-nums'>
            {formatUseTime(getLogUseTimeSeconds(row.original))}
          </span>
        ),
        meta: { label: t('Latency') },
      },
      {
        accessorKey: 'created_at',
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Time')} />
        ),
        cell: ({ row }) => (
          <span className='font-mono text-xs tabular-nums'>
            {formatTimestampToDate(row.original.created_at)}
          </span>
        ),
        meta: { label: t('Time') },
      },
      {
        accessorKey: 'ip',
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('IP')} />
        ),
        cell: ({ row }) => (
          <span className='font-mono text-xs'>{row.original.ip || '-'}</span>
        ),
        meta: { label: t('IP') },
      }
    )
    if (isAdmin) {
      columns.splice(isManagedScope ? 2 : 1, 0, {
        accessorKey: 'upstream_account_name',
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Account')} />
        ),
        cell: ({ row }) => (
          <span className='max-w-[160px] truncate'>
            {row.original.upstream_account_name || '-'}
          </span>
        ),
        meta: { label: t('Account') },
      })
    }
    return columns
  }, [isAdmin, isManagedScope, t])
}

export function UsageDetails() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const isAdmin = useIsAdmin()
  const { user } = useAuthStore((state) => state.auth)
  const accessScope: LogAccessScope = isAdmin
    ? 'admin'
    : (user?.role ?? 0) >= ROLE.AGENT
      ? 'agent'
      : 'self'
  const isManagedScope = accessScope !== 'self'
  const [activeTab, setActiveTab] = useState<UsageDetailsTab>('usage')
  const [filters, setFilters] = useState<UsageDetailsFilters>(defaultFilters)
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 20,
  })
  const [sorting, setSorting] = useState<SortingState>([
    { id: 'created_at', desc: true },
  ])
  const [cleanupOpen, setCleanupOpen] = useState(false)
  const [cleanupLoading, setCleanupLoading] = useState(false)
  const params = useMemo(
    () =>
      buildQueryParams(
        filters,
        pagination,
        isManagedScope,
        activeTab === 'errors' ? 5 : 2
      ),
    [activeTab, filters, isManagedScope, pagination]
  )
  const query = useQuery({
    queryKey: ['usage-details', activeTab, accessScope, params, t],
    queryFn: async () => {
      const result =
        accessScope === 'admin'
          ? await getAllLogs(params)
          : accessScope === 'agent'
            ? await getAgentLogs(params)
            : await getUserLogs(params)
      if (!result.success) {
        toast.error(result.message || t('Failed to load logs'))
        return DEFAULT_LOGS_DATA
      }
      return result.data || DEFAULT_LOGS_DATA
    },
    enabled: activeTab !== 'ranking',
  })
  const logs = (query.data?.items || []) as UsageLog[]
  const columns = useUsageDetailsColumns(isManagedScope, isAdmin)
  const table = useReactTable({
    data: logs,
    columns,
    state: { pagination, sorting },
    onPaginationChange: (updater) =>
      setPagination((prev) =>
        typeof updater === 'function' ? updater(prev) : updater
      ),
    onSortingChange: setSorting,
    manualPagination: true,
    pageCount: Math.ceil((query.data?.total || 0) / pagination.pageSize),
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    enableRowSelection: false,
  })

  const setFilter = <K extends keyof UsageDetailsFilters>(
    key: K,
    value: UsageDetailsFilters[K]
  ) => {
    setFilters((prev) => ({ ...prev, [key]: value }))
    setPagination((prev) => ({ ...prev, pageIndex: 0 }))
  }
  const resetFilters = () => {
    setFilters(defaultFilters())
    setPagination((prev) => ({ ...prev, pageIndex: 0 }))
  }
  const handleCleanup = async () => {
    if (!filters.startTime) return
    setCleanupLoading(true)
    try {
      const result = await deleteLogsBefore(
        Math.floor(filters.startTime.getTime() / 1000)
      )
      if (!result.success)
        throw new Error(result.message || t('Operation failed'))
      toast.success(t('Usage logs cleaned successfully'))
      setCleanupOpen(false)
      await query.refetch()
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('Operation failed')
      )
    } finally {
      setCleanupLoading(false)
    }
  }

  const filterField = (label: string, control: ReactNode) => (
    <label className='flex min-w-0 flex-1 flex-col gap-1.5 sm:min-w-[160px]'>
      <span className='text-muted-foreground text-xs font-medium'>{label}</span>
      {control}
    </label>
  )

  const tabs: Array<{ key: UsageDetailsTab; label: string; icon: LucideIcon }> =
    [
      { key: 'usage', label: t('Usage Details'), icon: FileText },
      { key: 'errors', label: t('Error Requests'), icon: TriangleAlert },
      ...(isManagedScope
        ? [
            {
              key: 'ranking' as const,
              label: t('User Ranking'),
              icon: ChartNoAxesCombined,
            },
          ]
        : []),
    ]

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Usage Details')}</SectionPageLayout.Title>
        <SectionPageLayout.Description>
          {t('Review token usage, costs, latency and routing details')}
        </SectionPageLayout.Description>
        <SectionPageLayout.Content>
          <div className='bg-card mb-3 flex flex-wrap items-center gap-1 rounded-xl border px-2 pt-2'>
            {tabs.map(({ key, label, icon: Icon }) => (
              <Button
                key={key}
                variant='ghost'
                className={
                  key === activeTab
                    ? 'border-primary text-primary rounded-t-lg border-b-2'
                    : 'text-muted-foreground'
                }
                onClick={() => {
                  setActiveTab(key)
                  setPagination((prev) => ({ ...prev, pageIndex: 0 }))
                }}
              >
                <Icon data-icon='inline-start' />
                {label}
              </Button>
            ))}
          </div>
          {activeTab === 'ranking' ? (
            <UsersRankingCard
              onViewAll={() => void navigate({ to: '/users' })}
            />
          ) : (
            <DataTablePage
              table={table}
              columns={columns}
              isLoading={query.isLoading}
              isFetching={query.isFetching}
              emptyTitle={
                activeTab === 'errors'
                  ? t('No Error Requests Found')
                  : t('No Usage Details Found')
              }
              emptyDescription={
                activeTab === 'errors'
                  ? t(
                      'Error requests will appear here when upstream calls fail.'
                    )
                  : t('Usage details will appear here once API calls are made.')
              }
              skeletonKeyPrefix='usage-details-skeleton'
              tableClassName='max-h-[calc(100dvh-20rem)] overflow-auto'
              tableHeaderClassName='bg-muted/30 sticky top-0 z-10'
              toolbar={
                <div className='bg-card rounded-xl border p-4 sm:p-5'>
                  <div className='grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4 xl:grid-cols-5'>
                    {isManagedScope &&
                      filterField(
                        t('User'),
                        <Input
                          value={filters.username}
                          onChange={(e) =>
                            setFilter('username', e.target.value)
                          }
                          onKeyDown={(e) =>
                            e.key === 'Enter' &&
                            setPagination((prev) => ({ ...prev, pageIndex: 0 }))
                          }
                          placeholder={t('Search by username...')}
                        />
                      )}
                    {filterField(
                      t('API Key'),
                      <Input
                        value={filters.token}
                        onChange={(e) => setFilter('token', e.target.value)}
                        placeholder={t('Search by API key...')}
                      />
                    )}
                    {filterField(
                      t('Model'),
                      <Input
                        value={filters.model}
                        onChange={(e) => setFilter('model', e.target.value)}
                        placeholder={t('Search by model...')}
                      />
                    )}
                    {isAdmin &&
                      filterField(
                        t('Account'),
                        <Input
                          value={filters.account}
                          onChange={(e) => setFilter('account', e.target.value)}
                          placeholder={t('Search by account...')}
                        />
                      )}
                    {filterField(
                      t('Group'),
                      <Input
                        value={filters.group}
                        onChange={(e) => setFilter('group', e.target.value)}
                        placeholder={t('Search by group...')}
                      />
                    )}
                    {isAdmin &&
                      filterField(
                        t('Type'),
                        <Select
                          value={filters.stream || 'all'}
                          onValueChange={(value) =>
                            setFilter(
                              'stream',
                              value === 'all'
                                ? ''
                                : value === 'stream'
                                  ? 'true'
                                  : 'false'
                            )
                          }
                          items={[
                            { value: 'all', label: t('Select') },
                            { value: 'stream', label: t('Stream') },
                            { value: 'sync', label: t('Sync') },
                          ]}
                        >
                          <SelectTrigger className='w-full'>
                            <SelectValue placeholder={t('Select')} />
                          </SelectTrigger>
                          <SelectContent alignItemWithTrigger={false}>
                            <SelectGroup>
                              <SelectItem value='all'>{t('Select')}</SelectItem>
                              <SelectItem value='stream'>
                                {t('Stream')}
                              </SelectItem>
                              <SelectItem value='sync'>{t('Sync')}</SelectItem>
                            </SelectGroup>
                          </SelectContent>
                        </Select>
                      )}
                    {isAdmin &&
                      activeTab === 'usage' &&
                      filterField(
                        t('Billing Type'),
                        <Select
                          value={filters.billingType || 'all'}
                          onValueChange={(value) =>
                            setFilter(
                              'billingType',
                              value === 'all'
                                ? ''
                                : (value as UsageDetailsFilters['billingType'])
                            )
                          }
                          items={[
                            { value: 'all', label: t('Select') },
                            { value: '0', label: t('Balance') },
                            { value: '1', label: t('Subscription') },
                          ]}
                        >
                          <SelectTrigger className='w-full'>
                            <SelectValue placeholder={t('Select')} />
                          </SelectTrigger>
                          <SelectContent alignItemWithTrigger={false}>
                            <SelectGroup>
                              <SelectItem value='all'>{t('Select')}</SelectItem>
                              <SelectItem value='0'>{t('Balance')}</SelectItem>
                              <SelectItem value='1'>
                                {t('Subscription')}
                              </SelectItem>
                            </SelectGroup>
                          </SelectContent>
                        </Select>
                      )}
                    {isAdmin &&
                      activeTab === 'usage' &&
                      filterField(
                        t('Billing Mode'),
                        <Select
                          value={filters.billingMode || 'all'}
                          onValueChange={(value) =>
                            setFilter(
                              'billingMode',
                              value === 'all'
                                ? ''
                                : (value as UsageDetailsFilters['billingMode'])
                            )
                          }
                          items={[
                            { value: 'all', label: t('Select') },
                            { value: 'token', label: t('Token-based') },
                            { value: 'per_request', label: t('Per Request') },
                            { value: 'image', label: t('Image') },
                            { value: 'video', label: t('Video') },
                            { value: 'tiered_expr', label: t('Tiered') },
                          ]}
                        >
                          <SelectTrigger className='w-full'>
                            <SelectValue placeholder={t('Select')} />
                          </SelectTrigger>
                          <SelectContent alignItemWithTrigger={false}>
                            <SelectGroup>
                              <SelectItem value='all'>{t('Select')}</SelectItem>
                              <SelectItem value='token'>
                                {t('Token-based')}
                              </SelectItem>
                              <SelectItem value='per_request'>
                                {t('Per Request')}
                              </SelectItem>
                              <SelectItem value='image'>
                                {t('Image')}
                              </SelectItem>
                              <SelectItem value='video'>
                                {t('Video')}
                              </SelectItem>
                              <SelectItem value='tiered_expr'>
                                {t('Tiered')}
                              </SelectItem>
                            </SelectGroup>
                          </SelectContent>
                        </Select>
                      )}
                    {filterField(
                      t('Date Range'),
                      <CompactDateTimeRangePicker
                        start={filters.startTime}
                        end={filters.endTime}
                        onChange={({ start, end }) => {
                          setFilters((prev) => ({
                            ...prev,
                            startTime: start,
                            endTime: end,
                          }))
                          setPagination((prev) => ({ ...prev, pageIndex: 0 }))
                        }}
                        className='w-full'
                      />
                    )}
                  </div>
                  <div className='mt-4 flex flex-wrap items-center justify-end gap-2 border-t pt-4'>
                    <Button
                      variant='outline'
                      onClick={() => void query.refetch()}
                      disabled={query.isFetching}
                    >
                      <RefreshCw data-icon='inline-start' />
                      {t('Refresh')}
                    </Button>
                    <Button variant='outline' onClick={resetFilters}>
                      {t('Reset')}
                    </Button>
                    <DataTableViewOptions table={table} />
                    {isAdmin && activeTab === 'usage' && (
                      <Button
                        variant='destructive'
                        onClick={() => setCleanupOpen(true)}
                      >
                        <Trash2 data-icon='inline-start' />
                        {t('Clear')}
                      </Button>
                    )}
                    {activeTab === 'usage' && (
                      <Button
                        onClick={() => exportLogs(logs, t)}
                        disabled={logs.length === 0}
                      >
                        <Download data-icon='inline-start' />
                        {t('Export Excel')}
                      </Button>
                    )}
                  </div>
                </div>
              }
              renderRow={(row) => (
                <TableRow key={row.id}>
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id} className='py-2'>
                      {flexRender(
                        cell.column.columnDef.cell,
                        cell.getContext()
                      )}
                    </TableCell>
                  ))}
                </TableRow>
              )}
            />
          )}
        </SectionPageLayout.Content>
      </SectionPageLayout>
      <AlertDialog open={cleanupOpen} onOpenChange={setCleanupOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Clear usage logs')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This permanently removes usage logs before the selected start time. This action cannot be undone.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={cleanupLoading}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              onClick={() => void handleCleanup()}
              disabled={cleanupLoading}
            >
              {t('Clear')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
