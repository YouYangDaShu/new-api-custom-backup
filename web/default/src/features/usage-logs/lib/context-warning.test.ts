import assert from 'node:assert/strict'
import test from 'node:test'

import { isGptLongContextWarningModel } from './context-warning.ts'

test('long-context warning is restricted to GPT-5.4/5.5 text models', () => {
  assert.equal(isGptLongContextWarningModel('gpt-5.4'), true)
  assert.equal(isGptLongContextWarningModel('GPT-5.5-pro'), true)
  assert.equal(isGptLongContextWarningModel('gpt-4'), false)
  assert.equal(isGptLongContextWarningModel('claude-3.5-sonnet'), false)
  assert.equal(isGptLongContextWarningModel('gpt-5.4-image'), false)
})
