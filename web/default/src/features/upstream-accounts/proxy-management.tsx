/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Add01Icon,
  Delete02Icon,
  Edit02Icon,
  FileImportIcon,
  Link01Icon,
  Loading03Icon,
  PlayIcon,
  RefreshIcon,
  TestTubeIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { PasswordInput } from '@/components/password-input'
import {
  createUpstreamProxy,
  createUpstreamProxiesBatch,
  deleteUpstreamProxy,
  deleteUpstreamProxiesBatch,
  listUpstreamAccounts,
  listUpstreamProxies,
  testUpstreamProxy,
  updateUpstreamProxy,
  updateUpstreamProxiesBatch,
} from './api'
import { BatchImportDialog } from './batch-import-dialog'
import type {
  UpstreamAccount,
  UpstreamProxy,
  UpstreamProxyFallback,
  UpstreamProxyPayload,
  UpstreamProxyProtocol,
  UpstreamProxyStatus,
} from './types'

type ProxyDraft = {
  name: string
  protocol: UpstreamProxyProtocol
  host: string
  port: string
  username: string
  password: string
  status: Exclude<UpstreamProxyStatus, 'expired'>
  fallbackMode: UpstreamProxyFallback
  backupProxyId: string
  expiresAt: string
  expiresDays: string
  expiryWarnDays: string
}

