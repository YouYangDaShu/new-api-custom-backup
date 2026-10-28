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
import { formatBillingCurrencyFromUSD } from '@/lib/currency'

import { TOKEN_UNIT_DIVISORS } from '../constants'
import type {
  BillingUsageSchema,
  BillingUsageUnit,
  PricingModel,
  TokenUnit,
} from '../types'
import {
  BILLING_PRICING_VARS,
  evaluateTierConditionAt,
  isTimeOnlyCondition,
  parseTaskTiersFromExpr,
  parseTiersFromExpr,
  splitBillingExprAndRequestRules,
  tryParseRequestRuleExpr,
  type BillingVar,
  type ParsedTaskTier,
  type ParsedTier,
} from './billing-expr'
import { getDisplayGroupRatio } from './model-helpers'
import {
  evaluateTaskVisualConfig,
  getTaskNumberFields,
  tryParseTaskVisualConfig,
} from './task-expr'

export type DynamicPriceOptions = {
  tokenUnit: TokenUnit
  showCurrencySymbol?: boolean
  showRechargePrice?: boolean
  priceRate?: number
  usdExchangeRate?: number
  groupRatioMultiplier?: number
  usageSchema?: BillingUsageSchema
  /** Clock used to pick the tier a time-conditional model is charging now. */
  now?: Date
}

export type DynamicPriceLabelKind = 'i18n' | 'schema'

export type DynamicPriceEntry = {
  key: string
  field: string
  label: string
  shortLabel: string
  /** `schema` labels are raw usage-field names and must not go through `t()`. */
  labelKind: DynamicPriceLabelKind
  value: number
  formatted: string
  formattedRange?: string
  unit: 'token' | BillingUsageUnit | 'request'
  variable?: BillingVar
  description?: string | Record<string, string>
}

export type CardExamplePrice = {
  label: string
  formatted: string
}

export type DynamicPricingTier = ParsedTier | ParsedTaskTier

export type DynamicPricingSummary = {
  tiers: DynamicPricingTier[]
  tier: DynamicPricingTier | null
  tierCount: number
  hasRequestRules: boolean
  isSpecialExpression: boolean
  /** Labels of the tiers that differ in price, in expression order. */
  tierLabels: string[]
  rawExpression: string
  entries: DynamicPriceEntry[]
  primaryEntries: DynamicPriceEntry[]
  secondaryEntries: DynamicPriceEntry[]
  isTaskUsage: boolean
  /** True when the advertised tier was selected by the wall clock. */
  isTimePricing?: boolean
}

export function getTaskUsageQuantityUnitLabelKey(
  unit: BillingUsageUnit | undefined
): string {
  if (unit === 'second') return 's'
  if (unit === 'token') return 'token (unit)'
  if (unit === 'credit') return 'credit'
  return 'unit'
}

export function getTaskUsagePriceUnitLabelKey(
  unit: BillingUsageUnit | undefined
): string {
  if (unit === 'second') return 'second'
  if (unit === 'token') return '1M token'
  if (unit === 'credit') return 'credit'
  return 'unit'
}

export function getDynamicPriceUnitLabelKey(
  entry: DynamicPriceEntry
): string | null {
  if (entry.unit === 'second') return 's'
  if (entry.unit === 'count') return 'unit'
  if (entry.unit === 'credit') return 'credit'
  // Chat token entries also use unit 'token' but keep the 1M-token label.
  if (entry.unit === 'token' && !entry.variable) return '1M token'
  if (entry.unit === 'request') return 'request'
  return null
}

const PRIMARY_DYNAMIC_FIELDS = new Set(['inputPrice', 'outputPrice'])

function isTaskPricingTier(tier: DynamicPricingTier): tier is ParsedTaskTier {
  return (
    Object.hasOwn(tier, 'unitPrices') &&
    typeof (tier as ParsedTaskTier).unitPrices === 'object'
  )
}

export function isDynamicPricingModel(model: PricingModel): boolean {
  return model.billing_mode === 'tiered_expr' && Boolean(model.billing_expr)
}

export function hasTaskUsageSchema(model: PricingModel): boolean {
  return Object.keys(model.billing_usage_schema ?? {}).length > 0
}

export function isTaskUsagePricingModel(model: PricingModel): boolean {
  return model.billing_mode === 'tiered_expr' && hasTaskUsageSchema(model)
}

export function isUnconfiguredTaskUsageModel(model: PricingModel): boolean {
  return (
    model.quota_type !== 1 &&
    hasTaskUsageSchema(model) &&
    !isDynamicPricingModel(model)
  )
}

export function getTaskPricingUnit(
  model: PricingModel
): BillingUsageUnit | null {
  const primaryField = getTaskNumberFields(model.billing_usage_schema)[0]
  return primaryField?.[1].unit ?? null
}

export function getDynamicDisplayGroupRatio(
  model: PricingModel,
  selectedGroup?: string
): number {
  return getDisplayGroupRatio(model, selectedGroup)
}

