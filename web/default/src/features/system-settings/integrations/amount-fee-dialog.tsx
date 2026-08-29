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
import { useEffect, useMemo } from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'

const createAmountFeeDialogSchema = (t: (key: string) => string) =>
  z.object({
    amount: z
      .number()
      .positive(t('Amount must be greater than 0'))
      .int(t('Amount must be a whole number')),
    feeRate: z
      .number()
      .min(0, t('Fee rate must be 0 or greater'))
      .max(1, t('Fee rate must be ≤ 1')),
  })

type AmountFeeDialogFormValues = z.infer<
  ReturnType<typeof createAmountFeeDialogSchema>
>

export type AmountFeeData = {
  amount: number
  feeRate: number
}

type AmountFeeDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (data: AmountFeeData) => void
  editData?: AmountFeeData | null
}

export function AmountFeeDialog({
  open,
  onOpenChange,
  onSave,
  editData,
}: AmountFeeDialogProps) {
  const { t } = useTranslation()
  const isEditMode = !!editData
  const schema = createAmountFeeDialogSchema(t)

  const form = useForm<AmountFeeDialogFormValues>({
    resolver: zodResolver(schema),
    defaultValues: { amount: 0, feeRate: 0 },
  })

  const feeRate = form.watch('feeRate')
  const feePercentage = useMemo(
    () => Math.round((feeRate || 0) * 10000) / 100,
    [feeRate]
  )

  useEffect(() => {
    form.reset(editData ?? { amount: 0, feeRate: 0 })
  }, [editData, form, open])

  const handleSubmit = (values: AmountFeeDialogFormValues) => {
    onSave(values)
    form.reset()
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-[500px]'>
        <DialogHeader>
          <DialogTitle>
            {isEditMode ? t('Edit processing fee') : t('Add processing fee')}
          </DialogTitle>
          <DialogDescription>
            {t(
              'Set the processing fee rate for one exact recharge amount. The fee is added after any discount.'
            )}
          </DialogDescription>
        </DialogHeader>

        <Form {...form}>
          <form
            onSubmit={form.handleSubmit(handleSubmit)}
            className='flex flex-col gap-4'
          >
            <FormField
              control={form.control}
              name='amount'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Recharge Amount (USD)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      step='1'
                      min='1'
                      placeholder={t('e.g., 100')}
                      {...field}
                      onChange={(event) =>
                        field.onChange(parseInt(event.target.value, 10) || 0)
                      }
                      disabled={isEditMode}
                    />
                  </FormControl>
                  <FormDescription>
                    {isEditMode
                      ? t('Amount cannot be changed when editing.')
                      : t(
                          'Match the exact amount shown in the recharge options.'
                        )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='feeRate'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Processing fee rate')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      step='0.01'
                      min='0'
                      max='1'
                      placeholder={t('e.g., 0.03')}
                      {...field}
                      onChange={(event) =>
                        field.onChange(parseFloat(event.target.value) || 0)
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Use a decimal between 0 and 1.')}{' '}
                    <span className='font-medium'>
                      {t('Current fee: {{percentage}}%', {
                        percentage: feePercentage,
                      })}
                    </span>
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <DialogFooter>
              <Button
                type='button'
                variant='outline'
                onClick={() => onOpenChange(false)}
              >
                {t('Cancel')}
              </Button>
              <Button type='submit'>
                {isEditMode ? t('Update') : t('Add')}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}
