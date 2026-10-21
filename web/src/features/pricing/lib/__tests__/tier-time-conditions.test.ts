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
  buildTierConditionExpr,
  countTierConditions,
  isTierConditionGroup,
  parseTierConditionExpr,
  parseTiersFromExpr,
  type TierCondition,
  type TierConditionGroup,
} from '../billing-expr'
import { getDynamicPricingSummary } from '../dynamic-price'
import {
  buildConditionStr,
  evalExprLocally,
  generateExprFromVisualConfig,
  tryParseVisualConfig,
  type TierConditionInputNode,
} from '../tier-expr'

const strip = (value: string) => value.replaceAll(/\s+/g, '')

const PEAK_TWO_WINDOWS =
  'weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") <= 5 && ' +
  '((hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12) || ' +
  '(hour("Asia/Shanghai") >= 14 && hour("Asia/Shanghai") < 18)) ? ' +
  'tier("峰时", p * 2 + c * 8 + cr * 0.04) : tier("闲时", p * 1 + c * 4 + cr * 0.02)'

const FLAT_PEAK =
  'weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") <= 5 && ' +
  'hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12 ? ' +
  'tier("峰时", p * 2 + c * 8 + cr * 0.04) : tier("闲时", p * 1 + c * 4 + cr * 0.02)'

function leaves(nodes: readonly unknown[]): TierCondition[] {
  return nodes.flatMap((node) => {
    if (isTierConditionGroup(node as never)) {
      return leaves((node as TierConditionGroup).conditions)
    }
    return [node as TierCondition]
  })
}

describe('tier condition parser', () => {
  test('parses a flat && chain into leaves', () => {
    const nodes = parseTierConditionExpr(
      'len < 200000 && p > 10 && hour("Asia/Shanghai") >= 9'
    )
    expect(nodes).not.toBeNull()
    expect(nodes).toHaveLength(3)
    expect(nodes?.every((node) => !isTierConditionGroup(node))).toBe(true)
  })

  test('parses a nested OR group and keeps the structure', () => {
    const nodes = parseTierConditionExpr(
      'weekday("Asia/Shanghai") >= 1 && ((hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12) || (hour("Asia/Shanghai") >= 14 && hour("Asia/Shanghai") < 18))'
    )
    expect(nodes).not.toBeNull()
    if (!nodes) throw new Error('expected nodes')
    expect(nodes).toHaveLength(2)
    const group = nodes[1]
    expect(isTierConditionGroup(group)).toBe(true)
    if (!isTierConditionGroup(group)) throw new Error('expected a group')
    expect(group.kind).toBe('any')
    expect(group.conditions).toHaveLength(2)
    expect(countTierConditions(nodes)).toBe(5)
  })

  test('rejects conditions it cannot represent instead of guessing', () => {
    expect(parseTierConditionExpr('p > 10 && foo > 3')).toBeNull()
    expect(parseTierConditionExpr('hour("Asia/Shanghai") > 24')).toBeNull()
    expect(parseTierConditionExpr('(p > 10')).toBeNull()
  })

  test('round-trips through buildTierConditionExpr', () => {
    const nodes = parseTierConditionExpr(PEAK_TWO_WINDOWS.split(' ? ')[0])
    expect(nodes).not.toBeNull()
    if (!nodes) throw new Error('expected nodes')
    const rebuilt = parseTierConditionExpr(buildTierConditionExpr(nodes))
    expect(rebuilt).toEqual(nodes)
  })
})

