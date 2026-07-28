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
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Textarea } from '@/components/ui/textarea'

export function BatchImportDialog({
  open,
  onOpenChange,
  title,
  description,
  example,
  onImport,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description: string
  example: string
  onImport: (items: unknown[]) => Promise<void>
}) {
  const { t } = useTranslation()
  const [value, setValue] = useState(example)
  const [busy, setBusy] = useState(false)
  const submit = async () => {
    try {
      const parsed = JSON.parse(value) as unknown
      if (
        !Array.isArray(parsed) ||
        parsed.length === 0 ||
        parsed.length > 100
      ) {
        throw new Error(t('Enter a JSON array containing 1 to 100 items'))
      }
      setBusy(true)
      await onImport(parsed)
      onOpenChange(false)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Invalid JSON'))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor='batch-import-json'>{t('JSON data')}</FieldLabel>
          <Textarea
            id='batch-import-json'
            className='min-h-80 font-mono text-xs'
            spellCheck={false}
            value={value}
            onChange={(event) => setValue(event.target.value)}
          />
          <FieldDescription>
            {t(
              'Sensitive credentials are encrypted by the server and are never returned by the API.'
            )}
          </FieldDescription>
        </Field>
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
            {t('Import')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
