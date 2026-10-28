// One-off check: does the clock-selected base price work with the expression
// shape that production actually stores (peak branch first)?
import { describe, expect, test } from 'vitest'
import { getDynamicPricingSummary } from '../dynamic-price'

// Copied verbatim from production option billing_setting.billing_expr.
const PROD_DEEPSEEK_V41 =
  'weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") <= 5 && ' +
  '((hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12) || ' +
  '(hour("Asia/Shanghai") >= 14 && hour("Asia/Shanghai") < 18)) ' +
  '? tier("峰时", p * 2 + c * 8 + cr * 0.04 + cc1h * 0.034) ' +
  ': tier("闲时", p * 1 + c * 4 + cr * 0.02 + cc1h * 0.017)'

const PROD_GPT_6_ASTRA =
  'len<272000 ? tier("base", p*10 + c*50 + cr*1 + cc*12.5) ' +
  ': tier("tier_2", p*20 + c*75 + cr*2 + cc*25)'

function modelOf(name: string, expr: string) {
  return {
    id: 1,
    model_name: name,
    quota_type: 0,
    model_ratio: 1,
    completion_ratio: 1,
    model_price: 0,
    billing_mode: 'tiered_expr',
    billing_expr: expr,
    enable_groups: ['default'],
  } as never
}

describe('production expression shapes', () => {
  test('deepseek peak-first expression advertises off-peak on a Wednesday night', () => {
    // 2026-10-27 is a Tuesday; 19:00Z = 03:00 Wednesday Shanghai -> off-peak.
    const summary = getDynamicPricingSummary(
      modelOf('deepseek-v4.1-flash', PROD_DEEPSEEK_V41),
      { tokenUnit: 'M', now: new Date('2026-10-27T19:00:00Z') }
    )
    expect(summary?.tier?.label).toBe('闲时')
    expect(summary?.isTimePricing).toBe(true)
    expect(summary?.primaryEntries.map((e) => e.value)).toEqual([1, 4])
  })

  test('deepseek peak-first expression advertises peak inside the window', () => {
    // 02:00Z = 10:00 Shanghai on a Wednesday -> inside 9-12 peak window.
    const summary = getDynamicPricingSummary(
      modelOf('deepseek-v4.1-flash', PROD_DEEPSEEK_V41),
      { tokenUnit: 'M', now: new Date('2026-10-28T02:00:00Z') }
    )
    expect(summary?.tier?.label).toBe('峰时')
    expect(summary?.primaryEntries.map((e) => e.value)).toEqual([2, 8])
  })

  test('deepseek on a Saturday midday is off-peak despite the hour', () => {
    // 2026-10-31 is a Saturday; 02:00Z = 10:00 Shanghai.
    const summary = getDynamicPricingSummary(
      modelOf('deepseek-v4.1-flash', PROD_DEEPSEEK_V41),
      { tokenUnit: 'M', now: new Date('2026-10-31T02:00:00Z') }
    )
    expect(summary?.tier?.label).toBe('闲时')
  })

  test('length-tiered model is untouched by the clock', () => {
    const summary = getDynamicPricingSummary(
      modelOf('gpt-6-astra', PROD_GPT_6_ASTRA),
      { tokenUnit: 'M', now: new Date('2026-10-28T02:00:00Z') }
    )
    expect(summary?.isTimePricing).toBe(false)
    expect(summary?.tier?.label).toBe('base')
    expect(summary?.primaryEntries.map((e) => e.value)).toEqual([10, 50])
  })
})
