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
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useApiKeys } from './api-keys-provider'

const CODEX_CONFIG_TOOL_DOWNLOADS = {
  windows:
    'https://docs.dkby.com/docs/doc/STN-Codex-ConfigMode.exe',
  macos:
    'https://docs.dkby.com/docs/doc/STN-Codex-Config-Writer-mac.command',
} as const

export function ApiKeysPrimaryButtons() {
  const { t } = useTranslation()
  const { setOpen } = useApiKeys()
  return (
    <div className='flex gap-2'>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant='outline'
              size='sm'
              aria-label={t('Download API Key Config Tool')}
            />
          }
        >
          <HugeiconsIcon icon={Download03Icon} data-icon='inline-start' />
          <span className='max-sm:hidden'>
            {t('Download API Key Config Tool')}
          </span>
          <HugeiconsIcon icon={ArrowDown01Icon} data-icon='inline-end' />
        </DropdownMenuTrigger>
        <DropdownMenuContent align='end' className='w-56'>
          <DropdownMenuGroup>
            <DropdownMenuItem
              render={
                <a
                  href={CODEX_CONFIG_TOOL_DOWNLOADS.windows}
                  download
                  target='_blank'
                  rel='noreferrer'
                />
              }
            >
              <HugeiconsIcon icon={ComputerIcon} data-icon='inline-start' />
              {t('Windows version')}
            </DropdownMenuItem>
            <DropdownMenuItem
              render={
                <a
                  href={CODEX_CONFIG_TOOL_DOWNLOADS.macos}
                  download
                  target='_blank'
                  rel='noreferrer'
                />
              }
            >
              <HugeiconsIcon icon={AppleIcon} data-icon='inline-start' />
              {t('macOS version')}
            </DropdownMenuItem>
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
