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
import { useApiKeys } from './api-keys-provider'

const STARNEXUS_KEY_TOOL_DOWNLOADS = {
  windows: 'https://dkby.com/downloads/starnexus-keytool/windows',
  macos: 'https://dkby.com/downloads/starnexus-keytool/macos',
  windowsBackup: 'https://dkby.com/downloads/starnexus-keytool/windows/github',
  macosBackup: 'https://dkby.com/downloads/starnexus-keytool/macos/github',
} as const

export function ApiKeysPrimaryButtons() {
  const { t } = useTranslation()
  const { setOpen } = useApiKeys()
  return (
    <div className='flex max-w-[calc(100vw-1.5rem)] flex-wrap items-center gap-3'>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              className='h-auto min-h-15 gap-2.5 rounded-xl border-blue-500 bg-linear-to-r from-blue-600 to-indigo-600 p-2 pr-3 text-white shadow-md shadow-blue-600/15 hover:brightness-110 focus-visible:ring-blue-400/60'
              aria-label={t('Download API Key Config Tool')}
            />
          }
        >
          <img
            src={keyToolLogo}
            alt=''
            width={40}
            height={40}
            className='size-10 shrink-0 rounded-lg bg-white object-contain p-1'
          />
          <span className='min-w-0 text-left'>
            <span className='block text-sm leading-5 font-semibold whitespace-normal'>
              {t('Download API Key Config Tool')}
            </span>
            <span className='block text-[11px] leading-4 font-normal text-blue-100'>
              StarNexus · Windows / macOS
            </span>
          </span>
          <HugeiconsIcon icon={Download03Icon} aria-hidden='true' />
          <HugeiconsIcon icon={ArrowDown01Icon} aria-hidden='true' />
        </DropdownMenuTrigger>
        <DropdownMenuContent
          align='end'
          sideOffset={8}
          className='w-80 max-w-[calc(100vw-2rem)] rounded-xl bg-white p-2 text-zinc-950 shadow-xl dark:bg-zinc-950 dark:text-zinc-50'
        >
          <div className='mb-1 flex items-center gap-2.5 border-b px-2 pt-2 pb-3'>
            <img
              src={keyToolLogo}
              alt=''
              width={36}
              height={36}
              className='size-9 shrink-0 rounded-lg bg-white object-contain p-1'
            />
            <div className='min-w-0 flex-1'>
              <p className='text-sm font-semibold'>{t('StarNexus Key Tool')}</p>
              <p className='text-muted-foreground mt-1 text-xs'>
                {t('Key setup · Health checks · Repairs')}
              </p>
            </div>
          </div>
          <DropdownMenuGroup>
            <DropdownMenuItem
              className='gap-3 rounded-lg px-2 py-3'
              render={
                <a
                  href={STARNEXUS_KEY_TOOL_DOWNLOADS.windows}
                  download
                  target='_blank'
                  rel='noopener noreferrer'
                />
              }
            >
              <HugeiconsIcon
                icon={ComputerIcon}
                className='size-5 text-blue-600 dark:text-blue-400'
                aria-hidden='true'
              />
              <span className='flex-1'>
                <span className='block font-medium'>
                  {t('Download for Windows')}
                </span>
                <span className='text-muted-foreground mt-1 block text-xs'>
                  {t('64-bit · EXE installer')}
                </span>
              </span>
              <HugeiconsIcon
                icon={Download03Icon}
                className='text-muted-foreground'
                aria-hidden='true'
              />
            </DropdownMenuItem>
            <DropdownMenuItem
              className='gap-3 rounded-lg px-2 py-3'
              render={
                <a
                  href={STARNEXUS_KEY_TOOL_DOWNLOADS.macos}
                  download
                  target='_blank'
                  rel='noopener noreferrer'
                />
              }
            >
              <HugeiconsIcon
                icon={AppleIcon}
                className='size-5 text-blue-600 dark:text-blue-400'
                aria-hidden='true'
              />
              <span className='flex-1'>
                <span className='block font-medium'>
                  {t('Download for Mac')}
                </span>
                <span className='text-muted-foreground mt-1 block text-xs'>
                  {t('Apple Silicon / Intel universal')}
                </span>
                <span className='text-muted-foreground mt-0.5 block text-xs'>
                  {t('Signed · Notarized by Apple')}
                </span>
              </span>
              <HugeiconsIcon
                icon={Download03Icon}
                className='text-muted-foreground'
                aria-hidden='true'
              />
            </DropdownMenuItem>
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            <DropdownMenuLabel className='px-2 pt-2'>
              {t('Backup downloads (GitHub)')}
            </DropdownMenuLabel>
            <div className='grid grid-cols-2 gap-1'>
              <DropdownMenuItem
                className='px-2 py-2'
                render={
                  <a
                    href={STARNEXUS_KEY_TOOL_DOWNLOADS.windowsBackup}
                    target='_blank'
                    rel='noopener noreferrer'
                  />
                }
              >
                <HugeiconsIcon icon={ComputerIcon} aria-hidden='true' />
                {t('Windows backup')}
              </DropdownMenuItem>
              <DropdownMenuItem
                className='px-2 py-2'
                render={
                  <a
                    href={STARNEXUS_KEY_TOOL_DOWNLOADS.macosBackup}
                    target='_blank'
                    rel='noopener noreferrer'
                  />
                }
              >
                <HugeiconsIcon icon={AppleIcon} aria-hidden='true' />
                {t('Mac backup')}
              </DropdownMenuItem>
            </div>
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>

      <Button size='sm' onClick={() => setOpen('create')}>
        <HugeiconsIcon icon={Add01Icon} data-icon='inline-start' />
        {t('Create API Key')}
      </Button>
    </div>
  )
}
