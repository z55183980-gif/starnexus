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
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { HeroBrandTech } from '../hero-brand-tech'
import { HeroQuickStartButton } from '../hero-quick-start-button'
import { HeroTerminalDemo } from '../hero-terminal-demo'

interface HeroProps {
  className?: string
  isAuthenticated?: boolean
}

export function Hero(props: HeroProps) {
  const { t } = useTranslation()

  return (
    <section className='relative z-10 flex flex-col items-center overflow-hidden px-6 pt-28 pb-16 md:pt-36 md:pb-24'>
      {/* Radial gradient background */}
      <div
        aria-hidden
        className='hero-section-glow pointer-events-none absolute inset-0 -z-10 opacity-25 dark:opacity-[0.12]'
      />
      {/* Grid pattern */}
      <div
        aria-hidden
        className='absolute inset-0 -z-10 bg-[linear-gradient(to_right,var(--border)_1px,transparent_1px),linear-gradient(to_bottom,var(--border)_1px,transparent_1px)] bg-[size:4rem_4rem] opacity-[0.08] [-webkit-mask-image:radial-gradient(ellipse_60%_50%_at_50%_30%,black_20%,transparent_100%)] [mask-image:radial-gradient(ellipse_60%_50%_at_50%_30%,black_20%,transparent_100%)]'
      />

      <div className='flex max-w-3xl flex-col items-center text-center'>
        <h1 className='sr-only'>
          {t('StarNexus')} — {t('Direct Access API for Global AI Models')}
        </h1>
        <HeroBrandTech
          className='hero-brand-tech landing-animate-fade-up mx-auto sm:max-w-md md:max-w-lg'
          style={{ animationDelay: '0ms' }}
        >
          <img
            src='/starnexus-hero-brand.png'
            alt={t('StarNexus')}
            width={334}
            height={115}
            className='hero-brand-tech__image'
          />
        </HeroBrandTech>
        <p
          className='landing-animate-fade-up text-foreground mt-5 text-3xl font-bold tracking-tight md:text-[2.25rem] md:leading-tight'
          style={{ animationDelay: '50ms' }}
        >
          {t('Direct Access API for Global AI Models')}
        </p>
        <div
          className='landing-animate-fade-up mt-8 flex flex-wrap items-center justify-center gap-3'
          style={{ animationDelay: '200ms' }}
        >
          {props.isAuthenticated ? (
            <HeroQuickStartButton to='/dashboard' />
          ) : (
            <>
              <HeroQuickStartButton to='/sign-up' />
              <Button
                variant='outline'
                className='border-border/50 hover:border-border hover:bg-muted/50 rounded-lg'
                render={<Link to='/pricing' />}
              >
                {t('View Pricing')}
              </Button>
            </>
          )}
        </div>
        <div
          className='landing-animate-fade-in mt-6 flex w-full justify-center'
          style={{ animationDelay: '250ms' }}
        >
          <img
            src='/hero-model-providers.png'
            srcSet='/hero-model-providers.png 569w, /hero-model-providers@2x.png 1138w'
            sizes='(max-width: 640px) 264px, (max-width: 1024px) 448px, 569px'
            alt={t('Supported AI models and platforms')}
            width={569}
            height={55}
            fetchPriority='high'
            decoding='sync'
            className='hero-model-providers'
          />
        </div>
      </div>

      <div
        className='landing-animate-fade-up w-full'
        style={{ animationDelay: '300ms' }}
      >
        <HeroTerminalDemo />
      </div>
    </section>
  )
}
