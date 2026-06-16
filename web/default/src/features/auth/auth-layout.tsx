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
import type { CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'
import { useDisplaySystemName } from '@/hooks/use-display-system-name'
import { useSystemConfig } from '@/hooks/use-system-config'
import { Skeleton } from '@/components/ui/skeleton'
import { LanguageSwitcher } from '@/components/language-switcher'

type AuthLayoutProps = {
  children: React.ReactNode
  variant?: 'default' | 'sign-in'
}

export function AuthLayout(props: AuthLayoutProps) {
  const { t } = useTranslation()
  const { logo, loading } = useSystemConfig()
  const displaySystemName = useDisplaySystemName()

  if (props.variant === 'sign-in') {
    return (
      <div className='relative min-h-svh overflow-hidden bg-[radial-gradient(circle_at_12%_16%,rgba(88,166,255,0.20),transparent_32%),radial-gradient(circle_at_84%_18%,rgba(167,139,250,0.18),transparent_30%),linear-gradient(135deg,#f7fbff_0%,#f5f8ff_48%,#f8f5ff_100%)] text-slate-900 dark:bg-[radial-gradient(circle_at_12%_16%,rgba(59,130,246,0.18),transparent_32%),radial-gradient(circle_at_84%_18%,rgba(124,58,237,0.18),transparent_30%),linear-gradient(135deg,#08111f_0%,#101827_48%,#151124_100%)] dark:text-white'>
        <div className='pointer-events-none absolute inset-0 bg-[linear-gradient(90deg,rgba(255,255,255,0.52),transparent_42%,rgba(255,255,255,0.34)),radial-gradient(circle_at_65%_76%,rgba(59,130,246,0.10),transparent_26%)] dark:bg-[linear-gradient(90deg,rgba(15,23,42,0.20),transparent_42%,rgba(15,23,42,0.18))]' />

        <div className='absolute top-4 right-5 z-20'>
          <LanguageSwitcher />
        </div>

        <Link
          to='/'
          className='absolute top-5 left-6 z-20 flex items-center gap-2 transition-opacity hover:opacity-85'
        >
          <div className='relative h-9 w-9 overflow-hidden rounded-xl bg-white/70 shadow-sm ring-1 ring-white/70 backdrop-blur dark:bg-white/10 dark:ring-white/10'>
            {loading ? (
              <Skeleton className='absolute inset-0 rounded-xl' />
            ) : (
              <img
                src={logo}
                alt={t('Logo')}
                className='h-full w-full object-cover'
              />
            )}
          </div>
          {loading ? (
            <Skeleton className='h-8 w-28' />
          ) : (
            <h1 className='bg-gradient-to-r from-blue-600 via-indigo-500 to-violet-500 bg-clip-text text-lg font-semibold tracking-tight text-transparent sm:text-xl'>
              {displaySystemName}
            </h1>
          )}
        </Link>

        <div className='relative z-10 mx-auto grid min-h-svh w-full max-w-7xl items-center gap-10 px-6 pt-24 pb-8 lg:grid-cols-[minmax(0,1fr)_420px] lg:px-10 lg:pt-20'>
          <section className='relative hidden min-h-[620px] flex-col lg:flex'>
            <div className='max-w-2xl space-y-5'>
              <p className='text-3xl font-semibold tracking-tight text-slate-800 sm:text-4xl dark:text-white'>
                {t('Stronger models, lower prices, easier integration')}
              </p>
              <p className='max-w-xl text-sm leading-6 text-slate-500 dark:text-slate-300/70'>
                {t(
                  'Deliver fast and convenient Web API access for developers, and build an AI API platform that is easy to use and easy to deploy.'
                )}
              </p>
            </div>

            <div className='absolute bottom-0 left-0 h-[430px] w-[430px] xl:h-[500px] xl:w-[500px]'>
              <AnimatedLoginGlobe />
            </div>
          </section>

          <main className='mx-auto w-full max-w-[420px]'>{props.children}</main>
        </div>
      </div>
    )
  }

  return (
    <div className='relative grid h-svh max-w-none'>
      <div className='absolute top-4 right-4 z-10 sm:top-6 sm:right-6'>
        <LanguageSwitcher />
      </div>

      <Link
        to='/'
        className='absolute top-4 left-4 z-10 flex items-center gap-2 transition-opacity hover:opacity-80 sm:top-8 sm:left-8'
      >
        <div className='relative h-8 w-8'>
          {loading ? (
            <Skeleton className='absolute inset-0 rounded-full' />
          ) : (
            <img
              src={logo}
              alt={t('Logo')}
              className='h-8 w-8 rounded-full object-cover'
            />
          )}
        </div>
        {loading ? (
          <Skeleton className='h-6 w-24' />
        ) : (
          <h1 className='text-xl font-medium'>{displaySystemName}</h1>
        )}
      </Link>
      <div className='container flex items-center pt-16 sm:pt-0'>
        <div className='mx-auto flex w-full flex-col justify-center space-y-2 px-4 py-8 sm:w-[480px] sm:p-8'>
          {props.children}
        </div>
      </div>
    </div>
  )
}

// ── Static data (computed once at module load) ────────────────────────────

type OrbitParticle = {
  startAngle: number
  radius: number
  dur: string
  delay: string
  color: string
  size: number
  ccw?: boolean
}

const ORBIT_PARTICLES: OrbitParticle[] = [
  { startAngle: 0,   radius: 168, dur: '10s', delay: '0s',   color: '#67e8f9', size: 2.2 },
  { startAngle: 55,  radius: 182, dur: '13s', delay: '1.5s', color: '#a78bfa', size: 1.8 },
  { startAngle: 125, radius: 174, dur: '11s', delay: '0.8s', color: '#f9a8d4', size: 2.0 },
  { startAngle: 205, radius: 186, dur: '15s', delay: '2.2s', color: '#818cf8', size: 1.5, ccw: true },
  { startAngle: 285, radius: 170, dur: '12s', delay: '0.4s', color: '#67e8f9', size: 1.8 },
  { startAngle: 345, radius: 179, dur: '9s',  delay: '1.8s', color: '#f0abfc', size: 1.4, ccw: true },
  { startAngle: 92,  radius: 190, dur: '14s', delay: '3.0s', color: '#a78bfa', size: 1.6 },
  { startAngle: 162, radius: 176, dur: '16s', delay: '2.5s', color: '#7dd3fc', size: 2.0, ccw: true },
]

type EmberParticle = {
  cx: number; cy: number; ex: number; ey: number
  dur: string; delay: string; color: string; r: number
}

const EMBER_PARTICLES: EmberParticle[] = Array.from({ length: 22 }, (_, i) => {
  const angle = (i / 22) * 2 * Math.PI
  const startR = 128 + (i % 5) * 5
  const cx = Math.round(220 + startR * Math.cos(angle))
  const cy = Math.round(220 + startR * Math.sin(angle))
  const dAngle = angle + ((i % 3 - 1) * Math.PI) / 7
  const dist = 38 + (i % 5) * 15
  const ex = Math.round(dist * Math.cos(dAngle))
  const ey = Math.round(dist * Math.sin(dAngle))
  const colors = ['#67e8f9', '#a78bfa', '#f9a8d4', '#7dd3fc', '#c4b5fd']
  return {
    cx, cy, ex, ey,
    dur: `${(2.6 + (i % 6) * 0.55).toFixed(1)}s`,
    delay: `${((i * 0.27) % 3.5).toFixed(2)}s`,
    color: colors[i % 5],
    r: 1.0 + (i % 4) * 0.35,
  }
})

const BG_STARS: Array<[number, number, string, number]> = [
  [38, 62, '0s', 3.2],
  [392, 56, '0.5s', 4],
  [410, 200, '1.0s', 3],
  [406, 336, '1.5s', 3.6],
  [48, 372, '2.0s', 2.8],
  [20, 248, '2.5s', 2.4],
  [170, 20, '0.8s', 3],
  [322, 26, '1.3s', 2.8],
  [430, 120, '1.9s', 2.2],
  [14, 140, '0.3s', 2],
]

const WISPS = [
  { x: 75,  y: 120, color: '#a78bfa', r: 42, dur: '8s',  delay: '0s', wx: -15, wy: -25 },
  { x: 340, y: 310, color: '#67e8f9', r: 38, dur: '10s', delay: '2s', wx:  12, wy:  20 },
  { x: 80,  y: 330, color: '#f0abfc', r: 34, dur: '9s',  delay: '4s', wx: -10, wy:  18 },
  { x: 362, y: 115, color: '#818cf8', r: 36, dur: '11s', delay: '1s', wx:  10, wy: -18 },
]

// ── Component ─────────────────────────────────────────────────────────────

function AnimatedLoginGlobe() {
  return (
    <div className='lp-scene absolute inset-0'>
      <style>{`
        @keyframes lp-float {
          0%,100% { transform:translateY(0); }
          50%      { transform:translateY(-18px); }
        }
        @keyframes lp-twinkle {
          0%,100% { opacity:.18; }
          50%      { opacity:1; }
        }
        @keyframes lp-shimmer {
          0%,100% { opacity:.78; }
          50%      { opacity:1; }
        }
        @keyframes lp-ember {
          0%   { opacity:.92; transform:translate(0,0) scale(1); }
          65%  { opacity:.45; }
          100% { opacity:0;   transform:translate(var(--ex),var(--ey)) scale(.18); }
        }
        @keyframes lp-inner-pulse {
          0%,100% { opacity:.08; }
          50%      { opacity:.32; }
        }
        @keyframes lp-wisp {
          0%,100% { opacity:.1;  transform:translate(0,0); }
          50%      { opacity:.24; transform:translate(var(--wx),var(--wy)); }
        }
        .lp-float        { animation:lp-float 7s ease-in-out infinite; }
        .lp-star         { animation:lp-twinkle 2.8s ease-in-out infinite; }
        .lp-ring         { animation:lp-shimmer 3.2s ease-in-out infinite; }
        .lp-ember        { animation:lp-ember var(--dur) ease-out infinite; animation-delay:var(--delay); }
        .lp-inner-pulse  { animation:lp-inner-pulse 4.5s ease-in-out infinite; }
        .lp-wisp         { animation:lp-wisp var(--dur,8s) ease-in-out infinite; animation-delay:var(--delay,0s); }
      `}</style>

      <div className='lp-float absolute inset-0'>
        <svg
          viewBox='0 0 440 440'
          className='absolute inset-0 h-full w-full'
          aria-hidden='true'
          xmlns='http://www.w3.org/2000/svg'
        >
          <defs>
            {/* Ethereal planet: icy luminous core → spectral cyan → soft violet → misty indigo */}
            <radialGradient id='lp-pg' cx='34%' cy='28%' r='72%'>
              <stop offset='0%'   stopColor='#f0fbff' />
              <stop offset='14%'  stopColor='#bae6fd' />
              <stop offset='34%'  stopColor='#c4b5fd' />
              <stop offset='58%'  stopColor='#a78bfa' />
              <stop offset='78%'  stopColor='#7c3aed' />
              <stop offset='100%' stopColor='#3b0764' />
            </radialGradient>

            {/* Inner luminescence (animated via .lp-inner-pulse) */}
            <radialGradient id='lp-inner' cx='40%' cy='36%' r='60%'>
              <stop offset='0%'   stopColor='#e0f2fe' />
              <stop offset='55%'  stopColor='#818cf8' stopOpacity='0.4' />
              <stop offset='100%' stopColor='transparent' />
            </radialGradient>

            {/* Atmosphere rim */}
            <radialGradient id='lp-atm' cx='50%' cy='50%' r='50%'>
              <stop offset='70%'  stopColor='transparent' />
              <stop offset='90%'  stopColor='#818cf8' stopOpacity='0.42' />
              <stop offset='100%' stopColor='#c4b5fd' stopOpacity='0.62' />
            </radialGradient>

            {/* Wider spectral aura */}
            <radialGradient id='lp-aura' cx='50%' cy='50%' r='50%'>
              <stop offset='76%'  stopColor='transparent' />
              <stop offset='100%' stopColor='#7dd3fc' stopOpacity='0.22' />
            </radialGradient>

            {/* Outer nebula haze */}
            <radialGradient id='lp-haze' cx='50%' cy='50%' r='50%'>
              <stop offset='0%'   stopColor='#7c3aed' stopOpacity='0.22' />
              <stop offset='100%' stopColor='#7c3aed' stopOpacity='0' />
            </radialGradient>

            {/* Primary ring: cyan → white → pink */}
            <linearGradient id='lp-rg1' x1='0' y1='0' x2='1' y2='0'>
              <stop offset='0%'   stopColor='#67e8f9' stopOpacity='0.06' />
              <stop offset='22%'  stopColor='#67e8f9' stopOpacity='0.95' />
              <stop offset='50%'  stopColor='#e0f2fe' />
              <stop offset='78%'  stopColor='#f0abfc' stopOpacity='0.95' />
              <stop offset='100%' stopColor='#f0abfc' stopOpacity='0.06' />
            </linearGradient>

            {/* Secondary ring: violet */}
            <linearGradient id='lp-rg2' x1='0' y1='0' x2='1' y2='0'>
              <stop offset='0%'   stopColor='#a78bfa' stopOpacity='0.04' />
              <stop offset='25%'  stopColor='#a78bfa' stopOpacity='0.72' />
              <stop offset='50%'  stopColor='#ddd6fe' stopOpacity='0.82' />
              <stop offset='75%'  stopColor='#c4b5fd' stopOpacity='0.72' />
              <stop offset='100%' stopColor='#c4b5fd' stopOpacity='0.04' />
            </linearGradient>

            <filter id='lp-glow'  x='-35%' y='-35%' width='170%' height='170%'>
              <feGaussianBlur stdDeviation='5' result='blur' />
              <feMerge><feMergeNode in='blur' /><feMergeNode in='SourceGraphic' /></feMerge>
            </filter>
            <filter id='lp-sglow' x='-80%' y='-80%' width='360%' height='360%'>
              <feGaussianBlur stdDeviation='2.5' result='blur' />
              <feMerge><feMergeNode in='blur' /><feMergeNode in='SourceGraphic' /></feMerge>
            </filter>
            <filter id='lp-pglow' x='-150%' y='-150%' width='400%' height='400%'>
              <feGaussianBlur stdDeviation='3.5' result='blur' />
              <feMerge><feMergeNode in='blur' /><feMergeNode in='SourceGraphic' /></feMerge>
            </filter>
            <filter id='lp-wglow' x='-50%' y='-50%' width='200%' height='200%'>
              <feGaussianBlur stdDeviation='20' />
            </filter>

            <clipPath id='lp-pc'><circle cx='220' cy='220' r='144' /></clipPath>
            <mask id='lp-rbm'>
              <rect x='0' y='0' width='440' height='220' fill='white' />
              <circle cx='220' cy='220' r='144' fill='black' />
            </mask>
            <mask id='lp-rfm'>
              <rect x='0' y='220' width='440' height='220' fill='white' />
            </mask>
          </defs>

          {/* Outer nebula haze */}
          <circle cx='220' cy='220' r='215' fill='url(#lp-haze)' />

          {/* Floating nebula wisps */}
          {WISPS.map((w) => (
            <circle
              key={`wisp-${w.x}-${w.y}`}
              cx={w.x} cy={w.y} r={w.r}
              fill={w.color}
              filter='url(#lp-wglow)'
              className='lp-wisp'
              style={{ '--dur': w.dur, '--delay': w.delay, '--wx': `${w.wx}px`, '--wy': `${w.wy}px` } as CSSProperties}
            />
          ))}

          {/* ── Ring BACK (behind planet) ── */}
          <g className='lp-ring'>
            <ellipse cx='220' cy='220' rx='215' ry='40' fill='none' stroke='url(#lp-rg1)' strokeWidth='9'  mask='url(#lp-rbm)' transform='rotate(-18,220,220)' filter='url(#lp-glow)' />
            <ellipse cx='220' cy='220' rx='223' ry='50' fill='none' stroke='url(#lp-rg2)' strokeWidth='5'  mask='url(#lp-rbm)' transform='rotate(-18,220,220)' />
          </g>

          {/* ── Ethereal planet body ── */}
          <circle cx='220' cy='220' r='144' fill='url(#lp-pg)' />

          {/* Surface light streaks */}
          <g clipPath='url(#lp-pc)' opacity='0.28'>
            <line x1='75'  y1='195' x2='285' y2='202' stroke='#bae6fd' strokeWidth='1.3' transform='rotate(-15,180,198)' />
            <line x1='88'  y1='212' x2='310' y2='220' stroke='#bae6fd' strokeWidth='1.8' transform='rotate(-15,199,216)' />
            <line x1='95'  y1='234' x2='285' y2='240' stroke='#c4b5fd' strokeWidth='1.2' transform='rotate(-13,190,237)' />
            <line x1='105' y1='254' x2='265' y2='260' stroke='#c4b5fd' strokeWidth='0.9' transform='rotate(-11,185,257)' />
          </g>

          {/* Inner ethereal pulse — the "soul glow" */}
          <circle cx='220' cy='220' r='144' fill='url(#lp-inner)' className='lp-inner-pulse' />

          {/* Atmosphere rim layers */}
          <circle cx='220' cy='220' r='144' fill='url(#lp-atm)' />
          <circle cx='220' cy='220' r='155' fill='url(#lp-aura)' />

          {/* Specular highlight */}
          <ellipse cx='185' cy='178' rx='58' ry='42' fill='white' fillOpacity='0.065' transform='rotate(-18,185,178)' />

          {/* ── Ring FRONT (in front of planet) ── */}
          <g className='lp-ring' style={{ animationDelay: '1.6s' }}>
            <ellipse cx='220' cy='220' rx='215' ry='40' fill='none' stroke='url(#lp-rg1)' strokeWidth='11' mask='url(#lp-rfm)' transform='rotate(-18,220,220)' filter='url(#lp-glow)' />
            <ellipse cx='220' cy='220' rx='223' ry='50' fill='none' stroke='url(#lp-rg2)' strokeWidth='5'  mask='url(#lp-rfm)' transform='rotate(-18,220,220)' />
          </g>

          {/* ── Orbital dust particles (SVG animateTransform – correct rotation around planet center) ── */}
          {ORBIT_PARTICLES.map((o, i) => {
            const rad = (o.startAngle * Math.PI) / 180
            return (
              <circle
                key={`orb-${i}`}
                cx={Math.round(220 + o.radius * Math.cos(rad))}
                cy={Math.round(220 + o.radius * Math.sin(rad))}
                r={o.size}
                fill={o.color}
                filter='url(#lp-pglow)'
              >
                <animateTransform
                  attributeName='transform'
                  type='rotate'
                  from={`0 220 220`}
                  to={`${o.ccw ? -360 : 360} 220 220`}
                  dur={o.dur}
                  begin={o.delay}
                  repeatCount='indefinite'
                />
              </circle>
            )
          })}

          {/* ── Ember particles – burst from surface, fade outward ── */}
          {EMBER_PARTICLES.map((e, i) => (
            <circle
              key={`emb-${i}`}
              cx={e.cx} cy={e.cy} r={e.r}
              fill={e.color}
              filter='url(#lp-pglow)'
              className='lp-ember'
              style={{ '--ex': `${e.ex}px`, '--ey': `${e.ey}px`, '--dur': e.dur, '--delay': e.delay } as CSSProperties}
            />
          ))}

          {/* ── Background stars ── */}
          {BG_STARS.map(([cx, cy, delay, r]) => (
            <circle
              key={`star-${cx}-${cy}`}
              cx={cx} cy={cy} r={r}
              fill='#f0e6ff'
              className='lp-star'
              filter='url(#lp-sglow)'
              style={{ animationDelay: delay } as CSSProperties}
            />
          ))}
        </svg>
      </div>
    </div>
  )
}
