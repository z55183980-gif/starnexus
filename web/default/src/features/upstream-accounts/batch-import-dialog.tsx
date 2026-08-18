/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.
*/
import { useEffect, useRef, useState } from 'react'
import { FileUploadIcon, Loading03Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { extractImportItems, type ImportCollectionKey } from './batch-import'

const MAX_IMPORT_FILE_SIZE = 5 * 1024 * 1024
const MAX_IMPORT_ITEMS = 100

export function BatchImportDialog({
  open,
  onOpenChange,
  title,
  description,
  collectionKey,
  onImport,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description: string
  collectionKey: ImportCollectionKey
  onImport: (
    items: unknown[],
    documents: unknown[],
    defaultConfig?: string
  ) => Promise<void>
}) {
  const { t } = useTranslation()
  const [busy, setBusy] = useState(false)
  const [files, setFiles] = useState<File[]>([])
  // Keep the first built-in account preset selected so imported credentials
  // can be used immediately without an additional manual edit.
  const [defaultConfig, setDefaultConfig] = useState('team')
  const [dragDepth, setDragDepth] = useState(0)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const dragActive = dragDepth > 0
  const fileNames = files.map((file) => file.name).join(', ')

  useEffect(() => {
    if (!open) {
      setFiles([])
      setDefaultConfig('team')
      setDragDepth(0)
      setBusy(false)
      if (fileInputRef.current) fileInputRef.current.value = ''
    }
  }, [open])

  const handleOpenChange = (nextOpen: boolean) => {
    if (busy && !nextOpen) return
    onOpenChange(nextOpen)
  }

  const isJsonFile = (file: File) => {
    return (
      file.name.toLowerCase().endsWith('.json') ||
      file.type === 'application/json'
    )
  }

  const selectFiles = (sourceFiles?: FileList | File[] | null) => {
    if (busy) return
    const incoming = Array.from(sourceFiles ?? [])
    const jsonFiles = incoming.filter(isJsonFile)

    if (jsonFiles.length === 0) {
      toast.error(t('Choose a JSON file'))
      return
    }
    if (jsonFiles.some((file) => file.size > MAX_IMPORT_FILE_SIZE)) {
      toast.error(t('The import file must not exceed 5 MB'))
      return
    }

    setFiles(jsonFiles)
  }

  const submit = async () => {
    if (files.length === 0) {
      toast.error(t('Choose a JSON file'))
      return
    }

    setBusy(true)
    try {
      const items: unknown[] = []
      const documents: unknown[] = []
      for (const file of files) {
        let parsed: unknown
        try {
          parsed = JSON.parse(await file.text()) as unknown
        } catch {
          throw new Error(`${file.name}: ${t('Invalid JSON')}`)
        }

        const fileItems = extractImportItems(parsed, collectionKey)
        if (!fileItems) {
          throw new Error(`${file.name}: ${t('Expected a JSON array.')}`)
        }
        documents.push(parsed)
        items.push(...fileItems)
      }

      if (items.length === 0 || items.length > MAX_IMPORT_ITEMS) {
        throw new Error(t('Enter a JSON array containing 1 to 100 items'))
      }

      await onImport(
        items,
        documents,
        collectionKey === 'accounts' && defaultConfig !== 'none'
          ? defaultConfig
          : undefined
      )
      onOpenChange(false)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Invalid JSON'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className='flex max-h-[calc(100svh-2rem)] min-w-0 flex-col overflow-hidden sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <Field className='min-w-0'>
          <FieldLabel>{t('Import file')}</FieldLabel>
          <div
            className={cn(
              'bg-muted/30 flex min-w-0 items-center justify-between gap-3 rounded-lg border border-dashed p-4 transition-colors',
              dragActive && 'border-primary bg-primary/5'
            )}
            onDragEnter={(event) => {
              event.preventDefault()
              setDragDepth((current) => current + 1)
            }}
            onDragOver={(event) => event.preventDefault()}
            onDragLeave={(event) => {
              event.preventDefault()
              setDragDepth((current) => Math.max(0, current - 1))
            }}
            onDrop={(event) => {
              event.preventDefault()
              setDragDepth(0)
              selectFiles(event.dataTransfer.files)
            }}
          >
            <span className='min-w-0 flex-1'>
              <span
                className='block truncate text-sm font-medium'
                title={fileNames || undefined}
              >
                {fileNames || t('Choose a JSON file or drag it here')}
              </span>
              <span className='text-muted-foreground block text-xs'>
                JSON (.json)
              </span>
            </span>
            <Button
              type='button'
              variant='outline'
              className='shrink-0'
              disabled={busy}
              onClick={() => fileInputRef.current?.click()}
            >
              <HugeiconsIcon
                data-icon='inline-start'
                icon={FileUploadIcon}
                strokeWidth={2}
              />
              {t('Choose a JSON file')}
            </Button>
          </div>
          <input
            ref={fileInputRef}
            type='file'
            accept='application/json,.json'
            multiple
            className='hidden'
            onChange={(event) => {
              selectFiles(event.target.files)
              event.target.value = ''
            }}
          />
          <FieldDescription>
            {t(
              'Sensitive credentials are encrypted by the server and are never returned by the API.'
            )}
          </FieldDescription>
        </Field>
        {collectionKey === 'accounts' && (
          <Field className='min-w-0'>
            <FieldLabel htmlFor='account-import-default-config'>
              {t('Default configuration')}
            </FieldLabel>
            <Select
              value={defaultConfig}
              onValueChange={(value) => {
                if (value !== null) setDefaultConfig(value)
              }}
              disabled={busy}
            >
              <SelectTrigger id='account-import-default-config'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='team'>team</SelectItem>
                <SelectItem value='none'>
                  {t('No default configuration')}
                </SelectItem>
              </SelectContent>
            </Select>
            <FieldDescription>
              {t(
                'Apply the selected configuration to imported accounts automatically.'
              )}
            </FieldDescription>
          </Field>
        )}
        <DialogFooter>
          <Button
            variant='outline'
            disabled={busy}
            onClick={() => onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button onClick={submit} disabled={busy}>
            {busy && (
              <HugeiconsIcon
                data-icon='inline-start'
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
