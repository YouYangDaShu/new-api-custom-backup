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
import {
  BILLING_CACHE_VAR_MAP,
  TIER_TIME_FUNCS,
  TIER_TIME_FUNC_RANGES,
  buildTierConditionExpr,
  findTernaryColon,
  findTopLevelCharacter,
  isTierConditionGroup,
  isTierTimeFunc,
  parseTierConditionExpr,
  type TierCondition,
  type TierConditionNode,
  type TierConditionVar,
  type TierTimeFunc,
  type TierTokenVar,
} from './billing-expr'

// Re-export the tier condition primitives from the module that already owns
// them, so the visual editor has a single import site.
export {
  TIER_TIME_FUNCS,
  TIER_TIME_FUNC_RANGES,
  isTierConditionGroup,
  isTierTimeFunc,
  type TierConditionNode,
  type TierConditionVar,
  type TierTimeFunc,
  type TierTokenVar,
}

export const CACHE_MODE_TIMED = 'timed'
export const CACHE_MODE_GENERIC = 'generic'
export type CacheMode = typeof CACHE_MODE_TIMED | typeof CACHE_MODE_GENERIC

export const DEFAULT_TIER_TIMEZONE = 'Asia/Shanghai'

export function isTierTimeValueInRange(
  timeFunc: TierTimeFunc,
  value: number | string
): boolean {
  const num = typeof value === 'number' ? value : Number(String(value).trim())
  if (!Number.isFinite(num) || !Number.isInteger(num)) return false
  const [min, max] = TIER_TIME_FUNC_RANGES[timeFunc]
  return num >= min && num <= max
}

export type TierConditionInput = {
  var: TierConditionVar
  op: '<' | '<=' | '>' | '>='
  /** Kept as a string while the user is typing; serialized as a number. */
  value: number | string
  /** IANA timezone; only meaningful when `var` is a time function. */
  timezone?: string
}

export type TierConditionGroupInput = {
  kind: 'all' | 'any'
  conditions: TierConditionInputNode[]
}

export type TierConditionInputNode =
  | TierConditionInput
  | TierConditionGroupInput

export function isTierConditionGroupInput(
  node: TierConditionInputNode
): node is TierConditionGroupInput {
  return 'kind' in node
}

/** Count comparison leaves in an in-progress editor tree. */
export function countTierConditionInputs(
  nodes: TierConditionInputNode[]
): number {
  return nodes.reduce(
    (total, node) =>
      total +
      (isTierConditionGroupInput(node)
        ? countTierConditionInputs(node.conditions)
        : 1),
    0
  )
}

/** Flatten a condition tree into its comparison leaves. */
export function flattenTierConditions(
  nodes: TierConditionInputNode[]
): TierConditionInput[] {
  return nodes.flatMap((node) =>
    isTierConditionGroupInput(node)
      ? flattenTierConditions(node.conditions)
      : [node]
  )
}

export type VisualTier = {
  label: string
  /**
   * Condition tree: comparisons plus `all`/`any` groups. A single tier can
   * therefore cover e.g. two peak windows without duplicating its prices.
   */
  conditions: TierConditionInputNode[]
  input_unit_cost: number
  output_unit_cost: number
  cache_mode: CacheMode
  cache_read_unit_cost?: number
  cache_create_unit_cost?: number
  cache_create_1h_unit_cost?: number
  image_unit_cost?: number
  image_output_unit_cost?: number
  audio_input_unit_cost?: number
  audio_output_unit_cost?: number
  [field: string]: unknown
}

export type VisualConfig = {
  tiers: VisualTier[]
}

export function getTierCacheMode(
  tier: Partial<VisualTier> | null | undefined
): CacheMode {
  if (tier?.cache_mode === CACHE_MODE_TIMED) return CACHE_MODE_TIMED
  if (tier?.cache_mode === CACHE_MODE_GENERIC) return CACHE_MODE_GENERIC
  return Number(tier?.cache_create_1h_unit_cost) > 0
    ? CACHE_MODE_TIMED
    : CACHE_MODE_GENERIC
}

