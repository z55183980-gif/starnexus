import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { getActiveApiProfile } from '../lib/apiProfiles'
import { useStore } from '../store'
import { useCloseOnEscape } from '../hooks/useCloseOnEscape'
import { usePreventBackgroundScroll } from '../hooks/usePreventBackgroundScroll'
import { CloseIcon, KeyIcon } from './icons'

export default function ApiKeyModal() {
  const showApiKeyModal = useStore((s) => s.showApiKeyModal)
  const setShowApiKeyModal = useStore((s) => s.setShowApiKeyModal)
  const settings = useStore((s) => s.settings)
  const setSettings = useStore((s) => s.setSettings)
  const activeProfile = getActiveApiProfile(settings)
  const [apiKey, setApiKey] = useState(activeProfile.apiKey)
  const [revealed, setRevealed] = useState(false)

  useEffect(() => {
    if (!showApiKeyModal) return
    setApiKey(activeProfile.apiKey)
    setRevealed(false)
  }, [showApiKeyModal, activeProfile.apiKey])

  const commitApiKey = (value: string) => {
    setSettings({
      profiles: settings.profiles.map((profile) =>
        profile.id === activeProfile.id ? { ...profile, apiKey: value } : profile,
      ),
    })
  }

  const handleClose = () => {
    commitApiKey(apiKey)
    setShowApiKeyModal(false)
  }

  useCloseOnEscape(showApiKeyModal, handleClose)
  usePreventBackgroundScroll(showApiKeyModal)

  if (!showApiKeyModal) return null

  return createPortal(
    <div
      data-no-drag-select
      className="fixed inset-0 z-[70] flex items-center justify-center p-4"
      onClick={handleClose}
    >
      <div className="absolute inset-0 bg-black/30 backdrop-blur-sm animate-overlay-in" />
      <div
        className="relative z-10 w-full max-w-md rounded-3xl border border-white/50 bg-white/95 p-5 shadow-2xl ring-1 ring-black/5 animate-modal-in dark:border-white/[0.08] dark:bg-gray-900/95 dark:ring-white/10"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h3 className="flex items-center gap-2 text-lg font-bold text-gray-800 dark:text-gray-100">
            <KeyIcon className="h-5 w-5 text-blue-500" />
            API Key
          </h3>
          <button
            type="button"
            onClick={handleClose}
            className="rounded-full p-1 text-gray-400 transition hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-white/[0.06] dark:hover:text-gray-200"
            aria-label="关闭"
          >
            <CloseIcon className="h-5 w-5" />
          </button>
        </div>

        <p className="mb-3 text-sm text-gray-500 dark:text-gray-400">
          当前配置：<span className="font-medium text-gray-700 dark:text-gray-200">{activeProfile.name}</span>
        </p>

        <div className="relative">
          <input
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            onBlur={(e) => commitApiKey(e.target.value)}
            type={revealed ? 'text' : 'password'}
            placeholder={activeProfile.provider === 'fal' ? 'FAL_KEY' : 'sk-...'}
            className="w-full rounded-xl border border-gray-200/70 bg-white/60 px-3 py-2.5 pr-10 text-sm text-gray-700 outline-none transition focus:border-blue-300 dark:border-white/[0.08] dark:bg-white/[0.03] dark:text-gray-200 dark:focus:border-blue-500/50"
            autoFocus
          />
          <button
            type="button"
            onClick={() => setRevealed((value) => !value)}
            className="absolute right-2 top-1/2 -translate-y-1/2 p-1 text-gray-400 transition-colors hover:text-gray-600"
            tabIndex={-1}
            aria-label={revealed ? '隐藏 API Key' : '显示 API Key'}
          >
            {revealed ? (
              <svg className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" viewBox="0 0 24 24">
                <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" />
                <circle cx="12" cy="12" r="3" />
              </svg>
            ) : (
              <svg className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" viewBox="0 0 24 24">
                <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94" />
                <path d="M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19" />
                <path d="M14.12 14.12a3 3 0 1 1-4.24-4.24" />
                <line x1="1" y1="1" x2="23" y2="23" />
              </svg>
            )}
          </button>
        </div>

        <p data-selectable-text className="mt-2 text-xs text-gray-500 dark:text-gray-500">
          支持通过查询参数覆盖：<code className="rounded bg-gray-100 px-1 py-0.5 dark:bg-white/[0.06]">?apiKey=</code>
        </p>
      </div>
    </div>,
    document.body,
  )
}
