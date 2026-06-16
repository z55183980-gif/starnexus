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
import { cn } from '@/lib/utils'

const RAY_ANGLES = [0, 30, 60, 90, 120, 150, 180, 210, 240, 270, 300, 330]

const PARTICLES = [
  { left: '8%', top: '42%', dx: '72px', dy: '0px', delay: '0s' },
  { left: '92%', top: '48%', dx: '-68px', dy: '0px', delay: '0.6s' },
  { left: '18%', top: '22%', dx: '56px', dy: '28px', delay: '1.1s' },
  { left: '82%', top: '28%', dx: '-52px', dy: '24px', delay: '1.8s' },
  { left: '22%', top: '72%', dx: '48px', dy: '-26px', delay: '0.4s' },
  { left: '78%', top: '68%', dx: '-54px', dy: '-22px', delay: '2.2s' },
  { left: '50%', top: '12%', dx: '0px', dy: '38px', delay: '1.4s' },
  { left: '50%', top: '88%', dx: '0px', dy: '-36px', delay: '2.6s' },
  { left: '35%', top: '50%', dx: '42px', dy: '0px', delay: '0.9s' },
  { left: '65%', top: '50%', dx: '-40px', dy: '0px', delay: '1.6s' },
  { left: '12%', top: '55%', dx: '64px', dy: '-8px', delay: '2s' },
  { left: '88%', top: '52%', dx: '-62px', dy: '6px', delay: '0.2s' },
] as const

function rayEndpoint(angleDeg: number): { x: number; y: number } {
  const rad = (angleDeg * Math.PI) / 180
  return {
    x: 50 + Math.cos(rad) * 46,
    y: 50 + Math.sin(rad) * 36,
  }
}

type HeroBrandTechProps = {
  children: React.ReactNode
  className?: string
  style?: React.CSSProperties
}

export function HeroBrandTech(props: HeroBrandTechProps) {
  return (
    <div className={cn('hero-brand-tech', props.className)} style={props.style}>
      <svg
        aria-hidden
        className='hero-brand-tech__svg'
        viewBox='0 0 100 100'
        preserveAspectRatio='none'
      >
        <defs>
          <linearGradient id='hero-tech-line-grad' x1='0%' y1='0%' x2='100%' y2='0%'>
            <stop offset='0%' stopColor='oklch(0.62 0.2 255 / 0)' />
            <stop offset='35%' stopColor='oklch(0.62 0.22 255 / 0.55)' />
            <stop offset='65%' stopColor='oklch(0.58 0.2 285 / 0.55)' />
            <stop offset='100%' stopColor='oklch(0.55 0.18 310 / 0)' />
          </linearGradient>
        </defs>
        <g className='hero-brand-tech__grid'>
          <line x1='4' y1='50' x2='96' y2='50' />
          <line x1='50' y1='8' x2='50' y2='92' />
          <line x1='12' y1='28' x2='88' y2='72' />
          <line x1='88' y1='28' x2='12' y2='72' />
        </g>
        <g className='hero-brand-tech__rays'>
          {RAY_ANGLES.map((angle) => {
            const end = rayEndpoint(angle)
            return (
              <line
                key={angle}
                x1='50'
                y1='50'
                x2={end.x}
                y2={end.y}
              />
            )
          })}
        </g>
        <g className='hero-brand-tech__nodes'>
          {RAY_ANGLES.map((angle) => {
            const end = rayEndpoint(angle)
            return <circle key={angle} cx={end.x} cy={end.y} r='0.65' />
          })}
          <circle cx='50' cy='50' r='1.1' className='hero-brand-tech__node-core' />
        </g>
      </svg>

      <div aria-hidden className='hero-brand-tech__particles'>
        {PARTICLES.map((particle, index) => (
          <span
            key={index}
            className='hero-brand-tech__particle'
            style={
              {
                left: particle.left,
                top: particle.top,
                '--dx': particle.dx,
                '--dy': particle.dy,
                animationDelay: particle.delay,
              } as React.CSSProperties
            }
          />
        ))}
      </div>

      <div className='hero-brand-tech__frame' aria-hidden>
        <span className='hero-brand-tech__corner hero-brand-tech__corner--tl' />
        <span className='hero-brand-tech__corner hero-brand-tech__corner--tr' />
        <span className='hero-brand-tech__corner hero-brand-tech__corner--bl' />
        <span className='hero-brand-tech__corner hero-brand-tech__corner--br' />
      </div>

      <div className='hero-brand-tech__content'>{props.children}</div>
    </div>
  )
}
