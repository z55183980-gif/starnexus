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
import { Analytics01Icon, UserListIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SectionPageLayout } from '@/components/layout'
import { UsersDeleteDialog } from './components/users-delete-dialog'
import { UsersMutateDrawer } from './components/users-mutate-drawer'
import { UsersPrimaryButtons } from './components/users-primary-buttons'
import { UsersProvider, useUsers } from './components/users-provider'
import { UsersStatistics } from './components/users-statistics'
import { UsersTable } from './components/users-table'

function UsersContent() {
  const { t } = useTranslation()
  const { open, setOpen, currentRow } = useUsers()
  const [view, setView] = useState<'statistics' | 'list'>('statistics')

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Users')}</SectionPageLayout.Title>
        <SectionPageLayout.Description>
          {view === 'statistics'
            ? t('Understand user growth, activity, and account health')
            : t('Manage users and their permissions')}
        </SectionPageLayout.Description>
        {view === 'list' && (
          <SectionPageLayout.Actions>
            <UsersPrimaryButtons />
          </SectionPageLayout.Actions>
        )}
        <SectionPageLayout.Content>
          <Tabs
            value={view}
            onValueChange={(value) => setView(value as 'statistics' | 'list')}
          >
            <TabsList variant='line'>
              <TabsTrigger value='statistics'>
                <HugeiconsIcon
                  icon={Analytics01Icon}
                  data-icon='inline-start'
                  strokeWidth={2}
                />
                {t('User Statistics')}
              </TabsTrigger>
              <TabsTrigger value='list'>
                <HugeiconsIcon
                  icon={UserListIcon}
                  data-icon='inline-start'
                  strokeWidth={2}
                />
                {t('User List')}
              </TabsTrigger>
            </TabsList>
            <TabsContent value='statistics'>
              <UsersStatistics onViewAll={() => setView('list')} />
            </TabsContent>
            <TabsContent value='list'>
              <UsersTable />
            </TabsContent>
          </Tabs>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <UsersMutateDrawer
        open={open === 'create' || open === 'update'}
        onOpenChange={(isOpen) => !isOpen && setOpen(null)}
        currentRow={open === 'update' ? currentRow || undefined : undefined}
      />
      <UsersDeleteDialog />
    </>
  )
}

export function Users() {
  return (
    <UsersProvider>
      <UsersContent />
    </UsersProvider>
  )
}
