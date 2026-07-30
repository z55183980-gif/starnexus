/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useEffect, useMemo, useState } from 'react'
import {
  Alert02Icon,
  CheckmarkCircle02Icon,
  Loading03Icon,
  PlayIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
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
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  anthropicAccountModels,
  openAIAccountModels,
} from './account-model-restriction'
import { testUpstreamAccount } from './api'
import type { UpstreamAccount, UpstreamAccountTestResult } from './types'

type TestMode = 'default' | 'compact'
type TestState = 'idle' | 'testing' | 'success' | 'error'

const accountStatusLabels = {
  active: 'Active',
  inactive: 'Inactive',
  error: 'Error',
  expired: 'Expired',
} as const

function parseExtraModels(extra: string) {
  try {
    const value = JSON.parse(extra) as Record<string, unknown>
    const result: string[] = []
    for (const key of ['test_model', 'model']) {
      if (typeof value[key] === 'string') result.push(value[key])
    }
    for (const key of ['supported_models', 'models']) {
      if (Array.isArray(value[key])) {
        result.push(...value[key].filter((item): item is string => typeof item === 'string'))
      }
    }
    return result
  } catch {
    return []
  }
}

function accountTestModels(account: UpstreamAccount) {
  const curated =
    account.platform === 'openai' ? openAIAccountModels : anthropicAccountModels
  const mappings = [
    ...Object.entries(account.metadata.model_mapping || {}).flat(),
    ...Object.entries(account.metadata.compact_model_mapping || {}).flat(),
  ]
  return Array.from(
    new Set(
      [...parseExtraModels(account.extra), ...mappings, ...curated]
        .map((model) => model.trim())
        .filter((model) => model && !model.includes('*'))
    )
  )
}

function defaultTestModel(account: UpstreamAccount, models: string[]) {
  const preferred =
    account.platform === 'openai'
      ? ['gpt-5.4', 'codex-auto-review']
      : ['claude-sonnet-4-5-20250929']
  return preferred.find((model) => models.includes(model)) || models[0] || ''
}

function resultLines(
  result: UpstreamAccountTestResult,
  translate: (key: string, options?: Record<string, unknown>) => string
) {
  const lines = [
    translate('Result: {{result}}', { result: result.result }),
    translate('Model: {{model}}', { model: result.model || '-' }),
    translate('Protocol: {{protocol}}', { protocol: result.protocol || '-' }),
    translate('HTTP status: {{status}}', { status: result.status_code || '-' }),
    translate('Total latency: {{latency}} ms', { latency: result.latency_ms }),
  ]
  if (result.first_output_latency_ms > 0) {
    lines.push(
      translate('First output latency: {{latency}} ms', {
        latency: result.first_output_latency_ms,
      })
    )
  }
  if (result.terminal_type) {
    lines.push(
      translate('Terminal event: {{event}}', { event: result.terminal_type })
    )
  }
  if (result.event_types?.length) {
    lines.push(
      translate('Events: {{events}}', { events: result.event_types.join(', ') })
    )
  }
  if (result.http_version) {
    lines.push(
      translate('HTTP version: {{version}}', { version: result.http_version })
    )
  }
  return lines
}

