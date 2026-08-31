import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { MultiSelect } from '@/components/multi-select'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { getGroups } from '@/features/users/api'

import {
  createChannelMonitor,
  deleteChannelMonitor,
  getAdminChannelMonitors,
  getChannelMonitorModels,
  runChannelMonitor,
  updateChannelMonitor,
  updateChannelMonitorConfig,
  type AdminChannelMonitor,
  type MonitorInput,
} from './api'
import {
  buildTimelineSlots,
  HISTORY_SLOTS,
  timelineTickClass,
} from './timeline'

const modelQueryStaleTime = 5 * 60 * 1000
const knownMonitorErrors = new Set([
  'invalid monitor name',
  'invalid monitor name, group, or model',
  'invalid monitor group',
  'interval must be between 30 and 86400 seconds',
  'group and model are required',
  'one or more extra models are not available in selected group',
  'primary model is not available in selected group',
  'selected group has no available models',
  'too many extra models',
])

const windows = [
  ['24h', '24h'],
  ['168h', '7d'],
  ['720h', '30d'],
] as const
function statusLabel(status: string, t: (key: string) => string) {
  if (status === 'success') return t('Available')
  if (status === 'failed' || status === 'timeout') return t('Unavailable')
  return t('No result')
}
function monitorErrorMessage(
  error: unknown,
  fallback: string,
  t: (key: string) => string
) {
  let message: string | undefined
  if (isAxiosError<{ message?: unknown }>(error)) {
    const responseMessage = error.response?.data?.message
    if (typeof responseMessage === 'string') message = responseMessage
  } else if (error instanceof Error) {
    message = error.message
  }
  if (message && knownMonitorErrors.has(message)) return t(message)
  return t(fallback)
}

const emptyForm: MonitorInput = {
  group: '',
  primary_model: '',
  extra_models: [],
  interval_seconds: 60,
  enabled: true,
}
function fieldLabel(
  field: 'group' | 'primary_model',
  t: (key: string) => string
) {
  if (field === 'group') return t('User-visible group name')
  return t('Model to check first')
}
function fieldDescription(
  field: 'group' | 'primary_model',
  t: (key: string) => string
) {
  if (field === 'group') {
    return t('The group name users select when making requests.')
  }
  return t('The main model used for the availability check.')
}

