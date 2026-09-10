import { describe, expect, test } from 'vitest'

import { isGptLongContextWarningModel } from './context-warning'

describe('GPT context warning model filter', () => {
  test('matches GPT text models regardless of case or surrounding whitespace', () => {
    expect(isGptLongContextWarningModel('gpt-5.4')).toBe(true)
    expect(isGptLongContextWarningModel(' GPT-5.6-SOL ')).toBe(true)
    expect(isGptLongContextWarningModel('gpt-4.1-mini')).toBe(true)
  })

  test('excludes non-GPT and GPT media models', () => {
    expect(isGptLongContextWarningModel('claude-4')).toBe(false)
    expect(isGptLongContextWarningModel('gpt-image-1')).toBe(false)
    expect(isGptLongContextWarningModel('gpt-5.4-image')).toBe(false)
    expect(isGptLongContextWarningModel('gpt-5.4-audio')).toBe(false)
    expect(isGptLongContextWarningModel('gpt-5.4-video')).toBe(false)
    expect(isGptLongContextWarningModel(undefined)).toBe(false)
  })
})