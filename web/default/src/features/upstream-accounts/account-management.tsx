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
  Delete02Icon,
  Edit02Icon,
  FileImportIcon,
  Link01Icon,
  Loading03Icon,
  RefreshIcon,
  TestTubeIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
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
import { Button } from '@/components/ui/button'
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
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
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
import { Textarea } from '@/components/ui/textarea'
import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import {
  completeUpstreamOAuth,
  createUpstreamAccount,
  createUpstreamAccountsBatch,
  createUpstreamPool,
  deleteUpstreamAccount,
  deleteUpstreamAccountsBatch,
  deleteUpstreamPool,
  listUpstreamAccounts,
  listUpstreamPoolMembers,
  listUpstreamPools,
  listUpstreamProxies,
  refreshUpstreamOAuth,
  replaceUpstreamPoolMembers,
  startUpstreamOAuth,
  testUpstreamAccount,
  updateUpstreamAccount,
  updateUpstreamAccountsBatch,
  updateUpstreamPool,
} from './api'
import { BatchImportDialog } from './batch-import-dialog'
import type {
  UpstreamAccount,
  UpstreamAccountPayload,
  UpstreamAccountPool,
  UpstreamAccountPoolMember,
  UpstreamAccountType,
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

type AccountDraft = {
  name: string
  notes: string
  type: UpstreamAccountType
  apiKey: string
  oauthInput: string
  proxyId: string
  concurrency: string
  priority: string
  weight: string
  status: 'active' | 'inactive' | 'error'
  schedulable: boolean
  poolIds: number[]
}

function accountDraft(account?: UpstreamAccount | null): AccountDraft {
  return {
    name: account?.name ?? '',
    notes: account?.notes ?? '',
    type: account?.type ?? 'oauth',
    apiKey: '',
    oauthInput: '',
    proxyId: account?.proxy_id ? String(account.proxy_id) : '',
    concurrency: String(account?.concurrency ?? 1),
    priority: String(account?.priority ?? 50),
    weight: String(account?.weight ?? 1),
    status: account?.status ?? 'active',
    schedulable: account?.schedulable ?? true,
    poolIds: account?.pool_ids ?? [],
  }
}

function AccountBatchUpdateDialog({
  open,
  onOpenChange,
  count,
  onApply,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  count: number
  onApply: (patch: Partial<UpstreamAccountPayload>) => Promise<void>
}) {
  const { t } = useTranslation()
  const [status, setStatus] = useState<
    'unchanged' | 'active' | 'inactive' | 'error'
  >('unchanged')
  const [schedulable, setSchedulable] = useState<
    'unchanged' | 'true' | 'false'
  >('unchanged')
  const [busy, setBusy] = useState(false)
  const submit = async () => {
    const patch: Partial<UpstreamAccountPayload> = {}
    if (status !== 'unchanged') patch.status = status
    if (schedulable !== 'unchanged') patch.schedulable = schedulable === 'true'
    if (Object.keys(patch).length === 0)
      return toast.error(t('Select at least one field to update'))
    setBusy(true)
    try {
      await onApply(patch)
      onOpenChange(false)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Batch update accounts')}</DialogTitle>
          <DialogDescription>
            {t('Update {{count}} selected accounts', { count })}
          </DialogDescription>
        </DialogHeader>
        <FieldGroup>
          <Field>
            <FieldLabel>{t('Status')}</FieldLabel>
            <Select
              value={status}
              onValueChange={(value) => setStatus(value as typeof status)}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='unchanged'>{t('Keep unchanged')}</SelectItem>
                <SelectItem value='active'>{t('Active')}</SelectItem>
                <SelectItem value='inactive'>{t('Inactive')}</SelectItem>
                <SelectItem value='error'>{t('Error')}</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel>{t('Schedulable')}</FieldLabel>
            <Select
              value={schedulable}
              onValueChange={(value) =>
                setSchedulable(value as typeof schedulable)
              }
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='unchanged'>{t('Keep unchanged')}</SelectItem>
                <SelectItem value='true'>{t('Enabled')}</SelectItem>
                <SelectItem value='false'>{t('Disabled')}</SelectItem>
              </SelectContent>
            </Select>
          </Field>
        </FieldGroup>
        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button disabled={busy} onClick={submit}>
            {busy && (
              <HugeiconsIcon
                icon={Loading03Icon}
                className='animate-spin'
                strokeWidth={2}
              />
            )}
            {t('Update')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function AccountDialog({
  open,
  onOpenChange,
  account,
  pools,
  proxies,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  account: UpstreamAccount | null
  pools: UpstreamAccountPool[]
  proxies: Array<{ id: number; name: string }>
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<AccountDraft>(() => accountDraft(account))
  const [busy, setBusy] = useState(false)
  const [oauthStarted, setOAuthStarted] = useState(false)

  const reset = () => {
    setDraft(accountDraft(account))
    setOAuthStarted(false)
  }

  const set = <K extends keyof AccountDraft>(key: K, value: AccountDraft[K]) =>
    setDraft((current) => ({ ...current, [key]: value }))

  const startOAuth = async () => {
    setBusy(true)
    try {
      const response = await startUpstreamOAuth({
        account_id: account?.id,
        proxy_id: draft.proxyId ? Number(draft.proxyId) : null,
      })
      if (!response.success || !response.data?.authorize_url) {
        throw new Error(response.message || t('Failed to start authorization'))
      }
      window.open(response.data.authorize_url, '_blank', 'noopener,noreferrer')
      setOAuthStarted(true)
      toast.success(t('Authorization page opened'))
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Request failed'))
    } finally {
      setBusy(false)
    }
  }

  const submit = async () => {
    if (!draft.name.trim()) {
      toast.error(t('Account name is required'))
      return
    }
    setBusy(true)
    try {
      const payload: UpstreamAccountPayload = {
        name: draft.name.trim(),
        notes: draft.notes.trim(),
        platform: 'openai',
        type: draft.type,
        extra: account?.extra || '{}',
        proxy_id: draft.proxyId ? Number(draft.proxyId) : null,
        concurrency: Math.max(1, Number(draft.concurrency) || 1),
        priority: Number(draft.priority) || 0,
        weight: Math.max(1, Number(draft.weight) || 1),
        status: draft.status,
        schedulable: draft.schedulable,
        auto_pause_on_expired: account?.auto_pause_on_expired ?? true,
        pool_ids: draft.poolIds,
      }
      if (draft.type === 'oauth') {
        if (!account && !draft.oauthInput.trim()) {
          throw new Error(t('Paste the OAuth callback URL or code'))
        }
        let accountId = account?.id
        if (draft.oauthInput.trim()) {
          const oauthResponse = await completeUpstreamOAuth({
            input: draft.oauthInput.trim(),
            name: draft.name.trim(),
            pool_ids: account ? draft.poolIds : [],
            proxy_id: draft.proxyId ? Number(draft.proxyId) : undefined,
          })
          if (!oauthResponse.success || !oauthResponse.data?.id) {
            throw new Error(oauthResponse.message || t('Save failed'))
          }
          accountId = oauthResponse.data.id
        }
        if (!accountId) throw new Error(t('Save failed'))
        const response = await updateUpstreamAccount(accountId, payload)
        if (!response.success)
          throw new Error(response.message || t('Save failed'))
      } else {
        if (draft.type === 'apikey' && draft.apiKey.trim()) {
          payload.credentials = { api_key: draft.apiKey.trim() }
        }
        if (!account && !payload.credentials) {
          throw new Error(t('API key is required'))
        }
        const response = account
          ? await updateUpstreamAccount(account.id, payload)
          : await createUpstreamAccount(payload)
        if (!response.success)
          throw new Error(response.message || t('Save failed'))
      }
      toast.success(account ? t('Account updated') : t('Account created'))
      onSaved()
      onOpenChange(false)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Save failed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next)
        if (!next) reset()
      }}
    >
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>
            {account ? t('Edit account') : t('Add account')}
          </DialogTitle>
          <DialogDescription>
            {t('Accounts can join multiple pools and are scheduled globally.')}
          </DialogDescription>
        </DialogHeader>
        <FieldGroup className='grid gap-4 sm:grid-cols-2'>
          <Field>
            <FieldLabel htmlFor='account-name'>{t('Name')}</FieldLabel>
            <Input
              id='account-name'
              value={draft.name}
              onChange={(event) => set('name', event.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel>{t('Credential type')}</FieldLabel>
            <Select
              value={draft.type}
              disabled={Boolean(account)}
              onValueChange={(value) =>
                set('type', value as UpstreamAccountType)
              }
            >
              <SelectTrigger className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='oauth'>{t('Codex OAuth')}</SelectItem>
                <SelectItem value='apikey'>{t('OpenAI API Key')}</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field className='sm:col-span-2'>
            <FieldLabel htmlFor='account-notes'>{t('Notes')}</FieldLabel>
            <Input
              id='account-notes'
              value={draft.notes}
              onChange={(event) => set('notes', event.target.value)}
            />
          </Field>
          {draft.type === 'apikey' && (
            <Field className='sm:col-span-2'>
              <FieldLabel htmlFor='account-key'>{t('API Key')}</FieldLabel>
              <Input
                id='account-key'
                type='password'
                autoComplete='new-password'
                value={draft.apiKey}
                placeholder={
                  account ? t('Leave empty to keep the current key') : ''
                }
                onChange={(event) => set('apiKey', event.target.value)}
              />
            </Field>
          )}
          {draft.type === 'oauth' && (
            <Field className='sm:col-span-2'>
              <div className='flex items-center justify-between gap-3'>
                <FieldLabel htmlFor='oauth-result'>
                  {t('Codex OAuth')}
                </FieldLabel>
                <Button
                  type='button'
                  variant='outline'
                  onClick={startOAuth}
                  disabled={busy}
                >
                  <HugeiconsIcon icon={Link01Icon} strokeWidth={2} />
                  {t(
                    account || oauthStarted
                      ? 'Open authorization again'
                      : 'Start authorization'
                  )}
                </Button>
              </div>
              <Textarea
                id='oauth-result'
                rows={3}
                value={draft.oauthInput}
                placeholder={t(
                  'Paste the callback URL, authorization code, or code#state'
                )}
                onChange={(event) => set('oauthInput', event.target.value)}
              />
              <FieldDescription>
                {t(
                  'The verifier is stored encrypted in the server, so authorization can complete on another instance.'
                )}
              </FieldDescription>
            </Field>
          )}
          <Field>
            <FieldLabel>{t('Proxy')}</FieldLabel>
            <Select
              value={draft.proxyId || 'none'}
              onValueChange={(value) =>
                set('proxyId', value === 'none' ? '' : String(value))
              }
            >
              <SelectTrigger className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='none'>
                  {t('Use pool or channel proxy')}
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
            <FieldLabel>{t('Status')}</FieldLabel>
            <Select
              value={draft.status}
              onValueChange={(value) =>
                set('status', value as AccountDraft['status'])
              }
            >
              <SelectTrigger className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='active'>{t('Active')}</SelectItem>
                <SelectItem value='inactive'>{t('Inactive')}</SelectItem>
                <SelectItem value='error'>{t('Error')}</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel htmlFor='account-concurrency'>
              {t('Concurrency')}
            </FieldLabel>
            <Input
              id='account-concurrency'
              type='number'
              min={1}
              value={draft.concurrency}
              onChange={(event) => set('concurrency', event.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor='account-priority'>{t('Priority')}</FieldLabel>
            <Input
              id='account-priority'
              type='number'
              value={draft.priority}
              onChange={(event) => set('priority', event.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor='account-weight'>{t('Weight')}</FieldLabel>
            <Input
              id='account-weight'
              type='number'
              min={1}
              value={draft.weight}
              onChange={(event) => set('weight', event.target.value)}
            />
          </Field>
          <Field
            orientation='horizontal'
            className='items-center self-end pb-2'
          >
            <Checkbox
              checked={draft.schedulable}
              onCheckedChange={(checked) =>
                set('schedulable', checked === true)
              }
            />
            <FieldLabel>{t('Schedulable')}</FieldLabel>
          </Field>
          <Field className='sm:col-span-2'>
            <FieldLabel>{t('Account pools')}</FieldLabel>
            <div className='grid gap-2 rounded-lg border p-3 sm:grid-cols-2'>
              {pools.length === 0 ? (
                <p className='text-muted-foreground text-sm'>
                  {t('Create an account pool first')}
                </p>
              ) : (
                pools.map((pool) => (
                  <label
                    key={pool.id}
                    className='flex items-center gap-2 text-sm'
                  >
                    <Checkbox
                      checked={draft.poolIds.includes(pool.id)}
                      onCheckedChange={(checked) =>
                        set(
                          'poolIds',
                          checked === true
                            ? [...draft.poolIds, pool.id]
                            : draft.poolIds.filter((id) => id !== pool.id)
                        )
                      }
                    />
                    <span>{pool.name}</span>
                    <Badge variant='outline'>{pool.credential_type}</Badge>
                  </label>
                ))
              )}
            </div>
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

type PoolDraft = {
  name: string
  description: string
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
        platform: 'openai',
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
                  <SelectItem value='mixed'>{t('Mixed')}</SelectItem>
                  <SelectItem value='oauth'>{t('Codex OAuth')}</SelectItem>
                  <SelectItem value='apikey'>{t('OpenAI API Key')}</SelectItem>
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
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const [accountDialog, setAccountDialog] = useState(false)
  const [batchImportOpen, setBatchImportOpen] = useState(false)
  const [batchUpdateOpen, setBatchUpdateOpen] = useState(false)
  const [bulkDeleteOpen, setBulkDeleteOpen] = useState(false)
  const [selectedAccountIds, setSelectedAccountIds] = useState<number[]>([])
  const [poolDialog, setPoolDialog] = useState(false)
  const [memberPool, setMemberPool] = useState<UpstreamAccountPool | null>(null)
  const [selectedAccount, setSelectedAccount] =
    useState<UpstreamAccount | null>(null)
  const [selectedPool, setSelectedPool] = useState<UpstreamAccountPool | null>(
    null
  )
  const [deleteTarget, setDeleteTarget] = useState<{
    kind: 'account' | 'pool'
    id: number
    name: string
  } | null>(null)
  const [busyId, setBusyId] = useState<number | null>(null)

  const accountsQuery = useQuery({
    queryKey: [...queryKeys.accounts, search, page],
    queryFn: () =>
      listUpstreamAccounts({
        page,
        page_size: 50,
        search: search || undefined,
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
    action: 'test' | 'refresh'
  ) => {
    setBusyId(account.id)
    try {
      const response =
        action === 'test'
          ? await testUpstreamAccount(account.id)
          : await refreshUpstreamOAuth(account.id)
      if (!response.success)
        throw new Error(response.message || t('Request failed'))
      toast.success(
        action === 'test'
          ? t('Account test succeeded')
          : t('Credential refreshed')
      )
      refresh()
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Request failed'))
    } finally {
      setBusyId(null)
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

  const importAccounts = async (items: unknown[]) => {
    const response = await createUpstreamAccountsBatch(
      items as UpstreamAccountPayload[]
    )
    if (!response.success)
      throw new Error(response.message || t('Import failed'))
    const failures = response.data?.failures.length ?? 0
    toast.success(
      t('Imported {{success}} accounts; {{failed}} failed', {
        success: response.data?.success_ids.length ?? 0,
        failed: failures,
      })
    )
    refresh()
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
          'Manage reusable OpenAI credentials and the local account pools referenced by channels.'
        )}
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        <Alert className='mb-4'>
          <AlertTitle>{t('Channel integration')}</AlertTitle>
          <AlertDescription>
            {t(
              'Add accounts to a pool here, then select Local account pool as the credential source in an OpenAI or Codex channel. The account itself is not a channel.'
            )}
          </AlertDescription>
        </Alert>
        <Tabs defaultValue='accounts'>
          <TabsList>
            <TabsTrigger value='accounts'>{t('Accounts')}</TabsTrigger>
            <TabsTrigger value='pools'>{t('Account pools')}</TabsTrigger>
          </TabsList>
          <TabsContent value='accounts' className='space-y-3 pt-3'>
            <div className='flex flex-wrap items-center justify-between gap-2'>
              <Input
                className='max-w-sm'
                value={search}
                placeholder={t('Search accounts')}
                onChange={(event) => {
                  setSearch(event.target.value)
                  setPage(1)
                }}
              />
              <div className='flex flex-wrap gap-2'>
                {selectedAccountIds.length > 0 && (
                  <>
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
                <Button
                  variant='outline'
                  onClick={() => setBatchImportOpen(true)}
                >
                  <HugeiconsIcon icon={FileImportIcon} strokeWidth={2} />
                  {t('Batch import')}
                </Button>
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
            </div>
            <div className='overflow-hidden rounded-lg border'>
              <Table>
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
                    <TableHead>{t('Account')}</TableHead>
                    <TableHead>{t('Type')}</TableHead>
                    <TableHead>{t('Pools')}</TableHead>
                    <TableHead>{t('Status')}</TableHead>
                    <TableHead>{t('Concurrency')}</TableHead>
                    <TableHead>{t('Last used')}</TableHead>
                    <TableHead className='text-right'>{t('Actions')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {accountsQuery.isLoading ? (
                    <TableRow>
                      <TableCell colSpan={8}>{t('Loading...')}</TableCell>
                    </TableRow>
                  ) : accounts.length === 0 ? (
                    <TableRow>
                      <TableCell
                        colSpan={8}
                        className='text-muted-foreground py-10 text-center'
                      >
                        {t('No accounts')}
                      </TableCell>
                    </TableRow>
                  ) : (
                    accounts.map((account) => (
                      <TableRow key={account.id}>
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
                          <div className='font-medium'>{account.name}</div>
                          {account.error_message && (
                            <div className='text-destructive max-w-64 truncate text-xs'>
                              {account.error_message}
                            </div>
                          )}
                        </TableCell>
                        <TableCell>
                          <Badge variant='outline'>
                            {account.type === 'oauth'
                              ? t('Codex OAuth')
                              : t('API Key')}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          <div className='flex max-w-72 flex-wrap gap-1'>
                            {account.pool_ids.length
                              ? account.pool_ids.map((id) => (
                                  <Badge key={id} variant='secondary'>
                                    {poolNames.get(id) || `#${id}`}
                                  </Badge>
                                ))
                              : '-'}
                          </div>
                        </TableCell>
                        <TableCell>
                          <div className='flex items-center gap-1'>
                            <Badge variant={statusVariant(account.status)}>
                              {account.status === 'active'
                                ? t('Active')
                                : account.status === 'inactive'
                                  ? t('Inactive')
                                  : t('Error')}
                            </Badge>
                            {!account.schedulable && (
                              <Badge variant='outline'>{t('Paused')}</Badge>
                            )}
                          </div>
                        </TableCell>
                        <TableCell>{account.concurrency}</TableCell>
                        <TableCell>{timestamp(account.last_used_at)}</TableCell>
                        <TableCell>
                          <div className='flex justify-end gap-1'>
                            <IconButton
                              label={t('Test')}
                              icon={
                                busyId === account.id
                                  ? Loading03Icon
                                  : TestTubeIcon
                              }
                              disabled={busyId === account.id}
                              onClick={() => runAccountAction(account, 'test')}
                            />
                            {account.type === 'oauth' && (
                              <IconButton
                                label={t('Refresh credential')}
                                icon={RefreshIcon}
                                disabled={busyId === account.id}
                                onClick={() =>
                                  runAccountAction(account, 'refresh')
                                }
                              />
                            )}
                            <IconButton
                              label={t('Edit')}
                              icon={Edit02Icon}
                              onClick={() => {
                                setSelectedAccount(account)
                                setAccountDialog(true)
                              }}
                            />
                            <IconButton
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
            <div className='flex justify-end'>
              <Button
                onClick={() => {
                  setSelectedPool(null)
                  setPoolDialog(true)
                }}
              >
                <HugeiconsIcon icon={Add01Icon} strokeWidth={2} />
                {t('Add account pool')}
              </Button>
            </div>
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
                    <TableHead className='text-right'>{t('Actions')}</TableHead>
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
                      <TableRow key={pool.id}>
                        <TableCell>
                          <div className='font-medium'>{pool.name}</div>
                          <div className='text-muted-foreground text-xs'>
                            {pool.description}
                          </div>
                        </TableCell>
                        <TableCell>
                          <Badge variant='outline'>
                            {pool.credential_type}
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
                        <TableCell>
                          <div className='flex justify-end gap-1'>
                            <IconButton
                              label={t('Manage pool members')}
                              icon={Link01Icon}
                              onClick={() => setMemberPool(pool)}
                            />
                            <IconButton
                              label={t('Edit')}
                              icon={Edit02Icon}
                              onClick={() => {
                                setSelectedPool(pool)
                                setPoolDialog(true)
                              }}
                            />
                            <IconButton
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
        <BatchImportDialog
          open={batchImportOpen}
          onOpenChange={setBatchImportOpen}
          title={t('Batch import accounts')}
          description={t(
            'Import up to 100 OpenAI API key or Codex OAuth accounts from a JSON array.'
          )}
          example={JSON.stringify(
            [
              {
                name: 'openai-account',
                platform: 'openai',
                type: 'apikey',
                credentials: { api_key: 'sk-...' },
                extra: '{}',
                concurrency: 1,
                priority: 50,
                weight: 1,
                status: 'active',
                schedulable: true,
                auto_pause_on_expired: true,
                pool_ids: [],
              },
            ],
            null,
            2
          )}
          onImport={importAccounts}
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
        <PoolMembersDialog
          open={!!memberPool}
          onOpenChange={(open) => {
            if (!open) setMemberPool(null)
          }}
          pool={memberPool}
          onSaved={refresh}
        />
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
