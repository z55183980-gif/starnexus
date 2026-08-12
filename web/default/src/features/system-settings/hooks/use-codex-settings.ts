import { useMutation, useQueryClient } from '@tanstack/react-query'
import i18next from 'i18next'
import { toast } from 'sonner'
import { syncCodexVersion, updateCodexSettings } from '../api'
import type { CodexSettingsUpdateRequest } from '../types'

export function useUpdateCodexSettings() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (request: CodexSettingsUpdateRequest) =>
      updateCodexSettings(request),
    onSuccess: (data) => {
      if (data.success) {
        queryClient.invalidateQueries({ queryKey: ['system-options'] })
        toast.success(i18next.t('Codex settings saved'))
      } else {
        toast.error(data.message || i18next.t('Failed to save Codex settings'))
      }
    },
    onError: (error: Error) => {
      toast.error(error.message || i18next.t('Failed to save Codex settings'))
    },
  })
}

export function useSyncCodexVersion() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: syncCodexVersion,
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['system-options'] })
      if (data.success) {
        toast.success(i18next.t('Codex official version synchronized'))
      } else {
        toast.error(data.message || i18next.t('Codex official version sync failed'))
      }
    },
    onError: (error: Error) => {
      toast.error(error.message || i18next.t('Codex official version sync failed'))
    },
  })
}
