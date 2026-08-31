import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { PageTransition } from '@/components/page-transition'
import { Button } from '@/components/ui/button'
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
import { formatLocalCurrencyAmount, getCurrencyDisplay } from '@/lib/currency'

import {
  LOTTERY_ADMIN_PAGE_SIZES,
  lotteryDrawDate,
  lotteryResetActionVariant,
} from './admin-utils'
import {
  getLotteryAdmin,
  resetLotteryAdmin,
  updateLotteryAdmin,
  type LotteryAdminData,
  type LotteryPrize,
} from './api'
import { createDraftPrize, type DraftPrize } from './draft-prize'

type DraftConfig = Omit<LotteryAdminData['config'], 'prizes'> & {
  prizes: DraftPrize[]
}

function prizesToDraft(prizes: LotteryPrize[]): DraftPrize[] {
  const totalWeight = prizes.reduce(
    (sum, prize) => sum + (prize.weight ?? 0),
    0
  )
  return prizes.map((prize) => ({
    id: prize.id,
    name: prize.name,
    amount: String(prize.amount ?? 0),
    probability:
      totalWeight > 0 ? String(((prize.weight ?? 0) / totalWeight) * 100) : '',
  }))
}

export function LotteryAdmin() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const query = useQuery({
    queryKey: ['lottery-admin', page, pageSize],
    queryFn: () => getLotteryAdmin(page, pageSize),
  })
  const config = query.data?.data.config
  const [draft, setDraft] = useState<DraftConfig | null>(null)
  const [syncedConfig, setSyncedConfig] = useState('')
  const [userId, setUserId] = useState('')
  const [validationError, setValidationError] = useState('')
  const currencyDisplay = getCurrencyDisplay()
  const currencySymbol =
    currencyDisplay.meta.kind === 'tokens' ? '' : currencyDisplay.meta.symbol

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
    onError: (error: Error) =>
      setValidationError(error.message || t('Failed to save lottery settings')),
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

  if (!draft) {
    return <PageTransition className='p-6'>{t('Loading')}</PageTransition>
  }

  const updatePrize = (
    index: number,
    field: 'amount' | 'probability',
    value: string
  ) => {
    setDraft({
      ...draft,
      prizes: draft.prizes.map((prize, prizeIndex) =>
        prizeIndex === index ? { ...prize, [field]: value } : prize
      ),
    })
    setValidationError('')
  }

  const save = () => {
    if (
      prizes.length === 0 ||
      prizes.some((prize) => {
        const amount = Number(prize.amount)
        const probability = Number(prize.probability)
        return (
          !prize.amount.trim() ||
          !Number.isFinite(amount) ||
          amount < 0 ||
          !prize.probability.trim() ||
          !Number.isFinite(probability) ||
          probability <= 0
        )
      })
    ) {
      setValidationError(
        t('Every tier needs a valid reward amount and probability')
      )
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
    if (
      !Number.isFinite(payload.threshold_display_amount) ||
      payload.threshold_display_amount <= 0
    ) {
      setValidationError(t('The spending threshold must be a positive amount'))
      return
    }
    update.mutate(payload)
  }

  return (
    <PageTransition className='mx-auto h-full w-full max-w-4xl overflow-y-auto p-6'>
      <div className='space-y-6'>
        <h1 className='text-2xl font-semibold'>{t('Lottery management')}</h1>
        <div className='space-y-5 rounded-xl border p-6'>
          <div className='flex items-center justify-between gap-4'>
            <span className='text-sm font-medium'>{t('Enable lottery')}</span>
            <Switch
              checked={draft.enabled}
              onCheckedChange={(enabled) => setDraft({ ...draft, enabled })}
              aria-label={t('Enable lottery')}
            />
          </div>
          <label className='flex max-w-sm items-center gap-3 text-sm font-medium'>
            <span className='whitespace-nowrap'>
              {t('Spending threshold')}{' '}
              {currencySymbol ? `(${currencySymbol})` : ''}
            </span>
            <Input
              type='number'
              min={0}
              step='any'
              value={draft.threshold_display_amount}
              onChange={(event) =>
                setDraft({
                  ...draft,
                  threshold_display_amount: Number(event.target.value),
                })
              }
            />
          </label>
          <div className='space-y-3'>
            <div className='flex flex-wrap items-center justify-between gap-3'>
              <h2 className='font-medium'>{t('Prize tiers')}</h2>
              <Button
                variant='outline'
                size='sm'
                onClick={() =>
                  setDraft({
                    ...draft,
                    prizes: [...prizes, createDraftPrize(prizes)],
                  })
                }
              >
                <Plus aria-hidden='true' />
                {t('Add tier')}
              </Button>
            </div>
            <Table className='min-w-[620px]'>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Tier')}</TableHead>
                  <TableHead>
                    {t('Reward amount')}{' '}
                    {currencySymbol ? `(${currencySymbol})` : ''}
                  </TableHead>
                  <TableHead>{t('Winning probability (%)')}</TableHead>
                  <TableHead className='w-12 text-right'>
                    {t('Actions')}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {prizes.map((prize, index) => (
                  <TableRow key={prize.id}>
                    <TableCell className='font-medium'>
                      {t('Tier')} {index + 1}
                    </TableCell>
                    <TableCell>
                      <Input
                        aria-label={`${t('Reward amount')} ${index + 1}`}
                        type='number'
                        min={0}
                        step='any'
                        value={prize.amount}
                        onChange={(event) =>
                          updatePrize(index, 'amount', event.target.value)
                        }
                      />
                    </TableCell>
                    <TableCell>
                      <Input
                        aria-label={`${t('Winning probability (%)')} ${index + 1}`}
                        type='number'
                        min={0}
                        max={100}
                        step='any'
                        value={prize.probability}
                        onChange={(event) =>
                          updatePrize(index, 'probability', event.target.value)
                        }
                      />
                    </TableCell>
                    <TableCell className='text-right'>
                      <Button
                        variant='ghost'
                        size='icon-sm'
                        onClick={() =>
                          setDraft({
                            ...draft,
                            prizes: prizes.filter(
                              (_, prizeIndex) => prizeIndex !== index
                            ),
                          })
                        }
                        disabled={prizes.length <= 1}
                        aria-label={t('Remove tier')}
                        title={t('Remove tier')}
                      >
                        <Trash2 aria-hidden='true' />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <div
              className={`text-sm ${Math.abs(totalProbability - 100) < 0.0001 ? 'text-muted-foreground' : 'text-destructive'}`}
            >
              {t('Total probability')}: {totalProbability.toFixed(2)}%
            </div>
            {validationError ? (
              <p className='text-destructive text-sm'>{validationError}</p>
            ) : null}
            <Button onClick={save} disabled={update.isPending}>
              {update.isPending ? t('Saving...') : t('Save lottery settings')}
            </Button>
          </div>
          <div className='border-destructive/40 bg-destructive/5 space-y-3 rounded-lg border p-4'>
            <div>
              <h2 className='font-medium'>{t('User progress')}</h2>
            </div>
            <div className='flex flex-wrap items-end gap-3'>
              <div className='space-y-1'>
                <span className='text-muted-foreground block text-xs'>
                  {t('All users')}
                </span>
                <Button
                  variant={lotteryResetActionVariant('all')}
                  onClick={() => reset.mutate(undefined)}
                  disabled={reset.isPending}
                >
                  {t('Reset all user progress')}
                </Button>
              </div>
              <div className='sm:border-border flex flex-wrap items-end gap-2 sm:border-l sm:pl-3'>
                <label className='space-y-1 text-sm'>
                  <span className='text-muted-foreground block text-xs'>
                    {t('User ID')}
                  </span>
                  <Input
                    className='w-32'
                    type='number'
                    min={1}
                    placeholder={t('User ID')}
                    value={userId}
                    onChange={(event) => setUserId(event.target.value)}
                  />
                </label>
                <Button
                  variant={lotteryResetActionVariant('user')}
                  onClick={() => reset.mutate(Number(userId))}
                  disabled={!userId || reset.isPending}
                >
                  {t('Reset user')}
                </Button>
              </div>
            </div>
          </div>
        </div>
        <div className='rounded-xl border p-6'>
          <div className='mb-4 grid gap-3 sm:grid-cols-2'>
            <div className='bg-muted/40 rounded-lg p-3 text-sm'>
              {t('Total draws')}:{' '}
              <span className='font-semibold'>
                {query.data?.data.total_draws ?? 0}
              </span>
            </div>
            <div className='bg-muted/40 rounded-lg p-3 text-sm'>
              {t('Total reward amount')}:{' '}
              <span className='font-semibold'>
                {formatLocalCurrencyAmount(query.data?.data.total_amount ?? 0)}
              </span>
            </div>
          </div>
          <h2 className='mb-3 font-medium'>{t('User progress')}</h2>
          {query.data?.data.users.map((user) => (
            <div
              className='grid grid-cols-[minmax(0,1fr)_minmax(0,2fr)_auto] items-center gap-3 border-b py-2 text-sm'
              key={user.user_id}
            >
              <span className='min-w-0 truncate' title={user.username}>
                {t('User ID')}: {user.user_id}
              </span>
              <span className='min-w-0 truncate'>
                {t('Username')}: {user.username || '\u2014'}
              </span>
              <span className='text-right tabular-nums'>
                <span className='block'>
                  {t('Consumption progress')}:{' '}
                  {formatLocalCurrencyAmount(user.accumulated)} /{' '}
                  {formatLocalCurrencyAmount(user.threshold)}
                </span>
                <span className='text-muted-foreground block'>
                  {t('Available draws')}: {user.available_draws} ·{' '}
                  {t('Drawn times')}: {user.draw_count}
                </span>
              </span>
            </div>
          ))}
          <div className='mt-4 flex items-center justify-between gap-3 text-sm'>
            <span className='text-muted-foreground'>
              {t('Total')}: {query.data?.data.users_total ?? 0}
            </span>
            <div className='flex items-center gap-2'>
              <label
                className='text-muted-foreground'
                htmlFor='lottery-page-size'
              >
                {t('Page size')}
              </label>
              <select
                id='lottery-page-size'
                className='bg-background h-8 rounded-md border px-2'
                value={pageSize}
                onChange={(event) => {
                  setPageSize(Number(event.target.value))
                  setPage(1)
                }}
              >
                {LOTTERY_ADMIN_PAGE_SIZES.map((size) => (
                  <option key={size} value={size}>
                    {size}
                  </option>
                ))}
              </select>
              <Button
                variant='outline'
                size='sm'
                onClick={() => setPage(page - 1)}
                disabled={page <= 1 || query.isFetching}
              >
                {t('Previous')}
              </Button>
              <Button
                variant='outline'
                size='sm'
                onClick={() => setPage(page + 1)}
                disabled={
                  query.isFetching ||
                  page * pageSize >= (query.data?.data.users_total ?? 0)
                }
              >
                {t('Next')}
              </Button>
            </div>
          </div>
          <h2 className='mt-6 mb-3 font-medium'>{t('Draw records')}</h2>
          <div className='overflow-x-auto'>
            <table className='w-full min-w-[40rem] text-left text-sm'>
              <thead>
                <TableRow>
                  <TableHead>{t('User ID')}</TableHead>
                  <TableHead>{t('Username')}</TableHead>
                  <TableHead>{t('Prize name')}</TableHead>
                  <TableHead>{t('Reward amount')}</TableHead>
                  <TableHead>{t('Draw time')}</TableHead>
                </TableRow>
              </thead>
              <TableBody>
                {(query.data?.data.draws ?? []).map((draw) => (
                  <TableRow key={draw.id}>
                    <TableCell>{draw.user_id}</TableCell>
                    <TableCell>{draw.username || '\u2014'}</TableCell>
                    <TableCell>{draw.prize_name}</TableCell>
                    <TableCell>
                      {formatLocalCurrencyAmount(draw.prize_amount)}
                    </TableCell>
                    <TableCell>
                      {lotteryDrawDate(draw.created_at).toLocaleString()}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </table>
          </div>
        </div>
      </div>
    </PageTransition>
  )
}
