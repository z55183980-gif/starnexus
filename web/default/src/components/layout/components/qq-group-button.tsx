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
import { QrCode } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  SUPPORT_QQ_GROUP_URL,
  SUPPORT_QQ_LOGO_URL,
} from '@/components/support-widget/constants'

function QQIcon({ className }: { className?: string }) {
  return (
    <img src={SUPPORT_QQ_LOGO_URL} alt='' className={className} aria-hidden />
  )
}
type QQGroupButtonProps = {
  className?: string
  variant?: 'public' | 'dashboard' | 'stack'
  onClick?: () => void
}

const publicButtonClassName =
  'inline-flex items-center gap-1.5 rounded-full bg-[#12B7F5] px-3.5 py-1.5 text-xs font-semibold text-white shadow-md shadow-sky-500/30 ring-1 ring-white/25 transition-all duration-200 hover:bg-[#0EA5E9] hover:shadow-lg hover:shadow-sky-500/35 active:scale-[0.98]'

const stackButtonClassName =
  'border-sky-400/25 bg-sky-400/10 text-sky-700 hover:border-sky-400/40 hover:bg-sky-400/15 dark:text-sky-50 inline-flex items-center justify-between gap-2 rounded-xl border px-3 py-3 text-base font-semibold tracking-tight transition-colors'

export function QQGroupButton({
  className,
  variant = 'public',
  onClick,
}: QQGroupButtonProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  return (
    <>
      <button
        type='button'
        className={cn(
          variant === 'stack' ? stackButtonClassName : publicButtonClassName,
          className
        )}
        onClick={() => {
          setOpen(true)
          onClick?.()
        }}
      >
        <span className='inline-flex items-center gap-1.5'>
          <QQIcon
            className={
              variant === 'stack' ? 'size-4 shrink-0' : 'size-3.5 shrink-0'
            }
          />
          <span>{t('Join QQ group')}</span>
        </span>
        {variant === 'stack' ? (
          <QrCode className='size-3.5 shrink-0 text-sky-600/80 dark:text-sky-100/80' />
        ) : null}
      </button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className='w-fit max-w-[calc(100vw-2rem)] sm:max-w-[calc(100vw-2rem)]'>
          <DialogHeader className='pr-8'>
            <DialogTitle>{t('Join QQ group')}</DialogTitle>
            <DialogDescription>
              {t('Scan the QQ group QR code to join')}
            </DialogDescription>
          </DialogHeader>
          <div className='inline-flex w-fit max-w-full rounded-xl bg-sky-500/10 p-2 ring-1 ring-sky-400/20'>
            <img
              src={SUPPORT_QQ_GROUP_URL}
              alt={t('Join QQ group')}
              className='border-border bg-background block h-auto max-h-[calc(100vh-10rem)] max-w-full rounded-lg border object-contain'
            />
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}