export function normalizeVisualTier(
  tier: Partial<VisualTier> = {}
): VisualTier {
  return {
    label: tier.label ?? '',
    input_unit_cost: Number(tier.input_unit_cost) || 0,
    output_unit_cost: Number(tier.output_unit_cost) || 0,
    cache_mode: getTierCacheMode(tier),
    conditions: Array.isArray(tier.conditions) ? tier.conditions : [],
    ...tier,
    cache_read_unit_cost: Number(tier.cache_read_unit_cost) || 0,
    cache_create_unit_cost: Number(tier.cache_create_unit_cost) || 0,
    cache_create_1h_unit_cost: Number(tier.cache_create_1h_unit_cost) || 0,
    image_unit_cost: Number(tier.image_unit_cost) || 0,
    image_output_unit_cost: Number(tier.image_output_unit_cost) || 0,
    audio_input_unit_cost: Number(tier.audio_input_unit_cost) || 0,
    audio_output_unit_cost: Number(tier.audio_output_unit_cost) || 0,
  }
}

export function createDefaultVisualConfig(): VisualConfig {
  return {
    tiers: [
      normalizeVisualTier({
        conditions: [],
        input_unit_cost: 0,
        output_unit_cost: 0,
        label: 'base',
        cache_mode: CACHE_MODE_GENERIC,
      }),
    ],
  }
}

export function normalizeVisualConfig(
  config: VisualConfig | null | undefined
): VisualConfig {
  if (!config || !Array.isArray(config.tiers) || config.tiers.length === 0) {
    return createDefaultVisualConfig()
  }
  return {
    ...config,
    tiers: config.tiers.map((tier) => normalizeVisualTier(tier)),
  }
}

/** Build the left-hand operand for one tier condition. */
export function buildTierConditionOperand(
  cond: TierConditionInput
): string | null {
  if (isTierTimeFunc(cond.var)) {
    const tz = (cond.timezone || DEFAULT_TIER_TIMEZONE).trim()
    if (!tz) return null
    if (!isTierTimeValueInRange(cond.var, cond.value)) return null
    return `${cond.var}(${JSON.stringify(tz)})`
  }
  return cond.var
}

/** Convert one editor comparison into the engine's `TierCondition` shape. */
export function toTierCondition(
  input: TierConditionInput
): TierCondition | null {
  if (!input.var || !input.op || input.value == null || input.value === '') {
    return null
  }
  const operand = buildTierConditionOperand(input)
  if (!operand) return null
  const value = Number(input.value)
  if (!Number.isFinite(value)) return null
  return isTierTimeFunc(input.var)
    ? {
        var: input.var,
        op: input.op,
        value,
        timezone: (input.timezone || DEFAULT_TIER_TIMEZONE).trim(),
      }
    : { var: input.var, op: input.op, value }
}

function inputNodeToConditionNode(
  node: TierConditionInputNode
): TierConditionNode | null {
  if (isTierConditionGroupInput(node)) {
    const inner = node.conditions
      .map(inputNodeToConditionNode)
      .filter((child): child is TierConditionNode => child !== null)
    if (inner.length === 0) return null
    return { kind: node.kind, conditions: inner }
  }
  return toTierCondition(node)
}

/**
 * Build the condition string for a tier.
 *
 * Incomplete rows are dropped (a half-typed value must not break the whole
 * expression), and an empty result means "fallback tier". Groups nest to any
 * depth, so a single tier can hold e.g. "weekdays AND (09-12 OR 14-18)".
 */
export function buildConditionStr(
  conditions: TierConditionInputNode[]
): string {
  if (!conditions || conditions.length === 0) return ''
  const nodes = conditions
    .map(inputNodeToConditionNode)
    .filter((node): node is TierConditionNode => node !== null)
  return buildTierConditionExpr(nodes)
}

function buildTierBodyExpr(tier: VisualTier): string {
  const parts: string[] = []
  const ic = Number(tier.input_unit_cost) || 0
  const oc = Number(tier.output_unit_cost) || 0
  parts.push(`p * ${ic}`)
  parts.push(`c * ${oc}`)
  for (const cv of BILLING_CACHE_VAR_MAP) {
    const v = Number((tier as Record<string, unknown>)[cv.field]) || 0
    if (v !== 0) parts.push(`${cv.exprVar} * ${v}`)
  }
  return parts.join(' + ')
}

