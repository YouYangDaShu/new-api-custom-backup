import { Plus, Trash2 } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldTitle,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { api } from '@/lib/api'
import { useSystemConfigStore } from '@/stores/system-config-store'

import {
  MAX_REWARD_RANKS,
  MAX_REWARD_VALUE,
  parseRewardInputs,
  rewardInputsFromApi,
} from './rewards'

type HistoryRow = {
  week_start: string
  rank: number
  total_tokens: number
  reward_quota: number
  reward_status: string
  created_at: number
}
type Config = {
  enabled: boolean
  rewards: number[]
  history: HistoryRow[]
  total_reward_count: number
  total_reward_quota: number
}

function formatYuanFromQuota(quota: number, quotaPerUnit: number) {
  if (
    !Number.isFinite(quota) ||
    !Number.isFinite(quotaPerUnit) ||
    quotaPerUnit <= 0
  ) {
    return '-'
  }
  return `${(quota / quotaPerUnit).toFixed(2)} 元`
}

function rewardStatusLabel(status: string, t: (key: string) => string) {
  if (status === 'awarded') return t('Awarded')
  return t('Not awarded')
}

export function TokenRankingAdminPage() {
  const { t } = useTranslation()
  const quotaPerUnit = useSystemConfigStore(
    (state) => state.config.currency.quotaPerUnit
  )
  const [config, setConfig] = useState<Config>({
    enabled: true,
    rewards: [],
    history: [],
    total_reward_count: 0,
    total_reward_quota: 0,
  })
  const [rewardInputs, setRewardInputs] = useState(() =>
    rewardInputsFromApi([])
  )
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const load = useCallback(async () => {
    try {
      const res = await api.get('/api/weekly-user-ranking/admin/')
      const data = res.data.data as Config
      setConfig(data)
      setRewardInputs(rewardInputsFromApi(data.rewards))
      setError('')
    } catch {
      setError(t('Unable to load token rankings'))
    } finally {
      setLoading(false)
    }
  }, [t])
  useEffect(() => {
    void load()
  }, [load])
  const save = async () => {
    const values = parseRewardInputs(rewardInputs)
    if (values === null) {
      setError(t('Invalid reward values'))
      return
    }
    setSaving(true)
    setError('')
    try {
      await api.put('/api/weekly-user-ranking/admin/', {
        enabled: config.enabled,
        rewards: values,
      })
      setConfig((current) => ({ ...current, rewards: values }))
    } catch {
      setError(t('Unable to save token rankings'))
    } finally {
      setSaving(false)
    }
  }
  return (
    <main className='mx-auto w-full max-w-5xl space-y-6 overflow-x-hidden p-4 sm:p-6'>
      <div>
        <h1 className='text-2xl font-semibold'>
          {t('Token ranking settings')}
        </h1>
        <p className='text-muted-foreground mt-1 max-w-3xl text-sm'>
          {t(
            'Rank users by successfully consumed Token. Settlement runs once a week. When disabled, no usage is counted and no rewards are issued during the disabled period; historical records remain.'
          )}
        </p>
      </div>
      {loading && <p>{t('Loading')}</p>}
      <section className='grid gap-3 sm:grid-cols-2'>
        <div className='rounded-lg border p-4 text-sm'>
          <div className='text-muted-foreground'>
            {t('Settlement batches')}
          </div>
          <div className='mt-1 text-xl font-semibold'>
            {config.total_reward_count}
          </div>
        </div>
        <div className='rounded-lg border p-4 text-sm'>
          <div className='text-muted-foreground'>
            {t('Total reward amount')}
          </div>
          <div className='mt-1 text-xl font-semibold'>
            {formatYuanFromQuota(config.total_reward_quota, quotaPerUnit)}
          </div>
        </div>
      </section>
      <section className='flex flex-col gap-5 rounded-lg border p-4'>
        <Field orientation='horizontal' className='rounded-lg border p-3'>
          <FieldContent>
            <FieldTitle id='ranking-enabled-label'>
              {t('Enable ranking and rewards')}
            </FieldTitle>
            <FieldDescription>
              {config.enabled ? t('Enabled') : t('Disabled')}
            </FieldDescription>
          </FieldContent>
          <Switch
            checked={config.enabled}
            onCheckedChange={(checked) =>
              setConfig((current) => ({ ...current, enabled: checked }))
            }
            aria-labelledby='ranking-enabled-label'
          />
        </Field>

        <FieldGroup className='gap-3'>
          <div className='flex flex-wrap items-center justify-between gap-3'>
            <div>
              <h2 className='text-sm font-medium'>{t('Reward tiers')}</h2>
              <p className='text-muted-foreground mt-0.5 text-xs'>
                {t('Configure up to 100 reward ranks.')}
              </p>
            </div>
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={rewardInputs.length >= MAX_REWARD_RANKS}
              onClick={() => setRewardInputs((current) => [...current, '0'])}
            >
              <Plus data-icon='inline-start' aria-hidden='true' />
              {t('Add reward tier')}
            </Button>
          </div>
          {rewardInputs.map((value, index) => (
            <Field
              orientation='horizontal'
              className='rounded-lg border p-3'
              key={`reward-rank-${index + 1}`}
            >
              <FieldLabel
                htmlFor={`reward-rank-${index + 1}`}
                className='min-w-20'
              >
                {t('Rank {{rank}}', { rank: index + 1 })}
              </FieldLabel>
              <div className='flex min-w-0 flex-1 items-center gap-2'>
                <span className='text-muted-foreground' aria-hidden='true'>
                  ¥
                </span>
                <Input
                  id={`reward-rank-${index + 1}`}
                  type='number'
                  min={0}
                  max={MAX_REWARD_VALUE}
                  step='any'
                  value={value}
                  aria-label={t('Reward amount')}
                  onChange={(event) =>
                    setRewardInputs((current) =>
                      current.map((reward, rewardIndex) =>
                        rewardIndex === index ? event.target.value : reward
                      )
                    )
                  }
                />
                <Button
                  type='button'
                  variant='ghost'
                  size='icon'
                  disabled={rewardInputs.length === 1}
                  aria-label={t('Remove reward for rank {{rank}}', {
                    rank: index + 1,
                  })}
                  title={t('Remove reward for rank {{rank}}', {
                    rank: index + 1,
                  })}
                  onClick={() =>
                    setRewardInputs((current) =>
                      current.filter((_, rewardIndex) => rewardIndex !== index)
                    )
                  }
                >
                  <Trash2 aria-hidden='true' />
                </Button>
              </div>
            </Field>
          ))}
        </FieldGroup>
        {error && <p className='text-destructive text-sm'>{error}</p>}
        <Button
          type='button'
          className='self-start'
          disabled={saving}
          onClick={() => void save()}
        >
          {t('Save settings')}
        </Button>
      </section>
      <section className='space-y-2'>
        <h2 className='font-medium'>{t('Reward history')}</h2>
        <div className='overflow-x-auto'>
          <table className='w-full min-w-[32rem] text-left text-sm'>
            <thead>
              <tr>
                <th className='p-2'>{t('Cycle')}</th>
                <th className='p-2'>{t('Place')}</th>
                <th className='p-2'>{t('Token usage')}</th>
                <th className='p-2'>{t('Reward amount')}</th>
                <th className='p-2'>{t('Payout status')}</th>
                <th className='p-2'>{t('Payout time')}</th>
              </tr>
            </thead>
            <tbody>
              {config.history.map((item) => (
                <tr
                  className='border-t'
                  key={`${item.week_start}-${item.rank}`}
                >
                  <td className='p-2'>{item.week_start}</td>
                  <td className='p-2'>#{item.rank}</td>
                  <td className='p-2'>{item.total_tokens.toLocaleString()}</td>
                  <td className='p-2'>
                    {formatYuanFromQuota(item.reward_quota, quotaPerUnit)}
                  </td>
                  <td className='p-2'>
                    {rewardStatusLabel(item.reward_status, t)}
                  </td>
                  <td className='p-2'>
                    {new Date(item.created_at * 1000).toLocaleString()}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </main>
  )
}
