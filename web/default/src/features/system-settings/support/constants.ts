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
import type { SupportChannelPreset, SupportChannelsConfig } from './types'

export const DEFAULT_SUPPORT_CHANNELS_CONFIG: SupportChannelsConfig = {
  enabled: true,
  panelTitle: 'StarNexus · Online support',
  panelSubtitle: '7×24 hour round-the-clock online customer service',
  channels: [
    {
      id: 'chatway',
      label: 'Online support',
      type: 'chatway',
      enabled: true,
      widgetId: 'XAUFzylpFcj9',
    },
    {
      id: 'whatsapp',
      label: 'WhatsApp',
      type: 'link',
      enabled: true,
      url: 'https://wa.me/85255183980',
      openInNewTab: true,
    },
    {
      id: 'telegram',
      label: 'Telegram',
      type: 'link',
      enabled: true,
      url: 'https://t.me/accattc',
      openInNewTab: true,
    },
    {
      id: 'wechat',
      label: 'WeChat',
      type: 'qrcode',
      enabled: false,
      imageUrl: '',
    },
    {
      id: 'zalo',
      label: 'Zalo',
      type: 'link',
      enabled: false,
      url: '',
      openInNewTab: true,
    },
  ],
}

export const SUPPORT_CHANNEL_PRESETS: SupportChannelPreset[] = [
  {
    id: 'chatway',
    label: 'Online support',
    type: 'chatway',
    widgetId: 'XAUFzylpFcj9',
    enabled: true,
  },
  {
    id: 'whatsapp',
    label: 'WhatsApp',
    type: 'link',
    url: 'https://wa.me/85255183980',
    openInNewTab: true,
    enabled: true,
  },
  {
    id: 'telegram',
    label: 'Telegram',
    type: 'link',
    url: 'https://t.me/accattc',
    openInNewTab: true,
    enabled: true,
  },
  {
    id: 'wechat',
    label: 'WeChat',
    type: 'qrcode',
    imageUrl: '',
    enabled: false,
  },
  {
    id: 'zalo',
    label: 'Zalo',
    type: 'link',
    url: 'https://zalo.me/',
    openInNewTab: true,
    enabled: false,
  },
]
