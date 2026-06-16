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
import { type ReactNode } from 'react'
import i18next from 'i18next'
import { CreditCard } from 'lucide-react'
import { SiStripe, SiTether, SiWechat } from 'react-icons/si'
import { PAYMENT_TYPES, PAYMENT_ICON_COLORS } from '../constants'

// ============================================================================
// UI Helper Functions
// ============================================================================

const HAS_LOCATION =
  typeof globalThis !== 'undefined' && 'location' in globalThis

/**
 * Resolves a backend-provided image URL to http(s) only. Rejects javascript:,
 * data:, blob:, file:, and URLs with userinfo, which are unsafe in <img src/>.
 */
function normalizeHttpIconUrl(raw: string | undefined | null): string | null {
  if (!raw) return null
  const s = raw.trim()
  if (!s) return null
  let url: URL
  try {
    url = HAS_LOCATION
      ? new URL(s, (globalThis as { location: Location }).location.href)
      : new URL(s)
  } catch {
    return null
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    return null
  }
  if (url.username || url.password) {
    return null
  }
  return url.toString()
}

/**
 * Get payment method icon component
 *
 * When iconUrl is provided, render an <img/> with that URL so custom
 * gateway logos can be configured per-method.
 */
export function getPaymentIcon(
  paymentType: string | undefined,
  className: string = 'h-4 w-4',
  iconUrl?: string,
  altName?: string
): ReactNode {
  const safeIconUrl = normalizeHttpIconUrl(iconUrl)
  if (safeIconUrl) {
    return (
      <img
        src={safeIconUrl}
        alt={altName || paymentType || 'payment'}
        className={className}
        style={{ objectFit: 'contain' }}
        loading='lazy'
        decoding='async'
        referrerPolicy='no-referrer'
      />
    )
  }

  if (!paymentType) {
    return <CreditCard className={className} />
  }

  switch (paymentType) {
    case PAYMENT_TYPES.USDT:
      return (
        <SiTether
          className={className}
          style={{ color: PAYMENT_ICON_COLORS[PAYMENT_TYPES.USDT] }}
        />
      )
    case PAYMENT_TYPES.STRIPE:
      return (
        <SiStripe
          className={className}
          style={{ color: PAYMENT_ICON_COLORS[PAYMENT_TYPES.STRIPE] }}
        />
      )
    default:
      return <CreditCard className={className} />
  }
}

type PaymentMethodBadgeProps = {
  icon: ReactNode
  label: string
}

function PaymentMethodBadge({ icon, label }: PaymentMethodBadgeProps) {
  return (
    <span className='flex shrink-0 items-center gap-1.5'>
      <span className='bg-background flex size-8 shrink-0 items-center justify-center rounded-lg border sm:size-9'>
        {icon}
      </span>
      <span className='text-xs font-medium whitespace-nowrap sm:text-sm'>
        {label}
      </span>
    </span>
  )
}

/**
 * Accepted payment methods shown beside Stripe in the wallet selector.
 */
export function StripeAcceptedPaymentMethods({
  iconClassName = 'h-4 w-4 sm:h-5 sm:w-5',
}: {
  iconClassName?: string
}) {
  return (
    <span className='flex shrink-0 items-center gap-2 sm:gap-3'>
      <PaymentMethodBadge
        icon={
          <SiWechat
            className={iconClassName}
            style={{ color: '#07C160' }}
          />
        }
        label={i18next.t('WeChat Pay')}
      />
      <PaymentMethodBadge
        icon={<CreditCard className={iconClassName} style={{ color: '#4B5563' }} />}
        label={i18next.t('Credit Card')}
      />
    </span>
  )
}
