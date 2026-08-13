/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useState } from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { RefreshCw, ShieldAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
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
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { SettingsSection } from '../components/settings-section'
import {
  useSyncCodexVersion,
  useUpdateCodexSettings,
} from '../hooks/use-codex-settings'
import { useResetForm } from '../hooks/use-reset-form'

const versionPattern = /^$|^[0-9]+(?:\.[0-9]+){1,3}(?:-[0-9A-Za-z.]+)?$/
const schema = z.object({
  'codex_setting.client_version': z
    .string()
    .trim()
    .max(64)
    .regex(versionPattern, 'Invalid version'),
  'codex_setting.version_auto_sync_enabled': z.boolean(),
  'codex_setting.disable_identity_enforcement': z.boolean(),
  'codex_setting.routing_hint_enabled': z.boolean(),
  'codex_setting.fingerprint_default_mode': z.enum([
    'off',
    'device',
    'session',
    'full',
  ]),
})

type CodexFormValues = z.infer<typeof schema>
type CodexSettingsCardProps = {
  defaultValues: CodexFormValues & {
    syncedVersion: string
    versionSyncedAt: number
    versionSyncStatus: string
    versionSyncError: string
    versionSyncAttemptedAt: number
  }
}

export function CodexSettingsCard({ defaultValues }: CodexSettingsCardProps) {
  const { t } = useTranslation()
  const updateCodexSettings = useUpdateCodexSettings()
  const syncCodexVersion = useSyncCodexVersion()
  const [showIdentityWarning, setShowIdentityWarning] = useState(false)
  const formDefaults: CodexFormValues = {
    'codex_setting.client_version':
      defaultValues['codex_setting.client_version'],
    'codex_setting.version_auto_sync_enabled':
      defaultValues['codex_setting.version_auto_sync_enabled'],
    'codex_setting.disable_identity_enforcement':
      defaultValues['codex_setting.disable_identity_enforcement'],
    'codex_setting.routing_hint_enabled':
      defaultValues['codex_setting.routing_hint_enabled'],
    'codex_setting.fingerprint_default_mode':
      defaultValues['codex_setting.fingerprint_default_mode'],
  }
  const form = useForm<CodexFormValues>({
    resolver: zodResolver(schema),
    defaultValues: formDefaults,
  })
  useResetForm(form, formDefaults)

  const onSubmit = async (values: CodexFormValues) => {
    // Codex settings form a single outbound policy. Submit the complete snapshot
    // so a stale default cannot silently turn Save Changes into a no-op.
    await updateCodexSettings.mutateAsync({ values })
  }

  const syncedAt = defaultValues.versionSyncedAt
    ? new Date(defaultValues.versionSyncedAt * 1000).toLocaleString()
    : t('Not synchronized yet')
  const attemptedAt = defaultValues.versionSyncAttemptedAt
    ? new Date(defaultValues.versionSyncAttemptedAt * 1000).toLocaleString()
    : t('Not synchronized yet')
  const fixedVersion = defaultValues['codex_setting.client_version'].trim()
  const effectiveVersion =
    fixedVersion || defaultValues.syncedVersion || '0.146.0'
  const effectiveVersionSource = fixedVersion
    ? t('Administrator fixed version')
    : defaultValues.syncedVersion
      ? t('Official synchronized version')
      : t('Compiled fallback version')
  const syncStatus = defaultValues.versionSyncStatus || 'idle'
  const syncFailed = syncStatus === 'failed'
  const syncInProgress = syncStatus === 'syncing' || syncCodexVersion.isPending

  return (
    <SettingsSection
      title={t('Codex OAuth outbound policy')}
      description={t(
        'Converge OAuth client identity, routing hints, and device fingerprints after all request overrides.'
      )}
    >
      {syncFailed && (
        <Alert variant='destructive'>
          <ShieldAlert className='h-4 w-4' />
          <AlertTitle>
            {t('Official version synchronization failed')}
          </AlertTitle>
          <AlertDescription>
            <div className='space-y-2'>
              <p>
                {defaultValues.versionSyncError ||
                  t(
                    'GitHub is unavailable. The last usable version remains active.'
                  )}
              </p>
              <p className='text-xs'>
                {t('Last attempt')}: {attemptedAt}
              </p>
            </div>
          </AlertDescription>
        </Alert>
      )}

      <Form {...form}>
        <form onSubmit={form.handleSubmit(onSubmit)} className='space-y-6'>
          <FormField
            control={form.control}
            name='codex_setting.client_version'
            render={({ field }) => (
              <FormItem className='max-w-md'>
                <FormLabel>{t('Fixed Codex client version')}</FormLabel>
                <FormControl>
                  <Input {...field} placeholder='0.146.0' autoComplete='off' />
                </FormControl>
                <FormDescription>
                  {t(
                    'Version priority: administrator fixed version, official stable synchronized version, then the compiled fallback. Leave empty to follow stable releases.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className='grid gap-4 sm:grid-cols-2'>
            <FormItem>
              <FormLabel>{t('Official stable version')}</FormLabel>
              <div className='flex gap-2'>
                <Input value={defaultValues.syncedVersion || '—'} disabled />
                <Button
                  type='button'
                  variant='outline'
                  onClick={() => syncCodexVersion.mutate()}
                  disabled={syncInProgress}
                >
                  <RefreshCw className={syncInProgress ? 'animate-spin' : ''} />
                  {t('Sync now')}
                </Button>
              </div>
            </FormItem>
            <FormItem>
              <FormLabel>{t('Last version synchronization')}</FormLabel>
              <Input value={syncedAt} disabled />
            </FormItem>
          </div>

          <div className='grid gap-4 sm:grid-cols-2'>
            <FormItem>
              <FormLabel>{t('Effective Codex version')}</FormLabel>
              <div className='flex items-center gap-2'>
                <Input value={effectiveVersion} disabled />
                <Badge variant={syncFailed ? 'destructive' : 'secondary'}>
                  {effectiveVersionSource}
                </Badge>
              </div>
              <FormDescription>
                {t(
                  'This is the version used for outbound OAuth identity headers.'
                )}
              </FormDescription>
            </FormItem>
            <FormItem>
              <FormLabel>{t('Synchronization status')}</FormLabel>
              <Input
                value={
                  syncInProgress
                    ? t('Synchronizing')
                    : syncFailed
                      ? t('Failed')
                      : syncStatus === 'success'
                        ? t('Success')
                        : t('Not synchronized yet')
                }
                disabled
              />
            </FormItem>
          </div>

          <FormField
            control={form.control}
            name='codex_setting.version_auto_sync_enabled'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between gap-4'>
                <div>
                  <FormLabel>
                    {t('Synchronize official stable versions')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'Checks the official OpenAI Codex releases every six hours and never downgrades.'
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
            name='codex_setting.routing_hint_enabled'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between gap-4'>
                <div>
                  <FormLabel>{t('Codex routing hint')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Builds the routing hint from the final upstream model and priority or flex service tier.'
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
            name='codex_setting.fingerprint_default_mode'
            render={({ field }) => (
              <FormItem>
                <FormLabel>
                  {t('Default Codex fingerprint convergence')}
                </FormLabel>
                <FormDescription>
                  {t(
                    'Accounts may override this default. It only applies to ordinary OAuth Responses requests.'
                  )}
                </FormDescription>
                <FormControl>
                  <ToggleGroup
                    variant='segmented'
                    value={[field.value]}
                    onValueChange={(values) => {
                      const value = values[0] as
                        | CodexFormValues['codex_setting.fingerprint_default_mode']
                        | undefined
                      if (value) field.onChange(value)
                    }}
                    aria-label={t('Default Codex fingerprint convergence')}
                    className='grid w-full grid-cols-2 gap-2 sm:max-w-xl sm:grid-cols-4'
                  >
                    <ToggleGroupItem value='off'>{t('Off')}</ToggleGroupItem>
                    <ToggleGroupItem value='device'>
                      {t('Device')}
                    </ToggleGroupItem>
                    <ToggleGroupItem value='session'>
                      {t('Session')}
                    </ToggleGroupItem>
                    <ToggleGroupItem value='full'>{t('Full')}</ToggleGroupItem>
                  </ToggleGroup>
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='codex_setting.disable_identity_enforcement'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between gap-4'>
                <div>
                  <FormLabel>
                    {t('Allow custom OAuth client identity')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'Compatibility escape hatch. When enabled, valid custom identity headers are preserved.'
                    )}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={(checked) => {
                      if (checked) {
                        setShowIdentityWarning(true)
                        return
                      }
                      field.onChange(false)
                    }}
                  />
                </FormControl>
              </FormItem>
            )}
          />

          <Button type='submit' disabled={updateCodexSettings.isPending}>
            {updateCodexSettings.isPending ? t('Saving...') : t('Save Changes')}
          </Button>
        </form>
      </Form>

      <AlertDialog
        open={showIdentityWarning}
        onOpenChange={setShowIdentityWarning}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Allow custom OAuth client identity?')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This compatibility escape hatch preserves custom identity headers and may cause upstream OAuth validation failures. Enable it only when required.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                form.reset({
                  ...form.getValues(),
                  'codex_setting.disable_identity_enforcement': true,
                })
                setShowIdentityWarning(false)
              }}
            >
              {t('Enable compatibility mode')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  )
}
