import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { api } from '@/lib/api'
import { SettingsSection } from '../components/settings-section'

export function WeeklyRankingSettingsSection(props: {
  enabled: boolean
  rewards: number[]
}) {
  const { t } = useTranslation()
  const [enabled, setEnabled] = useState(props.enabled)
  const [saving, setSaving] = useState(false)
  const [values, setValues] = useState(() => (props.rewards.length ? props.rewards.map(String) : ['0', '0', '0']))
  const [history, setHistory] = useState<Array<{ week_start: string; rank: number; reward_quota: number }>>([])

  useEffect(() => {
    setEnabled(props.enabled)
    setValues(props.rewards.length ? props.rewards.map(String) : ['0', '0', '0'])
  }, [props.enabled, props.rewards])

  useEffect(() => {
    void api.get('/api/weekly-user-ranking/admin/history').then((response) => {
      const data = response.data?.data
      if (Array.isArray(data)) setHistory(data)
    }).catch(() => undefined)
  }, [])

  async function save() {
    const rewards = values.map((value) => Number(value.trim() || '0'))
    if (rewards.some((value) => !Number.isFinite(value) || value < 0)) {
      toast.error(t('Invalid reward amount'))
      return
    }
    setSaving(true)
    try {
      const response = await api.put('/api/weekly-user-ranking/admin/', {
        enabled,
        rewards,
      })
      if (response.data?.success) {
        toast.success(t('Setting updated successfully'))
      } else {
        toast.error(response.data?.message || t('Failed to update setting'))
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Failed to update setting'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <SettingsSection title={t('Weekly user ranking rewards')}>
      <div className='space-y-4 rounded-lg border p-4'>
        <div className='flex items-center justify-between gap-4'>
          <div>
            <div className='text-sm font-medium'>{t('Enable weekly ranking rewards')}</div>
            <div className='text-muted-foreground text-xs'>{t('Disable new payouts while keeping historical records')}</div>
          </div>
          <Switch checked={enabled} onCheckedChange={setEnabled} />
        </div>
        <div className='grid gap-3 sm:grid-cols-3'>
          {values.map((value, index) => (
            <label key={`weekly-reward-${index + 1}`} className='space-y-1 text-sm'>
              <span>{t('Rank {{rank}}', { rank: index + 1 })}</span>
              <Input type='number' min={0} step='any' value={value} onChange={(event) => setValues((current) => current.map((item, itemIndex) => itemIndex === index ? event.target.value : item))} />
            </label>
          ))}
        </div>
        <Button type='button' onClick={() => void save()} disabled={saving}>{t('Save')}</Button>
      </div>
      {history.length > 0 ? (
        <div className='rounded-lg border p-4'>
          <div className='mb-2 text-sm font-medium'>{t('Reward history')}</div>
          <div className='space-y-1 text-xs'>
            {history.slice(0, 20).map((item) => <div key={`${item.week_start}-${item.rank}`} className='flex justify-between gap-3'><span>{item.week_start} · {t('Rank {{rank}}', { rank: item.rank })}</span><span>{item.reward_quota.toLocaleString()}</span></div>)}
          </div>
        </div>
      ) : null}
    </SettingsSection>
  )
}
