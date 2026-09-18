import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowDown, ArrowUp, Plus, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { PageTransition } from '@/components/page-transition'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
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
import {
  canRemoveDraftPrize,
  createDraftPrize,
  type DraftPrize,
} from './draft-prize'

type DraftConfig = Omit<LotteryAdminData['config'], 'prizes'> & {
  prizes: DraftPrize[]
}

function prizesToDraft(prizes: LotteryPrize[]): DraftPrize[] {
  return prizes.map((prize, index) => ({
    id: prize.id,
    name: prize.name,
    amount: String(prize.amount ?? 0),
    weight: String(prize.weight),
    prizeLevel: prize.prize_level,
    enabled: prize.enabled,
    sortOrder: prize.sort_order ?? index,
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
  const [pendingRemoval, setPendingRemoval] = useState<DraftPrize | null>(null)
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
  const totalWeight = prizes.reduce(
    (sum, prize) => sum + (prize.enabled ? Number(prize.weight) || 0 : 0),
    0
  )

  if (!draft) {
    return <PageTransition className='p-6'>{t('Loading')}</PageTransition>
  }

  const updatePrize = (
    index: number,
    field: 'name' | 'amount' | 'weight',
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

  const movePrize = (index: number, direction: -1 | 1) => {
    const target = index + direction
    if (target < 0 || target >= prizes.length) return
    const reordered = [...prizes]
    ;[reordered[index], reordered[target]] = [
      reordered[target],
      reordered[index],
    ]
    setDraft({
      ...draft,
      prizes: reordered.map((prize, sortOrder) => ({ ...prize, sortOrder })),
    })
  }

  const setPityBoundary = (
    boundary: 'base' | 'stage1' | 'stage2',
    value: number
  ) => {
    if (boundary === 'base') {
      setDraft({
        ...draft,
        pity_base_end: value,
        pity_stage_1_start: value + 1,
        pity_stage_1_rates: [],
      })
    } else if (boundary === 'stage1') {
      setDraft({
        ...draft,
        pity_stage_1_end: value,
        pity_stage_2_start: value + 1,
        pity_stage_1_rates: [],
        pity_stage_2_rates: [],
      })
    } else {
      setDraft({
        ...draft,
        pity_stage_2_end: value,
        pity_hard_threshold: value + 1,
        pity_stage_2_rates: [],
      })
    }
    setValidationError('')
  }

  const updatePityRate = (stage: 1 | 2, index: number, value: string) => {
    const key = stage === 1 ? 'pity_stage_1_rates' : 'pity_stage_2_rates'
    const expected =
      stage === 1
        ? draft.pity_stage_1_end - draft.pity_stage_1_start + 1
        : draft.pity_stage_2_end - draft.pity_stage_2_start + 1
    const rates = Array.from({ length: Math.max(expected, 0) }, (_, rateIndex) =>
      rateIndex === index ? Number(value) : draft[key][rateIndex] ?? Number.NaN
    )
    setDraft({ ...draft, [key]: rates })
    setValidationError('')
  }

  const save = () => {
    const activePrizes = prizes.filter((prize) => prize.enabled)
    const levels = new Set(prizes.map((prize) => prize.prizeLevel))
    if (
      prizes.length === 0 ||
      levels.size !== prizes.length ||
      prizes.some((prize) => {
        const amount = Number(prize.amount)
        const weight = Number(prize.weight)
        return (
          !prize.name.trim() ||
          !Number.isInteger(prize.prizeLevel) ||
          prize.prizeLevel <= 0 ||
          !prize.amount.trim() ||
          !Number.isFinite(amount) ||
          amount < 0 ||
          !prize.weight.trim() ||
          !Number.isInteger(weight) ||
          (prize.enabled && weight <= 0)
        )
      }) ||
      activePrizes.length === 0 ||
      totalWeight <= 0
    ) {
      setValidationError(t('Every tier needs a valid reward amount and weight'))
      return
    }
    if (draft.pity_enabled) {
      const expectedStage1 =
        draft.pity_stage_1_end - draft.pity_stage_1_start + 1
      const expectedStage2 =
        draft.pity_stage_2_end - draft.pity_stage_2_start + 1
      const allRates = [
        draft.pity_base_rate,
        ...draft.pity_stage_1_rates,
        ...draft.pity_stage_2_rates,
      ]
      if (
        !activePrizes.some((prize) => prize.prizeLevel === 1) ||
        draft.pity_stage_1_start !== draft.pity_base_end + 1 ||
        draft.pity_stage_2_start !== draft.pity_stage_1_end + 1 ||
        draft.pity_hard_threshold !== draft.pity_stage_2_end + 1 ||
        expectedStage1 < 1 ||
        expectedStage2 < 1 ||
        draft.pity_stage_1_rates.length !== expectedStage1 ||
        draft.pity_stage_2_rates.length !== expectedStage2 ||
        allRates.some(
          (rate, index) =>
            !Number.isFinite(rate) ||
            rate < 0 ||
            rate > 100 ||
            (index > 0 && rate < allRates[index - 1])
        )
      ) {
        setValidationError(t('Complete the increasing pity probability curve'))
        return
      }
    }
    const payload: LotteryAdminData['config'] = {
      ...draft,
      threshold_display_amount: Number(draft.threshold_display_amount),
      prizes: prizes.map((prize, sortOrder) => ({
        id: prize.id,
        name: prize.name.trim(),
        amount: Number(prize.amount),
        weight: Number(prize.weight),
        prize_level: prize.prizeLevel,
        enabled: prize.enabled,
        sort_order: sortOrder,
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
            <Table className='min-w-[760px]'>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Prize level')}</TableHead>
                  <TableHead>{t('Prize name')}</TableHead>
                  <TableHead>
                    {t('Reward amount')}{' '}
                    {currencySymbol ? `(${currencySymbol})` : ''}
                  </TableHead>
                  <TableHead>{t('Base weight')}</TableHead>
                  <TableHead>{t('Enabled')}</TableHead>
                  <TableHead className='w-28 text-right'>
                    {t('Actions')}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {prizes.map((prize, index) => {
                  return (
                    <TableRow key={prize.id}>
                      <TableCell className='font-medium'>
                        {t('Tier')} {prize.prizeLevel}
                      </TableCell>
                      <TableCell>
                        <Input
                          aria-label={`${t('Prize name')} ${prize.prizeLevel}`}
                          value={prize.name}
                          onChange={(event) =>
                            updatePrize(index, 'name', event.target.value)
                          }
                        />
                      </TableCell>
                      <TableCell>
                        <Input
                          aria-label={`${t('Reward amount')} ${prize.prizeLevel}`}
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
                          aria-label={`${t('Base weight')} ${prize.prizeLevel}`}
                          type='number'
                          min={1}
                          step={1}
                          value={prize.weight}
                          onChange={(event) =>
                            updatePrize(index, 'weight', event.target.value)
                          }
                        />
                      </TableCell>
                      <TableCell>
                        <Switch
                          checked={prize.enabled}
                          onCheckedChange={(enabled) =>
                            setDraft({
                              ...draft,
                              prizes: prizes.map((item, itemIndex) =>
                                itemIndex === index ? { ...item, enabled } : item
                              ),
                            })
                          }
                          aria-label={`${t('Enabled')} ${prize.prizeLevel}`}
                        />
                      </TableCell>
                      <TableCell className='text-right'>
                        <div className='flex justify-end gap-1'>
                          <Button
                            variant='ghost'
                            size='icon-sm'
                            onClick={() => movePrize(index, -1)}
                            disabled={index === 0}
                            aria-label={t('Move up')}
                            title={t('Move up')}
                          >
                            <ArrowUp aria-hidden='true' />
                          </Button>
                          <Button
                            variant='ghost'
                            size='icon-sm'
                            onClick={() => movePrize(index, 1)}
                            disabled={index === prizes.length - 1}
                            aria-label={t('Move down')}
                            title={t('Move down')}
                          >
                            <ArrowDown aria-hidden='true' />
                          </Button>
                          <Button
                            variant='ghost'
                            size='icon-sm'
                            onClick={() => setPendingRemoval(prize)}
                            disabled={
                              !canRemoveDraftPrize(prize.prizeLevel, prizes.length)
                            }
                            aria-label={t('Remove tier')}
                            title={t('Remove tier')}
                          >
                            <Trash2 aria-hidden='true' />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
            <div className='text-muted-foreground text-sm'>
              {t('Enabled base weight total')}: {totalWeight}
            </div>
            <div className='space-y-4 rounded-xl border p-4'>
              <div className='flex items-center justify-between gap-4'>
                <div>
                  <h2 className='font-medium'>{t('Hidden pity curve')}</h2>
                  <p className='text-muted-foreground mt-1 text-xs'>
                    {t('This curve only adjusts first-prize odds and is not shown to users')}
                  </p>
                </div>
                <Switch
                  checked={draft.pity_enabled}
                  onCheckedChange={(pity_enabled) =>
                    setDraft({ ...draft, pity_enabled })
                  }
                  aria-label={t('Enable hidden pity')}
                />
              </div>
              {draft.pity_enabled ? (
                <div className='space-y-4'>
                  <div className='grid gap-3 sm:grid-cols-3'>
                    <label className='space-y-1 text-sm'>
                      <span>{t('Base draws end')}</span>
                      <Input
                        type='number'
                        min={1}
                        value={draft.pity_base_end}
                        onChange={(event) =>
                          setPityBoundary('base', Number(event.target.value))
                        }
                      />
                    </label>
                    <label className='space-y-1 text-sm'>
                      <span>{t('Soft pity draws end')}</span>
                      <Input
                        type='number'
                        min={draft.pity_stage_1_start}
                        value={draft.pity_stage_1_end}
                        onChange={(event) =>
                          setPityBoundary('stage1', Number(event.target.value))
                        }
                      />
                    </label>
                    <label className='space-y-1 text-sm'>
                      <span>{t('Hard pity draws end')}</span>
                      <Input
                        type='number'
                        min={draft.pity_stage_2_start}
                        value={draft.pity_stage_2_end}
                        onChange={(event) =>
                          setPityBoundary('stage2', Number(event.target.value))
                        }
                      />
                    </label>
                  </div>
                  <label className='block max-w-xs space-y-1 text-sm'>
                    <span>{t('Base first-prize probability (%)')}</span>
                    <Input
                      type='number'
                      min={0}
                      max={100}
                      step='any'
                      value={draft.pity_base_rate}
                      onChange={(event) =>
                        setDraft({
                          ...draft,
                          pity_base_rate: Number(event.target.value),
                        })
                      }
                    />
                  </label>
                  {([1, 2] as const).map((stage) => {
                    const rates =
                      stage === 1
                        ? draft.pity_stage_1_rates
                        : draft.pity_stage_2_rates
                    const start =
                      stage === 1
                        ? draft.pity_stage_1_start
                        : draft.pity_stage_2_start
                    const length =
                      stage === 1
                        ? draft.pity_stage_1_end - draft.pity_stage_1_start + 1
                        : draft.pity_stage_2_end - draft.pity_stage_2_start + 1
                    const label =
                      stage === 1 ? t('Soft pity probability') : t('Hard pity probability')
                    return (
                      <div key={stage}>
                        <p className='mb-2 text-sm font-medium'>{label}</p>
                        <div className='grid grid-cols-2 gap-2 sm:grid-cols-5'>
                          {Array.from({ length: Math.max(length, 0) }, (_, index) => (
                            <label className='space-y-1 text-xs' key={start + index}>
                              <span>
                                {t('Draw number')} {start + index}
                              </span>
                              <Input
                                type='number'
                                min={0}
                                max={100}
                                step='any'
                                value={rates[index] ?? ''}
                                onChange={(event) =>
                                  updatePityRate(stage, index, event.target.value)
                                }
                              />
                            </label>
                          ))}
                        </div>
                        {length < 1 ? (
                          <p className='text-destructive mt-2 text-xs'>
                            {t('Each pity range must contain at least one draw')}
                          </p>
                        ) : null}
                      </div>
                    )
                  })}
                  <p className='text-muted-foreground text-xs'>
                    {t('Draw number')} {draft.pity_hard_threshold}: 100%
                  </p>
                </div>
              ) : null}
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
              <Select
                items={LOTTERY_ADMIN_PAGE_SIZES.map((size) => ({
                  value: String(size),
                  label: String(size),
                }))}
                value={String(pageSize)}
                onValueChange={(value) => {
                  setPageSize(Number(value))
                  setPage(1)
                }}
              >
                <SelectTrigger id='lottery-page-size' size='sm'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {LOTTERY_ADMIN_PAGE_SIZES.map((size) => (
                      <SelectItem key={size} value={String(size)}>
                        {size}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
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
          <Table className='min-w-[40rem]'>
            <TableHeader>
              <TableRow>
                <TableHead>{t('User ID')}</TableHead>
                <TableHead>{t('Username')}</TableHead>
                <TableHead>{t('Prize name')}</TableHead>
                <TableHead>{t('Reward amount')}</TableHead>
                <TableHead>{t('Draw time')}</TableHead>
              </TableRow>
            </TableHeader>
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
          </Table>
        </div>
      </div>
      <ConfirmDialog
        open={pendingRemoval !== null}
        onOpenChange={(open) => !open && setPendingRemoval(null)}
        title={t('Remove prize tier?')}
        desc={t(
          'Removing {{name}} takes effect after saving. A tier with draw history cannot be removed; disable it instead.',
          { name: pendingRemoval?.name ?? '' }
        )}
        destructive
        confirmText={t('Remove tier')}
        handleConfirm={() => {
          if (!pendingRemoval) return
          setDraft({
            ...draft,
            prizes: prizes.filter((prize) => prize.id !== pendingRemoval.id),
          })
          setPendingRemoval(null)
          setValidationError('')
        }}
      />
    </PageTransition>
  )
}
