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
import { describe, expect, test } from 'vitest'

import {
  parseTiersFromExpr,
  splitBillingExprAndRequestRules,
} from '../billing-expr'
import {
  DEFAULT_TIER_TIMEZONE,
  evalExprLocally,
  generateExprFromVisualConfig,
  normalizeVisualTier,
  tryParseVisualConfig,
  type ExtraTokenValues,
  type VisualTier,
} from '../tier-expr'

const EMPTY_EXTRA: ExtraTokenValues = {
  cacheReadTokens: 0,
  cacheCreateTokens: 0,
  cacheCreate1hTokens: 0,
  imageTokens: 0,
  imageOutputTokens: 0,
  audioInputTokens: 0,
  audioOutputTokens: 0,
}

function tier(overrides: Partial<VisualTier> = {}): VisualTier {
  return normalizeVisualTier({
    label: 'peak',
    input_unit_cost: 2,
    output_unit_cost: 8,
    ...overrides,
  })
}

describe('tier conditions with clock fields', () => {
  test('builds weekday and hour bounds as a single AND chain', () => {
    const expr = generateExprFromVisualConfig({
      tiers: [
        tier({
          conditions: [
            {
              var: 'weekday',
              op: '>=',
              value: 1,
              timezone: 'Asia/Shanghai',
            },
            {
              var: 'weekday',
              op: '<=',
              value: 5,
              timezone: 'Asia/Shanghai',
            },
            { var: 'hour', op: '>=', value: 9, timezone: 'Asia/Shanghai' },
            { var: 'hour', op: '<', value: 12, timezone: 'Asia/Shanghai' },
          ],
        }),
        tier({ label: 'offpeak', input_unit_cost: 1, output_unit_cost: 4 }),
      ],
    })

    expect(expr).toBe(
      'weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") <= 5 && ' +
        'hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12 ? ' +
        'tier("peak", p * 2 + c * 8) : tier("offpeak", p * 1 + c * 4)'
    )
  })

  test('defaults the timezone when the condition omits one', () => {
    const expr = generateExprFromVisualConfig({
      tiers: [
        tier({ conditions: [{ var: 'hour', op: '>=', value: 9 }] }),
        tier({ label: 'offpeak' }),
      ],
    })
    expect(expr).toContain(`hour("${DEFAULT_TIER_TIMEZONE}") >= 9`)
  })

  test.each([
    ['hour above domain', { var: 'hour', op: '>=', value: 24 }],
    ['hour below domain', { var: 'hour', op: '<', value: -1 }],
    ['weekday above domain', { var: 'weekday', op: '>=', value: 7 }],
    ['month below domain', { var: 'month', op: '>=', value: 0 }],
    ['non-integer minute', { var: 'minute', op: '>=', value: 30.5 }],
  ])(
    'drops out-of-domain %s instead of emitting a tautology',
    (_name, cond) => {
      const expr = generateExprFromVisualConfig({
        tiers: [
          tier({ conditions: [cond as never] }),
          tier({ label: 'offpeak' }),
        ],
      })
      // The condition is dropped, so the first tier becomes the unconditional
      // chain head: no ternary is emitted at all.
      expect(expr).not.toContain('?')
    }
  )

  test('round-trips a time-conditioned tier expression through the parser', () => {
    const expr =
      'weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") <= 5 && ' +
      'hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12 ? ' +
      'tier("peak", p * 2 + c * 8 + cr * 0.04) : ' +
      'tier("offpeak", p * 1 + c * 4 + cr * 0.02)'

    const config = tryParseVisualConfig(expr)
    expect(config).not.toBeNull()
    if (!config) throw new Error('expected a parsed visual config')
    const peak = config.tiers[0]
    expect(peak.label).toBe('peak')
    expect(peak.conditions).toEqual([
      { var: 'weekday', op: '>=', value: 1, timezone: 'Asia/Shanghai' },
      { var: 'weekday', op: '<=', value: 5, timezone: 'Asia/Shanghai' },
      { var: 'hour', op: '>=', value: 9, timezone: 'Asia/Shanghai' },
      { var: 'hour', op: '<', value: 12, timezone: 'Asia/Shanghai' },
    ])
    // cache coefficients must survive the round trip too
    expect(peak.cache_read_unit_cost).toBeCloseTo(0.04, 10)
    expect(peak.input_unit_cost).toBe(2)
    expect(peak.output_unit_cost).toBe(8)
    expect(config.tiers[1].conditions).toEqual([])

    // regenerate must be byte-identical (whitespace-insensitive), otherwise
    // the editor silently rewrites the admin's expression on open.
    const strip = (value: string) => value.replaceAll(/\s+/g, '')
    expect(strip(generateExprFromVisualConfig(config))).toBe(strip(expr))
  })

  test('still parses plain token-only conditions unchanged', () => {
    const expr =
      'len < 200000 ? tier("short", p * 1 + c * 2) : tier("long", p * 3 + c * 4)'
    const config = tryParseVisualConfig(expr)
    expect(config).not.toBeNull()
    if (!config) throw new Error('expected a parsed visual config')
    expect(config.tiers[0].conditions).toEqual([
      { var: 'len', op: '<', value: 200000 },
    ])
    expect(config.tiers[0].conditions[0].timezone).toBeUndefined()
  })

  test('lemonGir-style expression parses into tiers instead of raw mode', () => {
    // The competitor's documented expression uses || inside the hour group,
    // which the tier parser cannot summarize; it must fall back cleanly
    // rather than mis-report prices.
    const their =
      'weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") <= 5 && ' +
      '((hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12) || ' +
      '(hour("Asia/Shanghai") >= 14 && hour("Asia/Shanghai") < 18)) ? ' +
      'tier("峰时", p * 2 + cr * 0.04 + c * 8) : tier("闲时", p * 1 + cr * 0.02 + c * 4)'
    expect(tryParseVisualConfig(their)).toBeNull()
    expect(parseTiersFromExpr(their)).toEqual([])
  })

  test('parseTiersFromExpr summarizes a flat time-conditioned chain', () => {
    const expr =
      'weekday("Asia/Shanghai") >= 1 && hour("Asia/Shanghai") >= 9 ? ' +
      'tier("peak", p * 2 + c * 8) : tier("offpeak", p * 1 + c * 4)'
    const tiers = parseTiersFromExpr(expr)
    expect(tiers).toHaveLength(2)
    expect(tiers[0].conditions).toEqual([
      { var: 'weekday', op: '>=', value: 1, timezone: 'Asia/Shanghai' },
      { var: 'hour', op: '>=', value: 9, timezone: 'Asia/Shanghai' },
    ])
    expect(tiers[1].conditions).toEqual([])
  })

  test('parseTiersFromExpr rejects out-of-domain clock bounds', () => {
    const expr =
      'hour("Asia/Shanghai") >= 25 ? tier("peak", p * 2 + c * 8) : tier("offpeak", p * 1 + c * 4)'
    expect(parseTiersFromExpr(expr)).toEqual([])
  })

  test('split keeps request rules separate from tier conditions', () => {
    const expr =
      'weekday("Asia/Shanghai") >= 1 ? tier("peak", p * 2 + c * 8) : tier("offpeak", p * 1 + c * 4)'
    const split = splitBillingExprAndRequestRules(
      `${expr} * (header("x-priority") != "" ? 1.5 : 1)`
    )
    expect(split.billingExpr).toBe(expr)
    expect(split.requestRuleExpr).toContain('header')
  })
})

