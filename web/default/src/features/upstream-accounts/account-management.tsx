/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useEffect, useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Add01Icon,
  ArrowDown01Icon,
  ChartIcon,
  Clock01Icon,
  Delete02Icon,
  Edit02Icon,
  FileExportIcon,
  FileImportIcon,
  Link01Icon,
  Loading03Icon,
  MoreHorizontalIcon,
  PlayIcon,
  RefreshIcon,
  Rocket01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
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
import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { ChannelMutateDrawer } from '@/features/channels/components/drawers/channel-mutate-drawer'
import { AccountBatchUpdateDialog } from './account-batch-update-dialog'
import { AccountDialog } from './account-dialog'
import {
  AccountScheduledTestsDialog,
  AccountStatsDialog,
} from './account-more-dialogs'
import {
  AccountCapacityCell,
  AccountIdentityCell,
  AccountPlatformCell,
  AccountPoolsCell,
  AccountSchedulingCell,
  AccountStatusCell,
  AccountUsageCell,
} from './account-runtime-cells'
import { AccountTestDialog } from './account-test-dialog'
import {
  createUpstreamPool,
  deleteUpstreamAccount,
  deleteUpstreamAccountsBatch,
  deleteUpstreamPool,
  exportUpstreamAccounts,
  importUpstreamData,
  listUpstreamAccounts,
  listUpstreamPoolMembers,
  listUpstreamPools,
  listUpstreamProxies,
  recoverUpstreamAccount,
  recoverUpstreamAccounts,
  refreshUpstreamOAuth,
  replaceUpstreamPoolMembers,
  startUpstreamOAuth,
  updateUpstreamAccountsBatch,
  updateUpstreamPool,
} from './api'
import { mergeAccountImportDocuments } from './batch-import'
import { BatchImportDialog } from './batch-import-dialog'
import { CRSSyncDialog } from './crs-sync-dialog'
import type {
  UpstreamAccount,
  UpstreamAccountPayload,
  UpstreamAccountPool,
  UpstreamAccountPoolMember,
  UpstreamAccountType,
  UpstreamPlatform,
  UpstreamPoolPayload,
} from './types'

const queryKeys = {
  accounts: ['upstream-accounts'] as const,
  pools: ['upstream-account-pools'] as const,
  proxies: ['upstream-proxies'] as const,
}

function timestamp(value?: number | null) {
  if (!value) return '-'
  return new Date(value * 1000).toLocaleString()
}

function statusVariant(status: string) {
  if (status === 'active') return 'default' as const
  if (status === 'error') return 'destructive' as const
  return 'secondary' as const
}

function credentialTypeLabel(
  platform: UpstreamPlatform,
  type: UpstreamAccountType | 'mixed',
  translate: (key: string) => string
) {
  if (type === 'mixed') return translate('Mixed')
  if (type === 'oauth') {
    return platform === 'anthropic'
      ? translate('Claude OAuth')
      : translate('Codex OAuth')
  }
  if (type === 'setup_token') return translate('Setup Token')
  if (type === 'bedrock') return translate('AWS Bedrock')
  if (type === 'service_account') return translate('Vertex AI')
  return platform === 'anthropic'
    ? translate('Anthropic API Key')
    : translate('OpenAI API Key')
}

function IconButton({
  label,
  icon,
  onClick,
  disabled,
  destructive,
}: {
  label: string
  icon: typeof Edit02Icon
  onClick: () => void
  disabled?: boolean
  destructive?: boolean
}) {
  return (
    <Button
      type='button'
      size='icon-sm'
      variant={destructive ? 'destructive' : 'ghost'}
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={onClick}
    >
      <HugeiconsIcon icon={icon} strokeWidth={2} />
    </Button>
  )
}

function RowActionButton({
  label,
  icon,
  onClick,
  disabled,
  destructive,
}: {
  label: string
  icon: typeof Edit02Icon
  onClick: () => void
  disabled?: boolean
  destructive?: boolean
}) {
  return (
    <Button
      type='button'
      variant='ghost'
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={onClick}
      className={cn(
        'text-muted-foreground h-auto min-w-10 flex-col gap-0.5 rounded-lg px-1.5 py-1.5 font-normal',
        destructive && 'hover:bg-destructive/10 hover:text-destructive'
      )}
    >
      <HugeiconsIcon icon={icon} strokeWidth={1.5} />
      <span className='text-xs'>{label}</span>
    </Button>
  )
}

type PoolDraft = {
  name: string
  description: string
  platform: UpstreamPlatform
  credentialType: UpstreamAccountType | 'mixed'
  status: 'active' | 'inactive'
  defaultProxyId: string
  topK: string
}

function poolSchedulerTopK(pool?: UpstreamAccountPool | null) {
  try {
    const parsed = JSON.parse(pool?.scheduler_config || '{}') as {
      top_k?: number
    }
    return String(Math.max(0, parsed.top_k || 0))
  } catch {
    return '0'
  }
}

