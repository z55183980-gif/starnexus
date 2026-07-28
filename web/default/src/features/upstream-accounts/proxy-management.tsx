/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Add01Icon,
  Delete02Icon,
  Edit02Icon,
  FileImportIcon,
  Loading03Icon,
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
import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import {
  createUpstreamProxy,
  createUpstreamProxiesBatch,
  deleteUpstreamProxy,
  deleteUpstreamProxiesBatch,
  listUpstreamProxies,
  testUpstreamProxy,
  updateUpstreamProxy,
  updateUpstreamProxiesBatch,
} from './api'
import { BatchImportDialog } from './batch-import-dialog'
import type {
  UpstreamProxy,
  UpstreamProxyFallback,
  UpstreamProxyPayload,
  UpstreamProxyProtocol,
} from './types'

type ProxyDraft = {
  name: string
  protocol: UpstreamProxyProtocol
  host: string
  port: string
  username: string
  password: string
  status: 'active' | 'inactive'
  fallbackMode: UpstreamProxyFallback
  backupProxyId: string
  expiresAt: string
  expiryWarnDays: string
}

function unixSecondsToLocalDateTime(value?: number | null) {
  if (!value) return ''
  const date = new Date(value * 1000)
  if (Number.isNaN(date.getTime())) return ''
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000)
    .toISOString()
    .slice(0, 16)
}

