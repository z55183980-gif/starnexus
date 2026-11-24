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
import { useCallback, useRef, useState } from 'react'
import i18next from 'i18next'
import { toast } from 'sonner'
import type { TurnstileInstance } from '@/components/turnstile'
import { useStatus } from '@/hooks/use-status'

/**
 * Hook for managing Turnstile verification
 */
export function useTurnstile() {
  const { status } = useStatus()
  const [turnstileToken, setTurnstileTokenState] = useState('')
  const [turnstileError, setTurnstileError] = useState<string | null>(null)
  const turnstileRef = useRef<TurnstileInstance | null>(null)

  const isTurnstileEnabled = !!(
    status?.turnstile_check && status?.turnstile_site_key
  )
  const turnstileSiteKey = status?.turnstile_site_key || ''

  const setTurnstileToken = useCallback((token: string) => {
    setTurnstileTokenState(token)
    if (token) {
      setTurnstileError(null)
    }
  }, [])

  const handleTurnstileExpire = useCallback(() => {
    setTurnstileTokenState('')
  }, [])

  const handleTurnstileError = useCallback((errorCode?: string) => {
    setTurnstileTokenState('')
    setTurnstileError(errorCode || 'error')
  }, [])

  const resetTurnstile = useCallback(() => {
    setTurnstileTokenState('')
    setTurnstileError(null)
    turnstileRef.current?.reset()
  }, [])

  /**
   * Validate if turnstile is ready when required
   */
  const validateTurnstile = useCallback((): boolean => {
    if (isTurnstileEnabled && !turnstileToken) {
      if (turnstileError) {
        resetTurnstile()
        toast.error(
          i18next.t('Human verification failed to load, retrying...')
        )
        return false
      }
      toast.info(
        i18next.t('Please wait a moment, human check is initializing...')
      )
      return false
    }
    return true
  }, [isTurnstileEnabled, resetTurnstile, turnstileError, turnstileToken])

  return {
    isTurnstileEnabled,
    turnstileSiteKey,
    turnstileToken,
    turnstileError,
    turnstileRef,
    setTurnstileToken,
    handleTurnstileExpire,
    handleTurnstileError,
    resetTurnstile,
    validateTurnstile,
  }
}
