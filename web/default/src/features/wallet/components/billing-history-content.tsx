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
import { useEffect, useMemo, useState } from 'react'
import {
  functionalUpdate,
  getCoreRowModel,
  getFacetedRowModel,
  getFacetedUniqueValues,
  getFilteredRowModel,
  getPaginationRowModel,
  useReactTable,
  type PaginationState,
} from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { DataTablePage, DataTableToolbar } from '@/components/data-table'
import { useBillingHistory } from '../hooks/use-billing-history'
import { useBillingHistoryColumns } from './billing-history-columns'

type BillingHistoryContentProps = {
  tableClassName?: string
  paginationInFooter?: boolean
}

export function BillingHistoryContent({
  tableClassName = 'max-h-[calc(100dvh-13rem)] overflow-auto sm:max-h-[calc(100dvh-14rem)]',
  paginationInFooter = true,
}: BillingHistoryContentProps) {
  const { t } = useTranslation()
  const {
    records,
    total,
    page,
    pageSize,
    keyword,
    searchDraft,
    loading,
    completing,
    isAdmin,
    handlePageChange,
    handlePageSizeChange,
    handleSearchDraftChange,
    handleApplySearch,
    handleResetSearch,
    handleCompleteOrder,
  } = useBillingHistory()

  const [confirmTradeNo, setConfirmTradeNo] = useState<string | null>(null)

  const pagination = useMemo<PaginationState>(
    () => ({
      pageIndex: page - 1,
      pageSize,
    }),
    [page, pageSize]
  )

  const handleCompleteOrderClick = (tradeNo: string) => {
    setConfirmTradeNo(tradeNo)
  }

  const columns = useBillingHistoryColumns({
    isAdmin,
    completing,
    onCompleteOrder: handleCompleteOrderClick,
  })

  const table = useReactTable({
    data: records,
    columns,
    state: { pagination },
    onPaginationChange: (updater) => {
      const next = functionalUpdate(updater, pagination)
      if (next.pageIndex !== pagination.pageIndex) {
        handlePageChange(next.pageIndex + 1)
      }
      if (next.pageSize !== pagination.pageSize) {
        handlePageSizeChange(next.pageSize)
      }
    },
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getFacetedRowModel: getFacetedRowModel(),
    getFacetedUniqueValues: getFacetedUniqueValues(),
    manualPagination: true,
    pageCount: Math.max(1, Math.ceil(total / pageSize)),
  })

  const pageCount = table.getPageCount()
  useEffect(() => {
    if (page > pageCount) {
      handlePageChange(pageCount)
    }
  }, [page, pageCount, handlePageChange])

  const handleSearchKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      handleApplySearch()
    }
  }

  const handleConfirmComplete = async () => {
    if (!confirmTradeNo) {
      return
    }

    const success = await handleCompleteOrder(confirmTradeNo)
    if (success) {
      setConfirmTradeNo(null)
    }
  }

  const hasSearchFilters = !!keyword || !!searchDraft

  return (
    <>
      <DataTablePage
        table={table}
        columns={columns}
        isLoading={loading}
        emptyTitle={t('No billing records found')}
        emptyDescription={
          keyword
            ? t('Try adjusting your search')
            : t('Your transaction history will appear here')
        }
        skeletonKeyPrefix='billing-history-skeleton'
        tableClassName={tableClassName}
        tableHeaderClassName='bg-muted/30 sticky top-0 z-10'
        paginationInFooter={paginationInFooter}
        toolbar={
          <DataTableToolbar
            table={table}
            hideViewOptions
            hasAdditionalFilters={hasSearchFilters}
            onReset={handleResetSearch}
            onSearch={handleApplySearch}
            searchLoading={loading}
            customSearch={
              <Input
                placeholder={
                  isAdmin
                    ? t('Search by order number or username...')
                    : t('Search by order number...')
                }
                value={searchDraft}
                onChange={(e) => handleSearchDraftChange(e.target.value)}
                onKeyDown={handleSearchKeyDown}
                className='w-full sm:w-[200px] lg:w-[280px]'
              />
            }
            additionalSearch={
              <Select
                items={[
                  { value: '10', label: t('10 / page') },
                  { value: '20', label: t('20 / page') },
                  { value: '50', label: t('50 / page') },
                  { value: '100', label: t('100 / page') },
                ]}
                value={pageSize.toString()}
                onValueChange={(value) => {
                  if (value !== null) {
                    handlePageSizeChange(parseInt(value, 10))
                  }
                }}
              >
                <SelectTrigger className='h-9 w-[92px] sm:w-32'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    <SelectItem value='10'>{t('10 / page')}</SelectItem>
                    <SelectItem value='20'>{t('20 / page')}</SelectItem>
                    <SelectItem value='50'>{t('50 / page')}</SelectItem>
                    <SelectItem value='100'>{t('100 / page')}</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            }
          />
        }
      />

      <AlertDialog
        open={!!confirmTradeNo}
        onOpenChange={(open) => !open && setConfirmTradeNo(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Complete Order')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'Are you sure you want to manually complete this order? The user will be credited with the corresponding quota.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={completing}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={handleConfirmComplete}
              disabled={completing}
            >
              {completing ? t('Processing...') : t('Confirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