function PoolDialog({
  open,
  onOpenChange,
  pool,
  proxies,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  pool: UpstreamAccountPool | null
  proxies: Array<{ id: number; name: string }>
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const [busy, setBusy] = useState(false)
  const [draft, setDraft] = useState<PoolDraft>({
    name: pool?.name ?? '',
    description: pool?.description ?? '',
    platform: pool?.platform ?? 'openai',
    credentialType: pool?.credential_type ?? 'mixed',
    status: pool?.status ?? 'active',
    defaultProxyId: pool?.default_proxy_id ? String(pool.default_proxy_id) : '',
    topK: poolSchedulerTopK(pool),
  })
  const set = <K extends keyof PoolDraft>(key: K, value: PoolDraft[K]) =>
    setDraft((current) => ({ ...current, [key]: value }))
  const submit = async () => {
    if (!draft.name.trim()) return toast.error(t('Pool name is required'))
    setBusy(true)
    try {
      let schedulerConfig: Record<string, unknown> = {}
      try {
        schedulerConfig = JSON.parse(pool?.scheduler_config || '{}')
      } catch {
        schedulerConfig = {}
      }
      schedulerConfig.version = 1
      schedulerConfig.top_k = Math.max(
        0,
        Math.min(100, Number(draft.topK) || 0)
      )
      const payload: UpstreamPoolPayload = {
        name: draft.name.trim(),
        description: draft.description.trim(),
        platform: draft.platform,
        credential_type: draft.credentialType,
        status: draft.status,
        default_proxy_id: draft.defaultProxyId
          ? Number(draft.defaultProxyId)
          : null,
        scheduler_config: JSON.stringify(schedulerConfig),
      }
      const response = pool
        ? await updateUpstreamPool(pool.id, payload)
        : await createUpstreamPool(payload)
      if (!response.success)
        throw new Error(response.message || t('Save failed'))
      toast.success(
        pool ? t('Account pool updated') : t('Account pool created')
      )
      onSaved()
      onOpenChange(false)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Save failed'))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>
            {pool ? t('Edit account pool') : t('Add account pool')}
          </DialogTitle>
          <DialogDescription>
            {t(
              'Channels reference pools; accounts remain reusable across channels.'
            )}
          </DialogDescription>
        </DialogHeader>
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor='pool-name'>{t('Name')}</FieldLabel>
            <Input
              id='pool-name'
              value={draft.name}
              onChange={(event) => set('name', event.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor='pool-description'>
              {t('Description')}
            </FieldLabel>
            <Input
              id='pool-description'
              value={draft.description}
              onChange={(event) => set('description', event.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel>{t('Platform')}</FieldLabel>
            <Select
              value={draft.platform}
              disabled={Boolean(pool)}
              onValueChange={(value) =>
                setDraft((current) => ({
                  ...current,
                  platform: value as UpstreamPlatform,
                  credentialType: 'mixed',
                }))
              }
            >
              <SelectTrigger className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value='openai'>OpenAI</SelectItem>
                  <SelectItem value='anthropic'>Anthropic</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
          <div className='grid gap-4 sm:grid-cols-2'>
            <Field>
              <FieldLabel>{t('Credential type')}</FieldLabel>
              <Select
                value={draft.credentialType}
                onValueChange={(value) =>
                  set('credentialType', value as PoolDraft['credentialType'])
                }
              >
                <SelectTrigger className='w-full'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value='mixed'>{t('Mixed')}</SelectItem>
                    <SelectItem value='oauth'>
                      {draft.platform === 'openai'
                        ? t('Codex OAuth')
                        : t('Claude OAuth')}
                    </SelectItem>
                    {draft.platform === 'anthropic' && (
                      <SelectItem value='setup_token'>
                        {t('Setup Token')}
                      </SelectItem>
                    )}
                    <SelectItem value='apikey'>
                      {draft.platform === 'openai'
                        ? t('OpenAI API Key')
                        : t('Anthropic API Key')}
                    </SelectItem>
                    {draft.platform === 'anthropic' && (
                      <>
                        <SelectItem value='bedrock'>AWS Bedrock</SelectItem>
                        <SelectItem value='service_account'>
                          Vertex AI
                        </SelectItem>
                      </>
                    )}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel>{t('Status')}</FieldLabel>
              <Select
                value={draft.status}
                onValueChange={(value) =>
                  set('status', value as PoolDraft['status'])
                }
              >
                <SelectTrigger className='w-full'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='active'>{t('Active')}</SelectItem>
                  <SelectItem value='inactive'>{t('Inactive')}</SelectItem>
                </SelectContent>
              </Select>
            </Field>
          </div>
          <Field>
            <FieldLabel>{t('Default proxy')}</FieldLabel>
            <Select
              value={draft.defaultProxyId || 'none'}
              onValueChange={(value) =>
                set('defaultProxyId', value === 'none' ? '' : String(value))
              }
            >
              <SelectTrigger className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='none'>
                  {t('Use channel proxy or direct connection')}
                </SelectItem>
                {proxies.map((proxy) => (
                  <SelectItem key={proxy.id} value={String(proxy.id)}>
                    {proxy.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel htmlFor='pool-top-k'>{t('Scheduler top K')}</FieldLabel>
            <Input
              id='pool-top-k'
              type='number'
              min={0}
              max={100}
              value={draft.topK}
              onChange={(event) => set('topK', event.target.value)}
            />
            <FieldDescription>
              {t(
                'Use 0 to consider every eligible account in the priority tier.'
              )}
            </FieldDescription>
          </Field>
        </FieldGroup>
        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={submit} disabled={busy}>
            {busy && (
              <HugeiconsIcon
                icon={Loading03Icon}
                className='animate-spin'
                strokeWidth={2}
              />
            )}
            {t('Save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function PoolMembersDialog({
  open,
  onOpenChange,
  pool,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  pool: UpstreamAccountPool | null
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const [members, setMembers] = useState<UpstreamAccountPoolMember[]>([])
  const [accountId, setAccountId] = useState('')
  const [busy, setBusy] = useState(false)
  const membersQuery = useQuery({
    queryKey: ['upstream-account-pool-members', pool?.id],
    queryFn: () => listUpstreamPoolMembers(pool!.id),
    enabled: open && !!pool,
  })
  const accountsQuery = useQuery({
    queryKey: ['upstream-accounts-for-pool-members', pool?.id],
    queryFn: () => listUpstreamAccounts({ page: 1, page_size: 100 }),
    enabled: open && !!pool,
  })
  useEffect(() => {
    if (membersQuery.data?.data) setMembers(membersQuery.data.data)
  }, [membersQuery.data])
  const accounts = accountsQuery.data?.data?.items ?? []
  const availableAccounts = accounts.filter(
    (account) =>
      account.platform === pool?.platform &&
      (pool?.credential_type === 'mixed' ||
        account.type === pool?.credential_type) &&
      !members.some((member) => member.account_id === account.id)
  )
  const addMember = () => {
    const selected = accounts.find(
      (account) => account.id === Number(accountId)
    )
    if (!selected || !pool) return
    setMembers((current) => [
      ...current,
      {
        pool_id: pool.id,
        account_id: selected.id,
        priority: selected.priority,
        weight: selected.weight,
        created_at: 0,
        name: selected.name,
        platform: selected.platform,
        type: selected.type,
        status: selected.status,
        schedulable: selected.schedulable,
      },
    ])
    setAccountId('')
  }
  const save = async () => {
    if (!pool) return
    setBusy(true)
    try {
      const response = await replaceUpstreamPoolMembers(
        pool.id,
        members.map((member) => ({
          account_id: member.account_id,
          priority: member.priority,
          weight: Math.max(1, member.weight),
        }))
      )
      if (!response.success)
        throw new Error(response.message || t('Save failed'))
      toast.success(t('Account pool members updated'))
      onSaved()
      onOpenChange(false)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Save failed'))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle>{t('Manage pool members')}</DialogTitle>
          <DialogDescription>{pool?.name}</DialogDescription>
        </DialogHeader>
        <div className='flex gap-2'>
          <Select
            value={accountId}
            onValueChange={(value) => setAccountId(value ?? '')}
          >
            <SelectTrigger className='flex-1'>
              <SelectValue placeholder={t('Select an account')} />
            </SelectTrigger>
            <SelectContent>
              {availableAccounts.map((account) => (
                <SelectItem key={account.id} value={String(account.id)}>
                  {account.name} · {account.type}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button variant='outline' disabled={!accountId} onClick={addMember}>
            <HugeiconsIcon icon={Add01Icon} strokeWidth={2} />
            {t('Add')}
          </Button>
        </div>
        <div className='max-h-[50vh] overflow-auto rounded-lg border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Account')}</TableHead>
                <TableHead>{t('Priority')}</TableHead>
                <TableHead>{t('Weight')}</TableHead>
                <TableHead className='w-12' />
              </TableRow>
            </TableHeader>
            <TableBody>
              {members.map((member) => (
                <TableRow key={member.account_id}>
                  <TableCell>
                    <div className='font-medium'>{member.name}</div>
                    <div className='text-muted-foreground text-xs'>
                      {member.type} · {member.status}
                    </div>
                  </TableCell>
                  <TableCell>
                    <Input
                      className='w-24'
                      type='number'
                      value={member.priority}
                      onChange={(event) =>
                        setMembers((current) =>
                          current.map((item) =>
                            item.account_id === member.account_id
                              ? {
                                  ...item,
                                  priority: Number(event.target.value) || 0,
                                }
                              : item
                          )
                        )
                      }
                    />
                  </TableCell>
                  <TableCell>
                    <Input
                      className='w-24'
                      type='number'
                      min={1}
                      value={member.weight}
                      onChange={(event) =>
                        setMembers((current) =>
                          current.map((item) =>
                            item.account_id === member.account_id
                              ? {
                                  ...item,
                                  weight: Number(event.target.value) || 1,
                                }
                              : item
                          )
                        )
                      }
                    />
                  </TableCell>
                  <TableCell>
                    <IconButton
                      label={t('Remove')}
                      icon={Delete02Icon}
                      destructive
                      onClick={() =>
                        setMembers((current) =>
                          current.filter(
                            (item) => item.account_id !== member.account_id
                          )
                        )
                      }
                    />
                  </TableCell>
                </TableRow>
              ))}
              {!membersQuery.isLoading && members.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={4}
                    className='text-muted-foreground py-8 text-center'
                  >
                    {t('No pool members')}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button disabled={busy} onClick={save}>
            {busy && (
              <HugeiconsIcon
                icon={Loading03Icon}
                className='animate-spin'
                strokeWidth={2}
              />
            )}
            {t('Save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function AccountManagement() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [activeTab, setActiveTab] = useState<'accounts' | 'pools'>('accounts')
  const [search, setSearch] = useState('')
  const [platformFilter, setPlatformFilter] = useState('all')
  const [typeFilter, setTypeFilter] = useState('all')
  const [statusFilter, setStatusFilter] = useState('all')
  const [schedulableFilter, setSchedulableFilter] = useState('all')
  const [poolFilter, setPoolFilter] = useState('all')
  const [proxyFilter, setProxyFilter] = useState('all')
  const [page, setPage] = useState(1)
  const [accountDialog, setAccountDialog] = useState(false)
  const [batchImportOpen, setBatchImportOpen] = useState(false)
  const [crsSyncOpen, setCRSSyncOpen] = useState(false)
  const [exporting, setExporting] = useState(false)
  const [exportConfirmOpen, setExportConfirmOpen] = useState(false)
  const [batchUpdateOpen, setBatchUpdateOpen] = useState(false)
  const [bulkDeleteOpen, setBulkDeleteOpen] = useState(false)
  const [selectedAccountIds, setSelectedAccountIds] = useState<number[]>([])
  const [poolDialog, setPoolDialog] = useState(false)
  const [memberPool, setMemberPool] = useState<UpstreamAccountPool | null>(null)
  const [selectedAccount, setSelectedAccount] =
    useState<UpstreamAccount | null>(null)
  const [statsAccount, setStatsAccount] = useState<UpstreamAccount | null>(null)
  const [scheduledTestsAccount, setScheduledTestsAccount] =
    useState<UpstreamAccount | null>(null)
  const [testingAccount, setTestingAccount] = useState<UpstreamAccount | null>(
    null
  )
  const [selectedPool, setSelectedPool] = useState<UpstreamAccountPool | null>(
    null
  )
  const [publishingPool, setPublishingPool] =
    useState<UpstreamAccountPool | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<{
    kind: 'account' | 'pool'
    id: number
    name: string
  } | null>(null)
  const [busyId, setBusyId] = useState<number | null>(null)
  const [schedulingBusyId, setSchedulingBusyId] = useState<number | null>(null)
  const [recoveringSelected, setRecoveringSelected] = useState(false)

  const accountsQuery = useQuery({
    queryKey: [
      ...queryKeys.accounts,
      search,
      page,
      platformFilter,
      typeFilter,
      statusFilter,
      schedulableFilter,
      poolFilter,
      proxyFilter,
    ],
    queryFn: () =>
      listUpstreamAccounts({
        page,
        page_size: 50,
        search: search || undefined,
        platform: platformFilter === 'all' ? undefined : platformFilter,
        type: typeFilter === 'all' ? undefined : typeFilter,
        status: statusFilter === 'all' ? undefined : statusFilter,
        schedulable:
          schedulableFilter === 'all'
            ? undefined
            : schedulableFilter === 'true',
        pool_id: poolFilter === 'all' ? undefined : Number(poolFilter),
        proxy_id: proxyFilter === 'all' ? undefined : Number(proxyFilter),
      }),
  })
  const poolsQuery = useQuery({
    queryKey: queryKeys.pools,
    queryFn: listUpstreamPools,
  })
  const proxiesQuery = useQuery({
    queryKey: queryKeys.proxies,
    queryFn: listUpstreamProxies,
  })
  const accounts = accountsQuery.data?.data?.items ?? []
  const accountTotal = accountsQuery.data?.data?.total ?? 0
  const accountTotalPages = Math.max(1, Math.ceil(accountTotal / 50))
  const allCurrentPageAccountsSelected =
    accounts.length > 0 &&
    accounts.every((account) => selectedAccountIds.includes(account.id))
  const pools = useMemo(() => poolsQuery.data?.data ?? [], [poolsQuery.data])
  const proxies = proxiesQuery.data?.data ?? []
  const poolNames = useMemo(
    () => new Map(pools.map((pool) => [pool.id, pool.name])),
    [pools]
  )
  const refresh = () =>
    void Promise.all([
      queryClient.invalidateQueries({ queryKey: queryKeys.accounts }),
      queryClient.invalidateQueries({ queryKey: queryKeys.pools }),
      queryClient.invalidateQueries({ queryKey: queryKeys.proxies }),
    ])

  const runAccountAction = async (
    account: UpstreamAccount,
    action: 'test' | 'refresh' | 'recover'
  ) => {
    if (action === 'test') {
      setTestingAccount(account)
      return
    }
    setBusyId(account.id)
    try {
      if (action === 'recover') {
        const response = await recoverUpstreamAccount(account.id)
        if (!response.success)
          throw new Error(response.message || t('Request failed'))
        toast.success(t('Account scheduling restored'))
      } else {
        const response = await refreshUpstreamOAuth(account.id)
        if (!response.success)
          throw new Error(response.message || t('Request failed'))
        toast.success(t('Credential refreshed'))
      }
      refresh()
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Request failed'))
    } finally {
      setBusyId(null)
    }
  }

  const reauthorizeAccount = async (account: UpstreamAccount) => {
    setBusyId(account.id)
    try {
      const response = await startUpstreamOAuth({
        account_id: account.id,
        proxy_id: account.proxy_id,
        platform: account.platform,
        credential_type:
          account.type === 'setup_token' ? 'setup_token' : 'oauth',
      })
      if (!response.success || !response.data?.authorize_url) {
        throw new Error(response.message || t('Failed to start authorization'))
      }
      window.open(response.data.authorize_url, '_blank', 'noopener,noreferrer')
      setSelectedAccount(account)
      setAccountDialog(true)
      toast.success(t('Authorization page opened'))
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Request failed'))
    } finally {
      setBusyId(null)
    }
  }

  const toggleAccountScheduling = async (
    account: UpstreamAccount,
    schedulable: boolean
  ) => {
    setSchedulingBusyId(account.id)
    try {
      const response = await updateUpstreamAccountsBatch([account.id], {
        schedulable,
      })
      if (!response.success || response.data?.failures.length) {
        throw new Error(
          response.data?.failures[0]?.message ||
            response.message ||
            t('Update failed')
        )
      }
      await queryClient.invalidateQueries({ queryKey: queryKeys.accounts })
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Update failed'))
      await queryClient.invalidateQueries({ queryKey: queryKeys.accounts })
    } finally {
      setSchedulingBusyId(null)
    }
  }

  const recoverSelectedAccounts = async () => {
    setRecoveringSelected(true)
    try {
      const response = await recoverUpstreamAccounts(selectedAccountIds)
      if (!response.success) {
        throw new Error(response.message || t('Request failed'))
      }
      toast.success(
        t('Restored {{success}} accounts; {{failed}} failed', {
          success: response.data?.success_ids.length ?? 0,
          failed: response.data?.failures.length ?? 0,
        })
      )
      setSelectedAccountIds([])
      refresh()
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Request failed'))
    } finally {
      setRecoveringSelected(false)
    }
  }

  const confirmDelete = async () => {
    if (!deleteTarget) return
    try {
      const response =
        deleteTarget.kind === 'account'
          ? await deleteUpstreamAccount(deleteTarget.id)
          : await deleteUpstreamPool(deleteTarget.id)
      if (!response.success)
        throw new Error(response.message || t('Delete failed'))
      toast.success(t('Deleted'))
      setDeleteTarget(null)
      refresh()
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Delete failed'))
    }
  }

  const importAccounts = async (_items: unknown[], documents: unknown[]) => {
    const response = await importUpstreamData(
      mergeAccountImportDocuments(documents)
    )
    if (!response.success)
      throw new Error(response.message || t('Import failed'))
    const failures =
      (response.data?.account_failed ?? 0) + (response.data?.proxy_failed ?? 0)
    const successes = response.data?.account_created ?? 0
    if (successes === 0 && failures > 0) {
      throw new Error(response.data?.errors?.[0]?.message || t('Import failed'))
    }
    toast.success(
      t('Imported {{success}} accounts; {{failed}} failed', {
        success: successes,
        failed: failures,
      })
    )
    refresh()
  }

  const exportAccounts = async () => {
    if (exporting) return
    setExporting(true)
    setExportConfirmOpen(false)
    try {
      const response = await exportUpstreamAccounts(selectedAccountIds)
      if (!response.success || !response.data) {
        throw new Error(response.message || t('Export failed'))
      }
      const timestamp = new Date()
        .toISOString()
        .replace(/[-:]/g, '')
        .replace(/\..+$/, '')
      const blob = new Blob([JSON.stringify(response.data, null, 2)], {
        type: 'application/json',
      })
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = `starnexus-accounts-${timestamp}.json`
      document.body.appendChild(link)
      link.click()
      link.remove()
      URL.revokeObjectURL(url)
      toast.success(
        t('Exported {{count}} accounts', {
          count: response.data.accounts.length,
        })
      )
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Export failed'))
    } finally {
      setExporting(false)
    }
  }

  const deleteSelectedAccounts = async () => {
    const response = await deleteUpstreamAccountsBatch(selectedAccountIds)
    if (!response.success) {
      toast.error(response.message || t('Delete failed'))
      return
    }
    toast.success(
      t('Deleted {{success}} accounts; {{failed}} failed', {
        success: response.data?.success_ids.length ?? 0,
        failed: response.data?.failures.length ?? 0,
      })
    )
    setSelectedAccountIds([])
    setBulkDeleteOpen(false)
    refresh()
  }

  const updateSelectedAccounts = async (
    patch: Partial<UpstreamAccountPayload>
  ) => {
    const response = await updateUpstreamAccountsBatch(
      selectedAccountIds,
      patch
    )
    if (!response.success) {
      toast.error(response.message || t('Update failed'))
      return
    }
    toast.success(
      t('Updated {{success}} accounts; {{failed}} failed', {
        success: response.data?.success_ids.length ?? 0,
        failed: response.data?.failures.length ?? 0,
      })
    )
    setSelectedAccountIds([])
    refresh()
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Account Management')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t(
          'Manage reusable OpenAI and Anthropic credentials and the local account pools referenced by channels.'
        )}
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        <Tabs
          value={activeTab}
          onValueChange={(value) => setActiveTab(value as 'accounts' | 'pools')}
        >
          <div className='flex flex-wrap items-center gap-2'>
            <TabsList className='shrink-0'>
              <TabsTrigger value='accounts'>{t('Accounts')}</TabsTrigger>
              <TabsTrigger value='pools'>{t('Account pools')}</TabsTrigger>
            </TabsList>
            {activeTab === 'accounts' ? (
              <>
                <Input
                  className='max-w-sm min-w-48 flex-1'
                  value={search}
                  placeholder={t('Search accounts')}
                  onChange={(event) => {
                    setSearch(event.target.value)
                    setPage(1)
                  }}
                />
                <Select
                  items={[
                    { value: 'all', label: t('All platforms') },
                    { value: 'openai', label: 'OpenAI' },
                    { value: 'anthropic', label: 'Anthropic' },
                  ]}
                  value={platformFilter}
                  onValueChange={(value) => {
                    setPlatformFilter(value || 'all')
                    setPage(1)
                  }}
                >
                  <SelectTrigger className='w-36'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent align='start' alignItemWithTrigger={false}>
                    <SelectItem value='all'>{t('All platforms')}</SelectItem>
                    <SelectItem value='openai'>OpenAI</SelectItem>
                    <SelectItem value='anthropic'>Anthropic</SelectItem>
                  </SelectContent>
                </Select>
                <Select
                  items={[
                    { value: 'all', label: t('All credential types') },
                    { value: 'oauth', label: t('OAuth') },
                    { value: 'setup_token', label: t('Setup Token') },
                    { value: 'apikey', label: t('API key') },
                    { value: 'bedrock', label: t('AWS Bedrock') },
                    { value: 'service_account', label: t('Vertex') },
                  ]}
                  value={typeFilter}
                  onValueChange={(value) => {
                    setTypeFilter(value || 'all')
                    setPage(1)
                  }}
                >
                  <SelectTrigger className='w-44'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent align='start' alignItemWithTrigger={false}>
                    <SelectItem value='all'>
                      {t('All credential types')}
                    </SelectItem>
                    <SelectItem value='oauth'>{t('OAuth')}</SelectItem>
                    <SelectItem value='setup_token'>
                      {t('Setup Token')}
                    </SelectItem>
                    <SelectItem value='apikey'>{t('API key')}</SelectItem>
                    <SelectItem value='bedrock'>{t('AWS Bedrock')}</SelectItem>
                    <SelectItem value='service_account'>
                      {t('Vertex')}
                    </SelectItem>
                  </SelectContent>
                </Select>
                <Select
                  items={[
                    { value: 'all', label: t('All statuses') },
                    { value: 'active', label: t('Active') },
                    { value: 'inactive', label: t('Inactive') },
                    { value: 'error', label: t('Error') },
                    { value: 'expired', label: t('Expired') },
                  ]}
                  value={statusFilter}
                  onValueChange={(value) => {
                    setStatusFilter(value || 'all')
                    setPage(1)
                  }}
                >
                  <SelectTrigger className='w-36'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent align='start' alignItemWithTrigger={false}>
                    <SelectItem value='all'>{t('All statuses')}</SelectItem>
                    <SelectItem value='active'>{t('Active')}</SelectItem>
                    <SelectItem value='inactive'>{t('Inactive')}</SelectItem>
                    <SelectItem value='error'>{t('Error')}</SelectItem>
                    <SelectItem value='expired'>{t('Expired')}</SelectItem>
                  </SelectContent>
                </Select>
                <Select
                  items={[
                    { value: 'all', label: t('All scheduling states') },
                    { value: 'true', label: t('Schedulable') },
                    { value: 'false', label: t('Paused') },
                  ]}
                  value={schedulableFilter}
                  onValueChange={(value) => {
                    setSchedulableFilter(value || 'all')
                    setPage(1)
                  }}
                >
                  <SelectTrigger className='w-44'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent align='start' alignItemWithTrigger={false}>
                    <SelectItem value='all'>
                      {t('All scheduling states')}
                    </SelectItem>
                    <SelectItem value='true'>{t('Schedulable')}</SelectItem>
                    <SelectItem value='false'>{t('Paused')}</SelectItem>
                  </SelectContent>
                </Select>
                <Select
                  items={[
                    { value: 'all', label: t('All account pools') },
                    ...pools.map((pool) => ({
                      value: String(pool.id),
                      label: pool.name,
                    })),
                  ]}
                  value={poolFilter}
                  onValueChange={(value) => {
                    setPoolFilter(value || 'all')
                    setPage(1)
                  }}
                >
                  <SelectTrigger className='w-44'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent align='start' alignItemWithTrigger={false}>
                    <SelectItem value='all'>
                      {t('All account pools')}
                    </SelectItem>
                    {pools.map((pool) => (
                      <SelectItem key={pool.id} value={String(pool.id)}>
                        {pool.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Select
                  items={[
                    { value: 'all', label: t('All proxies') },
                    ...proxies.map((proxy) => ({
                      value: String(proxy.id),
                      label: proxy.name,
                    })),
                  ]}
                  value={proxyFilter}
                  onValueChange={(value) => {
                    setProxyFilter(value || 'all')
                    setPage(1)
                  }}
                >
                  <SelectTrigger className='w-40'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent align='start' alignItemWithTrigger={false}>
                    <SelectItem value='all'>{t('All proxies')}</SelectItem>
                    {proxies.map((proxy) => (
                      <SelectItem key={proxy.id} value={String(proxy.id)}>
                        {proxy.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <div className='ml-auto flex flex-wrap gap-2'>
                  {selectedAccountIds.length > 0 && (
                    <>
                      <Button
                        variant='outline'
                        disabled={recoveringSelected}
                        onClick={recoverSelectedAccounts}
                      >
                        <HugeiconsIcon
                          icon={
                            recoveringSelected ? Loading03Icon : RefreshIcon
                          }
                          className={
                            recoveringSelected ? 'animate-spin' : undefined
                          }
                          strokeWidth={2}
                        />
                        {t('Restore selected')}
                      </Button>
                      <Button
                        variant='outline'
                        onClick={() => setBatchUpdateOpen(true)}
                      >
                        <HugeiconsIcon icon={Edit02Icon} strokeWidth={2} />
                        {t('Batch update')}
                      </Button>
                      <Button
                        variant='destructive'
                        onClick={() => setBulkDeleteOpen(true)}
                      >
                        <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} />
                        {t('Delete selected ({{count}})', {
                          count: selectedAccountIds.length,
                        })}
                      </Button>
                    </>
                  )}
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      type='button'
                      disabled={exporting}
                      className={buttonVariants({ variant: 'outline' })}
                    >
                      <HugeiconsIcon
                        data-icon='inline-start'
                        icon={FileImportIcon}
                        strokeWidth={2}
                      />
                      {t('Import / Export')}
                      <HugeiconsIcon
                        data-icon='inline-end'
                        icon={ArrowDown01Icon}
                        strokeWidth={2}
                      />
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align='end' className='w-56'>
                      <DropdownMenuGroup>
                        <DropdownMenuLabel>
                          {t('Data operations')}
                        </DropdownMenuLabel>
                        <DropdownMenuItem onClick={() => setCRSSyncOpen(true)}>
                          <span className='flex size-7 items-center justify-center rounded-md bg-blue-500/10 text-blue-600 dark:text-blue-400'>
                            <HugeiconsIcon icon={RefreshIcon} strokeWidth={2} />
                          </span>
                          {t('Sync from CRS')}
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          onClick={() => setBatchImportOpen(true)}
                        >
                          <span className='flex size-7 items-center justify-center rounded-md bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'>
                            <HugeiconsIcon
                              icon={FileImportIcon}
                              strokeWidth={2}
                            />
                          </span>
                          {t('Import')}
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          onClick={() => setExportConfirmOpen(true)}
                        >
                          <span className='flex size-7 items-center justify-center rounded-md bg-violet-500/10 text-violet-600 dark:text-violet-400'>
                            <HugeiconsIcon
                              icon={FileExportIcon}
                              strokeWidth={2}
                            />
                          </span>
                          {selectedAccountIds.length > 0
                            ? t('Export selected')
                            : t('Export')}
                        </DropdownMenuItem>
                      </DropdownMenuGroup>
                    </DropdownMenuContent>
                  </DropdownMenu>
                  <Button
                    onClick={() => {
                      setSelectedAccount(null)
                      setAccountDialog(true)
                    }}
                  >
                    <HugeiconsIcon icon={Add01Icon} strokeWidth={2} />
                    {t('Add account')}
                  </Button>
                </div>
              </>
            ) : (
              <Button
                className='ml-auto'
                onClick={() => {
                  setSelectedPool(null)
                  setPoolDialog(true)
                }}
              >
                <HugeiconsIcon icon={Add01Icon} strokeWidth={2} />
                {t('Add account pool')}
              </Button>
            )}
          </div>
          <TabsContent value='accounts' className='space-y-3 pt-3'>
            <div className='overflow-x-auto rounded-lg border'>
              <Table className='min-w-[1260px]'>
                <TableHeader>
                  <TableRow>
                    <TableHead className='w-10'>
                      <Checkbox
                        aria-label={t('Select all accounts')}
                        checked={allCurrentPageAccountsSelected}
                        onCheckedChange={(checked) => {
                          const currentPageIds = new Set(
                            accounts.map((account) => account.id)
                          )
                          setSelectedAccountIds((current) =>
                            checked === true
                              ? Array.from(
                                  new Set([
                                    ...current,
                                    ...accounts.map((account) => account.id),
                                  ])
                                )
                              : current.filter((id) => !currentPageIds.has(id))
                          )
                        }}
                      />
                    </TableHead>
                    <TableHead>{t('Account ID')}</TableHead>
                    <TableHead>{t('Platform / Type')}</TableHead>
                    <TableHead>{t('Capacity')}</TableHead>
                    <TableHead>{t('Status')}</TableHead>
                    <TableHead>{t('Scheduling')}</TableHead>
                    <TableHead>{t('Groups')}</TableHead>
                    <TableHead>{t('Usage window')}</TableHead>
                    <TableHead className='w-36'>{t('Actions')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {accountsQuery.isLoading ? (
                    <TableRow>
                      <TableCell colSpan={9}>{t('Loading...')}</TableCell>
                    </TableRow>
                  ) : accounts.length === 0 ? (
                    <TableRow>
                      <TableCell
                        colSpan={9}
                        className='text-muted-foreground py-10 text-center'
                      >
                        {t('No accounts')}
                      </TableCell>
                    </TableRow>
                  ) : (
                    accounts.map((account) => (
                      <TableRow key={account.id} className='h-24 align-middle'>
                        <TableCell>
                          <Checkbox
                            aria-label={t('Select account {{name}}', {
                              name: account.name,
                            })}
                            checked={selectedAccountIds.includes(account.id)}
                            onCheckedChange={(checked) =>
                              setSelectedAccountIds((current) =>
                                checked === true
                                  ? current.includes(account.id)
                                    ? current
                                    : [...current, account.id]
                                  : current.filter((id) => id !== account.id)
                              )
                            }
                          />
                        </TableCell>
                        <TableCell>
                          <AccountIdentityCell account={account} />
                        </TableCell>
                        <TableCell>
                          <AccountPlatformCell account={account} />
                        </TableCell>
                        <TableCell>
                          <AccountCapacityCell account={account} />
                        </TableCell>
                        <TableCell>
                          <AccountStatusCell account={account} />
                        </TableCell>
                        <TableCell>
                          <AccountSchedulingCell
                            account={account}
                            busy={schedulingBusyId === account.id}
                            onChange={(checked) =>
                              void toggleAccountScheduling(account, checked)
                            }
                          />
                        </TableCell>
                        <TableCell>
                          <AccountPoolsCell
                            poolNames={account.pool_ids.map(
                              (id) => poolNames.get(id) || `#${id}`
                            )}
                          />
                        </TableCell>
                        <TableCell>
                          <AccountUsageCell account={account} />
                        </TableCell>
                        <TableCell className='w-36'>
                          <div className='flex items-center gap-1'>
                            <RowActionButton
                              label={t('Edit')}
                              icon={Edit02Icon}
                              onClick={() => {
                                setSelectedAccount(account)
                                setAccountDialog(true)
                              }}
                            />
                            <RowActionButton
                              label={t('Delete')}
                              icon={Delete02Icon}
                              destructive
                              onClick={() =>
                                setDeleteTarget({
                                  kind: 'account',
                                  id: account.id,
                                  name: account.name,
                                })
                              }
                            />
                            <DropdownMenu modal={false}>
                              <DropdownMenuTrigger
                                render={
                                  <Button
                                    type='button'
                                    variant='ghost'
                                    aria-label={t('More')}
                                    title={t('More')}
                                    className='text-muted-foreground h-auto min-w-10 flex-col gap-0.5 rounded-lg px-1.5 py-1.5 font-normal'
                                  />
                                }
                              >
                                <HugeiconsIcon
                                  icon={MoreHorizontalIcon}
                                  strokeWidth={1.5}
                                />
                                <span className='text-xs'>{t('More')}</span>
                              </DropdownMenuTrigger>
                              <DropdownMenuContent
                                align='end'
                                sideOffset={6}
                                className='w-52 rounded-xl p-1.5'
                              >
                                <DropdownMenuGroup>
                                  <DropdownMenuItem
                                    className='gap-2.5 px-3 py-2.5'
                                    disabled={busyId === account.id}
                                    onClick={() =>
                                      runAccountAction(account, 'test')
                                    }
                                  >
                                    <HugeiconsIcon
                                      icon={
                                        busyId === account.id
                                          ? Loading03Icon
                                          : PlayIcon
                                      }
                                      className={cn(
                                        'text-success',
                                        busyId === account.id && 'animate-spin'
                                      )}
                                      strokeWidth={2}
                                    />
                                    {t('Test Connection')}
                                  </DropdownMenuItem>
                                  <DropdownMenuItem
                                    className='gap-2.5 px-3 py-2.5'
                                    onClick={() => setStatsAccount(account)}
                                  >
                                    <HugeiconsIcon
                                      icon={ChartIcon}
                                      className='text-info'
                                      strokeWidth={2}
                                    />
                                    {t('View statistics')}
                                  </DropdownMenuItem>
                                  <DropdownMenuItem
                                    className='gap-2.5 px-3 py-2.5'
                                    onClick={() =>
                                      setScheduledTestsAccount(account)
                                    }
                                  >
                                    <HugeiconsIcon
                                      icon={Clock01Icon}
                                      className='text-warning'
                                      strokeWidth={2}
                                    />
                                    {t('Scheduled test')}
                                  </DropdownMenuItem>
                                  {(account.type === 'oauth' ||
                                    account.type === 'setup_token') && (
                                    <>
                                      <DropdownMenuItem
                                        className='gap-2.5 px-3 py-2.5'
                                        disabled={busyId === account.id}
                                        onClick={() =>
                                          void reauthorizeAccount(account)
                                        }
                                      >
                                        <HugeiconsIcon
                                          icon={Link01Icon}
                                          className='text-info'
                                          strokeWidth={2}
                                        />
                                        {t('Reauthorize')}
                                      </DropdownMenuItem>
                                      <DropdownMenuItem
                                        className='gap-2.5 px-3 py-2.5'
                                        disabled={
                                          busyId === account.id ||
                                          account.oauth_refresh_owner !==
                                            'starnexus'
                                        }
                                        onClick={() =>
                                          runAccountAction(account, 'refresh')
                                        }
                                      >
                                        <HugeiconsIcon
                                          icon={RefreshIcon}
                                          className='text-chart-5'
                                          strokeWidth={2}
                                        />
                                        {account.oauth_refresh_owner ===
                                        'starnexus'
                                          ? t('Refresh token')
                                          : t(
                                              'OAuth refresh is managed externally'
                                            )}
                                      </DropdownMenuItem>
                                    </>
                                  )}
                                  {(account.status === 'error' ||
                                    account.rate_limit_reset_at != null ||
                                    account.temp_unschedulable_until !=
                                      null) && (
                                    <>
                                      <DropdownMenuSeparator />
                                      <DropdownMenuItem
                                        className='gap-2.5 px-3 py-2.5'
                                        disabled={busyId === account.id}
                                        onClick={() =>
                                          runAccountAction(account, 'recover')
                                        }
                                      >
                                        <HugeiconsIcon
                                          icon={RefreshIcon}
                                          className='text-success'
                                          strokeWidth={2}
                                        />
                                        {t('Restore scheduling')}
                                      </DropdownMenuItem>
                                    </>
                                  )}
                                </DropdownMenuGroup>
                              </DropdownMenuContent>
                            </DropdownMenu>
                          </div>
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </div>
            <div className='flex items-center justify-end gap-2'>
              <span className='text-muted-foreground text-sm'>
                {t('Page {{page}} of {{total}}', {
                  page,
                  total: accountTotalPages,
                })}
              </span>
              <Button
                variant='outline'
                size='sm'
                disabled={page <= 1}
                onClick={() => setPage((current) => Math.max(1, current - 1))}
              >
                {t('Previous')}
              </Button>
              <Button
                variant='outline'
                size='sm'
                disabled={page >= accountTotalPages}
                onClick={() =>
                  setPage((current) => Math.min(accountTotalPages, current + 1))
                }
              >
                {t('Next')}
              </Button>
            </div>
          </TabsContent>
          <TabsContent value='pools' className='space-y-3 pt-3'>
            <div className='overflow-hidden rounded-lg border'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('Pool')}</TableHead>
                    <TableHead>{t('Credential type')}</TableHead>
                    <TableHead>{t('Accounts')}</TableHead>
                    <TableHead>{t('Channels')}</TableHead>
                    <TableHead>{t('Last 24 hours')}</TableHead>
                    <TableHead>{t('Status')}</TableHead>
                    <TableHead className='w-36'>{t('Actions')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {pools.length === 0 ? (
                    <TableRow>
                      <TableCell
                        colSpan={7}
                        className='text-muted-foreground py-10 text-center'
                      >
                        {t('No account pools')}
                      </TableCell>
                    </TableRow>
                  ) : (
                    pools.map((pool) => (
                      <TableRow key={pool.id} className='h-24 align-middle'>
                        <TableCell>
                          <div className='font-medium'>{pool.name}</div>
                          <div className='text-muted-foreground text-xs'>
                            {pool.description}
                          </div>
                        </TableCell>
                        <TableCell>
                          <Badge variant='outline'>
                            {credentialTypeLabel(
                              pool.platform,
                              pool.credential_type,
                              t
                            )}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          {pool.active_count} / {pool.account_count}
                        </TableCell>
                        <TableCell>{pool.channel_count}</TableCell>
                        <TableCell>
                          <div className='text-sm'>
                            {t('{{success}} success / {{errors}} errors', {
                              success: pool.success_count_24h,
                              errors: pool.error_count_24h,
                            })}
                          </div>
                          <div className='text-muted-foreground text-xs'>
                            {t('{{attempts}} attempts', {
                              attempts: pool.attempt_count_24h,
                            })}
                            {' · '}
                            {timestamp(pool.last_event_at)}
                          </div>
                        </TableCell>
                        <TableCell>
                          <Badge variant={statusVariant(pool.status)}>
                            {pool.status === 'active'
                              ? t('Active')
                              : t('Inactive')}
                          </Badge>
                        </TableCell>
                        <TableCell className='w-36'>
                          <div className='flex items-center gap-1'>
                            <RowActionButton
                              label={t('Edit')}
                              icon={Edit02Icon}
                              onClick={() => {
                                setSelectedPool(pool)
                                setPoolDialog(true)
                              }}
                            />
                            <RowActionButton
                              label={t('Delete')}
                              icon={Delete02Icon}
                              destructive
                              onClick={() =>
                                setDeleteTarget({
                                  kind: 'pool',
                                  id: pool.id,
                                  name: pool.name,
                                })
                              }
                            />
                            <DropdownMenu modal={false}>
                              <DropdownMenuTrigger
                                render={
                                  <Button
                                    type='button'
                                    variant='ghost'
                                    aria-label={t('More')}
                                    title={t('More')}
                                    className='text-muted-foreground h-auto min-w-10 flex-col gap-0.5 rounded-lg px-1.5 py-1.5 font-normal'
                                  />
                                }
                              >
                                <HugeiconsIcon
                                  icon={MoreHorizontalIcon}
                                  strokeWidth={1.5}
                                />
                                <span className='text-xs'>{t('More')}</span>
                              </DropdownMenuTrigger>
                              <DropdownMenuContent
                                align='end'
                                sideOffset={6}
                                className='w-52 rounded-xl p-1.5'
                              >
                                <DropdownMenuGroup>
                                  <DropdownMenuItem
                                    className='gap-2.5 px-3 py-2.5'
                                    disabled={pool.status !== 'active'}
                                    onClick={() => setPublishingPool(pool)}
                                  >
                                    <HugeiconsIcon
                                      icon={Rocket01Icon}
                                      strokeWidth={2}
                                    />
                                    {t('Publish as local channel')}
                                  </DropdownMenuItem>
                                  <DropdownMenuItem
                                    className='gap-2.5 px-3 py-2.5'
                                    onClick={() => setMemberPool(pool)}
                                  >
                                    <HugeiconsIcon
                                      icon={Link01Icon}
                                      strokeWidth={2}
                                    />
                                    {t('Manage pool members')}
                                  </DropdownMenuItem>
                                </DropdownMenuGroup>
                              </DropdownMenuContent>
                            </DropdownMenu>
                          </div>
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </div>
          </TabsContent>
        </Tabs>
        {accountDialog && (
          <AccountDialog
            key={selectedAccount?.id || 'new'}
            open={accountDialog}
            onOpenChange={setAccountDialog}
            account={selectedAccount}
            pools={pools}
            proxies={proxies}
            onSaved={refresh}
          />
        )}
        <AccountStatsDialog
          open={statsAccount !== null}
          account={statsAccount}
          onOpenChange={(open) => !open && setStatsAccount(null)}
        />
        <AccountScheduledTestsDialog
          open={scheduledTestsAccount !== null}
          account={scheduledTestsAccount}
          onOpenChange={(open) => !open && setScheduledTestsAccount(null)}
        />
        <AccountTestDialog
          open={testingAccount !== null}
          account={testingAccount}
          onOpenChange={(open) => !open && setTestingAccount(null)}
          onTested={refresh}
        />
        <BatchImportDialog
          open={batchImportOpen}
          onOpenChange={setBatchImportOpen}
          title={t('Batch import accounts')}
          description={t(
            'Import up to 100 OpenAI or Anthropic accounts from a JSON array.'
          )}
          collectionKey='accounts'
          onImport={importAccounts}
        />
        <CRSSyncDialog
          open={crsSyncOpen}
          onOpenChange={setCRSSyncOpen}
          onSynced={refresh}
        />
        <AccountBatchUpdateDialog
          open={batchUpdateOpen}
          onOpenChange={setBatchUpdateOpen}
          count={selectedAccountIds.length}
          onApply={updateSelectedAccounts}
        />
        {poolDialog && (
          <PoolDialog
            key={selectedPool?.id || 'new'}
            open={poolDialog}
            onOpenChange={setPoolDialog}
            pool={selectedPool}
            proxies={proxies}
            onSaved={refresh}
          />
        )}
        {publishingPool && (
          <ChannelMutateDrawer
            key={publishingPool.id}
            open
            onOpenChange={(open) => {
              if (!open) setPublishingPool(null)
            }}
            creationMode='local'
            initialAccountPool={publishingPool}
            onSaved={refresh}
          />
        )}
        <PoolMembersDialog
          open={!!memberPool}
          onOpenChange={(open) => {
            if (!open) setMemberPool(null)
          }}
          pool={memberPool}
          onSaved={refresh}
        />
        <AlertDialog
          open={exportConfirmOpen}
          onOpenChange={setExportConfirmOpen}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>{t('Export account data?')}</AlertDialogTitle>
              <AlertDialogDescription>
                {t(
                  'The export file contains decrypted account credentials. Store it securely and delete it when no longer needed.'
                )}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
              <AlertDialogAction
                disabled={exporting}
                onClick={() => void exportAccounts()}
              >
                {t('Export accounts')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
        <AlertDialog
          open={Boolean(deleteTarget)}
          onOpenChange={(open) => !open && setDeleteTarget(null)}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>{t('Confirm deletion')}</AlertDialogTitle>
              <AlertDialogDescription>
                {t(
                  'Delete {{name}}? Referenced pools or proxies must be detached first.',
                  { name: deleteTarget?.name }
                )}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
              <AlertDialogAction variant='destructive' onClick={confirmDelete}>
                {t('Delete')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
        <AlertDialog open={bulkDeleteOpen} onOpenChange={setBulkDeleteOpen}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>{t('Confirm deletion')}</AlertDialogTitle>
              <AlertDialogDescription>
                {t('Delete {{count}} selected accounts?', {
                  count: selectedAccountIds.length,
                })}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
              <AlertDialogAction
                variant='destructive'
                onClick={deleteSelectedAccounts}
              >
                {t('Delete')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
