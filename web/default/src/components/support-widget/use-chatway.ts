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
import { useEffect } from 'react'
import { CHATWAY_SCRIPT_ID } from './constants'

declare global {
  interface Window {
    $chatwayOnLoad?: () => void
    $chatway?: {
      hideChatwayIcon?: () => void
      openChatwayWidget?: () => void
    }
  }
}

function getChatwayScriptUrl(widgetId: string) {
  return `https://cdn.chatway.app/widget.js?id=${encodeURIComponent(widgetId)}`
}

export function openChatwayWidget(): boolean {
  const chatway = window.$chatway
  if (chatway && typeof chatway.openChatwayWidget === 'function') {
    chatway.openChatwayWidget()
    return true
  }
  return false
}

export function waitForChatwayAndOpen(): void {
  if (openChatwayWidget()) return

  let attempts = 0
  const intervalId = window.setInterval(() => {
    if (openChatwayWidget() || ++attempts > 50) {
      window.clearInterval(intervalId)
    }
  }, 100)
}

export function useChatway(widgetId: string | undefined): void {
  useEffect(() => {
    if (!widgetId?.trim()) return

    window.$chatwayOnLoad = function () {
      if (window.$chatway && typeof window.$chatway.hideChatwayIcon === 'function') {
        window.$chatway.hideChatwayIcon()
      }
    }

    const scriptUrl = getChatwayScriptUrl(widgetId.trim())
    const existing = document.getElementById(CHATWAY_SCRIPT_ID) as
      | HTMLScriptElement
      | null

    if (existing) {
      if (existing.src === scriptUrl) return
      existing.remove()
      delete window.$chatway
    }

    const script = document.createElement('script')
    script.id = CHATWAY_SCRIPT_ID
    script.src = scriptUrl
    script.async = true
    document.body.appendChild(script)
  }, [widgetId])
}