describe('visual config round trip', () => {
  test('flat clock conditions survive parse + regenerate', () => {
    const config = tryParseVisualConfig(FLAT_PEAK)
    expect(config).not.toBeNull()
    if (!config) throw new Error('expected a parsed visual config')
    expect(config.tiers).toHaveLength(2)
    const peakLeaves = leaves(config.tiers[0].conditions)
    expect(peakLeaves).toEqual([
      { var: 'weekday', op: '>=', value: 1, timezone: 'Asia/Shanghai' },
      { var: 'weekday', op: '<=', value: 5, timezone: 'Asia/Shanghai' },
      { var: 'hour', op: '>=', value: 9, timezone: 'Asia/Shanghai' },
      { var: 'hour', op: '<', value: 12, timezone: 'Asia/Shanghai' },
    ])
    expect(peakLeaves[0].value).toBe(1)
    expect(config.tiers[0].input_unit_cost).toBe(2)
    expect(config.tiers[0].output_unit_cost).toBe(8)
    expect(config.tiers[0].cache_read_unit_cost).toBeCloseTo(0.04, 10)
    expect(config.tiers[1].conditions).toEqual([])
    // Regenerate must be byte-identical (whitespace-insensitive), otherwise
    // the editor silently rewrites the admin's expression on open.
    expect(strip(generateExprFromVisualConfig(config))).toBe(strip(FLAT_PEAK))
  })

  test('nested two-window peak expression is editable, not raw mode', () => {
    const config = tryParseVisualConfig(PEAK_TWO_WINDOWS)
    expect(config).not.toBeNull()
    if (!config) throw new Error('expected a parsed visual config')
    expect(config.tiers).toHaveLength(2)
    const nodes = config.tiers[0].conditions
    // weekday >= 1, weekday <= 5, and one any-group holding the two windows.
    expect(nodes).toHaveLength(3)
    expect(isTierConditionGroup(nodes[2] as never)).toBe(true)
    expect(countTierConditions(nodes as never)).toBe(6)
    expect(leaves(nodes)).toHaveLength(6)
    expect(strip(generateExprFromVisualConfig(config))).toBe(
      strip(PEAK_TWO_WINDOWS)
    )
  })

  test('parses plain token-only conditions unchanged', () => {
    const expr =
      'len < 200000 ? tier("short", p * 1 + c * 2) : tier("long", p * 3 + c * 4)'
    const config = tryParseVisualConfig(expr)
    expect(config).not.toBeNull()
    if (!config) throw new Error('expected a parsed visual config')
    expect(config.tiers[0].conditions).toEqual([
      { var: 'len', op: '<', value: 200000 },
    ])
    const [first] = config.tiers[0].conditions
    expect(isTierConditionGroup(first as never)).toBe(false)
    expect('timezone' in first).toBe(false)
  })

  test('falls back to raw mode for expressions it cannot summarize', () => {
    // `max()` and unknown helpers are outside the tier grammar.
    expect(tryParseVisualConfig('max(p * 1, 5)')).toBeNull()
    expect(
      tryParseVisualConfig(
        'weekday("Asia/Shanghai") == 1 ? tier("a", p * 1 + c * 2) : tier("b", p * 2 + c * 4)'
      )
    ).toBeNull()
  })
})

describe('parseTiersFromExpr', () => {
  test('summarizes the nested peak/off-peak chain', () => {
    const tiers = parseTiersFromExpr(PEAK_TWO_WINDOWS)
    expect(tiers).toHaveLength(2)
    expect(tiers[0].label).toBe('峰时')
    expect(tiers[0].inputPrice).toBe(2)
    expect(tiers[0].outputPrice).toBe(8)
    expect(leaves(tiers[0].conditions)).toHaveLength(6)
    expect(tiers[1].label).toBe('闲时')
    expect(tiers[1].conditions).toEqual([])
  })

  test('summarizes a flat time-conditioned chain', () => {
    const tiers = parseTiersFromExpr(FLAT_PEAK)
    expect(tiers).toHaveLength(2)
    expect(leaves(tiers[0].conditions)).toHaveLength(4)
  })

  test('rejects chains that do not end in a fallback tier', () => {
    expect(parseTiersFromExpr('p > 10 ? tier("a", p * 1 + c * 2)')).toEqual([])
  })
})

describe('buildConditionStr', () => {
  test('serializes an any-group with parentheses', () => {
    const nodes: TierConditionInputNode[] = [
      { var: 'weekday', op: '>=', value: 1, timezone: 'Asia/Shanghai' },
      {
        kind: 'any',
        conditions: [
          { var: 'hour', op: '>=', value: 9, timezone: 'Asia/Shanghai' },
          { var: 'hour', op: '<', value: 12, timezone: 'Asia/Shanghai' },
        ],
      },
    ]
    expect(buildConditionStr(nodes)).toBe(
      'weekday("Asia/Shanghai") >= 1 && (hour("Asia/Shanghai") >= 9 || hour("Asia/Shanghai") < 12)'
    )
  })

  test('drops half-typed rows instead of emitting a broken expression', () => {
    expect(
      buildConditionStr([
        { var: 'hour', op: '>=', value: '', timezone: 'Asia/Shanghai' },
      ])
    ).toBe('')
    expect(buildConditionStr([])).toBe('')
  })
})

