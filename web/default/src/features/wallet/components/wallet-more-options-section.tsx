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
import { useState } from 'react'
import { ChevronDown, ExternalLink, Gift, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { TitledCard } from '@/components/ui/titled-card'

interface WalletMoreOptionsSectionProps {
  redemptionCode: string
  onRedemptionCodeChange: (code: string) => void
  onRedeem: () => void
  redeeming: boolean
  topupLink?: string
  redemptionEnabled?: boolean
}

export function WalletMoreOptionsSection({
  redemptionCode,
  onRedemptionCodeChange,
  onRedeem,
  redeeming,
  topupLink,
  redemptionEnabled = true,
}: WalletMoreOptionsSectionProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  if (!redemptionEnabled && !topupLink) {
    return null
  }

  return (
    <TitledCard
      title={t('More top-up options')}
      description={t('Redeem a code or use alternative top-up methods')}
      icon={<Gift className='h-4 w-4' />}
      contentClassName='pt-0'
    >
      <Collapsible open={open} onOpenChange={setOpen}>
        <CollapsibleTrigger
          render={
            <Button
              type='button'
              variant='outline'
              className='flex h-10 w-full items-center justify-between px-3 sm:h-11 sm:px-4'
            />
          }
        >
          <span className='text-sm font-medium'>{t('Have a Code?')}</span>
          <ChevronDown
            className={`text-muted-foreground size-4 shrink-0 transition-transform duration-200 ${
              open ? 'rotate-180' : ''
            }`}
          />
        </CollapsibleTrigger>

        <CollapsibleContent className='CollapsibleContent space-y-3 pt-4 sm:space-y-4'>
          {redemptionEnabled ? (
            <>
              <div className='grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,1fr)_auto]'>
                <div className='space-y-2'>
                  <Label htmlFor='redemption-code' className='sr-only'>
                    {t('Enter your redemption code')}
                  </Label>
                  <Input
                    id='redemption-code'
                    value={redemptionCode}
                    onChange={(e) => onRedemptionCodeChange(e.target.value)}
                    placeholder={t('Enter your redemption code')}
                    className='h-10 min-w-0'
                  />
                </div>
                <Button
                  onClick={onRedeem}
                  disabled={redeeming}
                  variant='outline'
                  className='h-10 w-full sm:w-auto sm:px-6'
                >
                  {redeeming && (
                    <Loader2 className='mr-2 h-4 w-4 animate-spin' />
                  )}
                  {t('Redeem')}
                </Button>
              </div>
              {topupLink && (
                <p className='text-muted-foreground text-xs'>
                  {t('Need a redemption code?')}{' '}
                  <a
                    href={topupLink}
                    target='_blank'
                    rel='noopener noreferrer'
                    className='inline-flex items-center gap-1 underline-offset-4 hover:underline'
                  >
                    {t('Get one here')}
                    <ExternalLink className='h-3 w-3' />
                  </a>
                </p>
              )}
            </>
          ) : (
            <Alert>
              <AlertDescription>
                {t(
                  'Redemption codes are disabled until the administrator confirms compliance terms.'
                )}
              </AlertDescription>
            </Alert>
          )}
        </CollapsibleContent>
      </Collapsible>
    </TitledCard>
  )
}
