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

export type VideoTokenTierKey =
  | 'default'
  | '480p'
  | '720p'
  | '1080p'
  | '4k'

export type VideoTokenTierPrice = {
  base: string
  with_video: string
}

export type VideoTokenModelPrice = Record<
  VideoTokenTierKey,
  VideoTokenTierPrice
>

export const VIDEO_TOKEN_TIER_KEYS: VideoTokenTierKey[] = [
  'default',
  '480p',
  '720p',
  '1080p',
  '4k',
]

export function isSeedanceVideoModel(modelName: string): boolean {
  const n = modelName.trim().toLowerCase()
  if (!n.includes('seedance')) return false
  if (!(n.startsWith('dreamina-') || n.startsWith('doubao-'))) return false
  // Only Seedance 2.x uses the resolution × with-video matrix.
  return n.includes('seedance-2') || n.includes('seedance2')
}

function isFastTierModel(modelName: string): boolean {
  const n = modelName.toLowerCase()
  return n.includes('fast') || n.includes('mini')
}

/** Official relative cards (pro / fast) used to seed empty matrices from input USD. */
const PRO_CARD: Record<VideoTokenTierKey, { base: number; with_video: number }> =
  {
    default: { base: 46, with_video: 28 },
    '480p': { base: 46, with_video: 28 },
    '720p': { base: 46, with_video: 28 },
    '1080p': { base: 51, with_video: 31 },
    '4k': { base: 26, with_video: 16 },
  }

const FAST_CARD: Record<
  VideoTokenTierKey,
  { base: number; with_video: number }
> = {
  default: { base: 37, with_video: 22 },
  '480p': { base: 37, with_video: 22 },
  '720p': { base: 37, with_video: 22 },
  '1080p': { base: 37, with_video: 22 },
  '4k': { base: 37, with_video: 22 },
}

function formatPrice(n: number): string {
  if (!Number.isFinite(n)) return ''
  const rounded = Math.round(n * 10000) / 10000
  return String(rounded)
}

export function emptyVideoTokenModelPrice(): VideoTokenModelPrice {
  return {
    default: { base: '', with_video: '' },
    '480p': { base: '', with_video: '' },
    '720p': { base: '', with_video: '' },
    '1080p': { base: '', with_video: '' },
    '4k': { base: '', with_video: '' },
  }
}

export function seedVideoTokenModelPrice(
  modelName: string,
  inputUsdPerM: number
): VideoTokenModelPrice {
  const card = isFastTierModel(modelName) ? FAST_CARD : PRO_CARD
  const baseOfficial = card.default.base
  const scale = baseOfficial > 0 ? inputUsdPerM / baseOfficial : 1
  const result = emptyVideoTokenModelPrice()
  for (const key of VIDEO_TOKEN_TIER_KEYS) {
    result[key] = {
      base: formatPrice(card[key].base * scale),
      with_video: formatPrice(card[key].with_video * scale),
    }
  }
  return result
}

export function parseVideoTokenModelPrice(
  raw: unknown
): VideoTokenModelPrice | null {
  if (!raw || typeof raw !== 'object') return null
  const src = raw as Record<string, unknown>
  const result = emptyVideoTokenModelPrice()
  let any = false
  for (const key of VIDEO_TOKEN_TIER_KEYS) {
    const tier = src[key]
    if (!tier || typeof tier !== 'object') continue
    const t = tier as Record<string, unknown>
    const base = t.base ?? t.no_video
    const withVideo = t.with_video ?? t.withVideo
    result[key] = {
      base: base === undefined || base === null ? '' : String(base),
      with_video:
        withVideo === undefined || withVideo === null ? '' : String(withVideo),
    }
    if (result[key].base || result[key].with_video) any = true
  }
  return any ? result : null
}

/** Serialize only tiers with at least one numeric value. */
export function serializeVideoTokenModelPrice(
  price: VideoTokenModelPrice
): Record<string, { base: number; with_video: number }> | null {
  const out: Record<string, { base: number; with_video: number }> = {}
  for (const key of VIDEO_TOKEN_TIER_KEYS) {
    const base = parseFloat(price[key].base)
    const withVideo = parseFloat(price[key].with_video)
    const hasBase = Number.isFinite(base) && base > 0
    const hasWith = Number.isFinite(withVideo) && withVideo > 0
    if (!hasBase && !hasWith) continue
    out[key] = {
      base: hasBase ? base : 0,
      with_video: hasWith ? withVideo : 0,
    }
  }
  return Object.keys(out).length > 0 ? out : null
}

/** Scale all matrix cells when the billing base (input / Default) changes. */
export function scaleVideoTokenModelPrice(
  price: VideoTokenModelPrice,
  nextBaseRaw: string
): VideoTokenModelPrice {
  const nextBase = parseFloat(nextBaseRaw)
  const prevBase =
    parseFloat(price.default.base) ||
    parseFloat(price['720p'].base) ||
    parseFloat(price['480p'].base)
  const result = emptyVideoTokenModelPrice()
  const fmt = (n: number) => String(Math.round(n * 10000) / 10000)

  if (!Number.isFinite(nextBase) || nextBase <= 0) {
    for (const key of VIDEO_TOKEN_TIER_KEYS) {
      result[key] = { ...price[key] }
    }
    result.default = { ...result.default, base: nextBaseRaw }
    result['480p'] = { ...result['480p'], base: nextBaseRaw }
    result['720p'] = { ...result['720p'], base: nextBaseRaw }
    return result
  }

  const scale =
    Number.isFinite(prevBase) && prevBase > 0 ? nextBase / prevBase : 1

  for (const key of VIDEO_TOKEN_TIER_KEYS) {
    const withV = parseFloat(price[key].with_video)
    const baseV = parseFloat(price[key].base)
    if (key === 'default' || key === '480p' || key === '720p') {
      result[key] = {
        base: fmt(nextBase),
        with_video: Number.isFinite(withV)
          ? fmt(withV * scale)
          : price[key].with_video,
      }
    } else {
      result[key] = {
        base: Number.isFinite(baseV) ? fmt(baseV * scale) : price[key].base,
        with_video: Number.isFinite(withV)
          ? fmt(withV * scale)
          : price[key].with_video,
      }
    }
  }
  return result
}