export function AccountTestDialog({
  account,
  open,
  onOpenChange,
  onTested,
}: {
  account: UpstreamAccount | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onTested: () => void
}) {
  const { t } = useTranslation()
  const models = useMemo(
    () => (account ? accountTestModels(account) : []),
    [account]
  )
  const [model, setModel] = useState('')
  const [mode, setMode] = useState<TestMode>('default')
  const [state, setState] = useState<TestState>('idle')
  const [lines, setLines] = useState<string[]>([])

  useEffect(() => {
    if (!account || !open) return
    setModel(defaultTestModel(account, models))
    setMode('default')
    setState('idle')
    setLines([t('Ready to test. Click Start test to begin.')])
  }, [account, models, open, t])

  if (!account) return null

  const startTest = async () => {
    if (!model || state === 'testing') return
    setState('testing')
    setLines([
      t('Starting account test...'),
      t('Model: {{model}}', { model }),
      t('Test mode: {{mode}}', {
        mode: mode === 'compact' ? t('Compact request') : t('Regular request'),
      }),
    ])
    try {
      const response = await testUpstreamAccount(account.id, model, mode)
      if (!response.success || !response.data) {
        throw new Error(response.message || t('Request failed'))
      }
      const result = response.data
      const succeeded = result.success
      setState(succeeded ? 'success' : 'error')
      setLines((current) => [
        ...current,
        succeeded ? t('Request completed') : t('Request failed'),
        ...resultLines(result, t),
      ])
      onTested()
    } catch (error) {
      setState('error')
      setLines((current) => [
        ...current,
        t('Request failed'),
        error instanceof Error ? error.message : t('Request failed'),
      ])
      onTested()
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{t('Test account connection')}</DialogTitle>
          <DialogDescription>
            {t('Choose the model and request mode used for this account probe.')}
          </DialogDescription>
        </DialogHeader>

        <div className='flex items-center gap-3 rounded-xl border bg-muted/30 p-3'>
          <div className='bg-primary text-primary-foreground flex size-10 shrink-0 items-center justify-center rounded-lg'>
            <HugeiconsIcon icon={PlayIcon} strokeWidth={2} />
          </div>
          <div className='min-w-0 flex-1'>
            <div className='truncate font-medium'>
              {account.metadata.email || account.name}
            </div>
            <div className='text-muted-foreground text-xs'>
              #{account.id} · {account.platform.toUpperCase()} ·{' '}
              {account.type.toUpperCase()}
            </div>
          </div>
          <Badge
            variant={
              account.status === 'active'
                ? 'success'
                : account.status === 'error' || account.status === 'expired'
                  ? 'destructive'
                  : 'secondary'
            }
          >
            {t(accountStatusLabels[account.status])}
          </Badge>
        </div>

        <FieldGroup>
          <Field>
            <FieldLabel>{t('Select test model')}</FieldLabel>
            <Select
              items={Object.fromEntries(models.map((item) => [item, item]))}
              value={model}
              onValueChange={(value) => value && setModel(value)}
              disabled={state === 'testing'}
            >
              <SelectTrigger className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {models.map((item) => (
                    <SelectItem key={item} value={item}>
                      {item}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>

          <Field>
            <FieldLabel>{t('Test mode')}</FieldLabel>
            <Select
              items={
                account.platform === 'openai'
                  ? {
                      default: t('Regular request'),
                      compact: t('Compact request'),
                    }
                  : { default: t('Regular request') }
              }
              value={mode}
              onValueChange={(value) => value && setMode(value as TestMode)}
              disabled={state === 'testing'}
            >
              <SelectTrigger className='w-full'>
                <SelectValue>
                  {mode === 'compact'
                    ? t('Compact request')
                    : t('Regular request')}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value='default'>{t('Regular request')}</SelectItem>
                  {account.platform === 'openai' && (
                    <SelectItem value='compact'>
                      {t('Compact request')}
                    </SelectItem>
                  )}
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
        </FieldGroup>

        <ScrollArea className='h-44 rounded-xl border bg-foreground text-background'>
          <div className='flex flex-col gap-1 p-3 font-mono text-xs leading-relaxed'>
            {lines.map((line, index) => (
              <div key={`${index}-${line}`} className='flex gap-2'>
                <span aria-hidden='true' className='opacity-60'>
                  &gt;
                </span>
                <span className='break-all'>{line}</span>
              </div>
            ))}
          </div>
        </ScrollArea>

        <div className='text-muted-foreground flex items-center gap-2 text-xs'>
          {state === 'testing' && (
            <HugeiconsIcon icon={Loading03Icon} className='animate-spin' strokeWidth={2} />
          )}
          {state === 'success' && (
            <HugeiconsIcon icon={CheckmarkCircle02Icon} className='text-success' strokeWidth={2} />
          )}
          {state === 'error' && (
            <HugeiconsIcon icon={Alert02Icon} className='text-destructive' strokeWidth={2} />
          )}
          <span>{t('Test model')}: {model || '-'}</span>
        </div>

        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Close')}
          </Button>
          <Button disabled={!model || state === 'testing'} onClick={startTest}>
            <HugeiconsIcon
              data-icon='inline-start'
              icon={state === 'testing' ? Loading03Icon : PlayIcon}
              className={state === 'testing' ? 'animate-spin' : undefined}
              strokeWidth={2}
            />
            {state === 'testing' ? t('Testing') : t('Start test')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
