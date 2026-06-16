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
import { SectionPageLayout } from '@/components/layout'
import { BillingHistoryContent } from '@/features/wallet/components/billing-history-content'

export function TopupRecords() {
  const { t } = useTranslation()

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Top-up Records')}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t('View and manage all user top-up orders and payment status')}
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        <BillingHistoryContent tableClassName='max-h-[calc(100dvh-16rem)] sm:max-h-[calc(100vh-14rem)]' />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
