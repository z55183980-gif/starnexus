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
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { AuthLayout } from '../auth-layout'
import { ForgotPasswordForm } from './components/forgot-password-form'

export function ForgotPassword() {
  const { t } = useTranslation()
  return (
    <AuthLayout variant='sign-in'>
      <div className='w-full rounded-[1.5rem] border border-white/70 bg-white/62 p-6 shadow-[0_24px_70px_rgba(30,41,59,0.18)] backdrop-blur-2xl dark:border-white/10 dark:bg-slate-950/58 sm:p-8'>
        <div className='mb-6 space-y-3 text-center'>
          <h2 className='text-2xl font-bold tracking-tight text-slate-950 dark:text-white'>
            {t('Forgot password')}
          </h2>
          <p className='text-sm text-slate-500 dark:text-slate-400'>
            {t(
              'Enter your registered email and we will send you a link to reset your password.'
            )}
          </p>
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
        </div>

        <ForgotPasswordForm className='space-y-4' />
      </div>
    </AuthLayout>
  )
}