function applyRechargeRate(
  price: number,
  showWithRecharge: boolean,
  priceRate: number,
  usdExchangeRate: number
): number {
  if (!showWithRecharge) return price
  return (price * priceRate) / usdExchangeRate
}

export function formatDynamicUnitPrice(
  valuePerMillionTokens: number,
  options: DynamicPriceOptions
): string {
  const groupRatio = options.groupRatioMultiplier ?? 1
  const priceRate = options.priceRate ?? 1
  const usdExchangeRate = options.usdExchangeRate ?? 1
  const priceUSD =
    (valuePerMillionTokens * groupRatio) /
    TOKEN_UNIT_DIVISORS[options.tokenUnit]
  const displayPrice = applyRechargeRate(
    priceUSD,
    options.showRechargePrice ?? false,
    priceRate,
    usdExchangeRate
  )

  return formatBillingCurrencyFromUSD(displayPrice, {
    showSymbol: options.showCurrencySymbol ?? true,
    digitsLarge: 4,
    digitsSmall: 6,
    abbreviate: false,
  })
}

export function formatTaskUsageUnitPrice(
  valuePerUnit: number,
  options: DynamicPriceOptions
): string {
  const groupRatio = options.groupRatioMultiplier ?? 1
  const priceRate = options.priceRate ?? 1
  const usdExchangeRate = options.usdExchangeRate ?? 1
  const priceUSD = valuePerUnit * groupRatio
  const displayPrice = applyRechargeRate(
    priceUSD,
    options.showRechargePrice ?? false,
    priceRate,
    usdExchangeRate
  )

  return formatBillingCurrencyFromUSD(displayPrice, {
    showSymbol: options.showCurrencySymbol ?? true,
    digitsLarge: 4,
    digitsSmall: 6,
    abbreviate: false,
  })
}

export function getDynamicPricingTiers(
  model: PricingModel
): DynamicPricingTier[] {
  if (!isDynamicPricingModel(model)) return []
  const { billingExpr } = splitBillingExprAndRequestRules(
    model.billing_expr || ''
  )
  if (isTaskUsagePricingModel(model)) {
    return parseTaskTiersFromExpr(billingExpr, model.billing_usage_schema, true)
  }
  return parseTiersFromExpr(billingExpr)
}

/**
 * Pick the tier a time-conditional model is charging at `now`.
 *
 * The chain is read in expression order and the first tier whose guard holds
 * wins, which is exactly what the billing engine does for one request. Tiers
 * are returned untouched when the chain is not clock-driven — a `len` or `p`
 * guard only becomes true or false for a specific request, so no wall clock
 * can decide it and the caller keeps advertising the first tier.
 *
 * Returns `null` when the clock cannot select a tier.
 */
export function getCurrentTimePricingTier(
  tiers: DynamicPricingTier[],
  now: Date
): DynamicPricingTier | null {
  if (tiers.length === 0) return null
  let sawTimeGuard = false
  for (const tier of tiers) {
    const conditions = (tier as ParsedTier).conditions
    if (!Array.isArray(conditions) || conditions.length === 0) {
      // The unconditional fallback only applies once every earlier guard has
      // been decided, so it is the answer only if a clock guard was read.
      return sawTimeGuard ? tier : null
    }
    if (!isTimeOnlyCondition(conditions)) return null
    sawTimeGuard = true
    const matches = conditions.every(
      (condition) => evaluateTierConditionAt(condition, now) === true
    )
    if (matches) return tier
  }
  return null
}

export function hasDynamicRequestRules(model: PricingModel): boolean {
  if (!isDynamicPricingModel(model)) return false
  const { requestRuleExpr } = splitBillingExprAndRequestRules(
    model.billing_expr || ''
  )
  return Boolean(tryParseRequestRuleExpr(requestRuleExpr || '')?.length)
}

export function getDynamicPriceEntries(
  tier: DynamicPricingTier | null,
  options: DynamicPriceOptions
): DynamicPriceEntry[] {
  if (!tier) return []

  if (isTaskPricingTier(tier) && options.usageSchema) {
    const usageEntries: DynamicPriceEntry[] = getTaskNumberFields(
      options.usageSchema
    ).flatMap(([field, definition]) => {
      const value = Number(tier.unitPrices[field])
      if (!Number.isFinite(value) || value < 0 || !definition.unit) return []
      return [
        {
          key: field,
          field,
          label: field,
          shortLabel: field,
          labelKind: 'schema',
          value,
          formatted: formatTaskUsageUnitPrice(value, options),
          unit: definition.unit,
          description: definition.description,
        } satisfies DynamicPriceEntry,
      ]
    })
    if (tier.constant > 0) {
      usageEntries.push({
        key: 'constant',
        field: 'constant',
        label: 'Additional charge',
        shortLabel: 'Additional charge',
        labelKind: 'i18n',
        value: tier.constant,
        formatted: formatTaskUsageUnitPrice(tier.constant, options),
        unit: 'request',
      })
    }
    return usageEntries
  }

  return BILLING_PRICING_VARS.flatMap((variable) => {
    if (!variable.field) return []
    const value = Number((tier as ParsedTier)[variable.field])
    if (!Number.isFinite(value) || value < 0) return []

    return [
      {
        key: variable.key,
        field: variable.field,
        label: variable.label,
        shortLabel: variable.shortLabel,
        labelKind: 'i18n' as const,
        value,
        formatted: formatDynamicUnitPrice(value, options),
        unit: 'token' as const,
        variable,
      },
    ]
  }).sort((a, b) => {
    const aPrimary = PRIMARY_DYNAMIC_FIELDS.has(a.field)
    const bPrimary = PRIMARY_DYNAMIC_FIELDS.has(b.field)
    if (aPrimary !== bPrimary) return aPrimary ? -1 : 1
    return 0
  })
}

