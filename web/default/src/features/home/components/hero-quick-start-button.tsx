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
import { ArrowRight, Rocket } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

interface HeroQuickStartButtonProps {
  to: string
  className?: string
}

export function HeroQuickStartButton(props: HeroQuickStartButtonProps) {
  const { t } = useTranslation()

  return (
    <Link
      to={props.to}
      className={cn('hero-quick-start-btn group', props.className)}
    >
      <span aria-hidden className='hero-quick-start-btn__glow' />
      <span className='hero-quick-start-btn__icon'>
        <Rocket className='size-7' strokeWidth={2} />
      </span>
      <span className='hero-quick-start-btn__label'>{t('Quick Start')}</span>
      <ArrowRight
        aria-hidden
        className='hero-quick-start-btn__arrow size-5 shrink-0'
      />
    </Link>
  )
}
