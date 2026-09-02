/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.
*/
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  encodeTokenGroupValue,
  parseTokenGroupValue,
} from './token-group-encoding.ts'

describe('smart token group encoding', () => {
  test('round-trips success-rate routing through the persisted token group', () => {
    const encoded = encodeTokenGroupValue({
      mode: 'smart',
      groups: [],
      smartMode: 'success_rate',
    })

    assert.equal(encoded, '@smart:success_rate')
    assert.deepEqual(parseTokenGroupValue(encoded), {
      mode: 'smart',
      groups: [],
      raw: '@smart:success_rate',
      smartMode: 'success_rate',
    })
  })

  test('normalizes success aliases when reading existing values', () => {
    assert.equal(
      parseTokenGroupValue('@smart:reliability').smartMode,
      'success_rate'
    )
  })
})
