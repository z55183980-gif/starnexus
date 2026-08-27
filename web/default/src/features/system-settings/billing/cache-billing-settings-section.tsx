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

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useMemo, type ChangeEvent } from 'react'
import * as z from 'zod'
import { Controller, useFieldArray, type Resolver } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Add01Icon, Delete02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import { SettingsSection } from '../components/settings-section'
import { useSettingsForm } from '../hooks/use-settings-form'
import { useUpdateOption } from '../hooks/use-update-option'
import type { CacheBillingFormValues } from './cache-billing-settings'

const createCacheBillingSchema = (t: (key: string) => string) =>
  z
    .object({
      rules: z.array(
        z.object({
          model: z.string().trim().min(1, t('Model name is required')),
          reduction_percentage_points: z.coerce
            .number()
            .min(0, t('Cache billing reduction must be between 0 and 100.'))
            .max(100, t('Cache billing reduction must be between 0 and 100.')),
        })
      ),
    })
    .superRefine((values, ctx) => {
      const seen = new Set<string>()
      values.rules.forEach((rule, index) => {
        const model = rule.model.trim()
        if (!model || !seen.has(model)) {
          seen.add(model)
          return
        }
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: t('Model names must be unique.'),
          path: ['rules', index, 'model'],
        })
      })
    })

type CacheBillingSettingsSectionProps = {
  defaultValues: CacheBillingFormValues
}

function createRule(): CacheBillingFormValues['rules'][number] {
  return {
    model: '',
    reduction_percentage_points: 0,
  }
}

export function CacheBillingSettingsSection({
  defaultValues,
}: CacheBillingSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const schema = useMemo(() => createCacheBillingSchema(t), [t])

  const { form, handleSubmit, isDirty, isSubmitting } =
    useSettingsForm<CacheBillingFormValues>({
      resolver: zodResolver(schema) as Resolver<
        CacheBillingFormValues,
        unknown,
        CacheBillingFormValues
      >,
      defaultValues,
      onSubmit: async (values) => {
        const offsets = Object.fromEntries(
          values.rules.map((rule) => [
            rule.model.trim(),
            Math.round(Number(rule.reduction_percentage_points) * 100),
          ])
        )
        await updateOption.mutateAsync({
          key: 'billing_setting.cache_billing_offset_bps',
          value: JSON.stringify(offsets),
        })
      },
    })

  const { fields, append, remove } = useFieldArray({
    control: form.control,
    name: 'rules',
  })

  const handleNumberChange =
    (onChange: (value: number | string) => void) =>
    (event: ChangeEvent<HTMLInputElement>) => {
      onChange(
        event.target.value === '' ? '' : event.currentTarget.valueAsNumber
      )
    }

  const formDisabled = updateOption.isPending || isSubmitting

  return (
    <SettingsSection
      title={t('Cache Billing Adjustments')}
      description={t(
        'Root-only. Reduces cache-read billing eligibility and projects adjusted usage counts to users; raw upstream and audit data remain internal.'
      )}
    >
      <FormNavigationGuard when={isDirty} />

      <form onSubmit={handleSubmit} className='flex flex-col gap-6'>
        <FormDirtyIndicator isDirty={isDirty} />

        <Alert>
          <AlertTitle>{t('Settlement example')}</AlertTitle>
          <AlertDescription>
            {t(
              'A monitored cache rate of 93.91% minus 3 percentage points is settled as 90.91%.'
            )}
          </AlertDescription>
        </Alert>

        {fields.length === 0 ? (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>
                {t('No cache billing adjustments configured')}
              </EmptyTitle>
              <EmptyDescription>
                {t(
                  'Add a model to configure an internal cache billing reduction.'
                )}
              </EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Button
                type='button'
                variant='outline'
                disabled={formDisabled}
                onClick={() => append(createRule())}
              >
                <HugeiconsIcon icon={Add01Icon} data-icon='inline-start' />
                {t('Add Model')}
              </Button>
            </EmptyContent>
          </Empty>
        ) : (
          <>
            <div className='flex justify-end'>
              <Button
                type='button'
                variant='outline'
                disabled={formDisabled}
                onClick={() => append(createRule())}
              >
                <HugeiconsIcon icon={Add01Icon} data-icon='inline-start' />
                {t('Add Model')}
              </Button>
            </div>

            <FieldGroup>
              {fields.map((rule, index) => (
                <div
                  key={rule.id}
                  className='grid gap-3 rounded-lg border p-3 md:grid-cols-[minmax(16rem,1fr)_minmax(16rem,1fr)_2.5rem]'
                >
                  <Controller
                    control={form.control}
                    name={`rules.${index}.model`}
                    render={({ field, fieldState }) => (
                      <Field data-invalid={fieldState.invalid || undefined}>
                        <FieldLabel htmlFor={`cache-billing-model-${rule.id}`}>
                          {t('Model Name')}
                        </FieldLabel>
                        <Input
                          id={`cache-billing-model-${rule.id}`}
                          placeholder='gpt-5.6-luna'
                          disabled={formDisabled}
                          aria-invalid={fieldState.invalid || undefined}
                          {...field}
                        />
                        <FieldError errors={[fieldState.error]} />
                      </Field>
                    )}
                  />

                  <Controller
                    control={form.control}
                    name={`rules.${index}.reduction_percentage_points`}
                    render={({ field, fieldState }) => (
                      <Field data-invalid={fieldState.invalid || undefined}>
                        <FieldLabel
                          htmlFor={`cache-billing-reduction-${rule.id}`}
                        >
                          {t('Internal cache billing reduction')}
                        </FieldLabel>
                        <InputGroup>
                          <InputGroupInput
                            id={`cache-billing-reduction-${rule.id}`}
                            type='number'
                            inputMode='decimal'
                            min='0'
                            max='100'
                            step='0.01'
                            disabled={formDisabled}
                            value={field.value ?? ''}
                            name={field.name}
                            onBlur={field.onBlur}
                            ref={field.ref}
                            onChange={handleNumberChange(field.onChange)}
                            aria-invalid={fieldState.invalid || undefined}
                          />
                          <InputGroupAddon align='inline-end'>
                            {t('percentage points')}
                          </InputGroupAddon>
                        </InputGroup>
                        <FieldError errors={[fieldState.error]} />
                      </Field>
                    )}
                  />

                  <div className='flex items-end justify-end'>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon'
                      disabled={formDisabled}
                      onClick={() => remove(index)}
                      aria-label={t('Delete Model')}
                    >
                      <HugeiconsIcon icon={Delete02Icon} />
                    </Button>
                  </div>
                </div>
              ))}
            </FieldGroup>
          </>
        )}

        <Button type='submit' disabled={formDisabled}>
          {updateOption.isPending ? t('Saving...') : t('Save Changes')}
        </Button>
      </form>
    </SettingsSection>
  )
}
