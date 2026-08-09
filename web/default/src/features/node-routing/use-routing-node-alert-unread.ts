import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useAuthStore } from '@/stores/auth-store'
import {
  getRoutingNodeAlertUnreadSummary,
  markRoutingNodeAlertsRead,
} from './api'

export function useRoutingNodeAlertUnread(enabled = true) {
  const queryClient = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const queryKey = ['routing-node-alerts', 'unread', userId ?? 0] as const
  const queryEnabled = enabled && Boolean(userId)
  const query = useQuery({
    queryKey,
    queryFn: async () => {
      const result = await getRoutingNodeAlertUnreadSummary()
      if (!result.success || !result.data) {
        throw new Error(result.message || 'Failed to load unread node alerts')
      }
      return result.data
    },
    enabled: queryEnabled,
    refetchInterval: queryEnabled ? 15_000 : false,
    refetchOnWindowFocus: true,
    retry: false,
  })
  const markReadMutation = useMutation({
    mutationFn: async () => {
      const result = await markRoutingNodeAlertsRead()
      if (!result.success || !result.data) {
        throw new Error(result.message || 'Failed to mark node alerts as read')
      }
      return result.data
    },
    onSuccess: (data) => {
      queryClient.setQueryData(queryKey, data)
    },
  })

  return {
    unreadCount: query.data?.unread_count ?? 0,
    isLoading: query.isLoading,
    markRead: markReadMutation.mutateAsync,
    isMarkingRead: markReadMutation.isPending,
  }
}
