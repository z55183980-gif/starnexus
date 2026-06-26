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
import { useMemo, useState } from 'react'
import { ChevronDown, ExternalLink, Headphones } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useSupportChannels } from '@/components/support-widget/use-support-channels'
import type { SupportChannel } from '@/features/system-settings/support/types'

const CONTACT_CHANNEL_IDS = ['wechat', 'telegram', 'whatsapp'] as const

const accentTriggerClassName =
  'inline-flex items-center gap-1.5 rounded-full bg-gradient-to-r from-cyan-500 via-sky-500 to-indigo-500 px-3.5 py-1.5 text-xs font-semibold text-white shadow-md shadow-cyan-500/30 ring-1 ring-white/25 transition-all duration-200 hover:brightness-110 hover:shadow-lg hover:shadow-cyan-500/35 active:scale-[0.98]'

const accentStackTitleClassName =
  'inline-flex items-center gap-2 rounded-xl bg-gradient-to-r from-cyan-500/15 via-sky-500/10 to-indigo-500/15 px-3 py-3 text-base font-semibold text-cyan-700 ring-1 ring-cyan-400/25 dark:text-cyan-50'

type ContactOwnerMenuProps = {
  className?: string
  /** Desktop nav link style (public header) */
  variant?: 'public' | 'dashboard' | 'stack'
  onItemClick?: () => void
}

function useContactChannels() {
  const config = useSupportChannels()

  return useMemo(() => {
    const byId = new Map(
      config.channels.map((channel) => [channel.id, channel])
    )
    return CONTACT_CHANNEL_IDS.map((id) => byId.get(id)).filter(
      (channel): channel is SupportChannel => Boolean(channel)
    )
  }, [config.channels])
}

function ContactQrcodeDialog({
  channel,
  onClose,
}: {
  channel: SupportChannel | null
  onClose: () => void
}) {
  const { t } = useTranslation()

  return (
    <Dialog
      open={Boolean(channel)}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <DialogContent className='sm:max-w-sm'>
        <DialogHeader>
          <DialogTitle>{channel?.label}</DialogTitle>
        </DialogHeader>
        {channel?.imageUrl ? (
          <img
            src={channel.imageUrl}
            alt={channel.label}
            className='border-border mx-auto h-56 w-56 rounded-lg border object-contain'
          />
        ) : (
          <p className='text-muted-foreground text-center text-sm'>
            {t('QR code is not configured. Please contact support.')}
          </p>
        )}
      </DialogContent>
    </Dialog>
  )
}

function ContactChannelItems({
  channels,
  onQrcode,
  onItemClick,
  itemClassName,
}: {
  channels: SupportChannel[]
  onQrcode: (channel: SupportChannel) => void
  onItemClick?: () => void
  itemClassName?: string
}) {
  const { t } = useTranslation()

  return (
    <>
      {channels.map((channel) => {
        const label = t(channel.label)

        if (channel.type === 'qrcode') {
          return (
            <button
              key={channel.id}
              type='button'
              className={cn(
                'hover:bg-accent flex w-full items-center justify-between rounded-md px-2 py-1.5 text-left text-sm transition-colors disabled:pointer-events-none disabled:opacity-50',
                itemClassName
              )}
              onClick={() => {
                onQrcode(channel)
                onItemClick?.()
              }}
            >
              <span>{label}</span>
            </button>
          )
        }

        const href = channel.url?.trim()
        if (!href) {
          return (
            <span
              key={channel.id}
              className={cn(
                'text-muted-foreground flex items-center justify-between px-2 py-1.5 text-sm opacity-50',
                itemClassName
              )}
            >
              {label}
            </span>
          )
        }

        return (
          <a
            key={channel.id}
            href={href}
            target={channel.openInNewTab === false ? '_self' : '_blank'}
            rel='noreferrer'
            className={cn(
              'hover:bg-accent flex items-center justify-between rounded-md px-2 py-1.5 text-sm transition-colors',
              itemClassName
            )}
            onClick={() => onItemClick?.()}
          >
            <span>{label}</span>
            <ExternalLink className='text-muted-foreground size-3.5' />
          </a>
        )
      })}
    </>
  )
}

export function ContactOwnerMenu({
  className,
  variant = 'public',
  onItemClick,
}: ContactOwnerMenuProps) {
  const { t } = useTranslation()
  const channels = useContactChannels()
  const [qrcodeChannel, setQrcodeChannel] = useState<SupportChannel | null>(
    null
  )

  if (channels.length === 0) {
    return null
  }

  if (variant === 'stack') {
    return (
      <>
        <div className={cn('flex flex-col gap-1', className)}>
          <span
            className={cn(accentStackTitleClassName, 'my-1 tracking-tight')}
          >
            <Headphones className='size-4 shrink-0' aria-hidden />
            {t('Contact site owner')}
          </span>
          <div className='flex flex-col gap-0.5 ps-3'>
            <ContactChannelItems
              channels={channels}
              onQrcode={setQrcodeChannel}
              onItemClick={onItemClick}
              itemClassName='py-2.5 text-base font-medium tracking-tight'
            />
          </div>
        </div>
        <ContactQrcodeDialog
          channel={qrcodeChannel}
          onClose={() => setQrcodeChannel(null)}
        />
      </>
    )
  }

  return (
    <>
      <DropdownMenu modal={false}>
        <DropdownMenuTrigger
          className={cn(accentTriggerClassName, className)}
          aria-label={t('Contact site owner')}
        >
          <Headphones className='size-3.5 shrink-0' aria-hidden />
          <span>{t('Contact site owner')}</span>
          <ChevronDown className='size-3 opacity-80' aria-hidden />
        </DropdownMenuTrigger>
        <DropdownMenuContent align='start' className='min-w-40'>
          {channels.map((channel) => {
            const label = t(channel.label)

            if (channel.type === 'qrcode') {
              return (
                <DropdownMenuItem
                  key={channel.id}
                  onClick={() => setQrcodeChannel(channel)}
                >
                  {label}
                </DropdownMenuItem>
              )
            }

            const href = channel.url?.trim()
            if (!href) {
              return (
                <DropdownMenuItem key={channel.id} disabled>
                  {label}
                </DropdownMenuItem>
              )
            }

            return (
              <DropdownMenuItem
                key={channel.id}
                render={
                  <a
                    href={href}
                    target={channel.openInNewTab === false ? '_self' : '_blank'}
                    rel='noreferrer'
                  />
                }
              >
                {label}
                <ExternalLink className='text-muted-foreground ms-auto size-3.5' />
              </DropdownMenuItem>
            )
          })}
        </DropdownMenuContent>
      </DropdownMenu>

      <ContactQrcodeDialog
        channel={qrcodeChannel}
        onClose={() => setQrcodeChannel(null)}
      />
    </>
  )
}