describe('evalExprLocally with clock functions', () => {
  const extras = {
    cacheReadTokens: 0,
    cacheCreateTokens: 0,
    cacheCreate1hTokens: 0,
    imageTokens: 0,
    imageOutputTokens: 0,
    audioInputTokens: 0,
    audioOutputTokens: 0,
  }

  test('nested OR group evaluates without throwing', () => {
    const result = evalExprLocally(
      PEAK_TWO_WINDOWS,
      1_000_000,
      1_000_000,
      extras
    )
    expect(result.error).toBeNull()
    // Peak doubles the off-peak price, so the cost is either 5e6 (off-peak)
    // or 1e7 (peak) depending on when the test runs.
    expect([5_000_000, 10_000_000]).toContain(result.cost)
    expect(['峰时', '闲时']).toContain(result.matchedTier)
  })

  test('flat clock chain evaluates', () => {
    const result = evalExprLocally(FLAT_PEAK, 1_000_000, 0, extras)
    expect(result.error).toBeNull()
    expect([1_000_000, 2_000_000]).toContain(result.cost)
  })
})

describe('buildConditionStr nested shapes', () => {
  test('an any group of two ranges keeps both bounds ANDed inside', () => {
    const nodes: TierConditionInputNode[] = [
      { var: 'weekday', op: '>=', value: 1, timezone: 'Asia/Shanghai' },
      { var: 'weekday', op: '<=', value: 5, timezone: 'Asia/Shanghai' },
      {
        kind: 'any',
        conditions: [
          {
            kind: 'all',
            conditions: [
              { var: 'hour', op: '>=', value: 9, timezone: 'Asia/Shanghai' },
              { var: 'hour', op: '<', value: 12, timezone: 'Asia/Shanghai' },
            ],
          },
          {
            kind: 'all',
            conditions: [
              { var: 'hour', op: '>=', value: 14, timezone: 'Asia/Shanghai' },
              { var: 'hour', op: '<', value: 18, timezone: 'Asia/Shanghai' },
            ],
          },
        ],
      },
    ]
    expect(buildConditionStr(nodes)).toBe(
      'weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") <= 5 && ' +
        '((hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12) || ' +
        '(hour("Asia/Shanghai") >= 14 && hour("Asia/Shanghai") < 18))'
    )
  })

  test('a bare any group of two bounds is the tautology the UI must avoid', () => {
    // Guards against regressing to `hour >= 9 || hour < 12`: that shape is
    // always true, so the peak tier would bill every request.
    const tautology: TierConditionInputNode[] = [
      {
        kind: 'any',
        conditions: [
          { var: 'hour', op: '>=', value: 9, timezone: 'Asia/Shanghai' },
          { var: 'hour', op: '<', value: 12, timezone: 'Asia/Shanghai' },
        ],
      },
    ]
    expect(buildConditionStr(tautology)).toBe(
      '(hour("Asia/Shanghai") >= 9 || hour("Asia/Shanghai") < 12)'
    )
  })
})

describe('base price comes from the fallback tier', () => {
  // Peak/off-peak models must advertise the off-peak rate as the base price.
  // Picking the first tier would advertise the peak rate and make the model
  // look like it always charges double.
  const model = {
    id: 1,
    model_name: 'deepseek-v4.1-flash',
    quota_type: 0,
    model_ratio: 37.5,
    completion_ratio: 1,
    model_price: 0,
    billing_mode: 'tiered_expr',
    billing_expr:
      'weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") <= 5 && ' +
      '((hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12) || ' +
      '(hour("Asia/Shanghai") >= 14 && hour("Asia/Shanghai") < 18)) ' +
      '? tier("峰时", p * 2 + c * 8 + cr * 0.04) ' +
      ': tier("闲时", p * 1 + c * 4 + cr * 0.02)',
    enable_groups: ['default'],
  } as never

  test('summary tier is the unconditional fallback, not the first tier', () => {
    const summary = getDynamicPricingSummary(model, { tokenUnit: 'M' })
    expect(summary?.tierCount).toBe(2)
    expect(summary?.tier?.label).toBe('闲时')
    expect(summary?.primaryEntries.map((entry) => entry.value)).toEqual([1, 4])
  })

  test('still reports the full spread across tiers', () => {
    const summary = getDynamicPricingSummary(model, { tokenUnit: 'M' })
    expect(summary?.primaryEntries[0]?.formattedRange).toBe('$1 – $2')
    expect(summary?.tierLabels).toEqual(['峰时', '闲时'])
  })
})