export function generateExprFromVisualConfig(
  config: VisualConfig | null | undefined
): string {
  if (!config || !config.tiers || config.tiers.length === 0) {
    return 'p * 0 + c * 0'
  }
  const tiers = config.tiers

  if (tiers.length === 1) {
    const tier = tiers[0]
    const label = tier.label || 'default'
    const body = `tier("${label}", ${buildTierBodyExpr(tier)})`
    const cond = buildConditionStr(tier.conditions)
    if (cond) {
      return `${cond} ? ${body} : p * 0 + c * 0`
    }
    return body
  }

  const parts: string[] = []
  for (let i = 0; i < tiers.length; i++) {
    const tier = tiers[i]
    const label = tier.label || `tier_${i + 1}`
    const body = `tier("${label}", ${buildTierBodyExpr(tier)})`
    const cond = buildConditionStr(tier.conditions)

    if (i < tiers.length - 1 && cond) {
      parts.push(`${cond} ? ${body}`)
    } else {
      parts.push(body)
    }
  }
  return parts.join(' : ')
}

/** Strip redundant outer parentheses around the whole expression. */
function unwrapVisualParens(expr: string): string {
  let current = expr
  for (let guard = 0; guard < 8; guard += 1) {
    if (!current.startsWith('(') || !current.endsWith(')')) return current
    let depth = 0
    let wrapsAll = true
    for (let i = 0; i < current.length; i += 1) {
      if (current[i] === '(') depth += 1
      else if (current[i] === ')') depth -= 1
      if (depth === 0 && i < current.length - 1) {
        wrapsAll = false
        break
      }
    }
    if (!wrapsAll || depth !== 0) return current
    current = current.slice(1, -1).trim()
  }
  return current
}

function tierConditionToInput(node: TierConditionNode): TierConditionInputNode {
  if (isTierConditionGroup(node)) {
    return {
      kind: node.kind,
      conditions: node.conditions.map(tierConditionToInput),
    }
  }
  return {
    var: node.var,
    op: node.op,
    value: node.value,
    ...(node.timezone ? { timezone: node.timezone } : {}),
  }
}

export function tryParseVisualConfig(
  exprStr: string | null | undefined
): VisualConfig | null {
  if (!exprStr) return null
  try {
    let body = exprStr.trim()
    const versionMatch = body.match(/^v\d+:([\s\S]*)$/)
    if (versionMatch) body = versionMatch[1].trim()
    body = unwrapVisualParens(body)

    const cacheVarNames = BILLING_CACHE_VAR_MAP.map((cv) => cv.exprVar)
    const optCacheStr = cacheVarNames
      .map((v) => `(?:\\s*\\+\\s*${v}\\s*\\*\\s*([\\d.eE+-]+))?`)
      .join('')
    const bodyPat = `p\\s*\\*\\s*([\\d.eE+-]+)\\s*\\+\\s*c\\s*\\*\\s*([\\d.eE+-]+)${optCacheStr}`
    const singleRe = new RegExp(`^tier\\("([^"]*)",\\s*${bodyPat}\\)$`)

    const tiers: VisualTier[] = []
    let rest = body
    for (let guard = 0; guard <= 32; guard += 1) {
      const simple = rest.match(singleRe)
      if (simple) {
        const tier: Record<string, unknown> = {
          conditions: [],
          input_unit_cost: Number(simple[2]),
          output_unit_cost: Number(simple[3]),
          label: simple[1],
        }
        BILLING_CACHE_VAR_MAP.forEach((cv, i) => {
          const val = simple[4 + i]
          if (val != null) tier[cv.field] = Number(val)
        })
        tiers.push(normalizeVisualTier(tier as Partial<VisualTier>))
        rest = ''
        break
      }

      const question = findTopLevelCharacter(rest, '?')
      if (question < 0) return null
      const colon = findTernaryColon(rest, question)
      if (colon < 0) return null
      const branch = rest.slice(question + 1, colon).trim()
      const parsedTier = branch.match(singleRe)
      if (!parsedTier) return null
      const nodes = parseTierConditionExpr(rest.slice(0, question))
      if (!nodes || nodes.length === 0) return null
      const tier: Record<string, unknown> = {
        conditions: nodes.map(tierConditionToInput),
        input_unit_cost: Number(parsedTier[2]),
        output_unit_cost: Number(parsedTier[3]),
        label: parsedTier[1],
      }
      BILLING_CACHE_VAR_MAP.forEach((cv, i) => {
        const val = parsedTier[4 + i]
        if (val != null) tier[cv.field] = Number(val)
      })
      tiers.push(normalizeVisualTier(tier as Partial<VisualTier>))
      rest = rest.slice(colon + 1).trim()
    }

    if (tiers.length === 0 || rest !== '') return null

    const cfg = normalizeVisualConfig({ tiers })
    const regenerated = generateExprFromVisualConfig(cfg)
    if (regenerated.replaceAll(/\s+/g, '') !== body.replaceAll(/\s+/g, '')) {
      return null
    }
    return cfg
  } catch {
    return null
  }
}

