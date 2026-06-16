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
import { useEffect } from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
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
import type { SupportChannel, SupportChannelType } from './types'

const channelTypes: SupportChannelType[] = ['link', 'chatway', 'qrcode']

function createSupportChannelDialogSchema(t: (key: string) => string) {
  return z
    .object({
      id: z
        .string()
        .min(1, t('Channel ID is required'))
        .regex(/^[a-z0-9_-]+$/, t('Channel ID must use lowercase letters, numbers, - or _')),
      label: z.string().min(1, t('Channel label is required')),
      type: z.enum(['link', 'chatway', 'qrcode']),
      enabled: z.boolean(),
      url: z.string().optional(),
      widgetId: z.string().optional(),
      imageUrl: z.string().optional(),
      openInNewTab: z.boolean().optional(),
    })
    .superRefine((values, ctx) => {
      if (values.type === 'link') {
        if (!values.url?.trim()) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: t('URL is required'),
            path: ['url'],
          })
        } else {
          try {
            const parsed = new URL(values.url)
            if (!['http:', 'https:'].includes(parsed.protocol)) {
              ctx.addIssue({
                code: z.ZodIssueCode.custom,
                message: t('URL must start with http:// or https://'),
                path: ['url'],
              })
            }
          } catch {
            ctx.addIssue({
              code: z.ZodIssueCode.custom,
              message: t('Invalid URL'),
              path: ['url'],
            })
          }
        }
      }
      if (values.type === 'chatway' && !values.widgetId?.trim()) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: t('Chatway widget ID is required'),
          path: ['widgetId'],
        })
      }
      if (values.type === 'qrcode') {
        if (!values.imageUrl?.trim()) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: t('QR code image URL is required'),
            path: ['imageUrl'],
          })
        } else {
          try {
            const parsed = new URL(values.imageUrl)
            if (!['http:', 'https:'].includes(parsed.protocol)) {
              ctx.addIssue({
                code: z.ZodIssueCode.custom,
                message: t('Image URL must start with http:// or https://'),
                path: ['imageUrl'],
              })
            }
          } catch {
            ctx.addIssue({
              code: z.ZodIssueCode.custom,
              message: t('Invalid image URL'),
              path: ['imageUrl'],
            })
          }
        }
      }
    })
}

export type SupportChannelFormValues = z.infer<
  ReturnType<typeof createSupportChannelDialogSchema>
>

type SupportChannelDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (data: SupportChannel) => void
  editData?: SupportChannel | null
  idReadOnly?: boolean
}

export function SupportChannelDialog({
  open,
  onOpenChange,
  onSave,
  editData,
  idReadOnly = false,
}: SupportChannelDialogProps) {
  const { t } = useTranslation()
  const isEditMode = Boolean(editData)
  const schema = createSupportChannelDialogSchema(t)

  const form = useForm<SupportChannelFormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      id: '',
      label: '',
      type: 'link',
      enabled: true,
      url: '',
      widgetId: '',
      imageUrl: '',
      openInNewTab: true,
    },
  })

  const channelType = form.watch('type')

  useEffect(() => {
    if (editData) {
      form.reset({
        id: editData.id,
        label: editData.label,
        type: editData.type,
        enabled: editData.enabled,
        url: editData.url ?? '',
        widgetId: editData.widgetId ?? '',
        imageUrl: editData.imageUrl ?? '',
        openInNewTab: editData.openInNewTab ?? true,
      })
      return
    }
    form.reset({
      id: '',
      label: '',
      type: 'link',
      enabled: true,
      url: '',
      widgetId: '',
      imageUrl: '',
      openInNewTab: true,
    })
  }, [editData, form, open])

  const handleSubmit = (values: SupportChannelFormValues) => {
    onSave({
      id: values.id.trim(),
      label: values.label.trim(),
      type: values.type,
      enabled: values.enabled,
      url: values.type === 'link' ? values.url?.trim() : undefined,
      widgetId: values.type === 'chatway' ? values.widgetId?.trim() : undefined,
      imageUrl: values.type === 'qrcode' ? values.imageUrl?.trim() : undefined,
      openInNewTab: values.type === 'link' ? values.openInNewTab : undefined,
    })
    form.reset()
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-[520px]'>
        <DialogHeader>
          <DialogTitle>
            {isEditMode ? t('Edit support channel') : t('Add support channel')}
          </DialogTitle>
          <DialogDescription>
            {t('Configure a customer support entry shown in the floating widget.')}
          </DialogDescription>
        </DialogHeader>

        <Form {...form}>
          <form
            onSubmit={form.handleSubmit(handleSubmit)}
            className='space-y-4'
          >
            <FormField
              control={form.control}
              name='id'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Channel ID')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t('e.g. wechat')}
                      disabled={idReadOnly}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Unique identifier. Cannot be changed after creation.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='label'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Channel label')}</FormLabel>
                  <FormControl>
                    <Input placeholder={t('WeChat')} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='type'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Channel type')}</FormLabel>
                  <Select
                    value={field.value}
                    onValueChange={field.onChange}
                    disabled={idReadOnly}
                  >
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue placeholder={t('Select channel type')} />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      {channelTypes.map((type) => (
                        <SelectItem key={type} value={type}>
                          {t(`support.channelType.${type}`)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='enabled'
              render={({ field }) => (
                <FormItem className='flex items-center justify-between rounded-lg border p-3'>
                  <div className='space-y-0.5'>
                    <FormLabel>{t('Enabled')}</FormLabel>
                    <FormDescription>
                      {t('Show this channel in the support panel when saved.')}
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

            {channelType === 'link' && (
              <>
                <FormField
                  control={form.control}
                  name='url'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('URL')}</FormLabel>
                      <FormControl>
                        <Input
                          placeholder='https://'
                          autoComplete='off'
                          {...field}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='openInNewTab'
                  render={({ field }) => (
                    <FormItem className='flex items-center justify-between rounded-lg border p-3'>
                      <div className='space-y-0.5'>
                        <FormLabel>{t('Open in new tab')}</FormLabel>
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
              </>
            )}

            {channelType === 'chatway' && (
              <FormField
                control={form.control}
                name='widgetId'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Chatway widget ID')}</FormLabel>
                    <FormControl>
                      <Input placeholder='XAUFzylpFcj9' {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}

            {channelType === 'qrcode' && (
              <FormField
                control={form.control}
                name='imageUrl'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('QR code image URL')}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder='https://example.com/wechat-qr.png'
                        autoComplete='off'
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'HTTPS URL to the WeChat or other QR code image. Upload the image elsewhere and paste the link here.'
                      )}
                    </FormDescription>
                    {field.value ? (
                      <img
                        src={field.value}
                        alt={t('QR code preview')}
                        className='border-border mt-2 h-28 w-28 rounded-md border object-contain'
                      />
                    ) : null}
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}

            <DialogFooter>
              <Button
                type='button'
                variant='outline'
                onClick={() => onOpenChange(false)}
              >
                {t('Cancel')}
              </Button>
              <Button type='submit'>{isEditMode ? t('Update') : t('Add')}</Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}
