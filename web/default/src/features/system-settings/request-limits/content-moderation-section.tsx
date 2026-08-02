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
import { useEffect, useMemo } from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import {
  CONTENT_MODERATION_CATEGORIES,
  parseContentModerationConfig,
  stringifyContentModerationConfig,
} from './content-moderation-utils'

const contentModerationSchema = z.object({
  enabled: z.boolean(),
  mode: z.enum(['pre_block', 'observe']),
  base_url: z.string().min(1),
  model: z.string().min(1),
  api_keys_text: z.string().optional(),
  timeout_ms: z.coerce.number().int().min(500).max(30000),
  thresholds: z.record(z.string(), z.coerce.number().min(0).max(1)),
})

type ContentModerationFormValues = z.infer<typeof contentModerationSchema>

type ContentModerationSectionProps = {
  defaultValue: string
  embedded?: boolean
}

function toFormValues(raw: string): ContentModerationFormValues {
  const config = parseContentModerationConfig(raw)
  return {
    enabled: config.enabled,
    mode: config.mode,
    base_url: config.base_url,
    model: config.model,
    api_keys_text: config.api_keys.join('\n'),
    timeout_ms: config.timeout_ms,
    thresholds: { ...config.thresholds },
  }
}

export function ContentModerationSection({
  defaultValue,
  embedded = false,
}: ContentModerationSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const defaults = useMemo(() => toFormValues(defaultValue), [defaultValue])
  const form = useForm<ContentModerationFormValues>({
    resolver: zodResolver(contentModerationSchema),
    defaultValues: defaults,
  })

  useEffect(() => {
    form.reset(defaults)
  }, [defaults, form])

  const onSubmit = async (values: ContentModerationFormValues) => {
    const apiKeys = (values.api_keys_text ?? '')
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean)
    const payload = stringifyContentModerationConfig({
      enabled: values.enabled,
      mode: values.mode,
      base_url: values.base_url.trim(),
      model: values.model.trim(),
      api_keys: apiKeys,
      timeout_ms: values.timeout_ms,
      thresholds: values.thresholds,
    })
    await updateOption.mutateAsync({
      key: 'ContentModerationConfig',
      value: payload,
    })
  }

  const formBody = (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} className='space-y-6'>
        <FormField
          control={form.control}
          name='enabled'
          render={({ field }) => (
            <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
              <div className='space-y-0.5'>
                <FormLabel>{t('Enable content moderation')}</FormLabel>
                <FormDescription>
                  {t(
                    'When enabled, the latest user prompt is checked via Moderations API before billing.'
                  )}
                </FormDescription>
              </div>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='mode'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Global Mode')}</FormLabel>
              <Select value={field.value} onValueChange={field.onChange}>
                <FormControl>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                </FormControl>
                <SelectContent>
                  <SelectItem value='pre_block'>
                    {t('Front interception (pre_block)')}
                  </SelectItem>
                  <SelectItem value='observe'>
                    {t('Observe only (observe)')}
                  </SelectItem>
                </SelectContent>
              </Select>
              <FormDescription>
                {t(
                  'Pre-block stops flagged requests. Observe audits in the background and never blocks.'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='base_url'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('OpenAI Base URL')}</FormLabel>
              <FormControl>
                <Input {...field} placeholder='https://api.openai.com' />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='model'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Model Name')}</FormLabel>
              <FormControl>
                <Input {...field} placeholder='omni-moderation-latest' />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='timeout_ms'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('HTTP Timeout (ms)')}</FormLabel>
              <FormControl>
                <Input type='number' {...field} />
              </FormControl>
              <FormDescription>
                {t('Maximum wait time for the Moderations API response.')}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name='api_keys_text'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('OpenAI API Keys')}</FormLabel>
              <FormControl>
                <Textarea
                  {...field}
                  rows={4}
                  placeholder={t('One API key per line')}
                />
              </FormControl>
              <FormDescription>
                {t(
                  'Masked keys are kept on save unless you replace the line with a new key. Clear all lines to remove keys.'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <div className='space-y-3'>
          <FormLabel>{t('Category thresholds')}</FormLabel>
          <FormDescription>
            {t(
              'Flag when any category score is greater than or equal to its threshold.'
            )}
          </FormDescription>
          <div className='grid gap-3 md:grid-cols-2'>
            {CONTENT_MODERATION_CATEGORIES.map((category) => (
              <FormField
                key={category}
                control={form.control}
                name={`thresholds.${category}`}
                render={({ field }) => (
                  <FormItem>
                    <FormLabel className='font-mono text-xs'>
                      {category}
                    </FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        step='0.01'
                        min={0}
                        max={1}
                        value={field.value ?? 0}
                        onChange={field.onChange}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            ))}
          </div>
        </div>

        <Button type='submit' disabled={updateOption.isPending}>
          {updateOption.isPending ? t('Saving...') : t('Save')}
        </Button>
      </form>
    </Form>
  )

  if (embedded) {
    return formBody
  }

  return (
    <SettingsSection
      title={t('Content Moderation (API)')}
      description={t(
        'Call OpenAI Moderations before upstream. Disabled by default. API failures fail open.'
      )}
    >
      {formBody}
    </SettingsSection>
  )
}
