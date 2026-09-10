/*
Copyright (C) 2023-2026 QuantumNous

Token group picker aligned with 汇流:
- multi-select real groups
- order = priority
- stored as single group / @manual:a,b,c / auto / @smart:balanced
*/
import { ArrowDown, ArrowUp, Plus, X } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'

import {
  encodeTokenGroupValue,
  parseTokenGroupValue,
  type SmartRouteMode,
} from '../lib/token-group-encoding'
import type { ApiKeyGroupOption } from './api-key-group-combobox'

type ApiKeyGroupPriorityPickerProps = {
  options: ApiKeyGroupOption[]
  value?: string
  onValueChange: (value: string) => void
  onModeChange?: (mode: 'auto' | 'manual' | 'smart') => void
  disabled?: boolean
}

function formatRatio(ratio: ApiKeyGroupOption['ratio']) {
  if (ratio === undefined || ratio === null || ratio === '') return null
  if (typeof ratio === 'number') {
    if (!Number.isFinite(ratio)) return null
    const formatted = Number.isInteger(ratio)
      ? String(ratio)
      : ratio.toFixed(6).replace(/\.?0+$/, '')
    return `${formatted}x`
  }
  return `${ratio}x`
}

export function ApiKeyGroupPriorityPicker({
  options,
  value,
  onValueChange,
  onModeChange,
  disabled,
}: ApiKeyGroupPriorityPickerProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [modeOpen, setModeOpen] = useState(false)
  const [search, setSearch] = useState('')

  const parsed = useMemo(() => parseTokenGroupValue(value), [value])
  // single / manual 都要显示已选；auto / smart 走智能面板
  const isAuto = parsed.mode === 'auto'
  const isSmart = parsed.mode === 'smart'
  // legacy auto / @smart:price 也当智能面板
  const isSmartLike = isAuto || isSmart
  const selected = isSmartLike ? [] : parsed.groups

  const realOptions = useMemo(
    () =>
      options.filter(
        (o) => o.value && o.value !== 'auto' && !o.value.startsWith('@smart:')
      ),
    [options]
  )

  // smart modes always available when there is ≥1 real model group
  const hasSmartOption = realOptions.length > 0

  let selectedModeLabel = parsed.groups[0] || t('Select a group')
  if (parsed.mode === 'auto') selectedModeLabel = 'auto'
  if (parsed.mode === 'smart') {
    selectedModeLabel = `@smart:${parsed.smartMode || 'balanced'}`
  }
  if (parsed.mode === 'manual' && parsed.groups.length > 0) {
    selectedModeLabel = parsed.groups.join(', ')
  }

  const available = useMemo(() => {
    const q = search.trim().toLowerCase()
    const selectedGroups = isSmartLike ? [] : parsed.groups
    return realOptions.filter((o) => {
      if (selectedGroups.includes(o.value)) return false
      if (!q) return true
      return (
        o.value.toLowerCase().includes(q) ||
        o.label.toLowerCase().includes(q) ||
        (o.desc || '').toLowerCase().includes(q)
      )
    })
  }, [realOptions, isSmartLike, parsed.groups, search])

  const ratioMap = useMemo(() => {
    const m = new Map<string, ApiKeyGroupOption['ratio']>()
    for (const o of realOptions) m.set(o.value, o.ratio)
    return m
  }, [realOptions])

  const emitManual = (groups: string[]) => {
    onValueChange(encodeTokenGroupValue({ mode: 'manual', groups }))
  }

  const addGroup = (g: string) => {
    if (!g || selected.includes(g)) return
    emitManual([...selected, g])
    setOpen(false)
    setSearch('')
  }

  const removeAt = (idx: number) => {
    emitManual(selected.filter((_, i) => i !== idx))
  }

  const move = (idx: number, delta: number) => {
    const j = idx + delta
    if (j < 0 || j >= selected.length) return
    const next = [...selected]
    const tmp = next[idx]
    next[idx] = next[j]
    next[j] = tmp
    emitManual(next)
  }

  const switchToManual = () => {
    // leave smart/auto → empty manual (user must pick ≥1)
    onModeChange?.('manual')
    emitManual([])
  }

  const switchToSmart = () => {
    onModeChange?.('smart')
    onValueChange(
      encodeTokenGroupValue({
        mode: 'smart',
        groups: [],
        smartMode: 'balanced',
      })
    )
  }

  const setSmartMode = (smartMode: SmartRouteMode) => {
    onValueChange(
      encodeTokenGroupValue({ mode: 'smart', groups: [], smartMode })
    )
  }

  const smartModeCopy = {
    balanced: {
      title: t('综合优先'),
      description: t('综合价格、成功率和延迟进行选择'),
    },
    price: {
      title: t('价格优先'),
      description: t('优先选择当前价格较低的可用分组'),
    },
    success_rate: {
      title: t('成功率优先'),
      description: t('优先选择近期成功率较高的可用分组'),
    },
  } as const
  const activeSmartMode = parsed.smartMode || 'balanced'

  return (
    <div
      data-slot='form-control'
      role='group'
      className='space-y-3'
      onClick={(event) => {
        if (event.target === event.currentTarget) setModeOpen(true)
      }}
    >
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <Popover open={modeOpen} onOpenChange={setModeOpen}>
          <PopoverTrigger
            render={
              <Button
                type='button'
                role='combobox'
                variant='outline'
                className='min-w-0 flex-1 justify-start text-left'
                disabled={disabled}
              />
            }
          >
            <span className='truncate'>{selectedModeLabel}</span>
          </PopoverTrigger>
          <PopoverContent
            className='w-[var(--anchor-width)] overflow-hidden p-0'
            align='start'
          >
            <Command shouldFilter={false}>
              <CommandInput
                placeholder={t('Search groups')}
                value={search}
                onValueChange={setSearch}
              />
              <CommandList className='max-h-[360px]'>
                <CommandEmpty>{t('No group found')}</CommandEmpty>
                <CommandGroup>
                  {options
                    .filter((option) => {
                      const q = search.trim().toLowerCase()
                      return (
                        !q ||
                        option.value.toLowerCase().includes(q) ||
                        option.label.toLowerCase().includes(q) ||
                        option.desc?.toLowerCase().includes(q)
                      )
                    })
                    .map((option) => (
                      <CommandItem
                        key={option.value}
                        value={option.value}
                        onSelect={() => {
                          onValueChange(option.value)
                          setModeOpen(false)
                          setSearch('')
                        }}
                      >
                        <span className='truncate'>{option.label}</span>
                        {option.desc && option.desc !== option.label ? (
                          <span className='text-muted-foreground ml-2 truncate text-xs'>
                            {option.desc}
                          </span>
                        ) : null}
                      </CommandItem>
                    ))}
                </CommandGroup>
              </CommandList>
            </Command>
          </PopoverContent>
        </Popover>
        {hasSmartOption && (
          <Button
            type='button'
            size='sm'
            variant={isSmartLike ? 'outline' : 'default'}
            disabled={disabled}
            onClick={() => (isSmartLike ? switchToManual() : switchToSmart())}
          >
            {isSmartLike ? t('切换到手动选择') : t('智能路由')}
          </Button>
        )}
      </div>

      <div className='rounded-lg border p-3 sm:p-4'>
        {isSmartLike ? (
          <div className='text-muted-foreground space-y-2 text-sm'>
            <p className='text-foreground font-medium'>{t('智能路由')}</p>
            <p>{smartModeCopy[activeSmartMode].title}</p>
            <p>{smartModeCopy[activeSmartMode].description}</p>
            <p>{t('仅使用实际包含当前模型的可用分组')}</p>
            <div className='flex flex-wrap gap-2'>
              {(
                [
                  ['balanced', t('综合优先')],
                  ['price', t('价格优先')],
                  ['success_rate', t('成功率优先')],
                ] as const
              ).map(([mode, label]) => (
                <Button
                  key={mode}
                  type='button'
                  size='sm'
                  variant={
                    parsed.smartMode === mode ||
                    (mode === 'balanced' && !parsed.smartMode)
                      ? 'default'
                      : 'outline'
                  }
                  disabled={disabled}
                  onClick={() => setSmartMode(mode)}
                >
                  {label}
                </Button>
              ))}
            </div>
            <Badge variant='outline'>
              @smart:{parsed.smartMode || 'balanced'}
            </Badge>
          </div>
        ) : (
          <div className='space-y-3'>
            <div className='text-muted-foreground flex flex-wrap items-center gap-2 text-xs'>
              <span>
                {t(
                  'Select groups; multiple selections are allowed and order determines priority'
                )}
              </span>
              {selected.length > 0 && (
                <Badge variant='secondary' className='text-[10px]'>
                  {t('Selected')} {selected.length}
                </Badge>
              )}
            </div>

            <Popover open={open} onOpenChange={setOpen}>
              <PopoverTrigger
                render={
                  <Button
                    type='button'
                    variant='outline'
                    className='h-9 w-full justify-start gap-2'
                    disabled={disabled}
                  />
                }
              >
                <Plus className='size-4' />
                {t('Select groups to add')}
              </PopoverTrigger>
              <PopoverContent
                className='w-[var(--anchor-width)] overflow-hidden p-0'
                align='start'
                onWheel={(event) => event.stopPropagation()}
                onTouchMove={(event) => event.stopPropagation()}
                onPointerDown={(event) => event.stopPropagation()}
              >
                <Command shouldFilter={false}>
                  <CommandInput
                    placeholder={t('Search groups')}
                    value={search}
                    onValueChange={setSearch}
                  />
                  <CommandList className='max-h-[360px]'>
                    <CommandEmpty>{t('No group found')}</CommandEmpty>
                    <CommandGroup>
                      {available.map((opt) => (
                        <CommandItem
                          key={opt.value}
                          value={opt.value}
                          onSelect={() => addGroup(opt.value)}
                          onClick={() => addGroup(opt.value)}
                          className='cursor-pointer items-start gap-3 rounded-lg px-3 py-3'
                        >
                          <div className='flex min-w-0 flex-1 items-center justify-between gap-2'>
                            <div className='min-w-0'>
                              <div className='truncate font-medium'>
                                {opt.label}
                              </div>
                              {opt.desc && opt.desc !== opt.label && (
                                <div className='text-muted-foreground truncate text-xs'>
                                  {opt.desc}
                                </div>
                              )}
                            </div>
                            {formatRatio(opt.ratio) && (
                              <Badge
                                variant='outline'
                                className='shrink-0 text-[10px]'
                              >
                                {formatRatio(opt.ratio)}
                              </Badge>
                            )}
                          </div>
                        </CommandItem>
                      ))}
                    </CommandGroup>
                  </CommandList>
                </Command>
              </PopoverContent>
            </Popover>

            {selected.length > 0 && (
              <ul className='space-y-2'>
                {selected.map((g, idx) => (
                  <li
                    key={g}
                    className='bg-muted/20 flex items-center gap-2 rounded-md border px-2 py-1.5'
                  >
                    <Badge variant='secondary' className='shrink-0 text-[10px]'>
                      {idx + 1}
                    </Badge>
                    <div className='min-w-0 flex-1'>
                      <div className='truncate text-sm font-medium'>{g}</div>
                      {formatRatio(ratioMap.get(g)) && (
                        <div className='text-muted-foreground text-[10px]'>
                          {formatRatio(ratioMap.get(g))}
                        </div>
                      )}
                    </div>
                    <div className='flex shrink-0 items-center gap-0.5'>
                      <Button
                        type='button'
                        size='icon'
                        variant='ghost'
                        className='size-7'
                        disabled={disabled || idx === 0}
                        onClick={(e) => {
                          e.preventDefault()
                          e.stopPropagation()
                          move(idx, -1)
                        }}
                      >
                        <ArrowUp className='size-3.5' />
                      </Button>
                      <Button
                        type='button'
                        size='icon'
                        variant='ghost'
                        className='size-7'
                        disabled={disabled || idx === selected.length - 1}
                        onClick={(e) => {
                          e.preventDefault()
                          e.stopPropagation()
                          move(idx, 1)
                        }}
                      >
                        <ArrowDown className='size-3.5' />
                      </Button>
                      <Button
                        type='button'
                        size='icon'
                        variant='ghost'
                        className='size-7'
                        disabled={disabled}
                        onClick={(e) => {
                          e.preventDefault()
                          e.stopPropagation()
                          removeAt(idx)
                        }}
                      >
                        <X className='size-3.5' />
                      </Button>
                    </div>
                  </li>
                ))}
              </ul>
            )}

            {selected.length === 0 && (
              <p className='text-muted-foreground text-xs'>
                {t(
                  'In manual mode, select at least one group or this token cannot be used.'
                )}
              </p>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
