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
import { useMemo, useState } from 'react'
import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  Pencil,
  Plus,
  Search,
  Trash2,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { SUPPORT_CHANNEL_PRESETS } from './constants'
import { SupportChannelDialog } from './support-channel-dialog'
import type { SupportChannel, SupportChannelsConfig } from './types'
import { getChannelSummary, parseSupportChannelsConfig, stringifySupportChannelsConfig } from './utils'

type SupportSettingsVisualEditorProps = {
  value: string
  onChange: (value: string) => void
}

export function SupportSettingsVisualEditor({
  value,
  onChange,
}: SupportSettingsVisualEditorProps) {
  const { t } = useTranslation()
  const [searchText, setSearchText] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editData, setEditData] = useState<SupportChannel | null>(null)

  const config = useMemo(() => parseSupportChannelsConfig(value), [value])

  const updateConfig = (next: SupportChannelsConfig) => {
    onChange(stringifySupportChannelsConfig(next))
  }

  const filteredChannels = useMemo(() => {
    if (!searchText) return config.channels
    const lower = searchText.toLowerCase()
    return config.channels.filter(
      (channel) =>
        channel.label.toLowerCase().includes(lower) ||
        channel.id.toLowerCase().includes(lower) ||
        getChannelSummary(channel).toLowerCase().includes(lower)
    )
  }, [config.channels, searchText])

  const availablePresets = SUPPORT_CHANNEL_PRESETS.filter(
    (preset) => !config.channels.some((channel) => channel.id === preset.id)
  )

  const handleSaveChannel = (channel: SupportChannel) => {
    const exists = config.channels.some((item) => item.id === channel.id)
    const channels = exists
      ? config.channels.map((item) => (item.id === channel.id ? channel : item))
      : [...config.channels, channel]
    updateConfig({ ...config, channels })
  }

  const handleDelete = (id: string) => {
    updateConfig({
      ...config,
      channels: config.channels.filter((channel) => channel.id !== id),
    })
  }

  const handleToggle = (id: string, enabled: boolean) => {
    updateConfig({
      ...config,
      channels: config.channels.map((channel) =>
        channel.id === id ? { ...channel, enabled } : channel
      ),
    })
  }

  const moveChannel = (index: number, direction: -1 | 1) => {
    const targetIndex = index + direction
    if (targetIndex < 0 || targetIndex >= config.channels.length) return
    const channels = [...config.channels]
    const [item] = channels.splice(index, 1)
    channels.splice(targetIndex, 0, item)
    updateConfig({ ...config, channels })
  }

  const handleAddPreset = (presetId: string) => {
    const preset = SUPPORT_CHANNEL_PRESETS.find((item) => item.id === presetId)
    if (!preset) return
    handleSaveChannel({
      id: preset.id,
      label: preset.label,
      type: preset.type,
      enabled: preset.enabled ?? false,
      url: preset.url,
      widgetId: preset.widgetId,
      imageUrl: preset.imageUrl,
      openInNewTab: preset.openInNewTab,
    })
  }

  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-center gap-3'>
        <div className='relative min-w-[220px] flex-1'>
          <Search className='text-muted-foreground absolute top-2.5 left-2.5 h-4 w-4' />
          <Input
            placeholder={t('Search support channels...')}
            value={searchText}
            onChange={(event) => setSearchText(event.target.value)}
            className='pl-9'
          />
        </div>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant='outline'
                disabled={availablePresets.length === 0}
              />
            }
          >
            <Plus className='mr-2 h-4 w-4' aria-hidden />
            {t('Add from preset')}
            <ChevronDown className='ml-2 h-4 w-4' aria-hidden />
          </DropdownMenuTrigger>
          <DropdownMenuContent align='end'>
            {availablePresets.map((preset) => (
              <DropdownMenuItem
                key={preset.id}
                onClick={() => handleAddPreset(preset.id)}
              >
                {preset.label}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
        <Button
          onClick={() => {
            setEditData(null)
            setDialogOpen(true)
          }}
        >
          <Plus className='mr-2 h-4 w-4' />
          {t('Add support channel')}
        </Button>
      </div>

      {filteredChannels.length === 0 ? (
        <div className='text-muted-foreground rounded-lg border border-dashed p-8 text-center'>
          {searchText
            ? t('No support channels match your search')
            : t(
                'No support channels configured. Add a preset or create a custom channel.'
              )}
        </div>
      ) : (
        <div className='rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Channel label')}</TableHead>
                <TableHead>{t('Channel type')}</TableHead>
                <TableHead>{t('Target')}</TableHead>
                <TableHead>{t('Enabled')}</TableHead>
                <TableHead className='text-right'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filteredChannels.map((channel) => {
                const index = config.channels.findIndex(
                  (item) => item.id === channel.id
                )
                return (
                  <TableRow key={channel.id}>
                    <TableCell className='font-medium'>
                      {channel.label}
                      <div className='text-muted-foreground font-mono text-xs'>
                        {channel.id}
                      </div>
                    </TableCell>
                    <TableCell>{t(`support.channelType.${channel.type}`)}</TableCell>
                    <TableCell className='max-w-xs truncate font-mono text-sm'>
                      {channel.type === 'qrcode' && channel.imageUrl ? (
                        <div className='flex items-center gap-2'>
                          <img
                            src={channel.imageUrl}
                            alt={channel.label}
                            className='border-border h-8 w-8 rounded border object-contain'
                          />
                          <span className='truncate'>{channel.imageUrl}</span>
                        </div>
                      ) : (
                        getChannelSummary(channel)
                      )}
                    </TableCell>
                    <TableCell>
                      <Switch
                        checked={channel.enabled}
                        onCheckedChange={(checked) =>
                          handleToggle(channel.id, checked)
                        }
                        aria-label={t('Enabled')}
                      />
                    </TableCell>
                    <TableCell className='text-right'>
                      <div className='flex justify-end gap-1'>
                        <Button
                          variant='ghost'
                          size='icon-sm'
                          disabled={index <= 0}
                          onClick={() => moveChannel(index, -1)}
                        >
                          <ArrowUp className='h-4 w-4' />
                        </Button>
                        <Button
                          variant='ghost'
                          size='icon-sm'
                          disabled={index < 0 || index >= config.channels.length - 1}
                          onClick={() => moveChannel(index, 1)}
                        >
                          <ArrowDown className='h-4 w-4' />
                        </Button>
                        <Button
                          variant='ghost'
                          size='icon-sm'
                          onClick={() => {
                            setEditData(channel)
                            setDialogOpen(true)
                          }}
                        >
                          <Pencil className='h-4 w-4' />
                        </Button>
                        <Button
                          variant='ghost'
                          size='icon-sm'
                          onClick={() => handleDelete(channel.id)}
                        >
                          <Trash2 className='h-4 w-4' />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
      )}

      <SupportChannelDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onSave={handleSaveChannel}
        editData={editData}
        idReadOnly={Boolean(editData)}
      />
    </div>
  )
}
