import { useCallback, useEffect, useState, type KeyboardEvent } from 'react'
import { RotateCcw, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { SectionPageLayout } from '@/components/layout'

type LedgerRow = {
  id: number
  user_id: number
  action: string
  amount_usd: string
  source_user_id?: number | null
  source_topup_id?: number | null
  created_at: string
}

function formatLedgerAction(
  action: string,
  t: (key: string) => string
): string {
  switch (action) {
    case 'accrue':
      return t('Rebate accrual')
    case 'transfer':
      return t('Transfer to balance')
    default:
      return action
  }
}

export function AdminAffiliateRecords() {
  const { t } = useTranslation()
  const [rows, setRows] = useState<LedgerRow[]>([])
  const [loading, setLoading] = useState(true)
  const [keyword, setKeyword] = useState('')
  const [searchDraft, setSearchDraft] = useState('')

  const load = useCallback(async () => {
    try {
      setLoading(true)
      const params = new URLSearchParams({
        page: '1',
        page_size: '50',
      })
      if (keyword) {
        params.append('keyword', keyword)
      }
      const res = await api.get<{
        success: boolean
        data: { items: LedgerRow[]; total: number }
      }>(`/api/affiliate/admin/ledger?${params.toString()}`)
      if (res.data.success) {
        setRows(res.data.data.items ?? [])
      }
    } finally {
      setLoading(false)
    }
  }, [keyword])

  const handleApplySearch = useCallback(() => {
    setKeyword(searchDraft.trim())
  }, [searchDraft])

  const handleResetSearch = useCallback(() => {
    setSearchDraft('')
    setKeyword('')
  }, [])

  const handleSearchKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      handleApplySearch()
    }
  }

  useEffect(() => {
    void load()
  }, [load])

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Affiliate Rebate Records')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t('Audit trail for USD referral rebates and transfers.')}
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        <div className='mb-4 flex flex-col gap-2 sm:flex-row sm:items-center'>
          <Input
            placeholder={t(
              'Search by record ID, user ID, source user, top-up, or action...'
            )}
            value={searchDraft}
            onChange={(e) => setSearchDraft(e.target.value)}
            onKeyDown={handleSearchKeyDown}
            className='w-full sm:w-[360px]'
          />
          <div className='flex gap-2'>
            <Button onClick={handleApplySearch} disabled={loading}>
              <Search className='size-4' />
              {t('Search')}
            </Button>
            <Button
              variant='outline'
              onClick={handleResetSearch}
              disabled={loading || (!keyword && !searchDraft)}
            >
              <RotateCcw className='size-4' />
              {t('Reset')}
            </Button>
          </div>
        </div>
        {loading ? (
          <Skeleton className='h-32 w-full' />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('ID')}</TableHead>
                <TableHead>{t('User')}</TableHead>
                <TableHead>{t('Actions')}</TableHead>
                <TableHead>{t('Amount')}</TableHead>
                <TableHead>{t('Source user')}</TableHead>
                <TableHead>{t('Top-up')}</TableHead>
                <TableHead>{t('Time')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={7}
                    className='text-muted-foreground text-center'
                  >
                    {keyword
                      ? t('Try adjusting your search')
                      : t('No affiliate rebate records yet')}
                  </TableCell>
                </TableRow>
              ) : (
                rows.map((row) => (
                  <TableRow key={row.id}>
                    <TableCell>{row.id}</TableCell>
                    <TableCell>{row.user_id}</TableCell>
                    <TableCell>{formatLedgerAction(row.action, t)}</TableCell>
                    <TableCell>{row.amount_usd}</TableCell>
                    <TableCell>{row.source_user_id ?? '—'}</TableCell>
                    <TableCell>{row.source_topup_id ?? '—'}</TableCell>
                    <TableCell>{row.created_at}</TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