// ---------------------------------------------------------------------------
// Local cost evaluator (for the estimator preview)
// ---------------------------------------------------------------------------

/**
 * Read the clock fields of "now" in an IANA timezone for the local estimator.
 * Mirrors the backend `timeInZone`: an unknown/empty timezone falls back to
 * UTC instead of throwing, so the preview never diverges from settlement by
 * silently erroring out.
 */
function tierTimeInZone(tz: string): {
  hour: number
  minute: number
  weekday: number
  month: number
  day: number
} {
  const trimmed = (tz || '').trim()
  const now = new Date()
  let parts: Intl.DateTimeFormatPart[]
  try {
    parts = new Intl.DateTimeFormat('en-US', {
      timeZone: trimmed || 'UTC',
      hourCycle: 'h23',
      year: 'numeric',
      month: 'numeric',
      day: 'numeric',
      hour: 'numeric',
      minute: 'numeric',
      weekday: 'short',
    }).formatToParts(now)
  } catch {
    return tierTimeInZone('UTC')
  }
  const pick = (type: Intl.DateTimeFormatPartTypes) =>
    parts.find((part) => part.type === type)?.value ?? ''
  const weekdayNames = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
  return {
    hour: Number(pick('hour')),
    minute: Number(pick('minute')),
    weekday: Math.max(0, weekdayNames.indexOf(pick('weekday'))),
    month: Number(pick('month')),
    day: Number(pick('day')),
  }
}

const ESTIMATOR_VARS = [
  { var: 'cr', stateKey: 'cacheReadTokens' },
  { var: 'cc', stateKey: 'cacheCreateTokens' },
  { var: 'cc1h', stateKey: 'cacheCreate1hTokens' },
  { var: 'img', stateKey: 'imageTokens' },
  { var: 'img_o', stateKey: 'imageOutputTokens' },
  { var: 'ai', stateKey: 'audioInputTokens' },
  { var: 'ao', stateKey: 'audioOutputTokens' },
] as const

export type ExtraTokenValues = Record<
  (typeof ESTIMATOR_VARS)[number]['stateKey'],
  number
>

export type EvalResult = {
  cost: number
  matchedTier: string
  error: string | null
}

export function evalExprLocally(
  exprStr: string,
  promptTokens: number,
  completionTokens: number,
  extraTokenValues: ExtraTokenValues
): EvalResult {
  try {
    if (!exprStr || !exprStr.trim()) {
      return { cost: 0, matchedTier: '', error: null }
    }
    let matchedTier = ''
    const tierFn = (name: string, value: number) => {
      matchedTier = name
      return value
    }
    const cacheReadTokens = extraTokenValues.cacheReadTokens || 0
    const cacheCreateTokens = extraTokenValues.cacheCreateTokens || 0
    const cacheCreate1hTokens = extraTokenValues.cacheCreate1hTokens || 0
    const len =
      promptTokens + cacheReadTokens + cacheCreateTokens + cacheCreate1hTokens
    const env: Record<string, unknown> = {
      p: promptTokens,
      c: completionTokens,
      len,
      tier: tierFn,
      max: Math.max,
      min: Math.min,
      abs: Math.abs,
      ceil: Math.ceil,
      floor: Math.floor,
      hour: (tz: string) => tierTimeInZone(tz).hour,
      minute: (tz: string) => tierTimeInZone(tz).minute,
      weekday: (tz: string) => tierTimeInZone(tz).weekday,
      month: (tz: string) => tierTimeInZone(tz).month,
      day: (tz: string) => tierTimeInZone(tz).day,
    }
    for (const field of ESTIMATOR_VARS) {
      env[field.var] = extraTokenValues[field.stateKey] || 0
    }
    const fn = new Function(
      ...Object.keys(env),
      `"use strict"; return (${exprStr});`
    )
    const cost = Number(fn(...Object.values(env))) || 0
    return { cost, matchedTier, error: null }
  } catch (e) {
    const message = e instanceof Error ? e.message : String(e)
    return { cost: 0, matchedTier: '', error: message }
  }
}

export function exprUsesExtraVars(exprStr: string): boolean {
  if (!exprStr) return false
  const varNames = ESTIMATOR_VARS.map((f) => f.var).join('|')
  return new RegExp(`\\b(${varNames})\\b`).test(exprStr)
}

export const ESTIMATOR_EXTRA_FIELDS = ESTIMATOR_VARS
