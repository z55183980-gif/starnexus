/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useMemo, useState } from 'react'
import {
  Add01Icon,
  ArrowDown01Icon,
  ArrowRight01Icon,
  Cancel01Icon,
  Delete02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { getLobeIcon } from '@/lib/lobe-icon'
import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import { FieldDescription } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import type { UpstreamPlatform } from './types'

export type AccountModelRestrictionMode = 'whitelist' | 'mapping'
export type AccountModelMapping = { from: string; to: string }

const openAIModels = [
  'gpt-5.2',
  'gpt-5.2-2025-12-11',
  'gpt-5.2-chat-latest',
  'gpt-5.2-pro',
  'gpt-5.2-pro-2025-12-11',
  'gpt-5.6',
  'gpt-5.6-sol',
  'gpt-5.6-terra',
  'gpt-5.6-luna',
  'gpt-5.5',
  'gpt-5.4',
  'gpt-5.4-mini',
  'gpt-5.4-2026-03-05',
  'gpt-5.3-codex-spark',
  'codex-auto-review',
  'gpt-4o-audio-preview',
  'gpt-4o-realtime-preview',
  'gpt-image-1',
  'gpt-image-1.5',
  'gpt-image-2',
]

const anthropicModels = [
  'claude-3-5-sonnet-20241022',
  'claude-3-5-sonnet-20240620',
  'claude-3-5-haiku-20241022',
  'claude-3-7-sonnet-20250219',
  'claude-sonnet-4-20250514',
  'claude-opus-4-20250514',
  'claude-opus-4-1-20250805',
  'claude-sonnet-4-5-20250929',
  'claude-haiku-4-5-20251001',
  'claude-opus-4-5-20251101',
  'claude-opus-4-6',
  'claude-opus-4-7',
  'claude-opus-4-8',
  'claude-opus-5',
  'claude-sonnet-4-6',
  'claude-sonnet-5',
  'claude-fable-5',
]

export function splitAccountModelMapping(mapping?: Record<string, string>): {
  allowedModels: string[]
  modelMappings: AccountModelMapping[]
} {
  const allowedModels: string[] = []
  const modelMappings: AccountModelMapping[] = []
  for (const [rawFrom, rawTo] of Object.entries(mapping || {})) {
    const from = rawFrom.trim()
    const to = rawTo.trim()
    if (!from || !to) continue
    if (from === to) allowedModels.push(from)
    else modelMappings.push({ from, to })
  }
  return { allowedModels, modelMappings }
}

export function AccountModelRestriction({
  platform,
  mode,
  allowedModels,
  modelMappings,
  disabled = false,
  onModeChange,
  onAllowedModelsChange,
  onModelMappingsChange,
}: {
  platform: UpstreamPlatform
  mode: AccountModelRestrictionMode
  allowedModels: string[]
  modelMappings: AccountModelMapping[]
  disabled?: boolean
  onModeChange: (mode: AccountModelRestrictionMode) => void
  onAllowedModelsChange: (models: string[]) => void
  onModelMappingsChange: (mappings: AccountModelMapping[]) => void
}) {
  const { t } = useTranslation()
  const [customModel, setCustomModel] = useState('')
  const availableModels = useMemo(() => {
    const curated = platform === 'openai' ? openAIModels : anthropicModels
    return Array.from(
      new Set([
        ...curated,
        ...allowedModels,
        ...modelMappings.flatMap((mapping) => [mapping.from, mapping.to]),
      ])
    )
  }, [allowedModels, modelMappings, platform])
  const modelIcon = platform === 'openai' ? 'OpenAI' : 'Claude'

  const toggleModel = (model: string) => {
    onAllowedModelsChange(
      allowedModels.includes(model)
        ? allowedModels.filter((item) => item !== model)
        : [...allowedModels, model]
    )
  }

  const addCustomModel = () => {
    const model = customModel.trim()
    if (!model || allowedModels.includes(model)) return
    onAllowedModelsChange([...allowedModels, model])
    setCustomModel('')
  }

  const updateMapping = (
    index: number,
    key: keyof AccountModelMapping,
    value: string
  ) => {
    onModelMappingsChange(
      modelMappings.map((mapping, mappingIndex) =>
        mappingIndex === index ? { ...mapping, [key]: value } : mapping
      )
    )
  }

  return (
    <div className='flex flex-col gap-4'>
      <ToggleGroup
        variant='outline'
        value={[mode]}
        onValueChange={(values) => {
          const value = values[0] as AccountModelRestrictionMode | undefined
          if (value) onModeChange(value)
        }}
        className='grid w-full grid-cols-2 gap-2'
      >
        <ToggleGroupItem
          value='whitelist'
          disabled={disabled}
          className='data-pressed:bg-success/20 data-pressed:text-success h-9 w-full rounded-lg border'
        >
          {t('Model Whitelist')}
        </ToggleGroupItem>
        <ToggleGroupItem
          value='mapping'
          disabled={disabled}
          className='data-pressed:bg-muted h-9 w-full rounded-lg border'
        >
          {t('Model Mapping')}
        </ToggleGroupItem>
      </ToggleGroup>

      {mode === 'whitelist' ? (
        <div className='flex flex-col gap-3'>
          <div className='rounded-lg border p-2'>
            {allowedModels.length > 0 ? (
              <div className='grid grid-cols-1 gap-1.5 sm:grid-cols-2'>
                {allowedModels.map((model) => (
                  <div
                    key={model}
                    className='bg-muted flex min-w-0 items-center gap-1 rounded-md py-1 pr-1 pl-2 text-xs'
                  >
                    <span className='shrink-0'>
                      {getLobeIcon(modelIcon, 14)}
                    </span>
                    <span className='min-w-0 flex-1 truncate'>{model}</span>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon-xs'
                      title={t('Remove model')}
                      disabled={disabled}
                      onClick={() => toggleModel(model)}
                    >
                      <HugeiconsIcon icon={Cancel01Icon} strokeWidth={2} />
                    </Button>
                  </div>
                ))}
              </div>
            ) : (
              <p className='text-muted-foreground px-1 py-3 text-center text-sm'>
                {t('All models are supported when no model is selected')}
              </p>
            )}

            <Popover>
              <PopoverTrigger
                render={
                  <Button
                    type='button'
                    variant='ghost'
                    className='mt-2 w-full justify-between border-t px-1 pt-2'
                    disabled={disabled}
                  />
                }
              >
                <span>
                  {t('{{count}} models', {
                    count: allowedModels.length,
                  })}
                </span>
                <HugeiconsIcon icon={ArrowDown01Icon} strokeWidth={2} />
              </PopoverTrigger>
              <PopoverContent
                align='start'
                className='w-[34rem] max-w-[80vw] p-0'
              >
                <Command>
                  <CommandInput placeholder={t('Search models')} />
                  <CommandList>
                    <CommandEmpty>{t('No matching results')}</CommandEmpty>
                    <CommandGroup>
                      {availableModels.map((model) => (
                        <CommandItem
                          key={model}
                          value={model}
                          data-checked={allowedModels.includes(model)}
                          onSelect={() => toggleModel(model)}
                        >
                          {getLobeIcon(modelIcon, 16)}
                          <span className='truncate'>{model}</span>
                        </CommandItem>
                      ))}
                    </CommandGroup>
                  </CommandList>
                </Command>
              </PopoverContent>
            </Popover>
          </div>

          <div className='flex flex-wrap gap-2'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={disabled}
              onClick={() => onAllowedModelsChange(availableModels)}
            >
              {t('Fill Related Models')}
            </Button>
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={disabled || allowedModels.length === 0}
              onClick={() => onAllowedModelsChange([])}
            >
              {t('Clear all')}
            </Button>
          </div>

          <div className='flex gap-2'>
            <Input
              value={customModel}
              disabled={disabled}
              placeholder={t('Enter model name')}
              onChange={(event) => setCustomModel(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter') {
                  event.preventDefault()
                  addCustomModel()
                }
              }}
            />
            <Button
              type='button'
              variant='outline'
              disabled={disabled || !customModel.trim()}
              onClick={addCustomModel}
            >
              <HugeiconsIcon
                data-icon='inline-start'
                icon={Add01Icon}
                strokeWidth={2}
              />
              {t('Add model')}
            </Button>
          </div>
          <FieldDescription>
            {t('Only selected request models can use this account.')}
          </FieldDescription>
        </div>
      ) : (
        <div className='flex flex-col gap-3'>
          <FieldDescription>
            {t('Map request model names to actual upstream model names.')}
          </FieldDescription>
          {modelMappings.map((mapping, index) => (
            <div
              key={`${index}-${mapping.from}`}
              className='grid grid-cols-[1fr_auto_1fr_auto] items-center gap-2'
            >
              <Input
                value={mapping.from}
                disabled={disabled}
                placeholder={t('Request Model')}
                onChange={(event) =>
                  updateMapping(index, 'from', event.target.value)
                }
              />
              <HugeiconsIcon
                icon={ArrowRight01Icon}
                className='text-muted-foreground'
                strokeWidth={2}
              />
              <Input
                value={mapping.to}
                disabled={disabled}
                placeholder={t('Actual Model')}
                onChange={(event) =>
                  updateMapping(index, 'to', event.target.value)
                }
              />
              <Button
                type='button'
                variant='ghost'
                size='icon'
                title={t('Delete mapping')}
                disabled={disabled}
                onClick={() =>
                  onModelMappingsChange(
                    modelMappings.filter(
                      (_, mappingIndex) => mappingIndex !== index
                    )
                  )
                }
              >
                <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} />
              </Button>
            </div>
          ))}
          <Button
            type='button'
            variant='outline'
            className='w-full border-dashed'
            disabled={disabled}
            onClick={() =>
              onModelMappingsChange([...modelMappings, { from: '', to: '' }])
            }
          >
            <HugeiconsIcon
              data-icon='inline-start'
              icon={Add01Icon}
              strokeWidth={2}
            />
            {t('Add mapping')}
          </Button>
        </div>
      )}
    </div>
  )
}
