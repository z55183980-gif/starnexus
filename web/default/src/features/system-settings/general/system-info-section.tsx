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
import * as z from 'zod'
import type { Resolver } from 'react-hook-form'
import { useFieldArray } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Plus, RotateCcw, Trash2 } from 'lucide-react'
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
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import { SettingsSection } from '../components/settings-section'
import { useSettingsForm } from '../hooks/use-settings-form'
import { useUpdateOption } from '../hooks/use-update-option'

type ApiBaseUrlItem = {
  title: string
  url: string
}

const DEFAULT_API_BASE_URL_TITLE = 'API 请求地址'

function createApiBaseUrlItem(): ApiBaseUrlItem {
  return {
    title: '',
    url: '',
  }
}

function normalizeApiBaseUrl(url: string): string {
  return url.trim().replace(/\/+$/, '')
}

function normalizeApiBaseUrlItem(value: unknown): ApiBaseUrlItem | null {
  if (typeof value === 'string') {
    const url = normalizeApiBaseUrl(value)
    return url ? { title: DEFAULT_API_BASE_URL_TITLE, url } : null
  }

  if (!value || typeof value !== 'object') {
    return null
  }

  const item = value as Partial<ApiBaseUrlItem>
  const url = typeof item.url === 'string' ? normalizeApiBaseUrl(item.url) : ''
  if (!url) {
    return null
  }

  const title =
    typeof item.title === 'string' && item.title.trim()
      ? item.title.trim()
      : DEFAULT_API_BASE_URL_TITLE

  return { title, url }
}

function parseApiBaseUrlItems(value: unknown): ApiBaseUrlItem[] {
  if (Array.isArray(value)) {
    return value
      .map((item) => normalizeApiBaseUrlItem(item))
      .filter((item): item is ApiBaseUrlItem => Boolean(item))
  }

  if (typeof value !== 'string') {
    return [createApiBaseUrlItem()]
  }

  const raw = value.trim()
  if (!raw) {
    return [createApiBaseUrlItem()]
  }

  try {
    const parsed = JSON.parse(raw) as unknown
    const items = parseApiBaseUrlItems(parsed)
    return items.length > 0 ? items : [createApiBaseUrlItem()]
  } catch {
    const items = raw
      .split(/[\n,]+/)
      .map((item) => normalizeApiBaseUrlItem(item))
      .filter((item): item is ApiBaseUrlItem => Boolean(item))
    return items.length > 0 ? items : [createApiBaseUrlItem()]
  }
}

function getNormalizedApiBaseUrlItems(
  items: ApiBaseUrlItem[]
): ApiBaseUrlItem[] {
  const seen = new Set<string>()
  const normalized: ApiBaseUrlItem[] = []

  for (const item of items) {
    const url = normalizeApiBaseUrl(item.url)
    if (!url || seen.has(url)) {
      continue
    }
    seen.add(url)
    normalized.push({
      title: item.title.trim() || DEFAULT_API_BASE_URL_TITLE,
      url,
    })
  }

  return normalized
}

function serializeApiBaseUrlItems(items: ApiBaseUrlItem[]): string {
  const normalized = getNormalizedApiBaseUrlItems(items)
  return normalized.length > 0 ? JSON.stringify(normalized) : ''
}

const apiBaseUrlItemSchema = z
  .object({
    title: z.string(),
    url: z.string(),
  })
  .superRefine((item, ctx) => {
    const title = item.title.trim()
    const url = item.url.trim()

    if (!title && !url) {
      return
    }

    if (!title) {
      ctx.addIssue({
        code: 'custom',
        path: ['title'],
        message: 'Title is required',
      })
    }

    if (!url) {
      ctx.addIssue({
        code: 'custom',
        path: ['url'],
        message: 'URL is required',
      })
      return
    }

    if (!z.string().url().safeParse(url).success) {
      ctx.addIssue({
        code: 'custom',
        path: ['url'],
        message: 'Invalid URL',
      })
    }
  })

const _systemInfoSchema = z.object({
  theme: z.object({
    frontend: z.enum(['default', 'classic']),
  }),
  SystemName: z.string().min(1),
  ServerAddress: z.string().optional(),
  ApiBaseUrl: z.array(apiBaseUrlItemSchema),
  Logo: z.string().url().optional().or(z.literal('')),
  Footer: z.string().optional(),
  About: z.string().optional(),
  HomePageContent: z.string().optional(),
  legal: z.object({
    user_agreement: z.string().optional(),
    privacy_policy: z.string().optional(),
  }),
})

