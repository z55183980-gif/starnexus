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
import { useTranslation } from 'react-i18next'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  formatGPTImageTierPrice,
  formatVideoTierPrice,
  getSpecialPricingKind,
  getVideoPriceRange,
  getVideoPricingTiers,
  GPT_IMAGE_TIERS,
  type SpecialPriceFormatOptions,
} from '../lib/special-price'
import type { PricingModel } from '../types'

type SpecialPricingProps = SpecialPriceFormatOptions & {
  model: PricingModel
}

export function SpecialPricingSummary(props: SpecialPricingProps) {
  const { t } = useTranslation()
  const kind = getSpecialPricingKind(props.model)
  if (!kind) return null

  if (kind === 'gpt-image') {
    return (
      <>
        {GPT_IMAGE_TIERS.map((tier) => (
          <span key={tier} className='text-muted-foreground whitespace-nowrap'>
            {tier.toUpperCase()}{' '}
            <span className='text-foreground font-mono font-semibold'>
              {formatGPTImageTierPrice(props.model, tier, props)}
            </span>
          </span>
        ))}
        <span className='text-muted-foreground/60 whitespace-nowrap'>
          / {t('image')}
        </span>
      </>
    )
  }

  const baseRange = getVideoPriceRange(props.model, 'base', props)
  const videoRange = getVideoPriceRange(props.model, 'with_video', props)
  const unit = props.tokenUnit === 'K' ? '1K' : '1M'
  return (
    <>
      {baseRange && (
        <span className='text-muted-foreground whitespace-nowrap'>
          {t('Text-to-video')}{' '}
          <span className='text-foreground font-mono font-semibold'>
            {baseRange}
          </span>
          /{unit}
        </span>
      )}
      {videoRange && (
        <span className='text-muted-foreground whitespace-nowrap'>
          {t('Video-to-video')}{' '}
          <span className='text-foreground font-mono font-semibold'>
            {videoRange}
          </span>
          /{unit}
        </span>
      )}
    </>
  )
}

export function SpecialPricingMatrix(props: SpecialPricingProps) {
  const { t } = useTranslation()
  const kind = getSpecialPricingKind(props.model)
  if (!kind) return null

  if (kind === 'gpt-image') {
    return (
      <div className='grid grid-cols-1 gap-2 sm:grid-cols-3'>
        {GPT_IMAGE_TIERS.map((tier) => (
          <div key={tier} className='bg-muted/20 rounded-lg border p-3'>
            <div className='text-muted-foreground text-xs'>
              {tier.toUpperCase()}
            </div>
            <div className='text-foreground mt-1 font-mono text-base font-semibold tabular-nums'>
              {formatGPTImageTierPrice(props.model, tier, props)}
              <span className='text-muted-foreground/40 ml-1 text-xs font-normal'>
                / {t('image')}
              </span>
            </div>
          </div>
        ))}
      </div>
    )
  }

  const unit = props.tokenUnit === 'K' ? '1K' : '1M'
  return (
    <div className='overflow-x-auto rounded-lg border'>
      <Table className='text-sm'>
        <TableHeader>
          <TableRow className='hover:bg-transparent'>
            <TableHead>{t('Resolution')}</TableHead>
            <TableHead className='text-right'>{t('Text-to-video')}</TableHead>
            <TableHead className='text-right'>{t('Video-to-video')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {getVideoPricingTiers(props.model).map(([resolution, tier]) => (
            <TableRow key={resolution}>
              <TableCell className='text-muted-foreground py-2.5 text-xs font-medium uppercase'>
                {resolution === 'default' ? t('Default') : resolution}
              </TableCell>
              <TableCell className='py-2.5 text-right font-mono'>
                {formatVideoTierPrice(props.model, tier.base, props)}
              </TableCell>
              <TableCell className='py-2.5 text-right font-mono'>
                {formatVideoTierPrice(props.model, tier.with_video, props)}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <p className='text-muted-foreground/50 px-3 py-2 text-right text-[10px]'>
        {t('Prices shown per')} {unit} {t('tokens')}
      </p>
    </div>
  )
}
