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
import { useEffect, useMemo, useRef, useState, type ChangeEvent } from 'react'
import * as z from 'zod'
import type { Resolver } from 'react-hook-form'
import { useFieldArray } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import {
  Add01Icon,
  Cancel01Icon,
  Delete02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { Switch } from '@/components/ui/switch'
import { searchUsers } from '@/features/users/api'
import type { User } from '@/features/users/types'
import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import { SettingsSection } from '../components/settings-section'
import { useSettingsForm } from '../hooks/use-settings-form'
import { useUpdateOption } from '../hooks/use-update-option'

const tokenPricingRuleSchema = z.object({
  name: z.string(),
  enabled: z.boolean(),
  input_ratio: z.coerce.number().min(0),
  output_ratio: z.coerce.number().min(0),
  models_text: z.string(),
  groups_text: z.string(),
  user_ids_text: z.string().refine(
    (value) =>
      !value ||
      value
        .split(',')
        .map((item) => item.trim())
        .filter(Boolean)
        .every((item) => /^[1-9]\d*$/.test(item)),
    'User IDs must be comma-separated positive integers.'
  ),
})

const tokenPricingSchema = z.object({
  enabled: z.boolean(),
  rules: z.array(tokenPricingRuleSchema),
})

type TokenPricingFormValues = z.infer<typeof tokenPricingSchema>
type TokenPricingRuleValues = TokenPricingFormValues['rules'][number]

type TokenPricingSettingsSectionProps = {
  defaultValues: TokenPricingFormValues
  groupOptions: string[]
}

function splitList(value: string | undefined): string[] {
  return (value || '')
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function parseUserIds(value: string | undefined): number[] {
  return splitList(value).map((item) => Number(item))
}

function formatUserLabel(user: User): string {
  const displayName =
    user.display_name && user.display_name !== user.username
      ? ` · ${user.display_name}`
      : ''
  return `${user.username}${displayName} (#${user.id})`
}

function getVisibleBadgeCount(labels: string[], width: number): number {
  if (labels.length === 0) {
    return 0
  }
  if (!width) {
    return Math.min(labels.length, 2)
  }
  const reservedWidth = labels.length > 1 ? 36 : 0
  let usedWidth = 0
  let count = 0
  for (const label of labels) {
    const badgeWidth = Math.min(Math.max(label.length * 7 + 38, 64), 148)
    if (usedWidth + badgeWidth + reservedWidth > width && count > 0) {
      break
    }
    usedWidth += badgeWidth + 4
    count += 1
  }
  return Math.max(1, count)
}

function UserSelector(props: {
  value: string
  onChange: (value: string) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [selectedUsers, setSelectedUsers] = useState<Record<number, User>>({})
  const triggerRef = useRef<HTMLDivElement>(null)
  const [triggerWidth, setTriggerWidth] = useState(0)
  const selectedIds = useMemo(() => parseUserIds(props.value), [props.value])

  const usersQuery = useQuery({
    queryKey: ['token-pricing-users', keyword],
    queryFn: () => searchUsers({ keyword, page_size: 20 }),
    enabled: open && !props.disabled,
  })

  const users = usersQuery.data?.data?.items ?? []
  const usersById = new Map([
    ...Object.values(selectedUsers).map((user) => [user.id, user] as const),
    ...users.map((user) => [user.id, user] as const),
  ])
  const selectedLabels = selectedIds.map((userId) => {
    const user = usersById.get(userId)
    return user ? user.username : `#${userId}`
  })
  const visibleCount = getVisibleBadgeCount(selectedLabels, triggerWidth)

  useEffect(() => {
    const element = triggerRef.current
    if (!element) {
      return
    }
    setTriggerWidth(element.clientWidth)
    const observer = new ResizeObserver((entries) => {
      setTriggerWidth(entries[0]?.contentRect.width ?? 0)
    })
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  const updateSelected = (ids: number[]) => {
    props.onChange(ids.join(', '))
  }

  const toggleUser = (user: User) => {
    const userId = user.id
    if (selectedIds.includes(userId)) {
      updateSelected(selectedIds.filter((id) => id !== userId))
      return
    }
    setSelectedUsers((current) => ({ ...current, [userId]: user }))
    updateSelected([...selectedIds, userId])
  }

  const removeUser = (userId: number) => {
    updateSelected(selectedIds.filter((id) => id !== userId))
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            type='button'
            variant='outline'
            disabled={props.disabled}
            className='h-9 w-full justify-start overflow-hidden px-2'
          >
            <div
              ref={triggerRef}
              className='flex min-w-0 flex-1 items-center gap-1 overflow-hidden'
            >
              {selectedIds.length === 0 ? (
                <span className='text-muted-foreground truncate'>
                  {t('Search users...')}
                </span>
              ) : (
                selectedIds.slice(0, visibleCount).map((userId) => {
                  const user = usersById.get(userId)
                  return (
                    <Badge key={userId} variant='secondary'>
                      {user ? user.username : `#${userId}`}
                      <span
                        role='button'
                        tabIndex={0}
                        className='ml-1 inline-flex'
                        onClick={(event) => {
                          event.preventDefault()
                          event.stopPropagation()
                          removeUser(userId)
                        }}
                        onKeyDown={(event) => {
                          if (event.key === 'Enter' || event.key === ' ') {
                            event.preventDefault()
                            event.stopPropagation()
                            removeUser(userId)
                          }
                        }}
                      >
                        <HugeiconsIcon icon={Cancel01Icon} />
                      </span>
                    </Badge>
                  )
                })
              )}
              {selectedIds.length > visibleCount && (
                <span className='text-muted-foreground text-xs'>
                  +{selectedIds.length - visibleCount}
                </span>
              )}
            </div>
          </Button>
        }
      />
      <PopoverContent className='w-80 p-0' align='start'>
        <Command shouldFilter={false}>
          <CommandInput
            placeholder={t('Search users...')}
            value={keyword}
            onValueChange={setKeyword}
          />
          <CommandEmpty>
            {usersQuery.isFetching ? t('Loading...') : t('No users found.')}
          </CommandEmpty>
          <CommandList>
            <CommandGroup>
              {users.map((user) => (
                <CommandItem
                  key={user.id}
                  value={String(user.id)}
                  data-checked={selectedIds.includes(user.id)}
                  onSelect={() => toggleUser(user)}
                >
                  <span className='truncate'>{formatUserLabel(user)}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

function GroupSelector(props: {
  value: string
  options: string[]
  onChange: (value: string) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const selectedGroups = useMemo(() => splitList(props.value), [props.value])
  const options = useMemo(
    () => Array.from(new Set([...props.options, ...selectedGroups])).sort(),
    [props.options, selectedGroups]
  )

  const toggleGroup = (group: string) => {
    const nextGroups = selectedGroups.includes(group)
      ? selectedGroups.filter((item) => item !== group)
      : [...selectedGroups, group]
    props.onChange(nextGroups.join(', '))
  }

  const removeGroup = (group: string) => {
    props.onChange(selectedGroups.filter((item) => item !== group).join(', '))
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            type='button'
            variant='outline'
            disabled={props.disabled}
            className='h-9 w-full justify-start overflow-hidden px-2'
          >
            <div className='flex min-w-0 flex-1 items-center gap-1 overflow-hidden'>
              {selectedGroups.length === 0 ? (
                <span className='text-muted-foreground truncate'>
                  {t('Select groups...')}
                </span>
              ) : (
                selectedGroups.map((group) => (
                  <Badge key={group} variant='secondary'>
                    {group}
                    <span
                      role='button'
                      tabIndex={0}
                      className='ml-1 inline-flex'
                      onClick={(event) => {
                        event.preventDefault()
                        event.stopPropagation()
                        removeGroup(group)
                      }}
                      onKeyDown={(event) => {
                        if (event.key === 'Enter' || event.key === ' ') {
                          event.preventDefault()
                          event.stopPropagation()
                          removeGroup(group)
                        }
                      }}
                    >
                      <HugeiconsIcon icon={Cancel01Icon} />
                    </span>
                  </Badge>
                ))
              )}
            </div>
          </Button>
        }
      />
      <PopoverContent className='w-72 p-0' align='start'>
        <Command>
          <CommandInput placeholder={t('Search groups...')} />
          <CommandEmpty>{t('No groups found.')}</CommandEmpty>
          <CommandList>
            <CommandGroup>
              {options.map((group) => (
                <CommandItem
                  key={group}
                  value={group}
                  data-checked={selectedGroups.includes(group)}
                  onSelect={() => toggleGroup(group)}
                >
                  <span className='truncate'>{group}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

function joinList(values: unknown): string {
  return Array.isArray(values)
    ? values
        .map((item) => String(item).trim())
        .filter(Boolean)
        .join(', ')
    : ''
}

function createRule(): TokenPricingRuleValues {
  return {
    name: '',
    enabled: true,
    input_ratio: 1,
    output_ratio: 1,
    models_text: '',
    groups_text: '',
    user_ids_text: '',
  }
}

export function parseTokenPricingDefaults(
  raw: string | undefined
): TokenPricingFormValues {
  if (!raw) {
    return { enabled: false, rules: [] }
  }
  try {
    const parsed = JSON.parse(raw) as Record<string, unknown>
    const rules = Array.isArray(parsed.rules)
      ? parsed.rules.map((rule) => {
          const data = rule as Record<string, unknown>
          return {
            name: typeof data.name === 'string' ? data.name : '',
            enabled: typeof data.enabled === 'boolean' ? data.enabled : true,
            input_ratio:
              typeof data.input_ratio === 'number' ? data.input_ratio : 1,
            output_ratio:
              typeof data.output_ratio === 'number' ? data.output_ratio : 1,
            models_text: joinList(data.models),
            groups_text: joinList(data.groups),
            user_ids_text: joinList(data.user_ids),
          }
        })
      : []

    if (
      rules.length === 0 &&
      (typeof parsed.input_ratio === 'number' ||
        typeof parsed.output_ratio === 'number')
    ) {
      rules.push({
        ...createRule(),
        name: 'Default',
        input_ratio:
          typeof parsed.input_ratio === 'number' ? parsed.input_ratio : 1,
        output_ratio:
          typeof parsed.output_ratio === 'number' ? parsed.output_ratio : 1,
      })
    }

    return {
      enabled: typeof parsed.enabled === 'boolean' ? parsed.enabled : false,
      rules,
    }
  } catch {
    return { enabled: false, rules: [] }
  }
}

export function TokenPricingSettingsSection({
  defaultValues,
  groupOptions,
}: TokenPricingSettingsSectionProps) {
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
    useSettingsForm<TokenPricingFormValues>({
      resolver: zodResolver(tokenPricingSchema) as Resolver<
        TokenPricingFormValues,
        unknown,
        TokenPricingFormValues
      >,
      defaultValues,
      onSubmit: async (values) => {
        await updateOption.mutateAsync({
          key: 'billing_setting.token_pricing',
          value: JSON.stringify({
            enabled: values.enabled,
            rules: values.rules.map((rule) => ({
              name: rule.name?.trim() || undefined,
              enabled: rule.enabled,
              input_ratio: rule.input_ratio,
              output_ratio: rule.output_ratio,
              models: splitList(rule.models_text),
              groups: splitList(rule.groups_text),
              user_ids: parseUserIds(rule.user_ids_text),
            })),
          }),
        })
      },
    })

  const { fields, append, remove } = useFieldArray({
    control: form.control,
    name: 'rules',
  })
  const tokenPricingEnabled = form.watch('enabled')
  const rulesDisabled =
    !tokenPricingEnabled || updateOption.isPending || isSubmitting

  return (
    <SettingsSection
      title={t('Token Pricing')}
      description={t(
        'Add pricing rules with scopes. If multiple rules match, the highest token ratio is used.'
      )}
    >
      <FormNavigationGuard when={isDirty} />

      <Form {...form}>
        <form onSubmit={handleSubmit} className='flex flex-col gap-6'>
          <FormDirtyIndicator isDirty={isDirty} />

          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                <div className='flex flex-col gap-1'>
                  <FormLabel className='text-base'>
                    {t('Enable Token Pricing')}
                  </FormLabel>
                  <p className='text-muted-foreground text-sm'>
                    {t(
                      'Matched rules adjust billable tokens before quota is calculated.'
                    )}
                  </p>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={updateOption.isPending}
                  />
                </FormControl>
              </FormItem>
            )}
          />

          <div className='flex items-center justify-between gap-3'>
            <div className='flex flex-col gap-1'>
              <h3 className='text-sm font-medium'>{t('Pricing Rules')}</h3>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Leave all scopes empty to apply a rule globally. Separate multiple values with commas.'
                )}
              </p>
            </div>
            <Button
              type='button'
              variant='outline'
              disabled={rulesDisabled}
              onClick={() => append(createRule())}
            >
              <HugeiconsIcon icon={Add01Icon} data-icon='inline-start' />
              {t('Add Rule')}
            </Button>
          </div>

          <div
            className={cn(
              'overflow-x-auto pb-1',
              rulesDisabled && 'opacity-50'
            )}
          >
            <div className='flex min-w-[1180px] flex-col gap-3'>
              {fields.map((field, index) => (
                <div
                  key={field.id}
                  className={cn(
                    'grid grid-cols-[7rem_7rem_7rem_7rem_minmax(15rem,1.15fr)_minmax(14rem,1fr)_minmax(17rem,1.25fr)_2.5rem] items-start gap-3 rounded-lg border p-3',
                    rulesDisabled && 'bg-muted/30'
                  )}
                >
                  <FormField
                    control={form.control}
                    name={`rules.${index}.enabled`}
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Status')}</FormLabel>
                        <FormControl>
                          <div className='flex h-9 items-center'>
                            <Switch
                              checked={field.value}
                              onCheckedChange={field.onChange}
                              disabled={rulesDisabled}
                            />
                          </div>
                        </FormControl>
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name={`rules.${index}.name`}
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Rule Name')}</FormLabel>
                        <FormControl>
                          <Input
                            className='h-9'
                            disabled={rulesDisabled}
                            {...field}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name={`rules.${index}.input_ratio`}
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Input Token Ratio')}</FormLabel>
                        <FormControl>
                          <Input
                            className='h-9'
                            type='number'
                            step='0.0001'
                            min='0'
                            disabled={rulesDisabled}
                            value={field.value ?? ''}
                            onChange={handleNumberChange(field.onChange)}
                            name={field.name}
                            onBlur={field.onBlur}
                            ref={field.ref}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name={`rules.${index}.output_ratio`}
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Output Token Ratio')}</FormLabel>
                        <FormControl>
                          <Input
                            className='h-9'
                            type='number'
                            step='0.0001'
                            min='0'
                            disabled={rulesDisabled}
                            value={field.value ?? ''}
                            onChange={handleNumberChange(field.onChange)}
                            name={field.name}
                            onBlur={field.onBlur}
                            ref={field.ref}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name={`rules.${index}.models_text`}
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Models')}</FormLabel>
                        <FormControl>
                          <Input
                            className='h-9'
                            disabled={rulesDisabled}
                            placeholder='gpt-4.1, claude-3-7-sonnet'
                            {...field}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name={`rules.${index}.groups_text`}
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Groups')}</FormLabel>
                        <FormControl>
                          <GroupSelector
                            value={field.value}
                            options={groupOptions}
                            onChange={field.onChange}
                            disabled={rulesDisabled}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name={`rules.${index}.user_ids_text`}
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Users')}</FormLabel>
                        <FormControl>
                          <UserSelector
                            value={field.value}
                            onChange={field.onChange}
                            disabled={rulesDisabled}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <div className='flex items-end justify-end pt-6'>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon'
                      disabled={rulesDisabled}
                      onClick={() => remove(index)}
                      aria-label={t('Delete Rule')}
                    >
                      <HugeiconsIcon icon={Delete02Icon} />
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          </div>

          <Button
            type='submit'
            disabled={updateOption.isPending || isSubmitting}
          >
            {updateOption.isPending ? t('Saving...') : t('Save Changes')}
          </Button>
        </form>
      </Form>
    </SettingsSection>
  )
}
