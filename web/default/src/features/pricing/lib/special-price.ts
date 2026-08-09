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
import type { PricingModel, TokenUnit, VideoTokenTierPrice } from '../types'
import { formatDirectPrice, getModelMinGroupRatio } from './price'

export const GPT_IMAGE_TIERS = ['1k', '2k', '4k'] as const
export const VIDEO_TIER_ORDER = [
  'default',
  '480p',
  '720p',
  '1080p',
  '4k',
] as const

export type SpecialPricingKind = 'gpt-image' | 'video-token'

export type SpecialPriceFormatOptions = {
  tokenUnit: TokenUnit
  showRechargePrice: boolean
  priceRate: number
  usdExchangeRate: number
  groupRatioMultiplier?: number
}

export function getSpecialPricingKind(
  model: PricingModel
): SpecialPricingKind | null {
  if (model.gpt_image_price && Object.keys(model.gpt_image_price).length > 0)
    return 'gpt-image'
  if (
    model.video_token_price &&
    Object.keys(model.video_token_price).length > 0
  )
    return 'video-token'
  return null
}

export function getSpecialPricingTypeLabel(model: PricingModel): string | null {
  const kind = getSpecialPricingKind(model)
  if (kind === 'gpt-image') return 'Image tiers'
  if (kind === 'video-token') return 'Video tiers'
  return null
}

export function getSpecialDisplayRatio(
  model: PricingModel,
  explicitRatio?: number
): number {
  return explicitRatio ?? getModelMinGroupRatio(model)
}

export function formatGPTImageTierPrice(
  model: PricingModel,
  tier: string,
  options: SpecialPriceFormatOptions
): string {
  const price = model.gpt_image_price?.[tier]
  if (!Number.isFinite(price)) return '-'
  return formatDirectPrice(
    Number(price),
    getSpecialDisplayRatio(model, options.groupRatioMultiplier),
    null,
    options.showRechargePrice,
    options.priceRate,
    options.usdExchangeRate
  )
}

export function getVideoPricingTiers(
  model: PricingModel
): Array<[string, VideoTokenTierPrice]> {
  const prices = model.video_token_price || {}
  const known = VIDEO_TIER_ORDER.filter((key) => prices[key]).map(
    (key) => [key, prices[key]] as [string, VideoTokenTierPrice]
  )
  const remaining = Object.keys(prices)
    .filter(
      (key) =>
        !VIDEO_TIER_ORDER.includes(key as (typeof VIDEO_TIER_ORDER)[number])
    )
    .sort()
    .map((key) => [key, prices[key]] as [string, VideoTokenTierPrice])
  return [...known, ...remaining]
}

export function formatVideoTierPrice(
  model: PricingModel,
  price: number,
  options: SpecialPriceFormatOptions
): string {
  if (!Number.isFinite(price) || price <= 0) return '-'
  return formatDirectPrice(
    price,
    getSpecialDisplayRatio(model, options.groupRatioMultiplier),
    options.tokenUnit,
    options.showRechargePrice,
    options.priceRate,
    options.usdExchangeRate
  )
}

export function getVideoPriceRange(
  model: PricingModel,
  field: keyof VideoTokenTierPrice,
  options: SpecialPriceFormatOptions
): string | null {
  const prices = getVideoPricingTiers(model)
    .map(([, tier]) => Number(tier[field]))
    .filter((price) => Number.isFinite(price) && price > 0)
  if (prices.length === 0) return null
  const min = Math.min(...prices)
  const max = Math.max(...prices)
  const minFormatted = formatVideoTierPrice(model, min, options)
  if (min === max) return minFormatted
  return `${minFormatted} – ${formatVideoTierPrice(model, max, options)}`
}
