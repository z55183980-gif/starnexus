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
  Activity,
  ChartNoAxesCombined,
  CreditCard,
  FileText,
  FlaskConical,
  ImageIcon,
  Key,
  LayoutDashboard,
  ListTodo,
  MessageSquare,
  Radio,
  Receipt,
  Route,
  Settings,
  ShieldCheck,
  Ticket,
  User,
  UserRoundCog,
  Globe2,
  Share2,
  Users,
  Wallet,
  Video,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { type SidebarData } from '@/components/layout/types'
import { useRoutingNodeAlertUnread } from '@/features/node-routing/use-routing-node-alert-unread'

/**
 * Root navigation groups for the application sidebar.
 *
 * These are shown when the URL does not match any nested sidebar view
 * registered in `layout/lib/sidebar-view-registry.ts`.
 */
export function useSidebarData(): SidebarData {
  const { t } = useTranslation()
  const userRole = useAuthStore((state) => state.auth.user?.role)
  const { unreadCount } = useRoutingNodeAlertUnread(
    userRole === ROLE.SUPER_ADMIN
  )

  return {
    navGroups: [
      {
        id: 'imageWorkbench',
        title: '',
        items: [
          {
            title: t('Image Workbench'),
            url: '/image-workbench',
            icon: ImageIcon,
          },
        ],
      },
      {
        id: 'chat',
        title: t('Chat'),
        items: [
          {
            title: t('Playground'),
            url: '/playground',
            icon: FlaskConical,
          },
          {
            title: t('Chat'),
            icon: MessageSquare,
            type: 'chat-presets',
          },
        ],
      },
      {
        id: 'general',
        title: t('General'),
        items: [
          {
            title: t('Overview'),
            url: '/dashboard/overview',
            icon: Activity,
          },
          {
            title: t('Dashboard'),
            url: '/dashboard/models',
            icon: LayoutDashboard,
          },
          {
            title: t('API Keys'),
            url: '/keys',
            icon: Key,
          },
          {
            title: t('Doubao Video'),
            icon: Video,
            items: [
              {
                title: t('Material Library'),
                url: '/doubao-video/materials',
              },
              {
                title: t('Access Keys'),
                url: '/doubao-video/access-keys',
              },
            ],
          },
          {
            title: t('Usage Logs'),
            url: '/usage-logs/common',
            icon: FileText,
          },
          {
            title: t('Usage Details'),
            url: '/usage-details',
            icon: ChartNoAxesCombined,
            adminOnly: true,
          },
          {
            title: t('Task Logs'),
            url: '/usage-logs/task',
            activeUrls: ['/usage-logs/drawing'],
            configUrls: ['/usage-logs/drawing', '/usage-logs/task'],
            icon: ListTodo,
          },
        ],
      },
      {
        id: 'personal',
        title: t('Personal'),
        items: [
          {
            title: t('Wallet'),
            url: '/wallet',
            icon: Wallet,
          },
          {
            title: t('Referral Program'),
            url: '/affiliate',
            icon: Share2,
          },
          {
            title: t('Profile'),
            url: '/profile',
            icon: User,
          },
        ],
      },
      {
        id: 'agent',
        title: t('Agent'),
        items: [
          {
            title: t('Users'),
            url: '/users',
            icon: Users,
          },
          {
            title: t('Usage Logs'),
            url: '/usage-logs/common',
            icon: FileText,
          },
          {
            title: t('Affiliate Rebate Records'),
            url: '/affiliate-records',
            icon: Share2,
          },
        ],
      },
      {
        id: 'admin',
        title: t('Admin'),
        items: [
          {
            title: t('Security Audit'),
            url: '/security-audit',
            icon: ShieldCheck,
            rootOnly: true,
          },
          {
            title: t('Business Monitor'),
            url: '/business-monitor',
            icon: ChartNoAxesCombined,
          },
          {
            title: t('Routing Management'),
            url: '/routing-management',
            icon: Route,
            rootOnly: true,
            unread: unreadCount > 0,
            unreadLabel: t('Unread node alerts'),
          },
          {
            title: t('Channels'),
            url: '/channels',
            icon: Radio,
          },
          {
            title: t('Account Management'),
            url: '/upstream-accounts',
            icon: UserRoundCog,
            rootOnly: true,
          },
          {
            title: t('IP Management'),
            url: '/upstream-proxies',
            icon: Globe2,
            rootOnly: true,
          },
          {
            title: t('Users'),
            url: '/users',
            icon: Users,
          },
          {
            title: t('Redemption Codes'),
            url: '/redemption-codes',
            icon: Ticket,
          },
          {
            title: t('Top-up Records'),
            url: '/topup-records',
            icon: Receipt,
          },
          {
            title: t('Affiliate Rebate Records'),
            url: '/affiliate-records',
            icon: Share2,
          },
          {
            title: t('Subscription Management'),
            url: '/subscriptions',
            icon: CreditCard,
          },
          {
            title: t('System Settings'),
            url: '/system-settings/site',
            activeUrls: ['/system-settings'],
            icon: Settings,
          },
        ],
      },
    ],
  }
}