export function getDynamicPricingSummary(
  model: PricingModel,
  options: DynamicPriceOptions
): DynamicPricingSummary | null {
  if (!isDynamicPricingModel(model)) return null

  const tiers = getDynamicPricingTiers(model)
  const isTaskUsage = isTaskUsagePricingModel(model)
  // A peak/off-peak model charges one of its tiers right now, so advertise the
  // tier the clock selects — the same branch the billing engine would take for
  // a request at this moment. Length-tiered and plain tiered models have no
  // clock guard, so `getCurrentTimePricingTier` returns null and the first tier
  // stays the reference price: the expression author puts the base tier there
  // (short-context rate, off-peak rate), and picking the unconditional fallback
  // instead would advertise long-context or peak rates as the base price.
  const timeTier = isTaskUsage
    ? null
    : getCurrentTimePricingTier(tiers, options.now ?? new Date())
  const tier = isTaskUsage
    ? (tiers.at(-1) ?? null)
    : (timeTier ?? tiers[0] ?? null)
  let entries = getDynamicPriceEntries(tier, {
    ...options,
    usageSchema: model.billing_usage_schema,
  })
  // The plaza shows one row per model, so a tiered model must advertise the
  // spread across its tiers instead of only the first (usually peak) tier.
  const priceRanges = new Map<string, { min: number; max: number }>()
  for (const entry of entries) {
    let min = Number.POSITIVE_INFINITY
    let max = Number.NEGATIVE_INFINITY
    for (const candidate of tiers) {
      if (isTaskUsage !== isTaskPricingTier(candidate)) continue
      const raw = isTaskUsage
        ? (candidate as ParsedTaskTier).unitPrices[entry.field]
        : (candidate as ParsedTier)[entry.field as string]
      const value = Number(raw)
      if (!Number.isFinite(value) || value < 0) continue
      min = Math.min(min, value)
      max = Math.max(max, value)
    }
    if (Number.isFinite(min) && Number.isFinite(max)) {
      priceRanges.set(entry.field, { min, max })
    }
  }
  entries = entries.map((entry) => {
    const range = priceRanges.get(entry.field)
    if (!range || range.min === range.max) return entry
    const format = isTaskUsage
      ? (value: number) => formatTaskUsageUnitPrice(value, options)
      : (value: number) => formatDynamicUnitPrice(value, options)
    return {
      ...entry,
      formattedRange: `${format(range.min)} – ${format(range.max)}`,
    }
  })
  const rawExpression = model.billing_expr || ''

  return {
    tiers,
    tier,
    tierCount: tiers.length,
    tierLabels: [...new Set(tiers.map((item) => item.label).filter(Boolean))],
    hasRequestRules: hasDynamicRequestRules(model),
    isSpecialExpression: rawExpression.trim().length > 0 && tiers.length === 0,
    rawExpression,
    entries,
    primaryEntries: isTaskUsage
      ? entries.filter((entry) => entry.unit !== 'request')
      : entries.filter((entry) => PRIMARY_DYNAMIC_FIELDS.has(entry.field)),
    secondaryEntries: isTaskUsage
      ? entries.filter((entry) => entry.unit === 'request')
      : entries.filter((entry) => !PRIMARY_DYNAMIC_FIELDS.has(entry.field)),
    isTaskUsage,
    isTimePricing: timeTier !== null,
  }
}

export function getCardExamplePrice(
  model: PricingModel,
  options: DynamicPriceOptions
): CardExamplePrice | null {
  if (!isTaskUsagePricingModel(model)) return null
  const schema = model.billing_usage_schema
  const firstExample = model.billing_usage_examples?.[0]
  if (!schema || !firstExample) return null

  const { billingExpr } = splitBillingExprAndRequestRules(
    model.billing_expr || ''
  )
  const config = tryParseTaskVisualConfig(billingExpr, schema)
  if (!config) return null

  const result = evaluateTaskVisualConfig(config, firstExample.facts, schema)
  if (!result) return null

  return {
    label: firstExample.label,
    formatted: formatTaskUsageUnitPrice(result.total, options),
  }
}
