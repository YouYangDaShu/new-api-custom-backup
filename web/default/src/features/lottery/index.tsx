import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  CircleAlert,
  Coins,
  Gift,
  Loader2,
  Sparkles,
  Ticket,
  Trophy,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { PageTransition } from '@/components/page-transition'
import { Button } from '@/components/ui/button'
import { formatLocalCurrencyAmount } from '@/lib/currency'

import { drawLottery, getLottery } from './api'

function formatPrizeAmount(amount: number | undefined) {
  return typeof amount === 'number' && Number.isFinite(amount)
    ? formatLocalCurrencyAmount(amount)
    : null
}

export function Lottery() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const query = useQuery({ queryKey: ['lottery'], queryFn: getLottery })
  const draw = useMutation({
    mutationFn: drawLottery,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['lottery'] }),
  })
  const data = query.data?.data

  const remaining = data ? Math.max(data.threshold - data.accumulated, 0) : 0
  const progressPercent =
    data && data.threshold > 0
      ? Math.min(Math.max((data.accumulated / data.threshold) * 100, 0), 100)
      : 0
  const result = draw.data?.data ?? data?.recent_draw
  const prizeAmount = formatPrizeAmount(result?.prize_amount)
  const canDraw = Boolean(
    data?.enabled && data.available_draws > 0 && !draw.isPending
  )

  let content
  if (query.isLoading) {
    content = (
      <div className='bg-card flex min-h-64 items-center justify-center rounded-2xl border'>
        <Loader2
          className='text-muted-foreground size-5 animate-spin'
          aria-hidden='true'
        />
        <span className='text-muted-foreground ml-2 text-sm'>
          {t('Loading')}
        </span>
      </div>
    )
  } else if (query.isError || !data) {
    content = (
      <div className='bg-card flex min-h-64 flex-col items-center justify-center rounded-2xl border px-6 text-center'>
        <CircleAlert
          className='text-destructive mb-3 size-6'
          aria-hidden='true'
        />
        <p className='text-sm'>{t('Request failed')}</p>
        <p className='text-muted-foreground mt-1 text-xs'>
          {t('Unable to load lottery information right now')}
        </p>
      </div>
    )
  } else {
    content = (
      <div className='space-y-4'>
        <div className='grid gap-4 lg:grid-cols-[minmax(0,1.35fr)_minmax(260px,0.65fr)]'>
          <section className='border-border bg-card text-card-foreground relative overflow-hidden rounded-2xl border p-5 shadow-sm sm:p-7'>
            <div className='relative z-10'>
              <div className='flex items-start justify-between gap-4'>
                <div>
                  <p className='text-[11px] font-semibold tracking-[0.2em] text-cyan-600 dark:text-cyan-300/80'>
                    {t('Current progress')}
                  </p>
                  <h2 className='mt-2 text-xl font-semibold tracking-tight sm:text-2xl'>
                    {t('Accumulated consumption this round')}
                  </h2>
                </div>
                <div className='flex size-10 shrink-0 items-center justify-center rounded-xl border border-cyan-500/20 bg-cyan-500/10 text-cyan-600 dark:border-cyan-300/20 dark:bg-cyan-300/10 dark:text-cyan-200'>
                  <Coins className='size-5' aria-hidden='true' />
                </div>
              </div>

              <div className='mt-8 flex items-end justify-between gap-4'>
                <div>
                  <p className='text-3xl font-semibold tabular-nums sm:text-4xl'>
                    {formatLocalCurrencyAmount(data.accumulated)}
                  </p>
                  <p className='text-muted-foreground mt-1 text-xs'>
                    {t('Total spent')}
                  </p>
                </div>
                <div className='text-right'>
                  <p className='text-lg font-medium text-cyan-600 tabular-nums dark:text-cyan-200'>
                    {formatLocalCurrencyAmount(remaining)}
                  </p>
                  <p className='text-muted-foreground mt-1 text-xs'>
                    {t('Remaining to next draw')}
                  </p>
                </div>
              </div>

              <div
                className='mt-5'
                aria-label={`${progressPercent.toFixed(0)}%`}
              >
                <div className='bg-muted h-2 overflow-hidden rounded-full'>
                  <div
                    className='h-full rounded-full bg-cyan-500 transition-[width] dark:bg-cyan-300'
                    style={{ width: `${progressPercent}%` }}
                  />
                </div>
                <div className='text-muted-foreground mt-2 flex justify-between text-xs'>
                  <span>{t('Progress')}</span>
                  <span className='tabular-nums'>
                    {formatLocalCurrencyAmount(data.threshold)} {t('Threshold')}
                  </span>
                </div>
                <p className='text-muted-foreground mt-3 text-xs leading-relaxed'>
                  {t(
                    'Rule: every {{threshold}} of spending earns 1 draw. The bar shows progress toward your next draw; any surplus rolls into the next round.',
                    { threshold: formatLocalCurrencyAmount(data.threshold) }
                  )}
                </p>
              </div>
            </div>
            <div
              className='absolute -right-10 -bottom-16 size-44 rounded-full border border-cyan-500/10 dark:border-cyan-200/10'
              aria-hidden='true'
            />
          </section>

          <section className='bg-card flex flex-col justify-between rounded-2xl border p-5 shadow-sm sm:p-6'>
            <div>
              <div className='flex items-center gap-3'>
                <div className='flex size-10 items-center justify-center rounded-xl bg-blue-500/10 text-blue-500 dark:bg-blue-400/10 dark:text-blue-300'>
                  <Ticket className='size-5' aria-hidden='true' />
                </div>
                <div>
                  <p className='text-sm font-medium'>{t('Available draws')}</p>
                  <p className='text-muted-foreground text-xs'>
                    {t('Draws ready to use')}
                  </p>
                </div>
              </div>
              <p className='mt-5 text-4xl font-semibold tabular-nums'>
                {data.available_draws}
              </p>
            </div>

            <div className='mt-7'>
              <Button
                className='h-11 w-full gap-2 bg-blue-600 text-white shadow-sm hover:bg-blue-500'
                disabled={!canDraw}
                onClick={() => draw.mutate()}
              >
                {draw.isPending ? (
                  <Loader2 className='size-4 animate-spin' aria-hidden='true' />
                ) : (
                  <Sparkles className='size-4' aria-hidden='true' />
                )}
                {draw.isPending ? t('Processing') : t('Draw now')}
              </Button>
              {!data.enabled ? (
                <p className='text-muted-foreground mt-3 text-center text-xs'>
                  {t('The lottery is currently closed')}
                </p>
              ) : null}
              {data.enabled && data.available_draws < 1 ? (
                <p className='text-muted-foreground mt-3 text-center text-xs'>
                  {t('Spend more to earn your next draw')}
                </p>
              ) : null}
            </div>
          </section>
        </div>

        <section className='bg-card rounded-2xl border p-5 shadow-sm sm:p-6'>
          <div className='flex items-center gap-3'>
            <div className='flex size-9 items-center justify-center rounded-lg bg-emerald-500/10 text-emerald-500'>
              <Gift className='size-4' aria-hidden='true' />
            </div>
            <div>
              <h2 className='text-sm font-semibold'>
                {t('Available rewards')}
              </h2>
              <p className='text-muted-foreground text-xs'>
                {t('Reward tiers')}
              </p>
            </div>
          </div>
          {data.prizes?.length ? (
            <ul className='mt-5 grid gap-3 sm:grid-cols-2 lg:grid-cols-3'>
              {data.prizes.map((prize) => {
                const amount = formatPrizeAmount(prize.amount)
                return (
                  <li
                    key={prize.id}
                    className='flex items-center justify-between gap-3 rounded-xl border px-4 py-3'
                  >
                    <span className='truncate text-sm font-medium'>
                      {prize.name}
                    </span>
                    {amount ? (
                      <span className='shrink-0 text-sm font-semibold text-emerald-500 tabular-nums'>
                        +{amount}
                      </span>
                    ) : null}
                  </li>
                )
              })}
            </ul>
          ) : (
            <p className='text-muted-foreground mt-5 rounded-xl border border-dashed px-4 py-5 text-center text-sm'>
              {t('No rewards configured yet')}
            </p>
          )}
        </section>

        {draw.isError ? (
          <div
            className='border-destructive/30 bg-destructive/5 text-destructive flex items-center gap-2 rounded-xl border px-4 py-3 text-sm'
            role='alert'
          >
            <CircleAlert className='size-4 shrink-0' aria-hidden='true' />
            {draw.error instanceof Error
              ? draw.error.message
              : t('Request failed')}
          </div>
        ) : null}

        <section
          className='bg-card rounded-2xl border p-5 shadow-sm sm:p-6'
          aria-live='polite'
        >
          <div className='flex items-center gap-3'>
            <div className='flex size-9 items-center justify-center rounded-lg bg-amber-500/10 text-amber-500'>
              <Trophy className='size-4' aria-hidden='true' />
            </div>
            <div>
              <h2 className='text-sm font-semibold'>{t('Draw result')}</h2>
              <p className='text-muted-foreground text-xs'>
                {t('Credits you can win')}
              </p>
            </div>
          </div>
          {result ? (
            <div className='mt-5 flex flex-wrap items-end justify-between gap-4 rounded-xl border border-amber-500/25 bg-amber-500/5 px-4 py-4'>
              <div>
                <p className='text-muted-foreground text-xs'>{t('Result')}</p>
                <p className='mt-1 text-xl font-semibold'>
                  {result.prize_name}
                </p>
              </div>
              {prizeAmount ? (
                <p className='text-2xl font-semibold text-amber-500 tabular-nums'>
                  +{prizeAmount}
                </p>
              ) : null}
            </div>
          ) : (
            <p className='text-muted-foreground mt-5 rounded-xl border border-dashed px-4 py-5 text-center text-sm'>
              {t('No draws yet')}
            </p>
          )}
        </section>
      </div>
    )
  }

  return (
    <PageTransition className='mx-auto h-full w-full max-w-5xl overflow-y-auto px-4 py-6 sm:px-6 sm:py-8'>
      <div className='space-y-6'>
        <header>
          <p className='text-primary text-[11px] font-semibold tracking-[0.22em] uppercase'>
            {t('Lottery')}
          </p>
          <h1 className='mt-2 text-3xl font-semibold tracking-tight sm:text-4xl'>
            {t('Lottery Center')}
          </h1>
          <p className='text-muted-foreground mt-2 max-w-xl text-sm'>
            {t(
              'Spend balance to earn draws; rewards are credited directly to your balance.'
            )}
          </p>
        </header>
        {content}
      </div>
    </PageTransition>
  )
}