function ProxyAccountsDialog({
  proxy,
  onOpenChange,
}: {
  proxy: UpstreamProxy | null
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['upstream-proxy-accounts', proxy?.id],
    queryFn: () =>
      listUpstreamAccounts({
        proxy_id: proxy?.id,
        page: 1,
        page_size: 100,
      }),
    enabled: proxy != null,
  })
  const accounts: UpstreamAccount[] = query.data?.data?.items ?? []

  return (
    <Dialog open={proxy != null} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>
            {t('Accounts')} · {proxy?.name}
          </DialogTitle>
          <DialogDescription>{proxy?.name}</DialogDescription>
        </DialogHeader>
        <div className='max-h-80 overflow-auto rounded-lg border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Name')}</TableHead>
                <TableHead>{t('Platform')}</TableHead>
                <TableHead>{t('Type')}</TableHead>
                <TableHead>{t('Notes')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {query.isLoading ? (
                <TableRow>
                  <TableCell colSpan={4}>{t('Loading...')}</TableCell>
                </TableRow>
              ) : query.isError ? (
                <TableRow>
                  <TableCell
                    colSpan={4}
                    className='text-destructive py-8 text-center'
                  >
                    {t('Failed to load')}
                  </TableCell>
                </TableRow>
              ) : accounts.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={4}
                    className='text-muted-foreground py-8 text-center'
                  >
                    {t('No accounts')}
                  </TableCell>
                </TableRow>
              ) : (
                accounts.map((account) => (
                  <TableRow key={account.id}>
                    <TableCell className='font-medium'>
                      {account.name}
                    </TableCell>
                    <TableCell>{account.platform}</TableCell>
                    <TableCell>{account.type}</TableCell>
                    <TableCell>{account.notes || '-'}</TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

const EXPIRY_PRESETS = [7, 30, 90, 180]

function localDateInput(date: Date) {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function unixSecondsToLocalDate(value?: number | null) {
  if (!value) return ''
  const date = new Date(value * 1000)
  if (Number.isNaN(date.getTime())) return ''
  return localDateInput(date)
}

function parseLocalDate(value: string) {
  const [year, month, day] = value.split('-').map(Number)
  if (!year || !month || !day) return null
  const date = new Date(year, month - 1, day)
  return Number.isNaN(date.getTime()) ? null : date
}

function addDaysToBase(base: string, days: number) {
  if (!Number.isFinite(days) || days <= 0) return ''
  const date = parseLocalDate(base) ?? new Date()
  date.setDate(date.getDate() + Math.floor(days))
  return localDateInput(date)
}

function daysFromBase(base: string, target: string) {
  const baseDate =
    parseLocalDate(base) ?? parseLocalDate(localDateInput(new Date()))
  const targetDate = parseLocalDate(target)
  if (!baseDate || !targetDate) return ''
  return String(
    Math.round((targetDate.getTime() - baseDate.getTime()) / 86_400_000)
  )
}

function formatProxyDateTime(value: number, locale: string) {
  return new Intl.DateTimeFormat(locale, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).format(new Date(value * 1000))
}

function proxyCountryLabel(country: string, locale: string) {
  const normalized = country.trim().toUpperCase()
  if (!/^[A-Z]{2}$/.test(normalized)) return country.trim()
  try {
    return (
      new Intl.DisplayNames([locale], { type: 'region' }).of(normalized) ||
      normalized
    )
  } catch {
    return normalized
  }
}

function proxyCountryFlagUrl(country: string) {
  const normalized = country.trim().toLowerCase()
  if (!/^[a-z]{2}$/.test(normalized)) return ''
  return `https://unpkg.com/flag-icons/flags/4x3/${normalized}.svg`
}

function proxyCountryFlagAlt(country: string, locale: string) {
  const normalized = country.trim().toUpperCase()
  if (!/^[A-Z]{2}$/.test(normalized)) return ''
  return proxyCountryLabel(normalized, locale)
}

function proxyLocation(proxy: UpstreamProxy, locale: string) {
  const parts = [
    proxy.observed_country
      ? proxyCountryLabel(proxy.observed_country, locale)
      : '',
    proxy.observed_region,
    proxy.observed_city,
  ].filter((part, index, values) => {
    const normalized = part.trim().toLowerCase()
    return (
      normalized.length > 0 &&
      values.findIndex((value) => value.trim().toLowerCase() === normalized) ===
        index
    )
  })
  return parts.join(' · ')
}

function proxyDraft(proxy?: UpstreamProxy | null): ProxyDraft {
  const expiresAt = unixSecondsToLocalDate(proxy?.expires_at)
  const expiryBase = unixSecondsToLocalDate(proxy?.created_at)
  return {
    name: proxy?.name ?? '',
    protocol: proxy?.protocol ?? 'http',
    host: proxy?.host ?? '',
    port: String(proxy?.port ?? 8080),
    username: proxy?.username ?? '',
    password: proxy?.password ?? '',
    status:
      proxy?.status === 'expired' ? 'inactive' : (proxy?.status ?? 'active'),
    fallbackMode: proxy?.fallback_mode ?? 'none',
    backupProxyId: proxy?.backup_proxy_id ? String(proxy.backup_proxy_id) : '',
    expiresAt,
    expiresDays: expiresAt ? daysFromBase(expiryBase, expiresAt) : '',
    expiryWarnDays: String(proxy ? (proxy.expiry_warn_days ?? 0) : 7),
  }
}

function ProxyBatchUpdateDialog({
  open,
  onOpenChange,
  count,
  onApply,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  count: number
  onApply: (patch: Partial<UpstreamProxyPayload>) => Promise<void>
}) {
  const { t } = useTranslation()
  const [status, setStatus] = useState<'unchanged' | 'active' | 'inactive'>(
    'unchanged'
  )
  const [busy, setBusy] = useState(false)
  const submit = async () => {
    if (status === 'unchanged')
      return toast.error(t('Select at least one field to update'))
    setBusy(true)
    try {
      await onApply({ status })
      onOpenChange(false)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Batch update proxies')}</DialogTitle>
          <DialogDescription>
            {t('Update {{count}} selected proxies', { count })}
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
                <SelectItem value='active'>
                  {t('Proxy status active')}
                </SelectItem>
                <SelectItem value='inactive'>
                  {t('Proxy status inactive')}
                </SelectItem>
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

function ProxyDialog({
  open,
  onOpenChange,
  proxy,
  proxies,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  proxy: UpstreamProxy | null
  proxies: UpstreamProxy[]
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<ProxyDraft>(() => proxyDraft(proxy))
  const [busy, setBusy] = useState(false)
  const [passwordDirty, setPasswordDirty] = useState(false)
  const expiryBase = unixSecondsToLocalDate(proxy?.created_at)
  const set = <K extends keyof ProxyDraft>(key: K, value: ProxyDraft[K]) =>
    setDraft((current) => ({ ...current, [key]: value }))
  const setExpiryDays = (value: string) => {
    const digits = value.replace(/\D/g, '')
    const days = Number(digits)
    const normalized =
      digits && Number.isFinite(days) && days > 0 ? String(days) : ''
    setDraft((current) => ({
      ...current,
      expiresDays: normalized,
      expiresAt: normalized ? addDaysToBase(expiryBase, days) : '',
    }))
  }
  const setExpiryDate = (value: string) =>
    setDraft((current) => ({
      ...current,
      expiresAt: value,
      expiresDays: value ? daysFromBase(expiryBase, value) : '',
    }))
  const submit = async () => {
    if (!draft.name.trim() || !draft.host.trim())
      return toast.error(t('Proxy name and host are required'))
    const port = Number(draft.port)
    if (!Number.isInteger(port) || port < 1 || port > 65535)
      return toast.error(t('Proxy port is invalid'))
    if (draft.fallbackMode === 'proxy' && !draft.backupProxyId)
      return toast.error(t('Backup proxy is required'))
    setBusy(true)
    try {
      const payload: UpstreamProxyPayload = {
        name: draft.name.trim(),
        protocol: draft.protocol,
        host: draft.host.trim(),
        port,
        status: draft.status,
        fallback_mode: draft.fallbackMode,
        backup_proxy_id:
          draft.fallbackMode === 'proxy' ? Number(draft.backupProxyId) : null,
        expires_at: draft.expiresAt
          ? Math.floor(new Date(draft.expiresAt).getTime() / 1000)
          : null,
        expiry_warn_days: Math.max(0, Number(draft.expiryWarnDays) || 0),
      }
      if (
        !proxy ||
        draft.username !== (proxy.username ?? '') ||
        passwordDirty
      ) {
        payload.auth = {
          username: draft.username,
          password: passwordDirty || !proxy ? draft.password : '',
        }
      }
      const response = proxy
        ? await updateUpstreamProxy(proxy.id, payload)
        : await createUpstreamProxy({
            ...payload,
            auth: payload.auth || { username: '', password: '' },
          })
      if (!response.success)
        throw new Error(response.message || t('Save failed'))
      toast.success(proxy ? t('Proxy updated') : t('Proxy created'))
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
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{proxy ? t('Edit proxy') : t('Add proxy')}</DialogTitle>
          <DialogDescription>
            {t(
              'Proxy precedence is account, pool default, channel, then direct connection.'
            )}
          </DialogDescription>
        </DialogHeader>
        <FieldGroup className='grid gap-4 sm:grid-cols-2'>
          <Field>
            <FieldLabel htmlFor='proxy-name'>{t('Name')}</FieldLabel>
            <Input
              id='proxy-name'
              value={draft.name}
              onChange={(event) => set('name', event.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel>{t('Protocol')}</FieldLabel>
            <Select
              value={draft.protocol}
              onValueChange={(value) =>
                set('protocol', value as UpstreamProxyProtocol)
              }
            >
              <SelectTrigger className='w-full'>
                <SelectValue>{draft.protocol.toUpperCase()}</SelectValue>
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  {['http', 'https', 'socks5', 'socks5h'].map((protocol) => (
                    <SelectItem key={protocol} value={protocol}>
                      {protocol.toUpperCase()}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel htmlFor='proxy-host'>{t('Host')}</FieldLabel>
            <Input
              id='proxy-host'
              value={draft.host}
              onChange={(event) => set('host', event.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor='proxy-port'>{t('Port')}</FieldLabel>
            <Input
              id='proxy-port'
              type='text'
              inputMode='numeric'
              pattern='[0-9]*'
              value={draft.port}
              onChange={(event) =>
                set('port', event.target.value.replace(/\D/g, ''))
              }
            />
          </Field>
          <Field>
            <FieldLabel htmlFor='proxy-username'>{t('Username')}</FieldLabel>
            <Input
              id='proxy-username'
              autoComplete='off'
              value={draft.username}
              onChange={(event) => set('username', event.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor='proxy-password'>{t('Password')}</FieldLabel>
            <PasswordInput
              id='proxy-password'
              autoComplete='new-password'
              value={draft.password}
              placeholder={
                proxy?.auth_configured
                  ? t('Leave empty to keep current authentication')
                  : ''
              }
              onChange={(event) => {
                set('password', event.target.value)
                setPasswordDirty(true)
              }}
            />
          </Field>
          <Field>
            <FieldLabel>{t('Status')}</FieldLabel>
            <Select
              value={draft.status}
              onValueChange={(value) =>
                set('status', value as ProxyDraft['status'])
              }
            >
              <SelectTrigger className='w-full'>
                <SelectValue>
                  {draft.status === 'active'
                    ? t('Proxy status active')
                    : t('Proxy status inactive')}
                </SelectValue>
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  <SelectItem value='active'>
                    {t('Proxy status active')}
                  </SelectItem>
                  <SelectItem value='inactive'>
                    {t('Proxy status inactive')}
                  </SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel>{t('Fallback')}</FieldLabel>
            <Select
              value={draft.fallbackMode}
              onValueChange={(value) =>
                set('fallbackMode', value as UpstreamProxyFallback)
              }
            >
              <SelectTrigger className='w-full'>
                <SelectValue>
                  {draft.fallbackMode === 'direct'
                    ? t('Fall back to direct')
                    : draft.fallbackMode === 'proxy'
                      ? t('Fall back to another proxy')
                      : t('No fallback')}
                </SelectValue>
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  <SelectItem value='none'>{t('No fallback')}</SelectItem>
                  <SelectItem value='direct'>
                    {t('Fall back to direct')}
                  </SelectItem>
                  <SelectItem value='proxy'>
                    {t('Fall back to another proxy')}
                  </SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
          {draft.fallbackMode === 'proxy' && (
            <Field>
              <FieldLabel>{t('Backup proxy')}</FieldLabel>
              <Select
                value={draft.backupProxyId}
                onValueChange={(value) => set('backupProxyId', String(value))}
              >
                <SelectTrigger className='w-full'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {proxies
                    .filter((item) => item.id !== proxy?.id)
                    .map((item) => (
                      <SelectItem key={item.id} value={String(item.id)}>
                        {item.name}
                      </SelectItem>
                    ))}
                </SelectContent>
              </Select>
            </Field>
          )}
          <Field className='sm:col-span-2'>
            <FieldLabel>{t('Validity')}</FieldLabel>
            <ToggleGroup
              value={
                EXPIRY_PRESETS.includes(Number(draft.expiresDays))
                  ? [draft.expiresDays]
                  : []
              }
              onValueChange={(values) => {
                const value = values.at(-1)
                if (value) setExpiryDays(value)
              }}
              variant='outline'
              spacing={1}
            >
              {EXPIRY_PRESETS.map((days) => (
                <ToggleGroupItem key={days} value={String(days)}>
                  {days} {t('days')}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
            <div className='grid gap-3 sm:grid-cols-2'>
              <Input
                id='proxy-expiry-days'
                type='text'
                inputMode='numeric'
                pattern='[0-9]*'
                aria-label={`${t('Validity')} (${t('days')})`}
                value={draft.expiresDays}
                onChange={(event) => setExpiryDays(event.target.value)}
              />
              <Input
                id='proxy-expires-at'
                type='date'
                aria-label={t('Expiration Time')}
                value={draft.expiresAt}
                onChange={(event) => setExpiryDate(event.target.value)}
              />
            </div>
            <FieldDescription>
              {t('Leave empty for never expires')}
            </FieldDescription>
          </Field>
          <Field>
            <FieldLabel htmlFor='proxy-warn-days'>
              {t('Expiry warning days')}
            </FieldLabel>
            <Input
              id='proxy-warn-days'
              type='text'
              inputMode='numeric'
              pattern='[0-9]*'
              value={draft.expiryWarnDays}
              onChange={(event) =>
                set('expiryWarnDays', event.target.value.replace(/\D/g, ''))
              }
            />
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

export function ProxyManagement() {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const query = useQuery({
    queryKey: ['upstream-proxies'],
    queryFn: listUpstreamProxies,
  })
  const proxies = useMemo(() => query.data?.data ?? [], [query.data?.data])
  const [search, setSearch] = useState('')
  const [protocolFilter, setProtocolFilter] = useState('all')
  const [statusFilter, setStatusFilter] = useState('all')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [batchImportOpen, setBatchImportOpen] = useState(false)
  const [batchUpdateOpen, setBatchUpdateOpen] = useState(false)
  const [bulkDeleteOpen, setBulkDeleteOpen] = useState(false)
  const [selectedIds, setSelectedIds] = useState<number[]>([])
  const [selected, setSelected] = useState<UpstreamProxy | null>(null)
  const [accountsProxy, setAccountsProxy] = useState<UpstreamProxy | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<UpstreamProxy | null>(null)
  const [testingId, setTestingId] = useState<number | null>(null)
  const [batchTesting, setBatchTesting] = useState(false)
  const [currentTime] = useState(() => Date.now())
  const filteredProxies = useMemo(() => {
    const normalizedSearch = search.trim().toLowerCase()
    return proxies.filter((proxy) => {
      if (protocolFilter !== 'all' && proxy.protocol !== protocolFilter) {
        return false
      }
      if (statusFilter !== 'all' && proxy.status !== statusFilter) {
        return false
      }
      if (!normalizedSearch) return true
      return [
        proxy.name,
        proxy.host,
        String(proxy.port),
        proxy.observed_ip,
        proxy.observed_country,
        proxy.observed_region,
        proxy.observed_city,
      ].some((value) => value.toLowerCase().includes(normalizedSearch))
    })
  }, [protocolFilter, proxies, search, statusFilter])
  const refresh = () =>
    void queryClient.invalidateQueries({ queryKey: ['upstream-proxies'] })
  const test = async (proxy: UpstreamProxy) => {
    setTestingId(proxy.id)
    try {
      const response = await testUpstreamProxy(proxy.id)
      if (!response.success)
        throw new Error(response.message || t('Proxy test failed'))
      toast.success(
        t('Proxy test succeeded in {{latency}} ms', {
          latency: response.data?.latency_ms ?? 0,
        })
      )
      refresh()
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('Proxy test failed')
      )
    } finally {
      setTestingId(null)
    }
  }
  const testBatch = async () => {
    if (batchTesting) return
    const ids =
      selectedIds.length > 0
        ? [...selectedIds]
        : filteredProxies.map((proxy) => proxy.id)
    if (ids.length === 0) {
      toast.info(t('No proxies to test'))
      return
    }

    setBatchTesting(true)
    try {
      let index = 0
      const worker = async () => {
        while (index < ids.length) {
          const id = ids[index]
          index += 1
          await testUpstreamProxy(id).catch(() => null)
        }
      }
      await Promise.all(
        Array.from({ length: Math.min(5, ids.length) }, () => worker())
      )
      toast.success(
        t('Batch test completed for {{count}} proxies', {
          count: ids.length,
        })
      )
      refresh()
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('Batch proxy test failed')
      )
    } finally {
      setBatchTesting(false)
    }
  }
  const remove = async () => {
    if (!deleteTarget) return
    try {
      const response = await deleteUpstreamProxy(deleteTarget.id)
      if (!response.success)
        throw new Error(response.message || t('Delete failed'))
      toast.success(t('Deleted'))
      setDeleteTarget(null)
      refresh()
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Delete failed'))
    }
  }
  const importProxies = async (items: unknown[]) => {
    const response = await createUpstreamProxiesBatch(
      items as UpstreamProxyPayload[]
    )
    if (!response.success)
      throw new Error(response.message || t('Import failed'))
    toast.success(
      t('Imported {{success}} proxies; {{failed}} failed', {
        success: response.data?.success_ids.length ?? 0,
        failed: response.data?.failures.length ?? 0,
      })
    )
    refresh()
  }
  const removeSelected = async () => {
    const response = await deleteUpstreamProxiesBatch(selectedIds)
    if (!response.success) {
      toast.error(response.message || t('Delete failed'))
      return
    }
    toast.success(
      t('Deleted {{success}} proxies; {{failed}} failed', {
        success: response.data?.success_ids.length ?? 0,
        failed: response.data?.failures.length ?? 0,
      })
    )
    setSelectedIds([])
    setBulkDeleteOpen(false)
    refresh()
  }
  const updateSelected = async (patch: Partial<UpstreamProxyPayload>) => {
    const response = await updateUpstreamProxiesBatch(selectedIds, patch)
    if (!response.success) {
      toast.error(response.message || t('Update failed'))
      return
    }
    toast.success(
      t('Updated {{success}} proxies; {{failed}} failed', {
        success: response.data?.success_ids.length ?? 0,
        failed: response.data?.failures.length ?? 0,
      })
    )
    setSelectedIds([])
    refresh()
  }
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('IP Management')}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t(
          'Manage upstream HTTP and SOCKS proxies, health checks, and fallback chains.'
        )}
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        <div className='flex flex-wrap items-center gap-2'>
          <Input
            className='max-w-sm min-w-48 flex-1'
            value={search}
            placeholder={t('Search proxies')}
            onChange={(event) => setSearch(event.target.value)}
          />
          <Select
            value={protocolFilter}
            onValueChange={(value) => setProtocolFilter(value ?? 'all')}
          >
            <SelectTrigger className='w-36'>
              <SelectValue>
                {protocolFilter === 'all'
                  ? t('All protocols')
                  : protocolFilter.toUpperCase()}
              </SelectValue>
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                <SelectItem value='all'>{t('All protocols')}</SelectItem>
                <SelectItem value='http'>HTTP</SelectItem>
                <SelectItem value='https'>HTTPS</SelectItem>
                <SelectItem value='socks5'>SOCKS5</SelectItem>
                <SelectItem value='socks5h'>SOCKS5H</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
          <Select
            value={statusFilter}
            onValueChange={(value) => setStatusFilter(value ?? 'all')}
          >
            <SelectTrigger className='w-32'>
              <SelectValue>
                {statusFilter === 'all'
                  ? t('All statuses')
                  : statusFilter === 'active'
                    ? t('Proxy status active')
                    : statusFilter === 'inactive'
                      ? t('Proxy status inactive')
                      : t('Proxy status expired')}
              </SelectValue>
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                <SelectItem value='all'>{t('All statuses')}</SelectItem>
                <SelectItem value='active'>
                  {t('Proxy status active')}
                </SelectItem>
                <SelectItem value='inactive'>
                  {t('Proxy status inactive')}
                </SelectItem>
                <SelectItem value='expired'>
                  {t('Proxy status expired')}
                </SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
          <div className='ml-auto flex flex-wrap gap-2'>
            <Button
              size='icon'
              variant='outline'
              title={t('Refresh')}
              aria-label={t('Refresh')}
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              <HugeiconsIcon
                icon={RefreshIcon}
                className={query.isFetching ? 'animate-spin' : ''}
                strokeWidth={2}
              />
            </Button>
            <Button
              variant='outline'
              title={t('Test Connection')}
              disabled={batchTesting || query.isFetching}
              onClick={() => void testBatch()}
            >
              <HugeiconsIcon
                icon={batchTesting ? Loading03Icon : PlayIcon}
                data-icon='inline-start'
                className={batchTesting ? 'animate-spin' : ''}
                strokeWidth={2}
              />
              {t('Test Connection')}
            </Button>
            {selectedIds.length > 0 && (
              <>
                <Button
                  variant='outline'
                  onClick={() => setBatchUpdateOpen(true)}
                >
                  <HugeiconsIcon
                    icon={Edit02Icon}
                    data-icon='inline-start'
                    strokeWidth={2}
                  />
                  {t('Batch update')}
                </Button>
                <Button
                  variant='destructive'
                  onClick={() => setBulkDeleteOpen(true)}
                >
                  <HugeiconsIcon
                    icon={Delete02Icon}
                    data-icon='inline-start'
                    strokeWidth={2}
                  />
                  {t('Delete selected ({{count}})', {
                    count: selectedIds.length,
                  })}
                </Button>
              </>
            )}
            <Button variant='outline' onClick={() => setBatchImportOpen(true)}>
              <HugeiconsIcon
                icon={FileImportIcon}
                data-icon='inline-start'
                strokeWidth={2}
              />
              {t('Batch import')}
            </Button>
            <Button
              onClick={() => {
                setSelected(null)
                setDialogOpen(true)
              }}
            >
              <HugeiconsIcon
                icon={Add01Icon}
                data-icon='inline-start'
                strokeWidth={2}
              />
              {t('Add proxy')}
            </Button>
          </div>
        </div>
        <div className='overflow-hidden rounded-lg border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className='w-10'>
                  <Checkbox
                    aria-label={t('Select all proxies')}
                    checked={
                      filteredProxies.length > 0 &&
                      filteredProxies.every((proxy) =>
                        selectedIds.includes(proxy.id)
                      )
                    }
                    onCheckedChange={(checked) =>
                      setSelectedIds(
                        checked === true
                          ? Array.from(
                              new Set([
                                ...selectedIds,
                                ...filteredProxies.map((proxy) => proxy.id),
                              ])
                            )
                          : selectedIds.filter(
                              (id) =>
                                !filteredProxies.some(
                                  (proxy) => proxy.id === id
                                )
                            )
                      )
                    }
                  />
                </TableHead>
                <TableHead>{t('Name')}</TableHead>
                <TableHead>{t('Endpoint')}</TableHead>
                <TableHead>{t('Location')}</TableHead>
                <TableHead>{t('Accounts')}</TableHead>
                <TableHead>{t('Latency')}</TableHead>
                <TableHead>{t('Expires')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
                <TableHead className='text-right'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {query.isLoading ? (
                <TableRow>
                  <TableCell colSpan={9}>{t('Loading...')}</TableCell>
                </TableRow>
              ) : filteredProxies.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={9}
                    className='text-muted-foreground py-10 text-center'
                  >
                    {t('No proxies')}
                  </TableCell>
                </TableRow>
              ) : (
                filteredProxies.map((proxy) => (
                  <TableRow key={proxy.id}>
                    <TableCell>
                      <Checkbox
                        aria-label={t('Select proxy {{name}}', {
                          name: proxy.name,
                        })}
                        checked={selectedIds.includes(proxy.id)}
                        onCheckedChange={(checked) =>
                          setSelectedIds((current) =>
                            checked === true
                              ? [...current, proxy.id]
                              : current.filter((id) => id !== proxy.id)
                          )
                        }
                      />
                    </TableCell>
                    <TableCell>
                      <div className='font-medium'>{proxy.name}</div>
                      <div className='text-muted-foreground text-xs'>
                        {proxy.fallback_mode === 'proxy'
                          ? `${t('Backup')} #${proxy.backup_proxy_id}`
                          : proxy.fallback_mode === 'direct'
                            ? t('Fall back to direct')
                            : t('No fallback')}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className='flex items-center gap-1'>
                        <Badge variant='outline'>
                          {proxy.protocol.toUpperCase()}
                        </Badge>
                        <Badge variant='secondary'>
                          {proxy.host}:{proxy.port}
                        </Badge>
                        <Button
                          size='icon-xs'
                          variant='ghost'
                          title={t('Copy')}
                          aria-label={t('Copy')}
                          onClick={() => {
                            void navigator.clipboard.writeText(
                              `${proxy.host}:${proxy.port}`
                            )
                            toast.success(t('Copied'))
                          }}
                        >
                          <HugeiconsIcon icon={Link01Icon} strokeWidth={2} />
                        </Button>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className='flex items-center gap-2'>
                        {proxyCountryFlagUrl(proxy.observed_country) && (
                          <img
                            src={proxyCountryFlagUrl(proxy.observed_country)}
                            alt={proxyCountryFlagAlt(
                              proxy.observed_country,
                              i18n.language
                            )}
                            className='h-4 w-6 rounded-sm'
                          />
                        )}
                        <span>
                          {proxyLocation(proxy, i18n.language) || '-'}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell>
                      {proxy.account_count > 0 ? (
                        <Button
                          size='xs'
                          variant='secondary'
                          onClick={() => setAccountsProxy(proxy)}
                        >
                          {proxy.account_count} {t('Accounts')}
                        </Button>
                      ) : (
                        <Badge variant='secondary'>0 {t('Accounts')}</Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      {proxy.latency_status === 'failed' ? (
                        <Badge variant='destructive'>
                          {t('Connection failed')}
                        </Badge>
                      ) : proxy.latency_ms == null ? (
                        '-'
                      ) : (
                        <Badge
                          variant={
                            proxy.latency_ms < 200 ? 'success' : 'warning'
                          }
                        >
                          {proxy.latency_ms}ms
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className='text-muted-foreground text-xs whitespace-nowrap'>
                      {proxy.expires_at
                        ? `${formatProxyDateTime(proxy.expires_at, i18n.language)} · ${
                            proxy.expires_at * 1000 <= currentTime
                              ? t('Expired')
                              : t('Remaining {{days}} days', {
                                  days: Math.ceil(
                                    (proxy.expires_at * 1000 - currentTime) /
                                      86_400_000
                                  ),
                                })
                          }`
                        : t('Never expires')}
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant={
                          proxy.status === 'active' ? 'success' : 'destructive'
                        }
                      >
                        {proxy.status === 'active'
                          ? t('Proxy status active')
                          : proxy.status === 'inactive'
                            ? t('Proxy status inactive')
                            : t('Proxy status expired')}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <div className='flex justify-end gap-1'>
                        <Button
                          size='sm'
                          variant='ghost'
                          disabled={testingId === proxy.id}
                          onClick={() => test(proxy)}
                        >
                          <HugeiconsIcon
                            icon={
                              testingId === proxy.id
                                ? Loading03Icon
                                : TestTubeIcon
                            }
                            data-icon='inline-start'
                            className={
                              testingId === proxy.id ? 'animate-spin' : ''
                            }
                            strokeWidth={2}
                          />
                          {t('Test')}
                        </Button>
                        <Button
                          size='sm'
                          variant='ghost'
                          onClick={() => {
                            setSelected(proxy)
                            setDialogOpen(true)
                          }}
                        >
                          <HugeiconsIcon
                            icon={Edit02Icon}
                            data-icon='inline-start'
                            strokeWidth={2}
                          />
                          {t('Edit')}
                        </Button>
                        <Button
                          size='sm'
                          variant='destructive'
                          onClick={() => setDeleteTarget(proxy)}
                        >
                          <HugeiconsIcon
                            icon={Delete02Icon}
                            data-icon='inline-start'
                            strokeWidth={2}
                          />
                          {t('Delete')}
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
        {dialogOpen && (
          <ProxyDialog
            key={selected?.id || 'new'}
            open={dialogOpen}
            onOpenChange={setDialogOpen}
            proxy={selected}
            proxies={proxies}
            onSaved={refresh}
          />
        )}
        <ProxyAccountsDialog
          proxy={accountsProxy}
          onOpenChange={(open) => !open && setAccountsProxy(null)}
        />
        <BatchImportDialog
          open={batchImportOpen}
          onOpenChange={setBatchImportOpen}
          title={t('Batch import proxies')}
          description={t(
            'Import up to 100 HTTP, HTTPS, SOCKS5, or SOCKS5H proxies from a JSON array.'
          )}
          collectionKey='proxies'
          onImport={importProxies}
        />
        <ProxyBatchUpdateDialog
          open={batchUpdateOpen}
          onOpenChange={setBatchUpdateOpen}
          count={selectedIds.length}
          onApply={updateSelected}
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
                  'Delete {{name}}? The server rejects deletion while the proxy is referenced.',
                  { name: deleteTarget?.name }
                )}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
              <AlertDialogAction variant='destructive' onClick={remove}>
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
                {t('Delete {{count}} selected proxies?', {
                  count: selectedIds.length,
                })}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
              <AlertDialogAction variant='destructive' onClick={removeSelected}>
                {t('Delete')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
