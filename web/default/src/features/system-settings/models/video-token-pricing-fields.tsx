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
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import {
  scaleVideoTokenModelPrice,
  VIDEO_TOKEN_TIER_KEYS,
  type VideoTokenModelPrice,
  type VideoTokenTierKey,
} from './video-token-price'

const SHARED_BASE_TIERS: VideoTokenTierKey[] = ['default', '480p', '720p']

const numericDraftRegex = /^(\d+(\.\d*)?|\.\d*)?$/

const TIER_LABEL_KEYS: Record<VideoTokenTierKey, string> = {
  default: 'Default',
  '480p': '480p',
  '720p': '720p',
  '1080p': '1080p',
  '4k': '4k',
}

type VideoTokenPricingFieldsProps = {
  value: VideoTokenModelPrice
  onChange: (next: VideoTokenModelPrice) => void
  onFillRelativeDefaults?: () => void
}

export function VideoTokenPricingFields(props: VideoTokenPricingFieldsProps) {
  const { t } = useTranslation()

  const updateCell = (
    tier: VideoTokenTierKey,
    field: 'base' | 'with_video',
    raw: string
  ) => {
    if (!numericDraftRegex.test(raw)) return
    // Default/480p/720p share the model input base; editing any scales the matrix.
    if (field === 'base' && SHARED_BASE_TIERS.includes(tier)) {
      props.onChange(scaleVideoTokenModelPrice(props.value, raw))
      return
    }
    props.onChange({
      ...props.value,
      [tier]: {
        ...props.value[tier],
        [field]: raw,
      },
    })
  }

  const isEmpty =
    !props.value.default.base &&
    !props.value['720p'].base &&
    !props.value['480p'].base &&
    !props.value['1080p'].base &&
    !props.value['4k'].base

  return (
    <div className='flex flex-col gap-3 rounded-lg border p-3'>
      <div className='flex items-start justify-between gap-2'>
        <div>
          <div className='text-sm font-medium'>{t('Video token billing')}</div>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t(
              'Relative unit prices in the same unit as Input price. Default/720p base should match Input. Leave all cells empty to use built-in Seedance ratios.'
            )}
          </p>
        </div>
        {isEmpty && props.onFillRelativeDefaults && (
          <button
            type='button'
            className='text-primary shrink-0 text-xs underline-offset-2 hover:underline'
            onClick={props.onFillRelativeDefaults}
          >
            {t('Fill relative defaults')}
          </button>
        )}
      </div>
      <div className='grid grid-cols-[72px_1fr_1fr] items-end gap-2 text-xs font-medium'>
        <div />
        <div className='text-muted-foreground'>{t('Base Price')}</div>
        <div className='text-muted-foreground'>{t('With video input')}</div>
      </div>
      {VIDEO_TOKEN_TIER_KEYS.map((tier) => (
        <div
          key={tier}
          className='grid grid-cols-[72px_1fr_1fr] items-center gap-2'
        >
          <FieldLabel className='text-muted-foreground text-xs'>
            {t(TIER_LABEL_KEYS[tier])}
          </FieldLabel>
          <Field className='gap-1'>
            <InputGroup>
              <InputGroupAddon>$</InputGroupAddon>
              <InputGroupInput
                inputMode='decimal'
                value={props.value[tier].base}
                placeholder='0'
                onChange={(e) => updateCell(tier, 'base', e.target.value)}
              />
              <InputGroupAddon align='inline-end'>/ 1M</InputGroupAddon>
            </InputGroup>
          </Field>
          <Field className='gap-1'>
            <InputGroup>
              <InputGroupAddon>$</InputGroupAddon>
              <InputGroupInput
                inputMode='decimal'
                value={props.value[tier].with_video}
                placeholder='0'
                onChange={(e) =>
                  updateCell(tier, 'with_video', e.target.value)
                }
              />
              <InputGroupAddon align='inline-end'>/ 1M</InputGroupAddon>
            </InputGroup>
          </Field>
        </div>
      ))}
      <FieldDescription>
        {t(
          'Leave empty to use built-in Seedance fallback ratios for this model.'
        )}
      </FieldDescription>
    </div>
  )
}
