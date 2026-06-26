import type { ChangeEvent } from 'react'
import * as z from 'zod'
import type { Resolver } from 'react-hook-form'
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
import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import { SettingsSection } from '../components/settings-section'
import { useSettingsForm } from '../hooks/use-settings-form'
import { useUpdateOption } from '../hooks/use-update-option'

const affiliateSchema = z.object({
  AffiliateEnabled: z.boolean(),
  AffiliateRebateRate: z.coerce.number().min(0).max(100),
  AgentAffiliateRebateRate: z.coerce.number().min(0).max(100),
  AffiliateRebateFreezeHours: z.coerce.number().min(0).max(720),
  AffiliateRebateDurationDays: z.coerce.number().min(0).max(3650),
  AffiliateRebatePerInviteeCapUSD: z.coerce.number().min(0),
})

type AffiliateFormValues = z.infer<typeof affiliateSchema>

type AffiliateRebateSettingsSectionProps = {
  defaultValues: AffiliateFormValues
}

export function AffiliateRebateSettingsSection({
  defaultValues,
}: AffiliateRebateSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const handleNumberChange =
    (onChange: (value: number | string) => void) =>
    (event: ChangeEvent<HTMLInputElement>) => {
      onChange(
        event.target.value === '' ? '' : event.currentTarget.valueAsNumber
      )
    }

  const { form, handleSubmit, isDirty, isSubmitting } =
    useSettingsForm<AffiliateFormValues>({
      resolver: zodResolver(affiliateSchema) as Resolver<
        AffiliateFormValues,
        unknown,
        AffiliateFormValues
      >,
      defaultValues,
      onSubmit: async (_data, changedFields) => {
        for (const [key, value] of Object.entries(changedFields)) {
          await updateOption.mutateAsync({
            key,
            value: value as string | number | boolean,
          })
        }
      },
    })

  return (
    <SettingsSection
      title={t('Affiliate Rebate (USD)')}
      description={t(
        'Configure recharge-based referral rebates in USD.'
      )}
    >
      <FormNavigationGuard when={isDirty} />
      <Form {...form}>
        <form onSubmit={handleSubmit} className='space-y-4'>
          <FormField
            control={form.control}
            name='AffiliateEnabled'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between rounded-lg border p-4'>
                <div>
                  <FormLabel>{t('Enable affiliate rebate')}</FormLabel>
                  <FormDescription>
                    {t('When disabled, new recharge rebates are not accrued.')}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch checked={field.value} onCheckedChange={field.onChange} />
                </FormControl>
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='AffiliateRebateRate'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('User rebate rate (%)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    step='0.01'
                    {...field}
                    onChange={handleNumberChange(field.onChange)}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='AgentAffiliateRebateRate'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Agent rebate rate (%)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    step='0.01'
                    {...field}
                    onChange={handleNumberChange(field.onChange)}
                  />
                </FormControl>
                <FormDescription>
                  {t('Default rebate rate for agent users when no user-specific rate is set.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='AffiliateRebateFreezeHours'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Rebate freeze hours')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    {...field}
                    onChange={handleNumberChange(field.onChange)}
                  />
                </FormControl>
                <FormDescription>{t('0 = no freeze')}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='AffiliateRebateDurationDays'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Rebate validity per invitee (days)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    {...field}
                    onChange={handleNumberChange(field.onChange)}
                  />
                </FormControl>
                <FormDescription>{t('0 = unlimited')}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='AffiliateRebatePerInviteeCapUSD'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Max rebate per invitee (USD)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    step='0.01'
                    {...field}
                    onChange={handleNumberChange(field.onChange)}
                  />
                </FormControl>
                <FormDescription>{t('0 = unlimited')}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <div className='flex items-center gap-2'>
            <Button type='submit' disabled={!isDirty || isSubmitting}>
              {t('Save')}
            </Button>
            <FormDirtyIndicator isDirty={isDirty} />
          </div>
        </form>
      </Form>
    </SettingsSection>
  )
}
