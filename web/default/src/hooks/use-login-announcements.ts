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
import { api } from '@/lib/api'
import type { AnnouncementItem } from '@/features/dashboard/types'

type UnreadLoginAnnouncementsResponse = {
  success: boolean
  message?: string
  data?: AnnouncementItem[]
}

function normalizeAnnouncementItem(
  item: Record<string, unknown>
): AnnouncementItem {
  const rawId = item.id
  const id =
    typeof rawId === 'number'
      ? rawId
      : typeof rawId === 'string'
        ? Number.parseInt(rawId, 10)
        : undefined

  return {
    id: Number.isFinite(id) ? id : undefined,
    content: String(item.content ?? ''),
    publishDate:
      typeof item.publishDate === 'string' ? item.publishDate : undefined,
    type: item.type as AnnouncementItem['type'],
    target: item.target as AnnouncementItem['target'],
    extra: typeof item.extra === 'string' ? item.extra : undefined,
  }
}

export function getLoginAnnouncementIds(items: AnnouncementItem[]): number[] {
  return items
    .map((item) => item.id)
    .filter((id): id is number => typeof id === 'number' && id > 0)
}

export function useLoginAnnouncements() {
  const queryClient = useQueryClient()

  const query = useQuery({
    queryKey: ['login-announcements', 'unread'],
    queryFn: async () => {
      const res = await api.get<UnreadLoginAnnouncementsResponse>(
        '/api/user/login-announcements/unread',
        { skipErrorHandler: true } as Record<string, unknown>
      )
      if (!res.data.success) {
        throw new Error(res.data.message || 'Failed to load login announcements')
      }
      return (res.data.data ?? []).map((item) =>
        normalizeAnnouncementItem(item as unknown as Record<string, unknown>)
      )
    },
    staleTime: 0,
    refetchOnMount: true,
    refetchOnWindowFocus: true,
    refetchInterval: false,
  })

  const markReadMutation = useMutation({
    mutationFn: async (ids: number[]) => {
      if (ids.length === 0) return
      const res = await api.post<{ success: boolean; message?: string }>(
        '/api/user/login-announcements/read',
        { ids }
      )
      if (!res.data.success) {
        throw new Error(res.data.message || 'Failed to mark announcements read')
      }
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ['login-announcements', 'unread'],
      })
    },
  })

  return {
    unreadAnnouncements: query.data ?? [],
    loading: query.isLoading,
    markRead: markReadMutation.mutateAsync,
    markingRead: markReadMutation.isPending,
  }
}
