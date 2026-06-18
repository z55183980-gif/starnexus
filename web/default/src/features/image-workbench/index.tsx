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
import { useEffect, useMemo, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link } from '@tanstack/react-router'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { fetchTokenKey, getApiKeys } from '@/features/keys/api'
import { useContentAreaRect } from './use-content-area-rect'

const STATIC_PLAYGROUND_PATH = '/image-playground/'
const ENABLED_TOKEN_STATUS = 1

function resolvePlaygroundBasePath(): string {
  const devUrl = import.meta.env.VITE_IMAGE_PLAYGROUND_DEV_URL?.trim()
  if (import.meta.env.DEV && devUrl) {
    return `${devUrl.replace(/\/$/, '')}/`
  }
  return STATIC_PLAYGROUND_PATH
}

function buildPlaygroundSrc(apiKey: string): string {
  const params = new URLSearchParams({
    apiUrl: '/v1',
    apiKey,
    apiMode: 'images',
    profileName: 'StarNexus',
    embedded: '1',
  })
  return `${resolvePlaygroundBasePath()}?${params.toString()}`
}

type ImageWorkbenchFrameProps = {
  title: string
  src: string
}

function ImageWorkbenchFrame(props: ImageWorkbenchFrameProps) {
  const rect = useContentAreaRect(true)

  if (!rect) return null

  return createPortal(
    <iframe
      title={props.title}
      src={props.src}
      className='border-0 bg-background'
      style={{
        position: 'fixed',
        top: rect.top,
        left: rect.left,
        width: rect.width,
        height: rect.height,
        zIndex: 1,
      }}
      allow='clipboard-read; clipboard-write'
    />,
    document.body
  )
}

export function ImageWorkbench() {
  const { t } = useTranslation()
  const [iframeSrc, setIframeSrc] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    async function loadToken() {
      setLoading(true)
      setError(null)

      try {
        const listRes = await getApiKeys({ p: 1, size: 20 })
        const items = listRes.data?.items ?? []
        const activeToken = items.find((item) => item.status === ENABLED_TOKEN_STATUS)

        if (!activeToken) {
          if (!cancelled) {
            setError('missing-token')
            setIframeSrc(null)
          }
          return
        }

        const keyRes = await fetchTokenKey(activeToken.id)
        const apiKey = keyRes.data?.key?.trim()

        if (!keyRes.success || !apiKey) {
          if (!cancelled) {
            setError(keyRes.message || 'token-fetch-failed')
            setIframeSrc(null)
          }
          return
        }

        if (!cancelled) {
          setIframeSrc(buildPlaygroundSrc(apiKey))
        }
      } catch {
        if (!cancelled) {
          setError('token-fetch-failed')
          setIframeSrc(null)
        }
      } finally {
        if (!cancelled) {
          setLoading(false)
        }
      }
    }

    void loadToken()

    return () => {
      cancelled = true
    }
  }, [])

  const errorContent = useMemo(() => {
    if (error === 'missing-token') {
      return {
        title: t('Image workbench needs an API key'),
        description: t(
          'Create or enable at least one API key, then reopen the image workbench.'
        ),
      }
    }

    return {
      title: t('Failed to load image workbench'),
      description: t('Please retry later or check your API key settings.'),
    }
  }, [error, t])

  if (loading) {
    return (
      <div className='flex h-full min-h-[320px] items-center justify-center'>
        <div className='text-muted-foreground flex items-center gap-2 text-sm'>
          <Loader2 className='size-4 animate-spin' />
          {t('Loading image workbench...')}
        </div>
      </div>
    )
  }

  if (!iframeSrc) {
    return (
      <div className='flex h-full min-h-[320px] items-center justify-center p-6'>
        <div className='max-w-md space-y-3 text-center'>
          <h2 className='text-lg font-semibold'>{errorContent.title}</h2>
          <p className='text-muted-foreground text-sm'>{errorContent.description}</p>
          <Button render={<Link to='/keys' />}>{t('Go to API Keys')}</Button>
        </div>
      </div>
    )
  }

  return <ImageWorkbenchFrame title={t('Image Workbench')} src={iframeSrc} />
}