type SystemInfoFormValues = z.infer<typeof _systemInfoSchema>

type SystemInfoSectionProps = {
  defaultValues: Omit<SystemInfoFormValues, 'ApiBaseUrl'> & {
    ApiBaseUrl: string | ApiBaseUrlItem[]
  }
}

function normalizeValue(value: unknown): string {
  if (value === undefined || value === null) return ''
  return typeof value === 'string' ? value : String(value)
}

export function SystemInfoSection({ defaultValues }: SystemInfoSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const normalizedDefaults: SystemInfoFormValues = {
    theme: {
      frontend:
        defaultValues.theme?.frontend === 'classic' ? 'classic' : 'default',
    },
    SystemName: normalizeValue(defaultValues.SystemName),
    ServerAddress: normalizeValue(defaultValues.ServerAddress),
    ApiBaseUrl: parseApiBaseUrlItems(defaultValues.ApiBaseUrl),
    Logo: normalizeValue(defaultValues.Logo),
    Footer: normalizeValue(defaultValues.Footer),
    About: normalizeValue(defaultValues.About),
    HomePageContent: normalizeValue(defaultValues.HomePageContent),
    legal: {
      user_agreement: normalizeValue(defaultValues.legal?.user_agreement),
      privacy_policy: normalizeValue(defaultValues.legal?.privacy_policy),
    },
  }

  const systemInfoSchemaWithI18n = z.object({
    theme: z.object({
      frontend: z.enum(['default', 'classic']),
    }),
    SystemName: z.string().min(1, {
      error: () => t('System name is required'),
    }),
    ServerAddress: z.string().optional(),
    ApiBaseUrl: z.array(apiBaseUrlItemSchema),
    Logo: z.string().url().optional().or(z.literal('')),
    Footer: z.string().optional(),
    About: z.string().optional(),
    HomePageContent: z.string().optional(),
    legal: z.object({
      user_agreement: z.string().optional(),
      privacy_policy: z.string().optional(),
    }),
  })

  const { form, handleSubmit, handleReset, isDirty, isSubmitting } =
    useSettingsForm<SystemInfoFormValues>({
      resolver: zodResolver(systemInfoSchemaWithI18n) as Resolver<
        SystemInfoFormValues,
        unknown,
        SystemInfoFormValues
      >,
      defaultValues: normalizedDefaults,
      onSubmit: async (data, changedFields) => {
        const apiBaseUrlChanged = Object.keys(changedFields).some(
          (key) => key === 'ApiBaseUrl' || key.startsWith('ApiBaseUrl.')
        )

        if (apiBaseUrlChanged) {
          await updateOption.mutateAsync({
            key: 'ApiBaseUrl',
            value: serializeApiBaseUrlItems(data.ApiBaseUrl),
          })
        }

        for (const [key, value] of Object.entries(changedFields)) {
          if (key === 'ApiBaseUrl' || key.startsWith('ApiBaseUrl.')) {
            continue
          }
          let v = normalizeValue(value)
          if (key === 'ServerAddress') {
            v = v.replace(/\/+$/, '')
          }
          await updateOption.mutateAsync({
            key,
            value: v,
          })
        }
      },
    })

  const {
    fields: apiBaseUrlFields,
    append: appendApiBaseUrl,
    remove: removeApiBaseUrl,
  } = useFieldArray({
    control: form.control,
    name: 'ApiBaseUrl',
  })

  return (
    <>
      <FormNavigationGuard when={isDirty} />

      <SettingsSection
        title={t('System Information')}
        description={t('Configure basic system information and branding')}
      >
        <Form {...form}>
          <form onSubmit={handleSubmit} className='flex flex-col gap-6'>
            <FormDirtyIndicator isDirty={isDirty} />
            <FormField
              control={form.control}
              name='theme.frontend'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Frontend Theme')}</FormLabel>
                  <Select
                    items={[
                      { value: 'default', label: t('Default (New Frontend)') },
                      {
                        value: 'classic',
                        label: t('Classic (Legacy Frontend)'),
                      },
                    ]}
                    onValueChange={field.onChange}
                    value={field.value}
                  >
                    <FormControl>
                      <SelectTrigger className='w-full'>
                        <SelectValue />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        <SelectItem value='default'>
                          {t('Default (New Frontend)')}
                        </SelectItem>
                        <SelectItem value='classic'>
                          {t('Classic (Legacy Frontend)')}
                        </SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FormDescription>
                    {t(
                      'Switch between the new frontend and the classic frontend. Changes take effect after page reload.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='SystemName'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('System Name')}</FormLabel>
                  <FormControl>
                    <Input placeholder={t('New API')} {...field} />
                  </FormControl>
                  <FormDescription>
                    {t('The name displayed across the application')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='ServerAddress'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Server Address')}</FormLabel>
                  <FormControl>
                    <Input placeholder='https://yourdomain.com' {...field} />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'The public URL of your server, used for OAuth callbacks, webhooks, and other external integrations'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <div className='flex flex-col gap-3'>
              <div className='flex items-start justify-between gap-3'>
                <div className='flex min-w-0 flex-col gap-1'>
                  <h3 className='text-sm font-medium'>{t('API Base URL')}</h3>
                  <p className='text-muted-foreground text-sm'>
                    {t('The API endpoint shown to users on the API Keys page')}
                  </p>
                </div>
                <Button
                  type='button'
                  variant='outline'
                  onClick={() => appendApiBaseUrl(createApiBaseUrlItem())}
                  disabled={isSubmitting || updateOption.isPending}
                >
                  <Plus data-icon='inline-start' />
                  {t('Add API address')}
                </Button>
              </div>

              <div className='flex flex-col gap-3'>
                {apiBaseUrlFields.map((item, index) => (
                  <div
                    key={item.id}
                    className='grid gap-3 rounded-lg border p-3 md:grid-cols-[minmax(10rem,0.7fr)_minmax(16rem,1fr)_2.5rem]'
                  >
                    <FormField
                      control={form.control}
                      name={`ApiBaseUrl.${index}.title`}
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Chinese Title')}</FormLabel>
                          <FormControl>
                            <Input
                              placeholder={t('e.g. Domestic API')}
                              disabled={isSubmitting || updateOption.isPending}
                              {...field}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    <FormField
                      control={form.control}
                      name={`ApiBaseUrl.${index}.url`}
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('URL')}</FormLabel>
                          <FormControl>
                            <Input
                              placeholder='https://api.xingyuapi.com'
                              disabled={isSubmitting || updateOption.isPending}
                              {...field}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    <div className='flex items-end justify-end'>
                      <Button
                        type='button'
                        variant='ghost'
                        size='icon'
                        onClick={() => removeApiBaseUrl(index)}
                        disabled={isSubmitting || updateOption.isPending}
                        aria-label={t('Remove')}
                      >
                        <Trash2 />
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            </div>

            <FormField
              control={form.control}
              name='Logo'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Logo URL')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t('https://example.com/logo.png')}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('URL to your logo image (optional)')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='Footer'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Footer')}</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder={t(
                        '© 2025 Your Company. All rights reserved.'
                      )}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Footer text displayed at the bottom of pages')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='About'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('About')}</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder={t(
                        'Enter HTML code (e.g., <p>About us...</p>) or a URL (e.g., https://example.com) to embed as iframe'
                      )}
                      rows={4}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Supports HTML markup or iframe embedding. Enter HTML code directly, or provide a complete URL to automatically embed it as an iframe.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='HomePageContent'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Home Page Content')}</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder={t('Welcome to our New API...')}
                      rows={6}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Content displayed on the home page (supports Markdown)'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='legal.user_agreement'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('User Agreement')}</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder={t(
                        'Provide Markdown, HTML, or an external URL for the user agreement'
                      )}
                      rows={6}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Leave empty to disable the agreement requirement. Supports Markdown, HTML, or a full URL to redirect users.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='legal.privacy_policy'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Privacy Policy')}</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder={t(
                        'Provide Markdown, HTML, or an external URL for the privacy policy'
                      )}
                      rows={6}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Leave empty to disable the privacy policy requirement. Supports Markdown, HTML, or a full URL to redirect users.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <div className='flex gap-2'>
              <Button
                type='submit'
                disabled={isSubmitting || updateOption.isPending}
              >
                {updateOption.isPending ? t('Saving...') : t('Save Changes')}
              </Button>
              <Button
                type='button'
                variant='outline'
                onClick={handleReset}
                disabled={!isDirty || updateOption.isPending || isSubmitting}
              >
                <RotateCcw className='mr-2 h-4 w-4' />
                {t('Reset')}
              </Button>
            </div>
          </form>
        </Form>
      </SettingsSection>
    </>
  )
}
