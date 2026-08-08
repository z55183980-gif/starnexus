/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useUpdateOption } from '../hooks/use-update-option'

const OPTION_KEY = 'GPTImagePrice'
const KNOWN_MODELS = [
  'gpt-image-1',
  'gpt-image-1-mini',
  'gpt-image-1.5',
  'gpt-image-2',
] as const
const TIERS = ['1k', '2k', '4k'] as const

type Tier = (typeof TIERS)[number]
type PriceDraft = Record<Tier, string>
type PriceDraftMap = Record<string, PriceDraft>

const emptyPriceDraft = (): PriceDraft => ({ '1k': '', '2k': '', '4k': '' })

function parsePrices(raw: string): PriceDraftMap {
  let parsed: unknown
  try {
    parsed = JSON.parse(raw || '{}')
  } catch {
    parsed = {}
  }
  const source =
    parsed && typeof parsed === 'object' && !Array.isArray(parsed)
      ? (parsed as Record<string, unknown>)
      : {}
  const names = new Set<string>([
    ...KNOWN_MODELS,
    ...Object.keys(source).filter((name) => name.startsWith('gpt-image-')),
  ])
  const result: PriceDraftMap = {}
  for (const name of names) {
    const value = source[name]
    const tiers =
      value && typeof value === 'object' && !Array.isArray(value)
        ? (value as Record<string, unknown>)
        : {}
    result[name] = {
      '1k': tiers['1k'] === undefined ? '' : String(tiers['1k']),
      '2k': tiers['2k'] === undefined ? '' : String(tiers['2k']),
      '4k': tiers['4k'] === undefined ? '' : String(tiers['4k']),
    }
  }
  return result
}

function serializePrices(drafts: PriceDraftMap) {
  const result: Record<string, Record<Tier, number>> = {}
  for (const [model, prices] of Object.entries(drafts)) {
    const values = TIERS.map((tier) => prices[tier].trim())
    if (values.every((value) => value === '')) continue
    if (values.some((value) => value === '')) {
      throw new Error(
        'Complete all 1K, 2K, and 4K prices for each enabled model.'
      )
    }
    const numeric = TIERS.map((tier) => Number(prices[tier]))
    if (numeric.some((value) => !Number.isFinite(value) || value < 0)) {
      throw new Error('GPT Image prices must be non-negative numbers.')
    }
    result[model] = {
      '1k': numeric[0],
      '2k': numeric[1],
      '4k': numeric[2],
    }
  }
  return result
}

type GPTImagePriceSettingsProps = {
  defaultValue: string
}

export function GPTImagePriceSettings({
  defaultValue,
}: GPTImagePriceSettingsProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [drafts, setDrafts] = useState<PriceDraftMap>(() =>
    parsePrices(defaultValue)
  )
  const [customModel, setCustomModel] = useState('')

  useEffect(() => {
    setDrafts(parsePrices(defaultValue))
  }, [defaultValue])

  const modelNames = useMemo(() => Object.keys(drafts).sort(), [drafts])

  const updatePrice = useCallback(
    (model: string, tier: Tier, value: string) => {
      if (!/^(\d+(\.\d*)?|\.\d*)?$/.test(value)) return
      setDrafts((current) => ({
        ...current,
        [model]: { ...current[model], [tier]: value },
      }))
    },
    []
  )

  const addCustomModel = useCallback(() => {
    const model = customModel.trim().toLowerCase()
    if (!model.startsWith('gpt-image-')) {
      toast.error(t('Model name must start with gpt-image-'))
      return
    }
    setDrafts((current) => ({
      ...current,
      [model]: current[model] || emptyPriceDraft(),
    }))
    setCustomModel('')
  }, [customModel, t])

  const removeCustomModel = useCallback((model: string) => {
    setDrafts((current) => {
      const next = { ...current }
      delete next[model]
      return next
    })
  }, [])

  const save = useCallback(async () => {
    try {
      const prices = serializePrices(drafts)
      await updateOption.mutateAsync({
        key: OPTION_KEY,
        value: JSON.stringify(prices),
      })
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Invalid price'
      if (
        message === 'Complete all 1K, 2K, and 4K prices for each enabled model.'
      ) {
        toast.error(
          t('Complete all 1K, 2K, and 4K prices for each enabled model.')
        )
      } else if (message === 'GPT Image prices must be non-negative numbers.') {
        toast.error(t('GPT Image prices must be non-negative numbers.'))
      } else {
        toast.error(t('Invalid price'))
      }
    }
  }, [drafts, t, updateOption])

  return (
    <div className='flex flex-col gap-4'>
      <Alert>
        <AlertTitle>{t('GPT Image resolution pricing')}</AlertTitle>
        <AlertDescription>
          {t(
            'Configure independent USD prices per generated image. The selected 1K, 2K, or 4K price is multiplied by the returned image count and group ratio.'
          )}
        </AlertDescription>
      </Alert>

      <FieldGroup>
        <Field orientation='horizontal'>
          <FieldLabel htmlFor='gpt-image-custom-model'>
            {t('Additional GPT Image model')}
          </FieldLabel>
          <InputGroup>
            <InputGroupInput
              id='gpt-image-custom-model'
              value={customModel}
              placeholder='gpt-image-2-2026-04-21'
              onChange={(event) => setCustomModel(event.target.value)}
            />
            <InputGroupAddon align='inline-end'>
              <Button
                type='button'
                variant='ghost'
                size='sm'
                onClick={addCustomModel}
              >
                {t('Add model')}
              </Button>
            </InputGroupAddon>
          </InputGroup>
        </Field>
      </FieldGroup>

      <div className='overflow-hidden rounded-md border'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Model')}</TableHead>
              <TableHead>{t('1K price')}</TableHead>
              <TableHead>{t('2K price')}</TableHead>
              <TableHead>{t('4K price')}</TableHead>
              <TableHead className='w-[96px] text-right'>
                {t('Actions')}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {modelNames.map((model) => (
              <TableRow key={model}>
                <TableCell className='font-medium'>{model}</TableCell>
                {TIERS.map((tier) => (
                  <TableCell key={tier}>
                    <Field>
                      <FieldLabel className='sr-only'>
                        {t('{{tier}} price for {{model}}', {
                          tier: tier.toUpperCase(),
                          model,
                        })}
                      </FieldLabel>
                      <InputGroup>
                        <InputGroupAddon>$</InputGroupAddon>
                        <InputGroupInput
                          inputMode='decimal'
                          value={drafts[model][tier]}
                          placeholder='0.00'
                          aria-label={t('{{tier}} price for {{model}}', {
                            tier: tier.toUpperCase(),
                            model,
                          })}
                          onChange={(event) =>
                            updatePrice(model, tier, event.target.value)
                          }
                        />
                        <InputGroupAddon align='inline-end'>
                          / {t('image')}
                        </InputGroupAddon>
                      </InputGroup>
                    </Field>
                  </TableCell>
                ))}
                <TableCell className='text-right'>
                  {!KNOWN_MODELS.includes(
                    model as (typeof KNOWN_MODELS)[number]
                  ) && (
                    <Button
                      type='button'
                      variant='ghost'
                      size='sm'
                      onClick={() => removeCustomModel(model)}
                    >
                      {t('Delete')}
                    </Button>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      <div>
        <Button type='button' onClick={save} disabled={updateOption.isPending}>
          {updateOption.isPending ? t('Saving...') : t('Save GPT Image prices')}
        </Button>
      </div>
    </div>
  )
}