function proxyDraft(proxy?: UpstreamProxy | null): ProxyDraft {
  return {
    name: proxy?.name ?? '',
    protocol: proxy?.protocol ?? 'http',
    host: proxy?.host ?? '',
    port: String(proxy?.port ?? 8080),
    username: '',
    password: '',
    status: proxy?.status ?? 'active',
    fallbackMode: proxy?.fallback_mode ?? 'none',
    backupProxyId: proxy?.backup_proxy_id ? String(proxy.backup_proxy_id) : '',
    expiresAt: unixSecondsToLocalDateTime(proxy?.expires_at),
    expiryWarnDays: String(proxy?.expiry_warn_days ?? 0),
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
                <SelectItem value='active'>{t('Active')}</SelectItem>
                <SelectItem value='inactive'>{t('Inactive')}</SelectItem>
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
  const set = <K extends keyof ProxyDraft>(key: K, value: ProxyDraft[K]) =>
    setDraft((current) => ({ ...current, [key]: value }))
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
      if (draft.username || draft.password)
        payload.auth = { username: draft.username, password: draft.password }
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
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {['http', 'https', 'socks5', 'socks5h'].map((protocol) => (
                  <SelectItem key={protocol} value={protocol}>
                    {protocol.toUpperCase()}
                  </SelectItem>
                ))}
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
              type='number'
              min={1}
              max={65535}
              value={draft.port}
              onChange={(event) => set('port', event.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor='proxy-username'>{t('Username')}</FieldLabel>
            <Input
              id='proxy-username'
              autoComplete='off'
              value={draft.username}
              placeholder={
                proxy?.auth_configured
                  ? t('Leave empty to keep current authentication')
                  : ''
              }
              onChange={(event) => set('username', event.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor='proxy-password'>{t('Password')}</FieldLabel>
            <Input
              id='proxy-password'
              type='password'
              autoComplete='new-password'
              value={draft.password}
              placeholder={
                proxy?.auth_configured
                  ? t('Leave empty to keep current authentication')
                  : ''
              }
              onChange={(event) => set('password', event.target.value)}
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
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='active'>{t('Active')}</SelectItem>
                <SelectItem value='inactive'>{t('Inactive')}</SelectItem>
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
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='none'>{t('No fallback')}</SelectItem>
                <SelectItem value='direct'>
                  {t('Fall back to direct')}
                </SelectItem>
                <SelectItem value='proxy'>
                  {t('Fall back to another proxy')}
                </SelectItem>
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
          <Field>
            <FieldLabel htmlFor='proxy-expires-at'>
              {t('Expiration Time')}
            </FieldLabel>
            <Input
              id='proxy-expires-at'
              type='datetime-local'
              value={draft.expiresAt}
              onChange={(event) => set('expiresAt', event.target.value)}
            />
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
              type='number'
              min={0}
              value={draft.expiryWarnDays}
              onChange={(event) => set('expiryWarnDays', event.target.value)}
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
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const query = useQuery({
    queryKey: ['upstream-proxies'],
    queryFn: listUpstreamProxies,
  })
  const proxies = query.data?.data ?? []
  const [dialogOpen, setDialogOpen] = useState(false)
  const [batchImportOpen, setBatchImportOpen] = useState(false)
  const [batchUpdateOpen, setBatchUpdateOpen] = useState(false)
  const [bulkDeleteOpen, setBulkDeleteOpen] = useState(false)
  const [selectedIds, setSelectedIds] = useState<number[]>([])
  const [selected, setSelected] = useState<UpstreamProxy | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<UpstreamProxy | null>(null)
  const [testingId, setTestingId] = useState<number | null>(null)
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
      <SectionPageLayout.Actions>
        {selectedIds.length > 0 && (
          <>
            <Button variant='outline' onClick={() => setBatchUpdateOpen(true)}>
              <HugeiconsIcon icon={Edit02Icon} strokeWidth={2} />
              {t('Batch update')}
            </Button>
            <Button
              variant='destructive'
              onClick={() => setBulkDeleteOpen(true)}
            >
              <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} />
              {t('Delete selected ({{count}})', { count: selectedIds.length })}
            </Button>
          </>
        )}
        <Button variant='outline' onClick={() => setBatchImportOpen(true)}>
          <HugeiconsIcon icon={FileImportIcon} strokeWidth={2} />
          {t('Batch import')}
        </Button>
        <Button
          onClick={() => {
            setSelected(null)
            setDialogOpen(true)
          }}
        >
          <HugeiconsIcon icon={Add01Icon} strokeWidth={2} />
          {t('Add proxy')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='overflow-hidden rounded-lg border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className='w-10'>
                  <Checkbox
                    aria-label={t('Select all proxies')}
                    checked={
                      proxies.length > 0 &&
                      selectedIds.length === proxies.length
                    }
                    onCheckedChange={(checked) =>
                      setSelectedIds(
                        checked === true ? proxies.map((proxy) => proxy.id) : []
                      )
                    }
                  />
                </TableHead>
                <TableHead>{t('Proxy')}</TableHead>
                <TableHead>{t('Endpoint')}</TableHead>
                <TableHead>{t('Observed IP')}</TableHead>
                <TableHead>{t('Latency')}</TableHead>
                <TableHead>{t('References')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
                <TableHead className='text-right'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {query.isLoading ? (
                <TableRow>
                  <TableCell colSpan={8}>{t('Loading...')}</TableCell>
                </TableRow>
              ) : proxies.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={8}
                    className='text-muted-foreground py-10 text-center'
                  >
                    {t('No proxies')}
                  </TableCell>
                </TableRow>
              ) : (
                proxies.map((proxy) => (
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
                      <Badge variant='outline'>
                        {proxy.protocol.toUpperCase()}
                      </Badge>{' '}
                      {proxy.host}:{proxy.port}
                    </TableCell>
                    <TableCell>
                      {proxy.observed_ip || '-'}
                      {proxy.observed_country && (
                        <div className='text-muted-foreground text-xs'>
                          {[
                            proxy.observed_country,
                            proxy.observed_region,
                            proxy.observed_city,
                          ]
                            .filter(Boolean)
                            .join(' / ')}
                        </div>
                      )}
                    </TableCell>
                    <TableCell>
                      {proxy.latency_ms == null
                        ? '-'
                        : `${proxy.latency_ms} ms`}
                    </TableCell>
                    <TableCell>
                      {t('{{accounts}} accounts, {{pools}} pools', {
                        accounts: proxy.account_count,
                        pools: proxy.pool_count,
                      })}
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant={
                          proxy.status === 'active' ? 'default' : 'secondary'
                        }
                      >
                        {proxy.status === 'active'
                          ? t('Active')
                          : t('Inactive')}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <div className='flex justify-end gap-1'>
                        <Button
                          size='icon-sm'
                          variant='ghost'
                          title={t('Test')}
                          aria-label={t('Test')}
                          disabled={testingId === proxy.id}
                          onClick={() => test(proxy)}
                        >
                          <HugeiconsIcon
                            icon={
                              testingId === proxy.id
                                ? Loading03Icon
                                : TestTubeIcon
                            }
                            className={
                              testingId === proxy.id ? 'animate-spin' : ''
                            }
                            strokeWidth={2}
                          />
                        </Button>
                        <Button
                          size='icon-sm'
                          variant='ghost'
                          title={t('Edit')}
                          aria-label={t('Edit')}
                          onClick={() => {
                            setSelected(proxy)
                            setDialogOpen(true)
                          }}
                        >
                          <HugeiconsIcon icon={Edit02Icon} strokeWidth={2} />
                        </Button>
                        <Button
                          size='icon-sm'
                          variant='destructive'
                          title={t('Delete')}
                          aria-label={t('Delete')}
                          onClick={() => setDeleteTarget(proxy)}
                        >
                          <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} />
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
        <BatchImportDialog
          open={batchImportOpen}
          onOpenChange={setBatchImportOpen}
          title={t('Batch import proxies')}
          description={t(
            'Import up to 100 HTTP, HTTPS, SOCKS5, or SOCKS5H proxies from a JSON array.'
          )}
          example={JSON.stringify(
            [
              {
                name: 'proxy-1',
                protocol: 'http',
                host: '127.0.0.1',
                port: 8080,
                auth: { username: '', password: '' },
                status: 'active',
                fallback_mode: 'none',
                expiry_warn_days: 7,
              },
            ],
            null,
            2
          )}
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
