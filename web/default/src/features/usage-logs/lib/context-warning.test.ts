// @ts-expect-error Vitest is provided by bunx for this isolated test.
import { describe, expect, test } from 'vitest'

import { isGptLongContextWarningModel } from './context-warning'

describe('GPT context warning model filter', () => {
  test('only matches eligible GPT text models', () => {
    expect(isGptLongContextWarningModel('gpt-5.4')).toBe(true)
    expect(isGptLongContextWarningModel('gpt-5.5-pro')).toBe(true)
    expect(isGptLongContextWarningModel('gpt-5.6-sol')).toBe(true)
    expect(isGptLongContextWarningModel('gpt-4.1')).toBe(true)
    expect(isGptLongContextWarningModel('gpt-5.4-image')).toBe(false)
    expect(isGptLongContextWarningModel('gpt-5.4-audio')).toBe(false)
  })
})
