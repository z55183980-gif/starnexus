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
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ArrowDown01Icon, Tick02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { cn } from '@/lib/utils'
import { useDebounce } from '@/hooks/use-debounce'
import { Button } from '@/components/ui/button'
import {
  Command,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { searchUsers } from '../api'

export type SelectedLogUser = { id: number; username: string }

type UserFilterSelectProps = {
  value?: SelectedLogUser
  onChange: (user: SelectedLogUser | undefined) => void
  className?: string
  sensitiveVisible?: boolean
}

export function UserFilterSelect(props: UserFilterSelectProps) {
  const { t } = useTranslation()
  const viewer = useAuthStore((state) => state.auth.user)
  const [open, setOpen] = useState(false)
  const [input, setInput] = useState('')
  const keyword = useDebounce(input.trim(), 300)
  const pending = keyword !== input.trim()
  const query = useQuery({
    queryKey: ['log-user-options', viewer?.id, viewer?.role, keyword],
    enabled: open && !!keyword && !pending,
    queryFn: async () => {
      const result = await searchUsers({ keyword, page_size: 20 })
      if (!result.success)
        throw new Error(result.message || 'Failed to search users')
      return result.data
    },
    staleTime: 30_000,
  })
  const close = () => {
    setOpen(false)
    setInput('')
  }
  let label = t('Search and select a user')
  if (props.value) {
    label = `${props.value.username || t('User')} (#${props.value.id})`
    if (props.sensitiveVisible === false) label = '••••'
  }
  const loading = pending || query.isFetching
  const users =
    !loading && !query.isError && keyword ? (query.data?.items ?? []) : []
  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (!next) setInput('')
      }}
    >
      <PopoverTrigger
        render={
          <Button
            variant='outline'
            role='combobox'
            aria-expanded={open}
            aria-label={t('Username')}
            className={cn('justify-between', props.className)}
          />
        }
      >
        <span className='truncate'>{label}</span>
        <HugeiconsIcon icon={ArrowDown01Icon} data-icon='inline-end' />
      </PopoverTrigger>
      <PopoverContent
        align='start'
        className='w-80 max-w-[calc(100vw-2rem)] p-0'
      >
        <Command shouldFilter={false}>
          <CommandInput
            placeholder={t('Search by username...')}
            value={input}
            onValueChange={setInput}
          />
          <CommandList>
            {props.value && !input.trim() && (
              <CommandGroup>
                <CommandItem
                  value='clear-user'
                  onSelect={() => {
                    props.onChange(undefined)
                    close()
                  }}
                >
                  {t('Clear selection')}
                </CommandItem>
              </CommandGroup>
            )}
            {!keyword && !pending && (
              <p className='text-muted-foreground p-3 text-sm'>
                {t('Type to search users')}
              </p>
            )}
            {loading && (
              <p role='status' className='text-muted-foreground p-3 text-sm'>
                {t('Loading...')}
              </p>
            )}
            {query.isError && !loading && (
              <div role='alert' className='p-3 text-sm'>
                <p>{t('Failed to search users')}</p>
                <Button variant='ghost' onClick={() => void query.refetch()}>
                  {t('Retry')}
                </Button>
              </div>
            )}
            {keyword && !loading && !query.isError && users.length === 0 && (
              <p className='text-muted-foreground p-3 text-sm'>
                {t('No Users Found')}
              </p>
            )}
            <CommandGroup>
              {users.map((user) => (
                <CommandItem
                  key={user.id}
                  value={String(user.id)}
                  onSelect={() => {
                    props.onChange({ id: user.id, username: user.username })
                    close()
                  }}
                >
                  <HugeiconsIcon
                    icon={Tick02Icon}
                    className={cn(
                      'size-4',
                      props.value?.id !== user.id && 'opacity-0'
                    )}
                    aria-hidden='true'
                  />
                  <span className='min-w-0 flex-1'>
                    <span className='block truncate'>
                      {user.username}{' '}
                      <span className='text-muted-foreground'>#{user.id}</span>
                    </span>
                    {user.display_name && (
                      <span className='text-muted-foreground block truncate text-xs'>
                        {user.display_name}
                      </span>
                    )}
                  </span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
          {!!query.data &&
            users.length > 0 &&
            query.data.total > users.length && (
              <p className='text-muted-foreground border-t p-2 text-xs'>
                {t('More users match. Refine your search.')}
              </p>
            )}
        </Command>
      </PopoverContent>
    </Popover>
  )
}
