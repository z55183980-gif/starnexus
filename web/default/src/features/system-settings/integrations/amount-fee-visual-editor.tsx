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
import { Add01Icon, Delete02Icon, Edit01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { StatusBadge } from '@/components/status-badge'
import { safeJsonParseWithValidation } from '../utils/json-parser'
import { isObjectRecord } from '../utils/json-validators'
import { AmountFeeDialog, type AmountFeeData } from './amount-fee-dialog'

type AmountFeeVisualEditorProps = {
  value: string
  onChange: (value: string) => void
}

export function AmountFeeVisualEditor({
  value,
  onChange,
}: AmountFeeVisualEditorProps) {
  const { t } = useTranslation()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editData, setEditData] = useState<AmountFeeData | null>(null)

  const fees = useMemo(() => {
    const parsed = safeJsonParseWithValidation<Record<string, unknown>>(value, {
      fallback: {},
      validator: isObjectRecord,
      validatorMessage: t('Amount fee must be a JSON object'),
      context: 'amount fees',
    })

    return Object.entries(parsed)
      .map(([amount, rate]) => ({
        amount: parseInt(amount, 10),
        feeRate: typeof rate === 'number' ? rate : parseFloat(String(rate)),
      }))
      .filter(
        (item) => Number.isFinite(item.amount) && Number.isFinite(item.feeRate)
      )
      .sort((a, b) => a.amount - b.amount)
  }, [value, t])

  const readFeeObject = () =>
    safeJsonParseWithValidation<Record<string, unknown>>(value, {
      fallback: {},
      validator: isObjectRecord,
      silent: true,
    })

  const handleSave = (data: AmountFeeData) => {
    const feeObject = readFeeObject()
    if (editData && editData.amount !== data.amount) {
      delete feeObject[editData.amount.toString()]
    }
    feeObject[data.amount.toString()] = data.feeRate
    onChange(JSON.stringify(feeObject, null, 2))
  }

  const handleDelete = (amount: number) => {
    const feeObject = readFeeObject()
    delete feeObject[amount.toString()]
    onChange(JSON.stringify(feeObject, null, 2))
  }

  const openAddDialog = () => {
    setEditData(null)
    setDialogOpen(true)
  }

  const openEditDialog = (fee: AmountFeeData) => {
    setEditData(fee)
    setDialogOpen(true)
  }

  const formatPercentage = (rate: number) =>
    `${Math.round(rate * 10000) / 100}%`

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
        <p className='text-muted-foreground text-sm'>
          {t('Configure processing fees by exact recharge amount')}
        </p>
        <Button
          type='button'
          size='sm'
          onClick={(event) => {
            event.preventDefault()
            event.stopPropagation()
            openAddDialog()
          }}
          className='w-full sm:w-auto'
        >
          <HugeiconsIcon icon={Add01Icon} data-icon='inline-start' />
          {t('Add fee tier')}
        </Button>
      </div>

      {fees.length === 0 ? (
        <div className='text-muted-foreground rounded-lg border border-dashed p-6 text-center text-sm'>
          {t(
            'No processing fees configured. Users will pay the regular amount.'
          )}
        </div>
      ) : (
        <div className='overflow-hidden rounded-md border'>
          <div className='overflow-x-auto'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Recharge Amount')}</TableHead>
                  <TableHead>{t('Processing fee rate')}</TableHead>
                  <TableHead>{t('Fee shown to users')}</TableHead>
                  <TableHead className='text-right'>{t('Actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {fees.map((fee) => (
                  <TableRow key={fee.amount}>
                    <TableCell>
                      <span className='font-mono text-sm'>${fee.amount}</span>
                    </TableCell>
                    <TableCell>
                      <code className='bg-muted rounded px-1.5 py-0.5 text-xs'>
                        {fee.feeRate.toFixed(4)}
                      </code>
                    </TableCell>
                    <TableCell>
                      <StatusBadge variant='warning' copyable={false}>
                        {formatPercentage(fee.feeRate)}
                      </StatusBadge>
                    </TableCell>
                    <TableCell className='text-right'>
                      <div className='flex justify-end gap-1'>
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon-sm'
                          aria-label={t('Edit processing fee')}
                          onClick={(event) => {
                            event.preventDefault()
                            event.stopPropagation()
                            openEditDialog(fee)
                          }}
                        >
                          <HugeiconsIcon icon={Edit01Icon} />
                        </Button>
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon-sm'
                          aria-label={t('Remove processing fee')}
                          onClick={(event) => {
                            event.preventDefault()
                            event.stopPropagation()
                            handleDelete(fee.amount)
                          }}
                        >
                          <HugeiconsIcon icon={Delete02Icon} />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </div>
      )}

      <AmountFeeDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onSave={handleSave}
        editData={editData}
      />
    </div>
  )
}
