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
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import type { SupportChannel } from '@/features/system-settings/support/types'
import { useChatway, waitForChatwayAndOpen } from './use-chatway'
import { useSupportChannels } from './use-support-channels'

function SupportChatIcon() {
  return (
    <svg width={26} height={26} viewBox='0 0 24 24' fill='none' aria-hidden>
      <path
        d='M7 10.5h10M7 14h6.5'
        stroke='currentColor'
        strokeWidth={2}
        strokeLinecap='round'
      />
      <path
        d='M6.5 4.5h11A2.5 2.5 0 0 1 20 7v8a2.5 2.5 0 0 1-2.5 2.5H14l-3.2 2.4a.8.8 0 0 1-1.28-.64V17.5H6.5A2.5 2.5 0 0 1 4 15V7a2.5 2.5 0 0 1 2.5-2.5Z'
        stroke='currentColor'
        strokeWidth={2}
        strokeLinejoin='round'
      />
    </svg>
  )
}

function SupportChannelButton({
  channel,
  isFirst,
  onOpenChatway,
  onOpenQrcode,
}: {
  channel: SupportChannel
  isFirst: boolean
  onOpenChatway: () => void
  onOpenQrcode: (channel: SupportChannel) => void
}) {
  const spacing = isFirst ? '' : 'mt-2'
  const baseClass = cn(
    'border-border bg-muted/50 hover:bg-muted flex w-full items-center justify-between rounded-xl border px-3 py-3 text-left text-sm font-semibold',
    spacing
  )
  const accentClass = cn(
    'flex items-center justify-between rounded-xl border border-cyan-400/20 bg-cyan-400/10 px-3 py-3 text-sm font-semibold text-cyan-700 hover:bg-cyan-400/15 dark:text-cyan-50',
    spacing
  )

  if (channel.type === 'chatway') {
    return (
      <button
        type='button'
        className={baseClass}
        aria-label={channel.label}
        onClick={(event) => {
          event.stopPropagation()
          onOpenChatway()
        }}
      >
        <span>{channel.label}</span>
        <span className='text-muted-foreground'>→</span>
      </button>
    )
  }

  if (channel.type === 'qrcode') {
    return (
      <button
        type='button'
        className={baseClass}
        aria-label={channel.label}
        onClick={(event) => {
          event.stopPropagation()
          onOpenQrcode(channel)
        }}
      >
        <span>{channel.label}</span>
        <span className='text-muted-foreground'>→</span>
      </button>
    )
  }

  const href = channel.url?.trim()
  if (!href) return null

  const isAccent = channel.id === 'telegram'
  return (
    <a
      href={href}
      target={channel.openInNewTab === false ? '_self' : '_blank'}
      rel='noreferrer'
      className={isAccent ? accentClass : baseClass}
    >
      <span>{channel.label}</span>
      <span
        className={
          isAccent
            ? 'text-cyan-600/80 dark:text-cyan-100/80'
            : 'text-muted-foreground'
        }
      >
        ↗
      </span>
    </a>
  )
}

export function SupportWidget() {
  const { t } = useTranslation()
  const config = useSupportChannels()
  const [panelOpen, setPanelOpen] = useState(false)
  const [qrcodeChannel, setQrcodeChannel] = useState<SupportChannel | null>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const toggleRef = useRef<HTMLButtonElement>(null)

  const visibleChannels = useMemo(
    () => config.channels.filter((channel) => channel.enabled),
    [config.channels]
  )

  const chatwayWidgetId = useMemo(() => {
    const chatway = visibleChannels.find((channel) => channel.type === 'chatway')
    return chatway?.widgetId?.trim() || undefined
  }, [visibleChannels])

  useChatway(chatwayWidgetId)

  const setOpen = useCallback((next: boolean) => {
    setPanelOpen(next)
  }, [])

  const handleOpenChatway = useCallback(() => {
    waitForChatwayAndOpen()
    setOpen(false)
  }, [setOpen])

  useEffect(() => {
    if (!panelOpen) return

    const handleDocumentClick = (event: MouseEvent) => {
      const target = event.target
      if (!(target instanceof Node)) return
      if (toggleRef.current?.contains(target)) return
      if (panelRef.current?.contains(target)) return
      setOpen(false)
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }

    document.addEventListener('click', handleDocumentClick)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('click', handleDocumentClick)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [panelOpen, setOpen])

  if (!config.enabled || visibleChannels.length === 0) {
    return null
  }

  const toggleTitle = panelOpen ? t('Close support') : t('Open support')
  const panelTitle =
    config.panelTitle.trim() || t('StarNexus · Online support')
  const panelSubtitle =
    config.panelSubtitle.trim() ||
    t('7×24 hour round-the-clock online customer service')

  return (
    <>
      <div className='pointer-events-none fixed bottom-5 right-5 z-[60] sm:bottom-6 sm:right-6'>
        <div className='pointer-events-auto'>
          <div
            ref={panelRef}
            id='sn-support-panel'
            className={cn(
              'border-border bg-background/95 mb-3 w-[min(92vw,360px)] overflow-hidden rounded-2xl border shadow-2xl ring-1 ring-black/5 backdrop-blur dark:ring-white/10',
              panelOpen ? 'block' : 'hidden'
            )}
            aria-hidden={panelOpen ? 'false' : 'true'}
          >
            <div className='border-border bg-gradient-to-r from-cyan-500/15 via-fuchsia-500/10 to-indigo-500/15 border-b px-4 py-3'>
              <div className='text-foreground text-sm font-extrabold'>
                {panelTitle}
              </div>
              <div className='text-muted-foreground mt-1 text-xs'>
                {panelSubtitle}
              </div>
            </div>
            <div className='p-3'>
              {visibleChannels.map((channel, index) => (
                <SupportChannelButton
                  key={channel.id}
                  channel={channel}
                  isFirst={index === 0}
                  onOpenChatway={handleOpenChatway}
                  onOpenQrcode={setQrcodeChannel}
                />
              ))}
            </div>
          </div>
          <button
            ref={toggleRef}
            id='sn-support-toggle'
            type='button'
            className='group inline-flex h-14 w-14 items-center justify-center rounded-full bg-gradient-to-br from-cyan-400 via-sky-400 to-indigo-500 text-slate-950 shadow-2xl shadow-cyan-500/25 ring-1 ring-white/15 hover:brightness-110'
            aria-expanded={panelOpen}
            aria-controls='sn-support-panel'
            title={toggleTitle}
            onClick={() => setOpen(!panelOpen)}
          >
            <span className='sr-only'>{toggleTitle}</span>
            <SupportChatIcon />
          </button>
        </div>
      </div>

      <Dialog
        open={Boolean(qrcodeChannel)}
        onOpenChange={(open) => {
          if (!open) setQrcodeChannel(null)
        }}
      >
        <DialogContent className='sm:max-w-sm'>
          <DialogHeader>
            <DialogTitle>{qrcodeChannel?.label}</DialogTitle>
          </DialogHeader>
          {qrcodeChannel?.imageUrl ? (
            <img
              src={qrcodeChannel.imageUrl}
              alt={qrcodeChannel.label}
              className='border-border mx-auto h-56 w-56 rounded-lg border object-contain'
            />
          ) : null}
        </DialogContent>
      </Dialog>
    </>
  )
}
