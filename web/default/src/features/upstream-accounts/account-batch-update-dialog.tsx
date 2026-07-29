/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useState } from 'react'
import { Loading03Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { UpstreamAccountPayload } from './types'

export function AccountBatchUpdateDialog({
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
  const [oauthRefreshOwner, setOAuthRefreshOwner] = useState<
    'unchanged' | 'starnexus' | 'external'
  >('unchanged')
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    const patch: Partial<UpstreamAccountPayload> = {}
    if (status !== 'unchanged') patch.status = status
    if (schedulable !== 'unchanged') patch.schedulable = schedulable === 'true'
    if (oauthRefreshOwner !== 'unchanged') {
      patch.oauth_refresh_owner = oauthRefreshOwner
    }
    if (Object.keys(patch).length === 0) {
      toast.error(t('Select at least one field to update'))
      return
    }
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
