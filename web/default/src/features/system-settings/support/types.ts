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

export type SupportChannelType = 'link' | 'chatway' | 'qrcode'

export type SupportChannel = {
  id: string
  label: string
  type: SupportChannelType
  enabled: boolean
  url?: string
  widgetId?: string
  imageUrl?: string
  openInNewTab?: boolean
}

export type SupportChannelsConfig = {
  enabled: boolean
  panelTitle: string
  panelSubtitle: string
  channels: SupportChannel[]
}

export type SupportSettings = {
  SupportChannels: string
}

export type SupportChannelPreset = {
  id: string
  label: string
  type: SupportChannelType
  url?: string
  widgetId?: string
  imageUrl?: string
  openInNewTab?: boolean
  enabled?: boolean
}
