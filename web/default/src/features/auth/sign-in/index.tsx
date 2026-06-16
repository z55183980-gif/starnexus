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
import { Link, useSearch } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { useStatus } from '@/hooks/use-status'
import { AuthLayout } from '../auth-layout'
import { TermsFooter } from '../components/terms-footer'
import { UserAuthForm } from './components/user-auth-form'

export function SignIn() {
  const { t } = useTranslation()
  const { redirect } = useSearch({ from: '/(auth)/sign-in' })
  const { status } = useStatus()

  return (
    <AuthLayout variant='sign-in'>
      <div className='w-full rounded-[1.5rem] border border-white/70 bg-white/62 p-6 shadow-[0_24px_70px_rgba(30,41,59,0.18)] backdrop-blur-2xl sm:p-8 dark:border-white/10 dark:bg-slate-950/58'>
        <div className='mb-6 space-y-4 text-center'>
          <h2 className='text-2xl font-bold tracking-tight text-slate-950 dark:text-white'>
            {t('User Login')}
          </h2>
          {!status?.self_use_mode_enabled &&
            status?.register_enabled !== false && (
              <p className='text-sm text-slate-500 dark:text-slate-400'>
                {t("Don't have an account?")}{' '}
                <Link
                  to='/sign-up'
                  className='font-medium text-sky-500 hover:text-sky-600'
                >
                  {t('Sign up')}
                </Link>
                .
              </p>
            )}
        </div>

        <UserAuthForm redirectTo={redirect} />

        <TermsFooter
          variant='sign-in'
          status={status}
          className='mt-5 text-center text-xs text-slate-500'
        />
      </div>
    </AuthLayout>
  )
}