describe('local estimator with clock fields', () => {
  test('evaluates hour()/weekday() instead of throwing ReferenceError', () => {
    const expr =
      'hour("Asia/Shanghai") >= 0 && hour("Asia/Shanghai") <= 23 ? ' +
      'tier("always", p * 2 + c * 8) : tier("never", p * 0 + c * 0)'
    const result = evalExprLocally(expr, 1_000_000, 1_000_000, EMPTY_EXTRA)
    expect(result.error).toBeNull()
    expect(result.matchedTier).toBe('always')
    // The estimator returns the raw expression value (unit price × token
    // count), not currency: 1M × 2 + 1M × 8.
    expect(result.cost).toBeCloseTo(10_000_000, 6)
  })

  test('falls back to UTC for an unknown timezone instead of erroring', () => {
    const expr =
      'month("Not/AZone") >= 1 ? tier("ok", p * 1 + c * 0) : tier("no", p * 0 + c * 0)'
    const result = evalExprLocally(expr, 1_000_000, 0, EMPTY_EXTRA)
    expect(result.error).toBeNull()
    expect(result.matchedTier).toBe('ok')
  })

  test('weekday bounds select between two branches deterministically', () => {
    const always =
      'weekday("Asia/Shanghai") >= 0 && weekday("Asia/Shanghai") <= 6 ? ' +
      'tier("week", p * 1 + c * 0) : tier("none", p * 0 + c * 0)'
    const result = evalExprLocally(always, 1_000_000, 0, EMPTY_EXTRA)
    expect(result.matchedTier).toBe('week')
    expect(result.cost).toBeCloseTo(1_000_000, 6)
  })
})
