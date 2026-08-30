import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { PageTransition } from '@/components/page-transition'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { formatLocalCurrencyAmount, getCurrencyDisplay } from '@/lib/currency'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { getLotteryAdmin, resetLotteryAdmin, updateLotteryAdmin, type LotteryAdminData, type LotteryPrize } from './api'
import { createDraftPrize, type DraftPrize } from './draft-prize'

type DraftConfig = Omit<LotteryAdminData['config'], 'prizes'> & { prizes: DraftPrize[] }

function prizesToDraft(prizes: LotteryPrize[]): DraftPrize[] {
  const totalWeight = prizes.reduce((sum, prize) => sum + (prize.weight ?? 0), 0)
  return prizes.map((prize) => ({
    id: prize.id,
    name: prize.name,
    amount: String(prize.amount ?? 0),
    probability: totalWeight > 0 ? String(((prize.weight ?? 0) / totalWeight) * 100) : '',
  }))
}

export function LotteryAdmin() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [page, setPage] = useState(1)
  const query = useQuery({ queryKey: ['lottery-admin', page], queryFn: () => getLotteryAdmin(page) })
  const config = query.data?.data.config
  const [draft, setDraft] = useState<DraftConfig | null>(null)
  const [syncedConfig, setSyncedConfig] = useState('')
  const [userId, setUserId] = useState('')
  const [validationError, setValidationError] = useState('')
  const currencyDisplay = getCurrencyDisplay()
  const currencySymbol = currencyDisplay.meta.kind === 'tokens' ? '' : currencyDisplay.meta.symbol

  useEffect(() => {
    if (!config) return
    // Only rebuild the draft when the server config actually changed, so
    // paging through user progress cannot clobber unsaved prize edits.
    const serialized = JSON.stringify(config)
    if (serialized === syncedConfig) return
    setSyncedConfig(serialized)
    setDraft({ ...config, prizes: prizesToDraft(config.prizes) })
  }, [config, syncedConfig])

  const update = useMutation({
    mutationFn: updateLotteryAdmin,
    onSuccess: () => {
      setValidationError('')
      client.invalidateQueries({ queryKey: ['lottery-admin'] })
    },
    onError: (error: Error) => setValidationError(error.message || t('Failed to save lottery settings')),
  })
  const reset = useMutation({
    mutationFn: (id?: number) => resetLotteryAdmin(id),
    onSuccess: () => client.invalidateQueries({ queryKey: ['lottery-admin'] }),
  })

  const prizes = draft?.prizes ?? []
  const totalProbability = prizes.reduce(
    (sum, prize) => sum + (Number(prize.probability) || 0),
    0
  )

  if (!draft) return <PageTransition className='p-6'>{t('Loading')}</PageTransition>

  const updatePrize = (index: number, field: 'amount' | 'probability', value: string) => {
    setDraft({ ...draft, prizes: draft.prizes.map((prize, prizeIndex) => prizeIndex === index ? { ...prize, [field]: value } : prize) })
    setValidationError('')
  }

  const save = () => {
    if (prizes.length === 0 || prizes.some((prize) => {
      const amount = Number(prize.amount)
      const probability = Number(prize.probability)
      return !prize.amount.trim() || !Number.isFinite(amount) || amount < 0 || !prize.probability.trim() || !Number.isFinite(probability) || probability <= 0
    })) {
      setValidationError(t('Every tier needs a valid reward amount and probability'))
      return
    }
    if (Math.abs(totalProbability - 100) > 0.0001) {
      setValidationError(t('Winning probabilities must total 100%'))
      return
    }
    const payload: LotteryAdminData['config'] = {
      enabled: draft.enabled,
      threshold_display_amount: Number(draft.threshold_display_amount),
      prizes: prizes.map((prize) => ({
        id: prize.id,
        name: prize.name,
        amount: Number(prize.amount),
        weight: Math.max(1, Math.round(Number(prize.probability) * 100)),
      })),
    }
    if (!Number.isFinite(payload.threshold_display_amount) || payload.threshold_display_amount <= 0) {
      setValidationError(t('The spending threshold must be a positive amount'))
      return
    }
    update.mutate(payload)
  }

  return <PageTransition className='mx-auto h-full w-full max-w-4xl overflow-y-auto p-6'>
    <div className='space-y-6'>
      <h1 className='text-2xl font-semibold'>{t('Lottery management')}</h1>
      <div className='space-y-5 rounded-xl border p-6'>
        <div className='flex items-center justify-between gap-4'>
          <span className='text-sm font-medium'>{t('Enable lottery')}</span>
          <Switch checked={draft.enabled} onCheckedChange={(enabled) => setDraft({ ...draft, enabled })} aria-label={t('Enable lottery')} />
        </div>
        <label className='flex max-w-sm items-center gap-3 text-sm font-medium'>
          <span className='whitespace-nowrap'>{t('Spending threshold')} {currencySymbol ? `(${currencySymbol})` : ''}</span>
          <Input type='number' min={0} step='any' value={draft.threshold_display_amount} onChange={(event) => setDraft({ ...draft, threshold_display_amount: Number(event.target.value) })} />
        </label>
        <div className='space-y-3'>
          <div className='flex flex-wrap items-center justify-between gap-3'>
            <h2 className='font-medium'>{t('Prize tiers')}</h2>
            <Button variant='outline' size='sm' onClick={() => setDraft({ ...draft, prizes: [...prizes, createDraftPrize(prizes)] })}>
              <Plus aria-hidden='true' />{t('Add tier')}
            </Button>
          </div>
          <Table className='min-w-[620px]'>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Tier')}</TableHead>
                <TableHead>{t('Reward amount')} {currencySymbol ? `(${currencySymbol})` : ''}</TableHead>
                <TableHead>{t('Winning probability (%)')}</TableHead>
                <TableHead className='w-12 text-right'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {prizes.map((prize, index) => <TableRow key={prize.id}>
                <TableCell className='font-medium'>{t('Tier')} {index + 1}</TableCell>
                <TableCell><Input aria-label={`${t('Reward amount')} ${index + 1}`} type='number' min={0} step='any' value={prize.amount} onChange={(event) => updatePrize(index, 'amount', event.target.value)} /></TableCell>
                <TableCell><Input aria-label={`${t('Winning probability (%)')} ${index + 1}`} type='number' min={0} max={100} step='any' value={prize.probability} onChange={(event) => updatePrize(index, 'probability', event.target.value)} /></TableCell>
                <TableCell className='text-right'>
                  <Button variant='ghost' size='icon-sm' onClick={() => setDraft({ ...draft, prizes: prizes.filter((_, prizeIndex) => prizeIndex !== index) })} disabled={prizes.length <= 1} aria-label={t('Remove tier')} title={t('Remove tier')}>
                    <Trash2 aria-hidden='true' />
                  </Button>
                </TableCell>
              </TableRow>)}
            </TableBody>
          </Table>
          <div className={`text-sm ${Math.abs(totalProbability - 100) < 0.0001 ? 'text-muted-foreground' : 'text-destructive'}`}>
            {t('Total probability')}: {totalProbability.toFixed(2)}%
          </div>
          {validationError ? <p className='text-destructive text-sm'>{validationError}</p> : null}
          <Button onClick={save} disabled={update.isPending}>{update.isPending ? t('Saving...') : t('Save lottery settings')}</Button>
        </div>
        <div className='flex flex-wrap gap-2'>
          <Button variant='outline' onClick={() => reset.mutate(undefined)} disabled={reset.isPending}>{t('Reset all user progress')}</Button>
          <Input className='max-w-32' type='number' min={1} placeholder={t('User ID')} value={userId} onChange={(event) => setUserId(event.target.value)} />
          <Button variant='outline' onClick={() => reset.mutate(Number(userId))} disabled={!userId || reset.isPending}>{t('Reset user')}</Button>
        </div>
      </div>
      <div className='rounded-xl border p-6'>
        <h2 className='mb-3 font-medium'>{t('User progress')}</h2>
        {query.data?.data.users.map((user) => <div className='grid grid-cols-[minmax(0,1fr)_minmax(0,2fr)_auto] items-center gap-3 border-b py-2 text-sm' key={user.user_id}>
          <span className='min-w-0 truncate' title={user.username}>{t('User ID')}: {user.user_id}</span>
          <span className='min-w-0 truncate'>{t('Username')}: {user.username || '\u2014'}</span>
          <span className='text-right tabular-nums'>
            <span className='block'>{t('Consumption progress')}: {formatLocalCurrencyAmount(user.accumulated)} / {formatLocalCurrencyAmount(user.threshold)}</span>
            <span className='text-muted-foreground block'>{t('Available draws')}: {user.available_draws}</span>
          </span>
        </div>)}
        <div className='mt-4 flex items-center justify-between gap-3 text-sm'>
          <span className='text-muted-foreground'>{t('Total')}: {query.data?.data.users_total ?? 0}</span>
          <div className='flex gap-2'>
            <Button variant='outline' size='sm' onClick={() => setPage(page - 1)} disabled={page <= 1 || query.isFetching}>{t('Previous')}</Button>
            <Button variant='outline' size='sm' onClick={() => setPage(page + 1)} disabled={query.isFetching || page * (query.data?.data.page_size ?? 20) >= (query.data?.data.users_total ?? 0)}>{t('Next')}</Button>
          </div>
        </div>
      </div>
    </div>
  </PageTransition>
}
