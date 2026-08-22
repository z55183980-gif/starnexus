/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { useEffect, useState } from 'react'
import { Loading03Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type {
  UpstreamAccountBatchFilters,
  UpstreamAccountPayload,
  UpstreamAccountPool,
  UpstreamProxy,
} from './types'

type BatchScope = 'selected' | 'filtered'
type Unchanged<T extends string> = 'unchanged' | T

export function AccountBatchUpdateDialog({
  open,
  onOpenChange,
  count,
  selectedIds,
  filteredCount,
  filters,
  proxies,
  pools,
  onApply,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  count: number
  selectedIds: number[]
  filteredCount: number
  filters: UpstreamAccountBatchFilters
  proxies: UpstreamProxy[]
  pools: UpstreamAccountPool[]
  onApply: (
    target: { ids: number[] } | { filters: UpstreamAccountBatchFilters },
    patch: Partial<UpstreamAccountPayload>
  ) => Promise<boolean>
}) {
  const { t } = useTranslation()
  const [scope, setScope] = useState<BatchScope>('selected')
  const [status, setStatus] =
    useState<Unchanged<'active' | 'inactive' | 'error'>>('unchanged')
  const [schedulable, setSchedulable] =
    useState<Unchanged<'true' | 'false'>>('unchanged')
  const [oauthRefreshOwner, setOAuthRefreshOwner] =
    useState<Unchanged<'starnexus' | 'external'>>('unchanged')
  const [proxyEnabled, setProxyEnabled] = useState(false)
  const [proxyId, setProxyId] = useState('none')
  const [concurrencyEnabled, setConcurrencyEnabled] = useState(false)
  const [concurrency, setConcurrency] = useState('1')
  const [priorityEnabled, setPriorityEnabled] = useState(false)
  const [priority, setPriority] = useState('50')
  const [weightEnabled, setWeightEnabled] = useState(false)
  const [weight, setWeight] = useState('1')
  const [loadFactorEnabled, setLoadFactorEnabled] = useState(false)
  const [loadFactor, setLoadFactor] = useState('')
  const [rateMultiplierEnabled, setRateMultiplierEnabled] = useState(false)
  const [rateMultiplier, setRateMultiplier] = useState('1')
  const [notesEnabled, setNotesEnabled] = useState(false)
  const [notes, setNotes] = useState('')
  const [poolIdsEnabled, setPoolIdsEnabled] = useState(false)
  const [poolIds, setPoolIds] = useState<number[]>([])
  const [baseUrlEnabled, setBaseUrlEnabled] = useState(false)
  const [baseUrl, setBaseUrl] = useState('')
  const [interceptWarmupEnabled, setInterceptWarmupEnabled] = useState(false)
  const [interceptWarmup, setInterceptWarmup] = useState(false)
  const [compactModeEnabled, setCompactModeEnabled] = useState(false)
  const [compactMode, setCompactMode] = useState<
    'auto' | 'force_on' | 'force_off'
  >('auto')
  const [busy, setBusy] = useState(false)

  const reset = () => {
    setScope('selected')
    setStatus('unchanged')
    setSchedulable('unchanged')
    setOAuthRefreshOwner('unchanged')
    setProxyEnabled(false)
    setProxyId('none')
    setConcurrencyEnabled(false)
    setConcurrency('1')
    setPriorityEnabled(false)
    setPriority('50')
    setWeightEnabled(false)
    setWeight('1')
    setLoadFactorEnabled(false)
    setLoadFactor('')
    setRateMultiplierEnabled(false)
    setRateMultiplier('1')
    setNotesEnabled(false)
    setNotes('')
    setPoolIdsEnabled(false)
    setPoolIds([])
    setBaseUrlEnabled(false)
    setBaseUrl('')
    setInterceptWarmupEnabled(false)
    setInterceptWarmup(false)
    setCompactModeEnabled(false)
    setCompactMode('auto')
  }

  useEffect(() => {
    if (!open) reset()
  }, [open])

  const parseNumber = (value: string, label: string, minimum: number) => {
    const parsed = Number(value)
    if (!Number.isFinite(parsed) || parsed < minimum) {
      toast.error(
        t('{{field}} must be at least {{minimum}}', { field: label, minimum })
      )
      return null
    }
    return parsed
  }

  const submit = async () => {
    const patch: Partial<UpstreamAccountPayload> = {}
    if (status !== 'unchanged') patch.status = status
    if (schedulable !== 'unchanged') patch.schedulable = schedulable === 'true'
    if (oauthRefreshOwner !== 'unchanged')
      patch.oauth_refresh_owner = oauthRefreshOwner
    if (proxyEnabled)
      patch.proxy_id = proxyId === 'none' ? null : Number(proxyId)
    if (concurrencyEnabled) {
      const value = parseNumber(concurrency, t('Concurrency'), 1)
      if (value === null) return
      patch.concurrency = value
    }
    if (priorityEnabled) {
      const value = parseNumber(priority, t('Priority'), 0)
      if (value === null) return
      patch.priority = value
    }
    if (weightEnabled) {
      const value = parseNumber(weight, t('Weight'), 1)
      if (value === null) return
      patch.weight = value
    }
    if (loadFactorEnabled) {
      const value =
        loadFactor.trim() === ''
          ? null
          : parseNumber(loadFactor, t('Load factor'), 1)
      if (loadFactor.trim() !== '' && value === null) return
      patch.load_factor = value
    }
    if (rateMultiplierEnabled) {
      const value = Number(rateMultiplier)
      if (!Number.isFinite(value) || value < 0) {
        toast.error(t('Rate multiplier must be 0 or greater'))
        return
      }
      patch.rate_multiplier = value
    }
    if (notesEnabled) patch.notes = notes
    if (poolIdsEnabled) patch.pool_ids = poolIds

    const credentialPatch: Record<string, unknown> = {}
    const extraPatch: Record<string, unknown> = {}
    if (baseUrlEnabled) credentialPatch.base_url = baseUrl.trim() || null
    if (interceptWarmupEnabled)
      credentialPatch.intercept_warmup_requests = interceptWarmup
    if (compactModeEnabled) extraPatch.openai_compact_mode = compactMode
    if (Object.keys(credentialPatch).length > 0)
      patch.credential_patch = credentialPatch
    if (Object.keys(extraPatch).length > 0) patch.extra_patch = extraPatch

    if (Object.keys(patch).length === 0) {
      toast.error(t('Select at least one field to update'))
      return
    }
    if (scope === 'filtered' && filteredCount === 0) {
      toast.error(t('No accounts match the current filters'))
      return
    }
    if (
      scope === 'filtered' &&
      !window.confirm(
        `${t('Are you sure?')} ${t(
          'Update {{count}} accounts matching the current filters',
          { count: filteredCount }
        )}`
      )
    ) {
      return
    }

    setBusy(true)
    try {
      const applied = await onApply(
        scope === 'filtered' ? { filters } : { ids: selectedIds },
        patch
      )
      if (applied) onOpenChange(false)
    } finally {
      setBusy(false)
    }
  }

  const togglePool = (id: number, checked: boolean) => {
    setPoolIds((current) =>
      checked
        ? [...new Set([...current, id])]
        : current.filter((value) => value !== id)
    )
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{t('Batch update accounts')}</DialogTitle>
          <DialogDescription>
            {scope === 'filtered'
              ? t('Update {{count}} accounts matching the current filters', {
                  count: filteredCount,
                })
              : t('Update {{count}} selected accounts', { count })}
          </DialogDescription>
        </DialogHeader>

        <FieldGroup>
          <Field>
            <FieldLabel>{t('Update scope')}</FieldLabel>
            <Select
              value={scope}
              onValueChange={(value) => setScope(value as BatchScope)}
            >
              <SelectTrigger className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value='selected'>
                    {t('Selected accounts ({{count}})', { count })}
                  </SelectItem>
                  <SelectItem value='filtered' disabled={filteredCount === 0}>
                    {t('All matching accounts ({{count}})', {
                      count: filteredCount,
                    })}
                  </SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>

          <div className='grid gap-4 md:grid-cols-2'>
            <Field>
              <FieldLabel>{t('Status')}</FieldLabel>
              <Select
                value={status}
                onValueChange={(value) => setStatus(value as typeof status)}
              >
                <SelectTrigger className='w-full'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value='unchanged'>
                      {t('Keep unchanged')}
                    </SelectItem>
                    <SelectItem value='active'>{t('Active')}</SelectItem>
                    <SelectItem value='inactive'>{t('Inactive')}</SelectItem>
                    <SelectItem value='error'>{t('Error')}</SelectItem>
                  </SelectGroup>
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
                <SelectTrigger className='w-full'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value='unchanged'>
                      {t('Keep unchanged')}
                    </SelectItem>
                    <SelectItem value='true'>{t('Enabled')}</SelectItem>
                    <SelectItem value='false'>{t('Disabled')}</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
          </div>

          <Field>
            <FieldLabel>{t('OAuth refresh owner')}</FieldLabel>
            <Select
              value={oauthRefreshOwner}
              onValueChange={(value) =>
                setOAuthRefreshOwner(value as typeof oauthRefreshOwner)
              }
            >
              <SelectTrigger className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value='unchanged'>
                    {t('Keep unchanged')}
                  </SelectItem>
                  <SelectItem value='starnexus'>{t('StarNexus')}</SelectItem>
                  <SelectItem value='external'>
                    {t('External system')}
                  </SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>

          <div className='grid gap-4 md:grid-cols-2'>
            <Field className='rounded-lg border p-3'>
              <FieldLabel className='flex items-center justify-between'>
                <span>{t('Proxy')}</span>
                <Checkbox
                  checked={proxyEnabled}
                  onCheckedChange={(checked) =>
                    setProxyEnabled(checked === true)
                  }
                />
              </FieldLabel>
              {proxyEnabled && (
                <Select value={proxyId} onValueChange={setProxyId}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value='none'>{t('No proxy')}</SelectItem>
                      {proxies.map((proxy) => (
                        <SelectItem key={proxy.id} value={String(proxy.id)}>
                          {proxy.name}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              )}
            </Field>
            <Field className='rounded-lg border p-3'>
              <FieldLabel className='flex items-center justify-between'>
                <span>{t('Concurrency')}</span>
                <Checkbox
                  checked={concurrencyEnabled}
                  onCheckedChange={(checked) =>
                    setConcurrencyEnabled(checked === true)
                  }
                />
              </FieldLabel>
              <Input
                disabled={!concurrencyEnabled}
                type='number'
                min={1}
                value={concurrency}
                onChange={(event) => setConcurrency(event.target.value)}
              />
            </Field>
            <Field className='rounded-lg border p-3'>
              <FieldLabel className='flex items-center justify-between'>
                <span>{t('Priority')}</span>
                <Checkbox
                  checked={priorityEnabled}
                  onCheckedChange={(checked) =>
                    setPriorityEnabled(checked === true)
                  }
                />
              </FieldLabel>
              <Input
                disabled={!priorityEnabled}
                type='number'
                min={0}
                value={priority}
                onChange={(event) => setPriority(event.target.value)}
              />
            </Field>
            <Field className='rounded-lg border p-3'>
              <FieldLabel className='flex items-center justify-between'>
                <span>{t('Weight')}</span>
                <Checkbox
                  checked={weightEnabled}
                  onCheckedChange={(checked) =>
                    setWeightEnabled(checked === true)
                  }
                />
              </FieldLabel>
              <Input
                disabled={!weightEnabled}
                type='number'
                min={1}
                value={weight}
                onChange={(event) => setWeight(event.target.value)}
              />
            </Field>
            <Field className='rounded-lg border p-3'>
              <FieldLabel className='flex items-center justify-between'>
                <span>{t('Load factor')}</span>
                <Checkbox
                  checked={loadFactorEnabled}
                  onCheckedChange={(checked) =>
                    setLoadFactorEnabled(checked === true)
                  }
                />
              </FieldLabel>
              <Input
                disabled={!loadFactorEnabled}
                type='number'
                min={1}
                placeholder={t('Leave empty to clear')}
                value={loadFactor}
                onChange={(event) => setLoadFactor(event.target.value)}
              />
            </Field>
            <Field className='rounded-lg border p-3'>
              <FieldLabel className='flex items-center justify-between'>
                <span>{t('Rate multiplier')}</span>
                <Checkbox
                  checked={rateMultiplierEnabled}
                  onCheckedChange={(checked) =>
                    setRateMultiplierEnabled(checked === true)
                  }
                />
              </FieldLabel>
              <Input
                disabled={!rateMultiplierEnabled}
                type='number'
                min={0}
                step='0.01'
                value={rateMultiplier}
                onChange={(event) => setRateMultiplier(event.target.value)}
              />
            </Field>
            <Field className='rounded-lg border p-3'>
              <FieldLabel className='flex items-center justify-between'>
                <span>{t('Notes')}</span>
                <Checkbox
                  checked={notesEnabled}
                  onCheckedChange={(checked) =>
                    setNotesEnabled(checked === true)
                  }
                />
              </FieldLabel>
              <Input
                disabled={!notesEnabled}
                value={notes}
                placeholder={t('Empty value clears notes')}
                onChange={(event) => setNotes(event.target.value)}
              />
            </Field>
            <Field className='rounded-lg border p-3'>
              <FieldLabel className='flex items-center justify-between'>
                <span>{t('Account pools')}</span>
                <Checkbox
                  checked={poolIdsEnabled}
                  onCheckedChange={(checked) =>
                    setPoolIdsEnabled(checked === true)
                  }
                />
              </FieldLabel>
              {poolIdsEnabled && (
                <div className='grid gap-2 sm:grid-cols-2'>
                  {pools.map((pool) => (
                    <label
                      key={pool.id}
                      className='flex items-center gap-2 text-sm'
                    >
                      <Checkbox
                        checked={poolIds.includes(pool.id)}
                        onCheckedChange={(checked) =>
                          togglePool(pool.id, checked === true)
                        }
                      />
                      <span className='truncate'>{pool.name}</span>
                    </label>
                  ))}
                </div>
              )}
            </Field>
          </div>

          <div className='rounded-lg border p-3'>
            <p className='mb-3 text-sm font-medium'>
              {t('Credential-backed settings')}
            </p>
            <div className='grid gap-4 md:grid-cols-2'>
              <Field>
                <FieldLabel className='flex items-center justify-between'>
                  <span>{t('Base URL')}</span>
                  <Checkbox
                    checked={baseUrlEnabled}
                    onCheckedChange={(checked) =>
                      setBaseUrlEnabled(checked === true)
                    }
                  />
                </FieldLabel>
                <Input
                  disabled={!baseUrlEnabled}
                  value={baseUrl}
                  placeholder='https://api.example.com'
                  onChange={(event) => setBaseUrl(event.target.value)}
                />
              </Field>
              <Field>
                <FieldLabel className='flex items-center justify-between'>
                  <span>{t('Intercept warmup requests')}</span>
                  <Checkbox
                    checked={interceptWarmupEnabled}
                    onCheckedChange={(checked) =>
                      setInterceptWarmupEnabled(checked === true)
                    }
                  />
                </FieldLabel>
                <Select
                  disabled={!interceptWarmupEnabled}
                  value={interceptWarmup ? 'true' : 'false'}
                  onValueChange={(value) =>
                    setInterceptWarmup(value === 'true')
                  }
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value='true'>{t('Enabled')}</SelectItem>
                      <SelectItem value='false'>{t('Disabled')}</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
              <Field>
                <FieldLabel className='flex items-center justify-between'>
                  <span>{t('Compact mode')}</span>
                  <Checkbox
                    checked={compactModeEnabled}
                    onCheckedChange={(checked) =>
                      setCompactModeEnabled(checked === true)
                    }
                  />
                </FieldLabel>
                <Select
                  disabled={!compactModeEnabled}
                  value={compactMode}
                  onValueChange={(value) =>
                    setCompactMode(value as typeof compactMode)
                  }
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value='auto'>{t('Automatic')}</SelectItem>
                      <SelectItem value='force_on'>{t('Force on')}</SelectItem>
                      <SelectItem value='force_off'>
                        {t('Force off')}
                      </SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
            </div>
          </div>
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
