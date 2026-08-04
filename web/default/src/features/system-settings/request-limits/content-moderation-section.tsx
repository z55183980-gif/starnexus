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
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { MultiSelect } from '@/components/multi-select'
import { PasswordInput } from '@/components/password-input'
import {
  listContentModerationProviderModels,
  testContentModerationAPIKey,
} from '@/features/security-audit/api'
import { getGroups } from '@/features/users/api'
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
    observe_hit_action: z.enum(['observe', 'pre_block']),
    model_type: z.enum(['general', 'dedicated']),
    provider: z.enum(['deepseek']),
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
    observe_hit_action: config.observe_hit_action,
    model_type: config.model_type,
    provider: config.provider,
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
  const [fetchingModels, setFetchingModels] = useState(false)
  const defaults = useMemo(() => toFormValues(defaultValue), [defaultValue])
  const [availableModels, setAvailableModels] = useState<string[]>(() =>
    defaults.model ? [defaults.model] : []
  )
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
    setAvailableModels(defaults.model ? [defaults.model] : [])
  }, [defaults, form])

  const allGroups = form.watch('all_groups')
  const modelFilterType = form.watch('model_filter_type')
  const modelType = form.watch('model_type')
  const mode = form.watch('mode')

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
      observe_hit_action: values.observe_hit_action,
      model_type: values.model_type,
      provider: values.provider,
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
    const modelTypeValue = form.getValues('model_type')
    const provider = form.getValues('provider')
    const timeoutMs = form.getValues('timeout_ms')
    if (!apiKey) {
      toast.error(t('Enter or save an API key before testing'))
      return
    }
    setTestingKey(true)
    try {
      const result = await testContentModerationAPIKey({
        api_key: apiKey,
        model_type: modelTypeValue,
        provider,
        base_url: baseUrl,
        model,
        timeout_ms: timeoutMs,
      })
      if (result.ok) {
        toast.success(
          t('Audit API key is valid ({{ms}} ms)', {
            ms: result.latency_ms,
          })
        )
      } else {
        toast.error(result.error || t('Audit API key test failed'))
      }
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('Audit API key test failed')
      )
    } finally {
      setTestingKey(false)
    }
  }

  const onFetchModels = async () => {
    const apiKey = (form.getValues('api_key') ?? '').trim()
    if (!apiKey) {
      toast.error(t('Enter or save an API key before testing'))
      return
    }
    setFetchingModels(true)
    try {
      const result = await listContentModerationProviderModels({
        provider: form.getValues('provider'),
        api_key: apiKey,
        base_url: form.getValues('base_url').trim(),
        timeout_ms: form.getValues('timeout_ms'),
      })
      form.setValue('base_url', result.base_url, { shouldDirty: true })
      setAvailableModels(result.models)
      const selectedModel = form.getValues('model')
      if (!result.models.includes(selectedModel)) {
        form.setValue('model', result.models[0] ?? '', {
          shouldDirty: true,
          shouldValidate: true,
        })
      }
      if (result.models.length > 0) {
        toast.success(
          t('Fetched {{count}} models', { count: result.models.length })
        )
      } else {
        toast.error(t('No models fetched from upstream'))
      }
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('Failed to fetch models')
      )
    } finally {
      setFetchingModels(false)
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
                        'Audit all client-supplied prompt text before it reaches upstream GPT/OpenAI accounts. Pre-block mode denies requests when the audit service is unavailable.'
                      )
                    : t(
                        'Checks outbound user, system, developer, and instruction text for policy-enforcement risk to upstream GPT/OpenAI accounts.'
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
            name='model_type'
            render={({ field }) => (
              <FormItem className='md:col-span-2'>
                <FormLabel>{t('Audit model type')}</FormLabel>
                <FormControl>
                  <ToggleGroup
                    value={[field.value]}
                    onValueChange={(value) => {
                      if (!value[0]) return
                      field.onChange(value[0])
                      if (value[0] === 'general') {
                        form.setValue('provider', 'deepseek')
                        form.setValue('model', 'deepseek-v4-flash')
                        setAvailableModels(['deepseek-v4-flash'])
                        if (
                          form.getValues('base_url') ===
                          'https://api.openai.com'
                        ) {
                          form.setValue('base_url', 'https://api.deepseek.com')
                        }
                        if (form.getValues('timeout_ms') === 3000) {
                          form.setValue('timeout_ms', 8000)
                        }
                      } else {
                        form.setValue('model', 'omni-moderation-latest')
                        setAvailableModels(['omni-moderation-latest'])
                        if (
                          form.getValues('base_url') ===
                          'https://api.deepseek.com'
                        ) {
                          form.setValue('base_url', 'https://api.openai.com')
                        }
                        if (form.getValues('timeout_ms') === 8000) {
                          form.setValue('timeout_ms', 3000)
                        }
                      }
                    }}
                    variant='outline'
                    spacing={0}
                    className='w-full'
                    aria-label={t('Audit model type')}
                  >
                    <ToggleGroupItem value='general' className='flex-1'>
                      {t('General-purpose model')}
                    </ToggleGroupItem>
                    <ToggleGroupItem value='dedicated' className='flex-1'>
                      {t('Dedicated model')}
                    </ToggleGroupItem>
                  </ToggleGroup>
                </FormControl>
                <FormDescription>
                  {modelType === 'general'
                    ? t(
                        'Recommended for full account-risk coverage, including fraud, cyber abuse, privacy abuse, intellectual property, and safeguard evasion.'
                      )
                    : t(
                        'Dedicated moderation covers core safety categories only. Use a general-purpose model for full upstream account-risk coverage.'
                      )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          {modelType === 'general' ? (
            <FormField
              control={form.control}
              name='provider'
              render={({ field }) => (
                <FormItem className='md:col-span-2'>
                  <FormLabel>{t('Provider')}</FormLabel>
                  <Select
                    items={[{ value: 'deepseek', label: 'DeepSeek' }]}
                    value={field.value}
                    onValueChange={(value) => {
                      if (value === 'deepseek') field.onChange(value)
                    }}
                  >
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        <SelectItem value='deepseek'>DeepSeek</SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FormDescription>
                    {t('Fetch Models')}: GET /models · POST /chat/completions
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          ) : null}

          <FormField
            control={form.control}
            name='api_key'
            render={({ field }) => (
              <FormItem className='md:col-span-2'>
                <FormLabel>{t('API Key')}</FormLabel>
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

          {mode === 'observe' ? (
            <FormField
              control={form.control}
              name='observe_hit_action'
              render={({ field }) => {
                const items = [
                  {
                    value: 'observe',
                    label: t('Continue observing'),
                  },
                  {
                    value: 'pre_block',
                    label: t('Upgrade to front interception'),
                  },
                ]
                return (
                  <FormItem>
                    <FormLabel>{t('Action after first observed hit')}</FormLabel>
                    <Select
                      items={items}
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
                          {items.map((item) => (
                            <SelectItem key={item.value} value={item.value}>
                              {item.label}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormDescription>
                      {t(
                        'After this user is first flagged in observe mode, later requests can be checked synchronously and blocked when flagged again.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )
              }}
            />
          ) : null}

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
                  {t('Maximum wait time for the audit API response.')}
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
                <FormLabel>{t('API Base URL')}</FormLabel>
                <div className='flex flex-col gap-2 sm:flex-row sm:items-center'>
                  <FormControl>
                    <Input
                      {...field}
                      placeholder={
                        modelType === 'general'
                          ? 'https://api.deepseek.com'
                          : 'https://api.openai.com'
                      }
                    />
                  </FormControl>
                  {modelType === 'general' ? (
                    <Button
                      type='button'
                      variant='outline'
                      disabled={fetchingModels || testingKey}
                      onClick={() => void onFetchModels()}
                      className='sm:shrink-0'
                    >
                      {fetchingModels ? t('Loading...') : t('Fetch Models')}
                    </Button>
                  ) : null}
                </div>
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
                {modelType === 'general' ? (
                  <Select
                    items={availableModels.map((model) => ({
                      value: model,
                      label: model,
                    }))}
                    value={field.value}
                    onValueChange={(value) => {
                      if (value) field.onChange(value)
                    }}
                    disabled={availableModels.length === 0}
                  >
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue placeholder={t('No models available')} />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        {availableModels.map((model) => (
                          <SelectItem key={model} value={model}>
                            {model}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                ) : (
                  <FormControl>
                    <Input {...field} placeholder='omni-moderation-latest' />
                  </FormControl>
                )}
                <FormMessage />
              </FormItem>
            )}
          />
        </div>

        <div className='space-y-4 rounded-lg border p-4'>
          <div>
            <h3 className='text-sm font-medium'>{t('Audit scope')}</h3>
            <p className='text-muted-foreground mt-1 text-sm'>
              {t(
                'Choose which request groups and models are checked by content audit. Out-of-scope traffic is skipped.'
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
        'Audit all client-supplied prompt text before it reaches upstream GPT/OpenAI accounts. Pre-block mode denies requests when the audit service is unavailable.'
      )}
    >
      {formBody}
    </SettingsSection>
  )
}
