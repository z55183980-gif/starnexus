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
import { useEffect, useRef, useState } from 'react'
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
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { SupportSettingsVisualEditor } from './support-settings-visual-editor'
import {
  formatSupportChannelsForEditor,
  normalizeSupportChannelsJson,
  parseSupportChannelsConfig,
} from './utils'

function createSupportSettingsSchema(t: (key: string) => string) {
  return z.object({
    SupportChannels: z.string().superRefine((value, ctx) => {
      try {
        const config = parseSupportChannelsConfig(value)
        if (!config.panelTitle.trim()) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: t('Panel title is required'),
          })
        }
        const ids = new Set<string>()
        for (const channel of config.channels) {
          if (ids.has(channel.id)) {
            ctx.addIssue({
              code: z.ZodIssueCode.custom,
              message: `${t('Duplicate channel ID')}: ${channel.id}`,
            })
            return
          }
          ids.add(channel.id)
        }
      } catch {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: t('Invalid JSON string.'),
        })
      }
    }),
  })
}

type SupportSettingsFormValues = {
  SupportChannels: string
}

type SupportSettingsSectionProps = {
  defaultValue: string
}

export function SupportSettingsSection({
  defaultValue,
}: SupportSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [editMode, setEditMode] = useState<'visual' | 'json'>('visual')
  const supportSchema = createSupportSettingsSchema(t)
  const formatted = formatSupportChannelsForEditor(defaultValue)

  const form = useForm<SupportSettingsFormValues>({
    resolver: zodResolver(supportSchema),
    mode: 'onChange',
    defaultValues: {
      SupportChannels: formatted,
    },
  })

  const initialNormalizedRef = useRef(
    normalizeSupportChannelsJson(defaultValue)
  )

  useEffect(() => {
    const nextFormatted = formatSupportChannelsForEditor(defaultValue)
    form.reset({ SupportChannels: nextFormatted })
    initialNormalizedRef.current = normalizeSupportChannelsJson(defaultValue)
  }, [defaultValue, form])

  const onSubmit = async (values: SupportSettingsFormValues) => {
    const normalized = normalizeSupportChannelsJson(values.SupportChannels)
    if (normalized === initialNormalizedRef.current) {
      return
    }

    await updateOption.mutateAsync({
      key: 'SupportChannels',
      value: normalized,
    })
    initialNormalizedRef.current = normalized
  }

  const config = parseSupportChannelsConfig(form.watch('SupportChannels'))

  const updateConfigField = (
    patch: Partial<ReturnType<typeof parseSupportChannelsConfig>>
  ) => {
    const current = parseSupportChannelsConfig(form.getValues('SupportChannels'))
    form.setValue(
      'SupportChannels',
      formatSupportChannelsForEditor(
        JSON.stringify({ ...current, ...patch })
      ),
      { shouldDirty: true, shouldValidate: true }
    )
  }

  return (
    <SettingsSection
      title={t('Customer Support Settings')}
      description={t(
        'Configure the floating customer support widget: channels, links, and QR code image URLs.'
      )}
    >
      <Form {...form}>
        <form onSubmit={form.handleSubmit(onSubmit)} className='space-y-6'>
          <div className='grid gap-4 md:grid-cols-2'>
            <FormItem className='flex items-center justify-between rounded-lg border p-4 md:col-span-2'>
              <div className='space-y-0.5'>
                <FormLabel>{t('Enable support widget')}</FormLabel>
                <FormDescription>
                  {t(
                    'When disabled, the floating support entry can be hidden on the user site (after the widget reads this setting).'
                  )}
                </FormDescription>
              </div>
              <Switch
                checked={config.enabled}
                onCheckedChange={(checked) =>
                  updateConfigField({ enabled: checked })
                }
              />
            </FormItem>

            <FormItem>
              <FormLabel>{t('Support panel title')}</FormLabel>
              <FormControl>
                <Input
                  value={config.panelTitle}
                  onChange={(event) =>
                    updateConfigField({ panelTitle: event.target.value })
                  }
                />
              </FormControl>
            </FormItem>

            <FormItem>
              <FormLabel>{t('Support panel subtitle')}</FormLabel>
              <FormControl>
                <Input
                  value={config.panelSubtitle}
                  onChange={(event) =>
                    updateConfigField({ panelSubtitle: event.target.value })
                  }
                />
              </FormControl>
            </FormItem>
          </div>

          <Tabs
            value={editMode}
            onValueChange={(value) => setEditMode(value as 'visual' | 'json')}
          >
            <TabsList className='grid w-full grid-cols-2'>
              <TabsTrigger value='visual'>{t('Visual')}</TabsTrigger>
              <TabsTrigger value='json'>{t('JSON')}</TabsTrigger>
            </TabsList>

            <TabsContent value='visual' className='mt-6'>
              <FormField
                control={form.control}
                name='SupportChannels'
                render={({ field }) => (
                  <FormItem>
                    <FormControl>
                      <SupportSettingsVisualEditor
                        value={field.value}
                        onChange={field.onChange}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </TabsContent>

            <TabsContent value='json' className='mt-6'>
              <FormField
                control={form.control}
                name='SupportChannels'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Support channels JSON')}</FormLabel>
                    <FormControl>
                      <Textarea rows={16} {...field} />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'JSON object with enabled, panelTitle, panelSubtitle, and channels array. Each channel supports types: link, chatway, qrcode.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </TabsContent>
          </Tabs>

          <Button type='submit' disabled={updateOption.isPending}>
            {updateOption.isPending
              ? t('Saving...')
              : t('Save support settings')}
          </Button>
        </form>
      </Form>
    </SettingsSection>
  )
}
