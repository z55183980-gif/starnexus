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
import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { MultiSelect } from '@/components/multi-select'
import { PasswordInput } from '@/components/password-input'
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
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { getGroups } from '@/features/users/api'
import { testContentModerationAPIKey } from '@/features/security-audit/api'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import {
  parseContentModerationConfig,
  stringifyContentModerationConfig,
} from './content-moderation-utils'

const contentModerationSchema = z
  .object({
    enabled: z.boolean(),
    mode: z.enum(['pre_block', 'observe']),
    base_url: z.string().min(1),
    model: z.string().min(1),
    api_key: z.string().optional(),
    timeout_ms: z.number().int().min(500).max(30000),
    all_groups: z.boolean(),
    groups: z.array(z.string()),
    model_filter_type: z.enum(['all', 'include', 'exclude']),
    model_filter_models_text: z.string().optional(),
  })
  .superRefine((values, ctx) => {
    if (
      (values.model_filter_type === 'include' ||
        values.model_filter_type === 'exclude') &&
      !(values.model_filter_models_text ?? '').trim()
    ) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['model_filter_models_text'],
        message: 'Enter at least one model when using include or exclude scope',
      })
    }
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
    api_key: config.api_keys[0] ?? '',
    timeout_ms: config.timeout_ms,
    all_groups: config.all_groups,
    groups: config.groups,
    model_filter_type: config.model_filter.type,
    model_filter_models_text: config.model_filter.models.join('\n'),
  }
}

