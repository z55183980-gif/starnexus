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
import { useDeferredValue, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ArrowDown01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { Check } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  Command,
  CommandEmpty,
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
import { getChannels, searchChannels } from '@/features/channels/api'
import type { Channel } from '@/features/channels/types'

type ChannelComboboxProps = {
  value?: string
  onValueChange: (value: string) => void
  className?: string
  placeholder?: string
  disabled?: boolean
}

function formatChannelLabel(channel: Channel): string {
  return `${channel.name} (#${channel.id})`
}

function matchesChannel(channel: Channel, search: string): boolean {
  const idText = String(channel.id)
  const name = channel.name.toLowerCase()
  return name.includes(search) || idText.includes(search)
}

function mergeChannels(lists: Channel[][]): Channel[] {
  const byId = new Map<number, Channel>()
  for (const list of lists) {
    for (const channel of list) {
      byId.set(channel.id, channel)
    }
  }
  return Array.from(byId.values()).sort((a, b) => a.id - b.id)
}

export function ChannelCombobox(props: ChannelComboboxProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [searchValue, setSearchValue] = useState('')
  const deferredSearch = useDeferredValue(searchValue.trim())
  const search = deferredSearch.toLowerCase()
  const shouldLoad = open || !!props.value

  const baseQuery = useQuery({
    queryKey: ['channels', 'log-filter'],
    queryFn: async () => {
      const res = await getChannels({
        p: 1,
        page_size: 200,
        id_sort: true,
      })
      return res.data?.items ?? []
    },
    staleTime: 60_000,
    enabled: shouldLoad,
  })

  const searchQuery = useQuery({
    queryKey: ['channels', 'log-filter-search', deferredSearch],
    queryFn: async () => {
      const res = await searchChannels({
        keyword: deferredSearch,
        page_size: 100,
        id_sort: true,
      })
      return res.data?.items ?? []
    },
    staleTime: 30_000,
    enabled: shouldLoad && deferredSearch.length > 0,
  })

  const channels = useMemo(
    () => mergeChannels([baseQuery.data ?? [], searchQuery.data ?? []]),
    [baseQuery.data, searchQuery.data]
  )

  const filteredChannels = useMemo(() => {
    if (!search) return channels
    return channels.filter((channel) => matchesChannel(channel, search))
  }, [channels, search])

  const selectedChannel = useMemo(() => {
    if (!props.value) return undefined
    return channels.find((channel) => String(channel.id) === props.value)
  }, [channels, props.value])

  const displayLabel = selectedChannel
    ? formatChannelLabel(selectedChannel)
    : props.value
      ? `#${props.value}`
      : props.placeholder || t('Channel')

  const handleSelect = (channelId: string) => {
    props.onValueChange(channelId === props.value ? '' : channelId)
    setOpen(false)
    setSearchValue('')
  }

  const isFetching = baseQuery.isFetching || searchQuery.isFetching

  return (
    <Popover
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen)
        if (!nextOpen) setSearchValue('')
      }}
    >
      <PopoverTrigger
        render={
          <button
            type='button'
            role='combobox'
            aria-expanded={open}
            disabled={props.disabled}
            data-size='default'
            data-placeholder={!props.value ? '' : undefined}
            className={cn(
              'group/select-trigger border-input focus-visible:border-ring focus-visible:ring-ring/50 data-placeholder:text-muted-foreground dark:bg-input/30 dark:hover:bg-input/50 flex h-8 w-fit items-center justify-between gap-1.5 rounded-lg border bg-transparent py-2 pr-2 pl-2.5 text-sm whitespace-nowrap transition-colors outline-none select-none focus-visible:ring-3 disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0',
              !props.value && 'text-muted-foreground',
              props.className
            )}
          />
        }
      >
        <span className='line-clamp-1 flex-1 truncate text-left'>
          {displayLabel}
        </span>
        <HugeiconsIcon
          icon={ArrowDown01Icon}
          strokeWidth={2}
          className={cn(
            'text-muted-foreground pointer-events-none size-4 transition-transform duration-200',
            open && 'rotate-180'
          )}
        />
      </PopoverTrigger>
      <PopoverContent
        align='start'
        className='w-[var(--anchor-width)] min-w-[220px] p-0'
        onWheel={(event) => event.stopPropagation()}
        onTouchMove={(event) => event.stopPropagation()}
        onPointerDown={(event) => event.stopPropagation()}
      >
        <Command shouldFilter={false}>
          <CommandInput
            placeholder={t('Search...')}
            value={searchValue}
            onValueChange={setSearchValue}
          />
          <CommandList className='max-h-64'>
            <CommandEmpty>
              {isFetching ? t('Loading...') : t('No channels found')}
            </CommandEmpty>
            <CommandGroup>
              {props.value ? (
                <CommandItem
                  value='__clear__'
                  onSelect={() => {
                    props.onValueChange('')
                    setOpen(false)
                    setSearchValue('')
                  }}
                >
                  <Check className='size-4 opacity-0' />
                  <span className='text-muted-foreground'>
                    {t('Clear selection')}
                  </span>
                </CommandItem>
              ) : null}
              {filteredChannels.map((channel) => {
                const channelId = String(channel.id)
                const selected = props.value === channelId
                return (
                  <CommandItem
                    key={channel.id}
                    value={channelId}
                    onSelect={() => handleSelect(channelId)}
                  >
                    <Check
                      className={cn(
                        'size-4',
                        selected ? 'opacity-100' : 'opacity-0'
                      )}
                    />
                    <span className='truncate'>
                      {formatChannelLabel(channel)}
                    </span>
                  </CommandItem>
                )
              })}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