export function ChannelMonitorAdminPage() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [form, setForm] = useState<MonitorInput>(emptyForm)
  const [editing, setEditing] = useState<number | null>(null)
  const [error, setError] = useState('')
  const [moduleEnabled, setModuleEnabled] = useState(true)
  const query = useQuery({
    queryKey: ['admin-channel-monitors'],
    queryFn: getAdminChannelMonitors,
    refetchInterval: (currentQuery) => {
      const monitors = currentQuery.state.data?.data ?? []
      const enabledIntervals = monitors
        .filter((monitor) => monitor.module_enabled && monitor.enabled)
        .map((monitor) => monitor.interval_seconds)
        .filter((interval) => Number.isFinite(interval) && interval >= 30)
      if (enabledIntervals.length === 0) return false
      return Math.max(30, Math.min(...enabledIntervals)) * 1000
    },
  })
  const groupsQuery = useQuery({
    queryKey: ['channel-monitor', 'system-groups'],
    queryFn: async () => {
      const response = await getGroups()
      if (!response.success) throw new Error(response.message)
      return response.data ?? []
    },
    staleTime: modelQueryStaleTime,
  })
  const modelsQuery = useQuery({
    queryKey: ['channel-monitor', 'models', form.group],
    queryFn: () => getChannelMonitorModels(form.group),
    enabled: form.group.trim().length > 0,
    staleTime: modelQueryStaleTime,
  })
  const groups = groupsQuery.data ?? []
  const availableModels = modelsQuery.data ?? []
  const availableModelSet = new Set(availableModels)
  const primaryOutsideAvailableModels =
    form.primary_model !== '' && !availableModelSet.has(form.primary_model)
  const primaryModelOptions = availableModels.map((model) => ({
    label: model,
    value: model,
    unavailable: false,
  }))
  if (primaryOutsideAvailableModels) {
    const unavailable = modelsQuery.isSuccess
    primaryModelOptions.unshift({
      label: unavailable
        ? t('{{model}} (currently unavailable)', {
            model: form.primary_model,
          })
        : form.primary_model,
      value: form.primary_model,
      unavailable,
    })
  }
  let primaryPlaceholder = t('Please select a primary model')
  if (!form.group) primaryPlaceholder = t('Please select a group first')
  if (form.group && modelsQuery.isPending) {
    primaryPlaceholder = t('Loading models...')
  }
  const primarySelectItems = [
    { label: primaryPlaceholder, value: null },
    ...primaryModelOptions,
  ]
  const extraModelOptions = availableModels
    .filter((model) => model !== form.primary_model)
    .map((model) => ({ label: model, value: model }))
  const unavailableExtraModels = modelsQuery.isSuccess
    ? form.extra_models.filter((model) => !availableModelSet.has(model))
    : []
  const selectedModelsValid =
    modelsQuery.isSuccess &&
    availableModels.length > 0 &&
    availableModelSet.has(form.primary_model) &&
    form.extra_models.length <= 10 &&
    form.extra_models.every(
      (model) => model !== form.primary_model && availableModelSet.has(model)
    )
  const groupValid =
    form.group.trim().length > 0 &&
    form.group.length <= 128 &&
    !/[\r\n]/.test(form.group)
  const formValid =
    groupValid &&
    form.primary_model.length <= 128 &&
    form.interval_seconds >= 30 &&
    form.interval_seconds <= 86400 &&
    selectedModelsValid
  const groupOptions =
    form.group && !groups.includes(form.group)
      ? [form.group, ...groups]
      : groups
  const groupPlaceholder = groupsQuery.isPending
    ? t('Loading groups...')
    : t('Please select a group')
  const groupSelectItems = [
    { label: groupPlaceholder, value: null },
    ...groupOptions.map((group) => ({ label: group, value: group })),
  ]
  const changeGroup = (value: string | null) => {
    const group = value ?? ''
    setError('')
    if (!group) {
      setForm((current) => ({
        ...current,
        group: '',
        primary_model: '',
        extra_models: [],
      }))
      return
    }
    setForm((current) => ({ ...current, group }))
    void client
      .fetchQuery({
        queryKey: ['channel-monitor', 'models', group],
        queryFn: () => getChannelMonitorModels(group),
        staleTime: modelQueryStaleTime,
      })
      .then((models) => {
        const modelSet = new Set(models)
        setForm((current) => {
          if (current.group !== group) return current
          const primaryModel = modelSet.has(current.primary_model)
            ? current.primary_model
            : ''
          const extraModels = current.extra_models
            .filter((model) => model !== primaryModel && modelSet.has(model))
            .slice(0, 10)
          if (
            primaryModel === current.primary_model &&
            extraModels.length === current.extra_models.length &&
            extraModels.every(
              (model, index) => model === current.extra_models[index]
            )
          ) {
            return current
          }
          return {
            ...current,
            primary_model: primaryModel,
            extra_models: extraModels,
          }
        })
      })
      .catch(() => undefined)
  }
  useEffect(() => {
    const configured = query.data?.data?.[0]?.module_enabled
    // oxlint-disable-next-line react/set-state-in-effect
    if (configured !== undefined) setModuleEnabled(configured)
  }, [query.data?.data])
  const toggleModule = useMutation({
    mutationFn: updateChannelMonitorConfig,
    onSuccess: (_data, enabled) => {
      setModuleEnabled(enabled)
      void client.invalidateQueries({ queryKey: ['admin-channel-monitors'] })
    },
    onError: () => setError(t('Unable to update channel monitor setting')),
  })
  const save = useMutation({
    mutationFn: () => {
      if (editing === null) {
        return createChannelMonitor({ ...form, name: form.group })
      }
      return updateChannelMonitor(editing, form)
    },
    onSuccess: () => {
      setForm(emptyForm)
      setEditing(null)
      setError('')
      void client.invalidateQueries({ queryKey: ['admin-channel-monitors'] })
    },
    onError: (saveError) =>
      setError(
        monitorErrorMessage(saveError, 'Unable to save channel monitor', t)
      ),
  })
  const remove = useMutation({
    mutationFn: deleteChannelMonitor,
    onSuccess: () =>
      void client.invalidateQueries({ queryKey: ['admin-channel-monitors'] }),
    onError: () => setError(t('Unable to delete channel monitor')),
  })
  const run = useMutation({
    mutationFn: runChannelMonitor,
    onSuccess: () =>
      void client.invalidateQueries({ queryKey: ['admin-channel-monitors'] }),
    onError: () => setError(t('Unable to run channel monitor')),
  })
  const submit = () => {
    if (!formValid) {
      setError(t('Please complete all required fields'))
      return
    }
    save.mutate()
  }
  const edit = (row: AdminChannelMonitor) => {
    setEditing(row.id)
    setForm({
      group: row.group,
      primary_model: row.primary_model,
      extra_models: row.extra_models,
      interval_seconds: row.interval_seconds,
      enabled: row.enabled,
    })
  }
  return (
    <main className='mx-auto w-full max-w-6xl space-y-6 overflow-x-hidden p-4 sm:p-6'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div>
          <h1 className='text-2xl font-semibold'>
            {t('Channel monitoring settings')}
          </h1>
          <p className='text-muted-foreground mt-1 max-w-3xl text-sm'>
            {t(
              'Channel monitoring checks selected groups and models with small scheduled requests. It never displays or changes production channel keys.'
            )}
          </p>
        </div>
        <div className='flex min-w-64 items-center justify-between gap-4 rounded-lg border p-3'>
          <div>
            <div id='monitor-module-label' className='text-sm font-medium'>
              {t('Monitoring master switch')}
            </div>
            <div className='text-muted-foreground mt-0.5 text-xs'>
              {moduleEnabled ? t('Enabled') : t('Disabled')}
            </div>
          </div>
          <Switch
            checked={moduleEnabled}
            onCheckedChange={(checked) => toggleModule.mutate(checked)}
            disabled={toggleModule.isPending}
            aria-labelledby='monitor-module-label'
          />
        </div>
      </div>
      <section className='space-y-3 rounded-lg border p-4'>
        <div className='grid gap-3 sm:grid-cols-2'>
          <label className='text-sm'>
            <span className='mb-1 block'>{fieldLabel('group', t)}</span>
            <Select
              items={groupSelectItems}
              value={form.group || null}
              onValueChange={changeGroup}
              disabled={groupsQuery.isPending || groupsQuery.isError}
            >
              <SelectTrigger className='h-auto min-h-10 w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  {groupOptions.map((group) => (
                    <SelectItem key={group} value={group}>
                      {group}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            {groupsQuery.isError && (
              <span className='text-destructive mt-1 block text-xs'>
                {t('Unable to load groups, please refresh and try again')}
              </span>
            )}
            {!groupsQuery.isError && (
              <span className='text-muted-foreground mt-1 block text-xs'>
                {fieldDescription('group', t)}
              </span>
            )}
          </label>
          <label className='text-sm'>
            <span className='mb-1 block'>{fieldLabel('primary_model', t)}</span>
            <Select
              items={primarySelectItems}
              value={form.primary_model || null}
              onValueChange={(value) =>
                setForm((current) => ({
                  ...current,
                  primary_model: value ?? '',
                  extra_models: current.extra_models.filter(
                    (model) => model !== value
                  ),
                }))
              }
              disabled={
                !form.group || modelsQuery.isPending || modelsQuery.isError
              }
            >
              <SelectTrigger className='h-auto min-h-10 w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  {primaryModelOptions.map((option) => (
                    <SelectItem
                      key={option.value}
                      value={option.value}
                      disabled={option.unavailable}
                    >
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            {!form.group && (
              <span className='text-muted-foreground mt-1 block text-xs'>
                {t('Please select a group to load models')}
              </span>
            )}
            {form.group && modelsQuery.isPending && (
              <span className='text-muted-foreground mt-1 block text-xs'>
                {t('Loading models...')}
              </span>
            )}
            {form.group && modelsQuery.isError && (
              <span className='text-destructive mt-1 block text-xs'>
                {t('Unable to load models, please try again')}
              </span>
            )}
            {modelsQuery.isSuccess && availableModels.length === 0 && (
              <span className='text-destructive mt-1 block text-xs'>
                {t('The selected group has no available models')}
              </span>
            )}
            {modelsQuery.isSuccess && availableModels.length > 0 && (
              <span className='text-muted-foreground mt-1 block text-xs'>
                {fieldDescription('primary_model', t)}
              </span>
            )}
          </label>
          <label className='text-sm'>
            <span className='mb-1 block'>{t('Check interval')}</span>
            <input
              type='number'
              min={30}
              max={86400}
              className='w-full rounded-md border px-3 py-2'
              value={form.interval_seconds}
              onChange={(e) =>
                setForm({ ...form, interval_seconds: Number(e.target.value) })
              }
            />
            <span className='text-muted-foreground mt-1 block text-xs'>
              {t('At least 30 seconds between checks.')}
            </span>
          </label>
        </div>
        <label className='text-sm'>
          <span className='mb-1 block'>{t('Additional models to check')}</span>
          <MultiSelect
            options={extraModelOptions}
            selected={form.extra_models}
            onChange={(models) =>
              setForm((current) => ({
                ...current,
                extra_models: models
                  .filter((model) => model !== current.primary_model)
                  .slice(0, 10),
              }))
            }
            placeholder={t('Select up to 10 additional models')}
            emptyText={t('No additional models available')}
            allowCreate={false}
            disabled={
              !form.group ||
              modelsQuery.isPending ||
              modelsQuery.isError ||
              availableModels.length === 0
            }
          />
          <span className='text-muted-foreground mt-1 block text-xs'>
            {t('Optional, up to 10 models. The primary model is excluded.')}
          </span>
          {unavailableExtraModels.length > 0 && (
            <span className='text-destructive mt-1 block text-xs'>
              {t('Currently unavailable models: {{models}}', {
                models: unavailableExtraModels.join(', '),
              })}
            </span>
          )}
        </label>
        <div className='flex items-center justify-between gap-4 rounded-lg border p-3'>
          <div>
            <label htmlFor='monitor-enabled' className='text-sm font-medium'>
              {t('Monitor enabled')}
            </label>
            <div className='text-muted-foreground mt-0.5 text-xs'>
              {form.enabled ? t('Enabled') : t('Disabled')}
            </div>
          </div>
          <Switch
            id='monitor-enabled'
            checked={form.enabled}
            onCheckedChange={(checked) =>
              setForm((current) => ({ ...current, enabled: checked }))
            }
          />
        </div>
        {error && <p className='text-destructive text-sm'>{error}</p>}
        <div className='flex flex-wrap gap-2'>
          <button
            type='button'
            className='bg-primary text-primary-foreground rounded-md px-3 py-2'
            disabled={
              save.isPending ||
              groupsQuery.isPending ||
              groupsQuery.isError ||
              !formValid
            }
            onClick={submit}
          >
            {editing === null ? t('Add monitor') : t('Save changes')}
          </button>
          {editing !== null && (
            <button
              type='button'
              className='rounded-md border px-3 py-2'
              onClick={() => {
                setEditing(null)
                setForm(emptyForm)
              }}
            >
              {t('Cancel changes')}
            </button>
          )}
        </div>
      </section>
      {query.isLoading && <p>{t('Loading')}</p>}
      {query.isError && (
        <p className='text-destructive'>
          {t('Unable to load channel monitors')}
        </p>
      )}
      <div className='space-y-3'>
        {(query.data?.data ?? []).map((row) => (
          <section className='rounded-lg border p-4' key={row.id}>
            <div className='flex flex-wrap items-start justify-between gap-3'>
              <div>
                <div className='font-medium'>{row.name}</div>
                <div className='text-muted-foreground text-sm'>
                  {row.group} · {row.primary_model} ·{' '}
                  {t('Check interval summary', {
                    seconds: row.interval_seconds,
                  })}
                </div>
                <div className='text-sm'>
                  {row.enabled ? t('Enabled') : t('Disabled')}
                </div>
              </div>
              <div className='flex flex-wrap gap-2'>
                <button
                  type='button'
                  className='rounded-md border px-3 py-1.5 text-sm'
                  onClick={() => run.mutate(row.id)}
                  disabled={run.isPending}
                >
                  {t('Run check now')}
                </button>
                <button
                  type='button'
                  className='rounded-md border px-3 py-1.5 text-sm'
                  onClick={() => edit(row)}
                >
                  {t('Edit monitor')}
                </button>
                <button
                  type='button'
                  className='text-destructive rounded-md border px-3 py-1.5 text-sm'
                  onClick={() => remove.mutate(row.id)}
                  disabled={remove.isPending}
                >
                  {t('Delete monitor')}
                </button>
              </div>
            </div>
            <div className='mt-4 space-y-3'>
              {Object.entries(row.models).map(([model, stat]) => (
                <div className='border-t pt-3' key={model}>
                  <div className='flex flex-wrap items-center justify-between gap-2 text-sm'>
                    <span className='font-medium'>{model}</span>
                    <span
                      className={
                        stat.status === 'success'
                          ? 'text-emerald-600'
                          : 'text-destructive'
                      }
                    >
                      {statusLabel(stat.status, t)}
                    </span>
                  </div>
                  <div className='text-muted-foreground mt-1 flex flex-wrap gap-3 text-xs'>
                    {windows.map(([key, label]) => (
                      <span key={key}>
                        {label}:{' '}
                        {stat.availability?.[key] === undefined
                          ? '-'
                          : `${stat.availability[key].toFixed(1)}%`}
                      </span>
                    ))}
                    {stat.last_checked > 0 && (
                      <span>
                        {t('Recent check time')}:{' '}
                        {new Date(stat.last_checked * 1000).toLocaleString()}
                      </span>
                    )}
                  </div>
                  <div className='text-muted-foreground mt-2 text-xs'>
                    {t('Recent {{count}} records', { count: HISTORY_SLOTS })}
                  </div>
                  <div
                    className='flex h-5 w-full min-w-0 items-end gap-px'
                    aria-label={t('Recent check history')}
                  >
                    {buildTimelineSlots(stat.history, HISTORY_SLOTS).map(
                      (item, position, slots) => {
                        const sampleOccurrence = item
                          ? slots
                              .slice(0, position)
                              .filter(
                                (previous) =>
                                  previous?.checked_at === item.checked_at
                              ).length
                          : 0
                        const key = item
                          ? `timeline-sample-${item.checked_at}-${sampleOccurrence}`
                          : `timeline-empty-${position}`
                        return (
                          <span
                            key={key}
                            className={`block min-w-0 flex-1 rounded-sm ${timelineTickClass(item)}`}
                            title={
                              item
                                ? `${statusLabel(item.status, t)} · ${new Date(item.checked_at * 1000).toLocaleString()}`
                                : t('No samples yet')
                            }
                          />
                        )
                      }
                    )}
                  </div>
                  {stat.error_summary && (
                    <p className='text-destructive mt-1 text-xs'>
                      {stat.error_summary}
                    </p>
                  )}
                </div>
              ))}
            </div>
          </section>
        ))}
      </div>
    </main>
  )
}