export function ContentModerationSection({
  defaultValue,
  embedded = false,
}: ContentModerationSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [testingKey, setTestingKey] = useState(false)
  const defaults = useMemo(() => toFormValues(defaultValue), [defaultValue])
  const thresholds = useMemo(
    () => parseContentModerationConfig(defaultValue).thresholds,
    [defaultValue]
  )
  const groupsQuery = useQuery({
    queryKey: ['groups'],
    queryFn: getGroups,
    staleTime: 60_000,
  })
  const groupOptions = useMemo(
    () =>
      (groupsQuery.data?.data ?? []).map((group) => ({
        label: group,
        value: group,
      })),
    [groupsQuery.data?.data]
  )
  const form = useForm<ContentModerationFormValues>({
    resolver: zodResolver(contentModerationSchema),
    defaultValues: defaults,
    shouldUnregister: false,
  })

  useEffect(() => {
    form.reset(defaults)
  }, [defaults, form])

  const allGroups = form.watch('all_groups')
  const modelFilterType = form.watch('model_filter_type')

  const onSubmit = async (values: ContentModerationFormValues) => {
    const apiKey = (values.api_key ?? '').trim()
    const filterModels = (values.model_filter_models_text ?? '')
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean)
    const groups = values.all_groups ? [] : (values.groups ?? [])
    const payload = stringifyContentModerationConfig({
      enabled: values.enabled,
      mode: values.mode,
      base_url: values.base_url.trim(),
      model: values.model.trim(),
      api_keys: apiKey ? [apiKey] : [],
      timeout_ms: values.timeout_ms,
      all_groups: values.all_groups,
      groups,
      model_filter: {
        type: values.model_filter_type,
        models: values.model_filter_type === 'all' ? [] : filterModels,
      },
      thresholds,
    })
    await updateOption.mutateAsync({
      key: 'ContentModerationConfig',
      value: payload,
    })
  }

  const onTestAPIKey = async () => {
    const apiKey = (form.getValues('api_key') ?? '').trim()
    const baseUrl = form.getValues('base_url').trim()
    const model = form.getValues('model').trim()
    const timeoutMs = form.getValues('timeout_ms')
    if (!apiKey) {
      toast.error(t('Enter or save an API key before testing'))
      return
    }
    setTestingKey(true)
    try {
      const result = await testContentModerationAPIKey({
        api_key: apiKey,
        base_url: baseUrl,
        model,
        timeout_ms: timeoutMs,
      })
      if (result.ok) {
        toast.success(
          t('Moderation API key is valid ({{ms}} ms)', {
            ms: result.latency_ms,
          })
        )
      } else {
        toast.error(result.error || t('Moderation API key test failed'))
      }
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Moderation API key test failed')
      )
    } finally {
      setTestingKey(false)
    }
  }

  const formBody = (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} className='space-y-5'>
        <FormField
          control={form.control}
          name='enabled'
          render={({ field }) => (
            <FormItem
              className={
                embedded
                  ? 'flex flex-row items-center justify-between gap-4 pb-1'
                  : 'flex flex-row items-center justify-between rounded-lg border px-4 py-3'
              }
            >
              <div className='space-y-0.5'>
                <FormLabel>
                  {embedded
                    ? t('Moderation settings')
                    : t('Enable content moderation')}
                </FormLabel>
                <FormDescription>
                  {embedded
                    ? t(
                        'Call OpenAI Moderations before upstream. Disabled by default. API failures fail open.'
                      )
                    : t(
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

        <div className='grid gap-4 md:grid-cols-2'>
          <FormField
            control={form.control}
            name='mode'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Global Mode')}</FormLabel>
                <Select
                  items={[
                    {
                      value: 'pre_block',
                      label: t('Front interception'),
                    },
                    {
                      value: 'observe',
                      label: t('Observe only'),
                    },
                  ]}
                  value={field.value}
                  onValueChange={(value) => {
                    if (!value) return
                    field.onChange(value)
                  }}
                >
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectItem value='pre_block'>
                        {t('Front interception')}
                      </SelectItem>
                      <SelectItem value='observe'>
                        {t('Observe only')}
                      </SelectItem>
                    </SelectGroup>
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
            name='timeout_ms'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('HTTP Timeout (ms)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    value={field.value}
                    onBlur={field.onBlur}
                    name={field.name}
                    ref={field.ref}
                    onChange={(event) =>
                      field.onChange(Number(event.target.value))
                    }
                  />
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
        </div>

        <FormField
          control={form.control}
          name='api_key'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('OpenAI API Key')}</FormLabel>
              <div className='flex flex-col gap-2 sm:flex-row sm:items-center'>
                <FormControl>
                  <PasswordInput
                    value={field.value ?? ''}
                    onBlur={field.onBlur}
                    name={field.name}
                    ref={field.ref}
                    onChange={field.onChange}
                    autoComplete='off'
                    placeholder={t('Enter API Key')}
                    className='flex-1'
                  />
                </FormControl>
                <Button
                  type='button'
                  variant='outline'
                  disabled={testingKey || updateOption.isPending}
                  onClick={() => void onTestAPIKey()}
                  className='sm:shrink-0'
                >
                  {testingKey ? t('Testing...') : t('Test')}
                </Button>
              </div>
              <FormMessage />
            </FormItem>
          )}
        />

        <div className='space-y-4 rounded-lg border p-4'>
          <div>
            <h3 className='text-sm font-medium'>{t('Audit scope')}</h3>
            <p className='text-muted-foreground mt-1 text-sm'>
              {t(
                'Choose which request groups and models are checked by Moderations. Out-of-scope traffic is skipped.'
              )}
            </p>
          </div>

          <FormField
            control={form.control}
            name='all_groups'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Audit groups')}</FormLabel>
                <Select
                  items={[
                    { value: 'all', label: t('All Groups') },
                    { value: 'selected', label: t('Selected groups') },
                  ]}
                  value={field.value ? 'all' : 'selected'}
                  onValueChange={(value) => {
                    if (!value) return
                    field.onChange(value === 'all')
                    if (value === 'all') {
                      form.setValue('groups', [])
                    }
                  }}
                >
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectItem value='all'>{t('All Groups')}</SelectItem>
                      <SelectItem value='selected'>
                        {t('Selected groups')}
                      </SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FormDescription>
                  {t(
                    'Matches the token/request channel group used for routing.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          {!allGroups ? (
            <FormField
              control={form.control}
              name='groups'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Selected groups')}</FormLabel>
                  <FormControl>
                    <MultiSelect
                      options={groupOptions}
                      selected={field.value ?? []}
                      onChange={field.onChange}
                      placeholder={t('Select groups...')}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Only requests using these groups are audited. Leave empty to audit none.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          ) : null}

          <FormField
            control={form.control}
            name='model_filter_type'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Model scope')}</FormLabel>
                <Select
                  items={[
                    { value: 'all', label: t('All models') },
                    {
                      value: 'include',
                      label: t('Only selected models'),
                    },
                    {
                      value: 'exclude',
                      label: t('Exclude selected models'),
                    },
                  ]}
                  value={field.value}
                  onValueChange={(value) => {
                    if (!value) return
                    field.onChange(value)
                    if (value === 'all') {
                      form.setValue('model_filter_models_text', '')
                    }
                  }}
                >
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectItem value='all'>{t('All models')}</SelectItem>
                      <SelectItem value='include'>
                        {t('Only selected models')}
                      </SelectItem>
                      <SelectItem value='exclude'>
                        {t('Exclude selected models')}
                      </SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FormDescription>
                  {t(
                    'Matching uses the client-requested model name (case-insensitive).'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          {modelFilterType !== 'all' ? (
            <FormField
              control={form.control}
              name='model_filter_models_text'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Model list')}</FormLabel>
                  <FormControl>
                    <Textarea
                      {...field}
                      rows={3}
                      placeholder={t('One model name per line')}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Enter at least one model when using include or exclude scope'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          ) : null}
        </div>

        <div className='flex justify-end'>
          <Button type='submit' disabled={updateOption.isPending}>
            {updateOption.isPending ? t('Saving...') : t('Save')}
          </Button>
        </div>
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
