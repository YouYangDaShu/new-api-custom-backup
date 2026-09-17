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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { announcementDismissSessionKey } from '@/components/announcement-popup-utils'
import { RichContent } from '@/components/rich-content'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  getUnreadAnnouncements,
  markAnnouncementsRead,
  type UnreadAnnouncementItem,
} from '@/lib/api'
import { getAnnouncementColorClass } from '@/lib/colors'
import { formatDateTimeObject } from '@/lib/time'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

function formatPublishDate(value?: string): string {
  if (!value) return ''
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  return formatDateTimeObject(d)
}

/**
 * Login popup: walk through unread system announcements.
 * - Prev / Next: browse only
 * - Later: close for this browser session (still unread next login)
 * - Mark all as read (全部已读): permanent server-side read for all current unread
 */
export function AnnouncementPopup() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const sessionSID = useAuthStore((state) => state.auth.session?.sid)
  const dismissSessionKey = announcementDismissSessionKey(userId, sessionSID)
  const [open, setOpen] = useState(false)
  const [index, setIndex] = useState(0)
  const [sessionDismissed, setSessionDismissed] = useState(() => {
    try {
      return sessionStorage.getItem(dismissSessionKey) === '1'
    } catch {
      return false
    }
  })

  useEffect(() => {
    try {
      setSessionDismissed(sessionStorage.getItem(dismissSessionKey) === '1')
    } catch {
      setSessionDismissed(false)
    }
  }, [dismissSessionKey])

  const { data, isLoading } = useQuery({
    queryKey: ['announcements-unread', userId, sessionSID],
    queryFn: getUnreadAnnouncements,
    enabled: typeof userId === 'number' && userId > 0,
    staleTime: 60_000,
    retry: 1,
  })

  const items: UnreadAnnouncementItem[] = useMemo(() => {
    if (!data?.success) return []
    return data.data?.items || []
  }, [data])

  useEffect(() => {
    if (sessionDismissed || isLoading) return
    if (items.length > 0) {
      setIndex(0)
      setOpen(true)
    } else {
      setOpen(false)
    }
  }, [items, isLoading, sessionDismissed])

  useEffect(() => {
    if (index >= items.length && items.length > 0) {
      setIndex(items.length - 1)
    }
  }, [index, items.length])

  const markAllMutation = useMutation({
    mutationFn: () => markAnnouncementsRead({ all: true }),
    onSuccess: async () => {
      // Permanent read succeeded — allow future new announcements in this tab.
      try {
        sessionStorage.removeItem(dismissSessionKey)
      } catch {
        // ignore
      }
      setSessionDismissed(false)
      setOpen(false)
      await queryClient.invalidateQueries({
        queryKey: ['announcements-unread', userId, sessionSID],
      })
      await queryClient.invalidateQueries({ queryKey: ['notice'] })
      await queryClient.invalidateQueries({ queryKey: ['status'] })
    },
  })

  const current = items[index]
  const total = items.length
  const isFirst = index <= 0
  const isLast = index >= total - 1

  const handleLater = () => {
    try {
      sessionStorage.setItem(dismissSessionKey, '1')
    } catch {
      // ignore
    }
    setSessionDismissed(true)
    setOpen(false)
  }

  const handlePrev = () => {
    setIndex((i) => Math.max(0, i - 1))
  }

  const handleNext = () => {
    if (isLast) {
      // Last item: treat as done browsing this pass, same as later (not permanent).
      handleLater()
      return
    }
    setIndex((i) => Math.min(total - 1, i + 1))
  }

  if (!open || !current) {
    return null
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        // Must use buttons; ignore outside / esc close as "later".
        if (!next) handleLater()
      }}
    >
      <DialogContent
        showCloseButton={false}
        className='grid h-[min(680px,calc(100dvh-2rem))] w-[min(560px,calc(100vw-2rem))] grid-rows-[auto_minmax(0,1fr)_auto] gap-0 overflow-hidden p-0 sm:max-w-none'
      >
        <DialogHeader className='border-b px-5 py-4'>
          <DialogTitle className='flex items-center justify-between gap-3 pr-2'>
            <span>{t('System announcement')}</span>
            <span className='text-muted-foreground font-mono text-xs font-normal tabular-nums'>
              {index + 1} / {total}
            </span>
          </DialogTitle>
          <DialogDescription className='flex flex-wrap items-center gap-2'>
            {current.publishDate ? (
              <span className='text-muted-foreground text-xs'>
                {formatPublishDate(current.publishDate)}
              </span>
            ) : null}
            {current.type ? (
              <Badge
                variant='outline'
                className={cn(
                  'text-[10px]',
                  getAnnouncementColorClass(current.type)
                )}
              >
                {current.type}
              </Badge>
            ) : null}
            {current.source === 'notice' ? (
              <Badge variant='secondary' className='text-[10px]'>
                {t('Notice')}
              </Badge>
            ) : null}
          </DialogDescription>
        </DialogHeader>

        <ScrollArea key={current.key} className='bg-muted/10 h-full min-h-0'>
          <div className='min-h-full px-5 py-4 pr-7'>
            {current.title ? (
              <div className='mb-3 text-base leading-snug font-semibold'>
                {current.title}
              </div>
            ) : null}
            <div className='prose prose-sm dark:prose-invert max-w-none text-sm leading-relaxed break-words [&_img]:mx-auto [&_img]:max-h-64 [&_img]:rounded-lg'>
              <RichContent content={current.content || ''} />
            </div>
            {current.extra ? (
              <div className='text-muted-foreground mt-4 border-t pt-3 text-xs leading-relaxed'>
                {current.extra}
              </div>
            ) : null}
          </div>
        </ScrollArea>

        <DialogFooter className='mx-0 mb-0 flex flex-col gap-2 rounded-none border-t px-5 py-4 sm:flex-col'>
          {/* Primary: one-click permanent read for all unread announcements */}
          <Button
            type='button'
            className='w-full'
            disabled={markAllMutation.isPending}
            onClick={() => markAllMutation.mutate()}
          >
            {markAllMutation.isPending ? t('Saving...') : t('Mark all as read')}
          </Button>
          <div className='grid w-full grid-cols-3 gap-2'>
            <Button
              type='button'
              variant='outline'
              disabled={isFirst || markAllMutation.isPending}
              onClick={handlePrev}
            >
              {t('Previous')}
            </Button>
            <Button
              type='button'
              variant='secondary'
              disabled={markAllMutation.isPending}
              onClick={handleLater}
            >
              {t('Later')}
            </Button>
            <Button
              type='button'
              variant='outline'
              disabled={markAllMutation.isPending}
              onClick={handleNext}
            >
              {isLast ? t('Close') : t('Next')}
            </Button>
          </div>
          <p className='text-muted-foreground text-center text-[11px] leading-relaxed'>
            {t(
              'Later closes this popup for now. Mark all as read so these will not show again after re-login.'
            )}
          </p>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
