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
import {
  Add01Icon,
  AppleIcon,
  ArrowDown01Icon,
  ComputerIcon,
  Download03Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import keyToolLogo from '@/assets/starnexus-keytool.png'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { getKeyToolPlatform } from '../lib/key-tool-platform'
import { useApiKeys } from './api-keys-provider'

const DOWNLOADS = [
  {
    platform: 'windows',
    label: 'Windows',
    icon: ComputerIcon,
    description: '64-bit · EXE installer',
    href: 'https://dkby.com/downloads/starnexus-keytool/windows',
    backup: 'https://dkby.com/downloads/starnexus-keytool/windows/github',
  },
  {
    platform: 'macos',
    label: 'macOS',
    icon: AppleIcon,
    description: 'Apple Silicon / Intel universal',
    href: 'https://dkby.com/downloads/starnexus-keytool/macos',
    backup: 'https://dkby.com/downloads/starnexus-keytool/macos/github',
  },
] as const

export function ApiKeysPrimaryButtons() {
  const { t } = useTranslation()
  const { setOpen } = useApiKeys()
  const platform =
    typeof navigator === 'undefined' ? null : getKeyToolPlatform(navigator)
  const downloads = [...DOWNLOADS].sort(
    (a, b) => Number(b.platform === platform) - Number(a.platform === platform)
  )

  return (
    <div className='flex max-w-[calc(100vw-1.5rem)] flex-wrap items-center gap-2'>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant='outline'
              aria-label={t('Download API Key Config Tool')}
            />
          }
        >
          <HugeiconsIcon icon={Download03Icon} data-icon='inline-start' />
          <span className='hidden sm:inline'>{t('Download config tool')}</span>
          <span className='sm:hidden'>{t('Config tool')}</span>
          <HugeiconsIcon icon={ArrowDown01Icon} data-icon='inline-end' />
        </DropdownMenuTrigger>
        <DropdownMenuContent
          align='end'
          sideOffset={8}
          className='w-80 max-w-[calc(100vw-2rem)] p-1.5'
        >
          <div className='flex items-center gap-2.5 px-2 py-2.5'>
            <img
              src={keyToolLogo}
              alt=''
              width={32}
              height={32}
              className='size-8 shrink-0 rounded-md object-contain'
            />
            <div className='min-w-0 flex-1'>
              <p className='text-sm font-medium'>{t('StarNexus Key Tool')}</p>
              <p className='text-muted-foreground mt-0.5 text-xs'>
                {t('Key setup · Health checks · Repairs')}
              </p>
            </div>
          </div>
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            {downloads.map((download) => (
              <DropdownMenuItem
                key={download.platform}
                className='gap-3 px-2 py-3'
                render={
                  <a
                    href={download.href}
                    target='_blank'
                    rel='noopener noreferrer'
                  />
                }
              >
                <HugeiconsIcon icon={download.icon} aria-hidden='true' />
                <span className='min-w-0 flex-1'>
                  <span className='flex flex-wrap items-center gap-2 font-medium'>
                    {download.label}
                    {platform === download.platform && (
                      <Badge variant='outline'>{t('For your system')}</Badge>
                    )}
                  </span>
                  <span className='text-muted-foreground mt-1 block text-xs'>
                    {t(download.description)}
                  </span>
                  {download.platform === 'macos' && (
                    <span className='text-muted-foreground mt-0.5 block text-xs'>
                      {t('Signed · Notarized by Apple')}
                    </span>
                  )}
                </span>
                <HugeiconsIcon icon={Download03Icon} aria-hidden='true' />
              </DropdownMenuItem>
            ))}
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            <DropdownMenuLabel>
              {t('Backup downloads (GitHub)')}
            </DropdownMenuLabel>
            <div className='grid grid-cols-2 gap-1'>
              {DOWNLOADS.map((download) => (
                <DropdownMenuItem
                  key={download.platform}
                  className='px-2 py-2'
                  render={
                    <a
                      href={download.backup}
                      target='_blank'
                      rel='noopener noreferrer'
                    />
                  }
                >
                  <HugeiconsIcon icon={download.icon} aria-hidden='true' />
                  {download.label}
                </DropdownMenuItem>
              ))}
            </div>
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>
      <Button onClick={() => setOpen('create')}>
        <HugeiconsIcon icon={Add01Icon} data-icon='inline-start' />
        {t('Create API Key')}
      </Button>
    </div>
  )
}
