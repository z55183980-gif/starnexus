/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useEffect, useState } from 'react'
import { Loading03Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
  previewUpstreamAccountsFromCRS,
  syncUpstreamAccountsFromCRS,
} from './api'
import type { CRSPreviewResult } from './types'

type CRSDraft = {
  base_url: string
  username: string
  password: string
  sync_proxies: boolean
}

const emptyDraft: CRSDraft = {
  base_url: '',
  username: '',
  password: '',
  sync_proxies: false,
}

export function CRSSyncDialog({
  open,
  onOpenChange,
  onSynced,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSynced: () => void
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<CRSDraft>(emptyDraft)
  const [preview, setPreview] = useState<CRSPreviewResult | null>(null)
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!open) {
      setDraft(emptyDraft)
      setPreview(null)
      setSelectedIds([])
      setBusy(false)
    }
  }, [open])

  const set = <K extends keyof CRSDraft>(key: K, value: CRSDraft[K]) =>
    setDraft((current) => ({ ...current, [key]: value }))

  const loadPreview = async () => {
    if (!draft.base_url.trim() || !draft.username.trim() || !draft.password) {
      toast.error(t('Enter the CRS service URL, username, and password'))
      return
    }
    setBusy(true)
    try {
      const response = await previewUpstreamAccountsFromCRS({
        ...draft,
        base_url: draft.base_url.trim(),
        username: draft.username.trim(),
      })
      if (!response.success || !response.data) {
        throw new Error(response.message || t('CRS preview failed'))
      }
      setPreview(response.data)
      setSelectedIds(
        response.data.new_accounts.map((account) => account.crs_account_id)
      )
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('CRS preview failed')
      )
    } finally {
      setBusy(false)
    }
  }

  const sync = async () => {
    setBusy(true)
    try {
      const response = await syncUpstreamAccountsFromCRS({
        ...draft,
        base_url: draft.base_url.trim(),
        username: draft.username.trim(),
        selected_account_ids: selectedIds,
      })
      if (!response.success || !response.data) {
        throw new Error(response.message || t('CRS sync failed'))
      }
      toast.success(
        t(
          'CRS sync completed: {{created}} created, {{updated}} updated, {{failed}} failed',
          {
            created: response.data.created,
            updated: response.data.updated,
            failed: response.data.failed,
          }
        )
      )
      onSynced()
      onOpenChange(false)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('CRS sync failed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{t('Sync accounts from CRS')}</DialogTitle>
          <DialogDescription>
            {t(
              'Connect to claude-relay-service from the server, preview its accounts, then choose which new accounts to create.'
            )}
          </DialogDescription>
        </DialogHeader>

        {!preview ? (
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor='crs-base-url'>
                {t('CRS service URL')}
              </FieldLabel>
              <Input
                id='crs-base-url'
                value={draft.base_url}
                placeholder='http://127.0.0.1:3000'
                onChange={(event) => set('base_url', event.target.value)}
              />
              <FieldDescription>
                {t(
                  'The address is requested by the StarNexus server and is checked by the configured SSRF protection.'
                )}
              </FieldDescription>
            </Field>
            <div className='grid gap-4 sm:grid-cols-2'>
              <Field>
                <FieldLabel htmlFor='crs-username'>{t('Username')}</FieldLabel>
                <Input
                  id='crs-username'
                  autoComplete='username'
                  value={draft.username}
                  onChange={(event) => set('username', event.target.value)}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor='crs-password'>{t('Password')}</FieldLabel>
                <Input
                  id='crs-password'
                  type='password'
                  autoComplete='current-password'
                  value={draft.password}
                  onChange={(event) => set('password', event.target.value)}
                />
              </Field>
            </div>
            <label className='flex items-start gap-3 rounded-lg border p-3'>
              <Checkbox
                checked={draft.sync_proxies}
                onCheckedChange={(checked) =>
                  set('sync_proxies', checked === true)
                }
              />
              <span className='space-y-1'>
                <span className='block text-sm font-medium'>
                  {t('Sync proxies')}
                </span>
                <span className='text-muted-foreground block text-sm'>
                  {t(
                    'Create matching StarNexus proxies for proxy settings returned by CRS.'
                  )}
                </span>
              </span>
            </label>
          </FieldGroup>
        ) : (
          <div className='space-y-4'>
            <div className='rounded-lg border p-3'>
              <div className='flex items-center justify-between gap-2'>
                <p className='text-sm font-medium'>
                  {t('Existing accounts to update')}
                </p>
                <Badge variant='secondary'>
                  {preview.existing_accounts.length}
                </Badge>
              </div>
              <p className='text-muted-foreground mt-1 text-sm'>
                {t(
                  'Existing CRS accounts are updated automatically; fields not returned by CRS keep their current values.'
                )}
              </p>
            </div>

            <div className='rounded-lg border'>
              <div className='flex items-center justify-between gap-2 border-b p-3'>
                <div>
                  <p className='text-sm font-medium'>
                    {t('New accounts to create')}
                  </p>
                  <p className='text-muted-foreground text-xs'>
                    {t('{{count}} selected', { count: selectedIds.length })}
                  </p>
                </div>
                <div className='flex gap-2'>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() =>
                      setSelectedIds(
                        preview.new_accounts.map(
                          (account) => account.crs_account_id
                        )
                      )
                    }
                  >
                    {t('Select all')}
                  </Button>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => setSelectedIds([])}
                  >
                    {t('Select none')}
                  </Button>
                </div>
              </div>
              <div className='max-h-64 overflow-y-auto p-2'>
                {preview.new_accounts.length === 0 ? (
                  <p className='text-muted-foreground p-3 text-center text-sm'>
                    {t('All CRS accounts are already synchronized')}
                  </p>
                ) : (
                  preview.new_accounts.map((account) => (
                    <label
                      key={account.crs_account_id}
                      className='hover:bg-muted flex items-center gap-3 rounded-md p-2'
                    >
                      <Checkbox
                        checked={selectedIds.includes(account.crs_account_id)}
                        onCheckedChange={(checked) =>
                          setSelectedIds((current) =>
                            checked === true
                              ? [...current, account.crs_account_id]
                              : current.filter(
                                  (id) => id !== account.crs_account_id
                                )
                          )
                        }
                      />
                      <span className='min-w-0 flex-1'>
                        <span className='block truncate text-sm font-medium'>
                          {account.name}
                        </span>
                        <span className='text-muted-foreground block truncate text-xs'>
                          {account.kind} · {account.platform} · {account.type}
                        </span>
                      </span>
                    </label>
                  ))
                )}
              </div>
            </div>

            {preview.skipped > 0 && (
              <p className='text-muted-foreground text-sm'>
                {t(
                  '{{count}} unsupported CRS accounts were skipped because their platform is not available in StarNexus.',
                  { count: preview.skipped }
                )}
              </p>
            )}
          </div>
        )}

        <DialogFooter>
          <Button
            variant='outline'
            onClick={() => (preview ? setPreview(null) : onOpenChange(false))}
            disabled={busy}
          >
            {preview ? t('Back') : t('Cancel')}
          </Button>
          <Button disabled={busy} onClick={preview ? sync : loadPreview}>
            {busy && (
              <HugeiconsIcon
                data-icon='inline-start'
                icon={Loading03Icon}
                className='animate-spin'
                strokeWidth={2}
              />
            )}
            {preview ? t('Sync selected accounts') : t('Preview')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
