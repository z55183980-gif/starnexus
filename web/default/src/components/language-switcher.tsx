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
import { useCallback } from 'react'
import {
  getInterfaceLanguageShortLabel,
  INTERFACE_LANGUAGE_OPTIONS,
  normalizeInterfaceLanguage,
} from '@/i18n/languages'
import { Check, ChevronDown, Globe } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

export function LanguageSwitcher() {
  const { i18n, t } = useTranslation()
  const user = useAuthStore((s) => s.auth.user)
  const currentLanguage = normalizeInterfaceLanguage(i18n.language)
  const currentShortLabel = getInterfaceLanguageShortLabel(currentLanguage)
  const currentLanguageLabel =
    INTERFACE_LANGUAGE_OPTIONS.find((lang) => lang.code === currentLanguage)
      ?.label ?? currentShortLabel

  const handleChangeLanguage = useCallback(
    async (code: string) => {
      await i18n.changeLanguage(code)
      if (user) {
        try {
          await api.put('/api/user/self', { language: code })
        } catch {
          // Best-effort persistence; don't block the UI on failure
        }
      }
    },
    [i18n, user]
  )

  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger
        render={
          <Button
            variant='ghost'
            className='group/lang text-muted-foreground hover:text-foreground h-9 gap-1 px-2 sm:px-2.5'
            aria-label={`${t('Change language')}, ${currentLanguageLabel}`}
            title={currentLanguageLabel}
          />
        }
      >
        <Globe
          className='size-4 shrink-0 transition-transform duration-300 ease-out group-hover/lang:rotate-12 group-aria-expanded/lang:rotate-12'
          aria-hidden='true'
        />
        <span
          key={currentShortLabel}
          className='min-w-[1.375rem] text-center text-[11px] font-semibold tracking-wide tabular-nums transition-opacity duration-300 motion-safe:animate-in motion-safe:fade-in-0 motion-safe:duration-200'
          aria-hidden='true'
        >
          {currentShortLabel}
        </span>
        <ChevronDown
          className='size-3 shrink-0 opacity-50 transition-transform duration-300 group-aria-expanded/lang:rotate-180'
          aria-hidden='true'
        />
      </DropdownMenuTrigger>
      <DropdownMenuContent align='end'>
        {INTERFACE_LANGUAGE_OPTIONS.map((lang) => (
          <DropdownMenuItem
            key={lang.code}
            onClick={() => handleChangeLanguage(lang.code)}
          >
            {lang.label}
            <Check
              size={14}
              className={cn(
                'ms-auto',
                currentLanguage !== lang.code && 'hidden'
              )}
            />
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
