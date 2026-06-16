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
import { useEffect, useState } from 'react'
import { Megaphone } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatDateTimeObject } from '@/lib/time'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Markdown } from '@/components/ui/markdown'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import type { AnnouncementItem } from '@/features/dashboard/types'
import {
  getLoginAnnouncementIds,
  useLoginAnnouncements,
} from '@/hooks/use-login-announcements'

function getAnnouncementKey(item: AnnouncementItem): string {
  if (item.id !== undefined && item.id !== null) {
    return `id:${item.id}`
  }
  return `content:${item.content}:${item.publishDate ?? ''}`
}

export function LoginAnnouncementDialog() {
  const { t } = useTranslation()
  const { unreadAnnouncements, loading, markRead, markingRead } =
    useLoginAnnouncements()
  const [open, setOpen] = useState(false)

  useEffect(() => {
    if (!loading && unreadAnnouncements.length > 0) {
      setOpen(true)
    }
  }, [loading, unreadAnnouncements.length])

  const handleOpenChange = async (nextOpen: boolean) => {
    setOpen(nextOpen)
    if (!nextOpen && unreadAnnouncements.length > 0) {
      const ids = getLoginAnnouncementIds(unreadAnnouncements)
      if (ids.length > 0) {
        try {
          await markRead(ids)
        } catch {
          /* keep dialog dismissible even if persistence fails */
        }
      }
    }
  }

  if (loading || unreadAnnouncements.length === 0) {
    return null
  }

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => void handleOpenChange(nextOpen)}>
      <DialogContent className='max-h-[88vh] sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <Megaphone className='text-muted-foreground size-4' />
            {t('Login Announcement')}
          </DialogTitle>
        </DialogHeader>

        <ScrollArea className='max-h-[56vh] pr-4'>
          <div className='flex flex-col gap-4'>
            {unreadAnnouncements.map((item, index) => {
              const publishDate = item.publishDate
                ? new Date(item.publishDate)
                : null
              const validDate =
                publishDate && !Number.isNaN(publishDate.getTime())

              return (
                <div
                  key={getAnnouncementKey(item)}
                  className='flex flex-col gap-2'
                >
                  <Markdown>{item.content || ''}</Markdown>
                  {item.extra && (
                    <Markdown className='text-muted-foreground text-xs'>
                      {item.extra}
                    </Markdown>
                  )}
                  {validDate && (
                    <time className='text-muted-foreground text-xs'>
                      {formatDateTimeObject(publishDate)}
                    </time>
                  )}
                  {index < unreadAnnouncements.length - 1 && <Separator />}
                </div>
              )
            })}
          </div>
        </ScrollArea>

        <DialogFooter>
          <Button
            onClick={() => void handleOpenChange(false)}
            disabled={markingRead}
          >
            {t('I know')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
