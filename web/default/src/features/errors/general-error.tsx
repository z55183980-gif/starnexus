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
import { useNavigate, useRouter } from '@tanstack/react-router'
import {
  Alert02Icon,
  ArrowLeft01Icon,
  Home01Icon,
  ReloadIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { useHeaderBrandName } from '@/hooks/use-display-system-name'
import { useSystemConfig } from '@/hooks/use-system-config'
import { Button } from '@/components/ui/button'
import { ContactOwnerMenu } from '@/components/layout/components/contact-owner-menu'

type GeneralErrorProps = React.HTMLAttributes<HTMLDivElement> & {
  minimal?: boolean
  error?: unknown
}

function getHttpStatus(error: unknown): number | undefined {
  if (typeof error !== 'object' || error === null) return undefined
  const response = (error as Record<string, unknown>).response
  if (typeof response !== 'object' || response === null) return undefined
  const status = (response as Record<string, unknown>).status
  return typeof status === 'number' ? status : undefined
}

export function GeneralError({
  className,
  minimal = false,
  error,
}: GeneralErrorProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { history } = useRouter()
  const { logo } = useSystemConfig()
  const brandName = useHeaderBrandName()
  const status = getHttpStatus(error)
  const isRateLimited = status === 429
  const statusCode = status ?? 500
  const title = isRateLimited
    ? t('Too many requests')
    : `${t('Oops! Something went wrong')} ${`:')`}`
  const description = isRateLimited
    ? t('Please wait a moment before trying again.')
    : t('Please try again later.')

  if (minimal) {
    return (
      <div
        className={cn(
          'flex w-full flex-col items-center justify-center gap-3 py-10 text-center',
          className
        )}
      >
        <span className='bg-destructive/10 text-destructive flex size-10 items-center justify-center rounded-md'>
          <HugeiconsIcon icon={Alert02Icon} size={22} strokeWidth={1.8} />
        </span>
        <div className='flex flex-col gap-1'>
          <p className='font-semibold'>{title}</p>
          <p className='text-muted-foreground text-sm'>{description}</p>
        </div>
        <Button variant='outline' onClick={() => window.location.reload()}>
          <HugeiconsIcon icon={ReloadIcon} data-icon='inline-start' />
          {t('Retry')}
        </Button>
      </div>
    )
  }

  return (
    <div
      className={cn(
        'bg-background text-foreground relative min-h-svh w-full overflow-hidden',
        className
      )}
    >
      <header className='absolute inset-x-0 top-0 z-10 mx-auto flex w-full max-w-6xl items-center justify-between px-5 py-5 sm:px-8 sm:py-6'>
        <button
          type='button'
          onClick={() => navigate({ to: '/' })}
          className='hover:bg-muted focus-visible:ring-ring/50 inline-flex min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-sm font-semibold transition-colors outline-none focus-visible:ring-3'
        >
          <span className='bg-muted flex size-7 shrink-0 items-center justify-center overflow-hidden rounded-md'>
            <img
              src={logo}
              alt={t('Logo')}
              className='size-full object-cover'
            />
          </span>
          <span className='max-w-52 truncate'>{brandName}</span>
        </button>
        <span className='text-muted-foreground font-mono text-xs font-medium'>
          HTTP {statusCode}
        </span>
      </header>

      <main className='relative mx-auto flex min-h-svh w-full max-w-6xl items-center px-5 py-24 sm:px-8'>
        <div className='grid w-full items-center gap-14 lg:grid-cols-[minmax(0,1fr)_19rem] lg:gap-20'>
          <section className='max-w-2xl'>
            <div className='mb-8 flex items-center gap-3'>
              <span className='bg-destructive/10 text-destructive flex size-11 items-center justify-center rounded-md'>
                <HugeiconsIcon icon={Alert02Icon} size={24} strokeWidth={1.8} />
              </span>
              <div className='flex flex-col gap-0.5'>
                <span className='text-muted-foreground text-xs font-semibold tracking-normal uppercase'>
                  HTTP {statusCode}
                </span>
                <span className='text-sm font-medium'>
                  {isRateLimited
                    ? t('Too many requests')
                    : t('Internal Server Error!')}
                </span>
              </div>
            </div>

            <h1 className='max-w-xl text-4xl leading-tight font-semibold sm:text-5xl lg:text-6xl'>
              {title}
            </h1>
            <p className='text-muted-foreground mt-5 max-w-xl text-base leading-7 sm:text-lg'>
              {t('We apologize for the inconvenience.')} {description}
            </p>

            <div className='mt-8 flex w-full max-w-xl flex-col gap-3 sm:flex-row'>
              <Button
                size='lg'
                className='w-full sm:w-auto'
                onClick={() => window.location.reload()}
              >
                <HugeiconsIcon icon={ReloadIcon} data-icon='inline-start' />
                {t('Retry')}
              </Button>
              <Button
                variant='outline'
                size='lg'
                className='w-full sm:w-auto'
                onClick={() => history.go(-1)}
              >
                <HugeiconsIcon
                  icon={ArrowLeft01Icon}
                  data-icon='inline-start'
                />
                {t('Go Back')}
              </Button>
              <Button
                variant='ghost'
                size='lg'
                className='w-full sm:w-auto'
                onClick={() => navigate({ to: '/' })}
              >
                <HugeiconsIcon icon={Home01Icon} data-icon='inline-start' />
                {t('Back to Home')}
              </Button>
            </div>

            <div className='mt-8 flex flex-col items-start gap-2 sm:flex-row sm:items-center'>
              <p className='text-muted-foreground text-sm'>
                {t('If this keeps happening, please contact the site owner.')}
              </p>
              <ContactOwnerMenu variant='dashboard' />
            </div>
          </section>

          <div
            aria-hidden='true'
            className='border-border/80 relative hidden aspect-square w-full border lg:block'
          >
            <div className='border-border/60 absolute inset-5 border' />
            <div className='absolute inset-0 grid place-items-center'>
              <span className='text-foreground text-[6.5rem] leading-none font-bold'>
                {statusCode}
              </span>
            </div>
            <span className='bg-background text-muted-foreground absolute top-0 left-6 -translate-y-1/2 px-2 font-mono text-[10px] font-medium tracking-normal uppercase'>
              HTTP {statusCode}
            </span>
            <span className='bg-destructive absolute right-5 bottom-5 size-2 rounded-full' />
            <span className='bg-muted-foreground/35 absolute right-9 bottom-5 size-2 rounded-full' />
            <span className='bg-muted-foreground/20 absolute right-13 bottom-5 size-2 rounded-full' />
          </div>
        </div>
      </main>
    </div>
  )
}
