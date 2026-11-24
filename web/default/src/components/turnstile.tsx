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
import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useRef,
} from 'react'

interface TurnstileApi {
  render: (element: HTMLElement, options: Record<string, unknown>) => string
  reset?: (widgetId?: string) => void
  remove?: (widgetId: string) => void
}

declare global {
  interface Window {
    turnstile?: TurnstileApi
  }
}

export interface TurnstileInstance {
  reset: () => void
}

interface TurnstileProps {
  siteKey: string
  onVerify: (token: string) => void
  onExpire?: () => void
  onError?: (errorCode?: string) => void
  className?: string
}

const TURNSTILE_SCRIPT_ID = 'cf-turnstile'
const TURNSTILE_SCRIPT_SRC =
  'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'

let turnstileScriptPromise: Promise<TurnstileApi> | null = null

function loadTurnstileScript(): Promise<TurnstileApi> {
  if (typeof window === 'undefined') {
    return Promise.reject(new Error('Window is not available'))
  }
  if (window.turnstile) {
    return Promise.resolve(window.turnstile)
  }
  if (turnstileScriptPromise) {
    return turnstileScriptPromise
  }

  turnstileScriptPromise = new Promise<TurnstileApi>((resolve, reject) => {
    let script = document.getElementById(
      TURNSTILE_SCRIPT_ID
    ) as HTMLScriptElement | null

    const handleLoad = () => {
      cleanup()
      if (window.turnstile) {
        resolve(window.turnstile)
      } else {
        turnstileScriptPromise = null
        reject(new Error('Turnstile API unavailable after script load'))
      }
    }

    const handleError = () => {
      cleanup()
      script?.remove()
      turnstileScriptPromise = null
      reject(new Error('Failed to load Cloudflare Turnstile script'))
    }

    const cleanup = () => {
      script?.removeEventListener('load', handleLoad)
      script?.removeEventListener('error', handleError)
    }

    if (!script) {
      script = document.createElement('script')
      script.id = TURNSTILE_SCRIPT_ID
      script.src = TURNSTILE_SCRIPT_SRC
      script.async = true
      script.defer = true
      script.addEventListener('load', handleLoad)
      script.addEventListener('error', handleError)
      document.head.appendChild(script)
      return
    }

    script.addEventListener('load', handleLoad)
    script.addEventListener('error', handleError)
  })

  return turnstileScriptPromise
}

export const Turnstile = forwardRef<TurnstileInstance, TurnstileProps>(
  function Turnstile(
    { siteKey, onVerify, onExpire, onError, className },
    forwardedRef
  ) {
    const containerRef = useRef<HTMLDivElement | null>(null)
    const widgetIdRef = useRef<string | null>(null)
    const onVerifyRef = useRef(onVerify)
    const onExpireRef = useRef(onExpire)
    const onErrorRef = useRef(onError)

    onVerifyRef.current = onVerify
    onExpireRef.current = onExpire
    onErrorRef.current = onError

    const renderWidget = () => {
      if (!containerRef.current || !window.turnstile || !siteKey) return
      if (widgetIdRef.current !== null) {
        try {
          window.turnstile.remove?.(widgetIdRef.current)
        } catch {
          /* empty */
        }
        widgetIdRef.current = null
      }
      containerRef.current.innerHTML = ''
      try {
        widgetIdRef.current = window.turnstile.render(containerRef.current, {
          sitekey: siteKey,
          callback: (token: string) => onVerifyRef.current(token),
          'expired-callback': () => onExpireRef.current?.(),
          'error-callback': (errorCode?: unknown) => {
            onExpireRef.current?.()
            onErrorRef.current?.(
              errorCode !== undefined && errorCode !== null
                ? String(errorCode)
                : undefined
            )
          },
        })
      } catch {
        onErrorRef.current?.('render-failed')
      }
    }

    useImperativeHandle(forwardedRef, () => ({
      reset: () => {
        if (widgetIdRef.current !== null && window.turnstile?.reset) {
          try {
            window.turnstile.reset(widgetIdRef.current)
            return
          } catch {
            /* fall through to full re-render */
          }
        }
        loadTurnstileScript()
          .then(() => {
            renderWidget()
          })
          .catch(() => {
            onErrorRef.current?.('script-load-failed')
          })
      },
    }))

    useEffect(() => {
      if (!siteKey) return
      let cancelled = false

      loadTurnstileScript()
        .then(() => {
          if (cancelled) return
          renderWidget()
        })
        .catch(() => {
          if (cancelled) return
          onExpireRef.current?.()
          onErrorRef.current?.('script-load-failed')
        })

      return () => {
        cancelled = true
        if (widgetIdRef.current !== null) {
          try {
            window.turnstile?.remove?.(widgetIdRef.current)
          } catch {
            /* empty */
          }
          widgetIdRef.current = null
        }
      }
    }, [siteKey])

    return <div ref={containerRef} className={className} />
  }
)
