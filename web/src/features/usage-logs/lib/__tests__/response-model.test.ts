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
import { describe, expect, it } from 'vitest'

import { isResponseModelMismatch } from '../response-model'

describe('isResponseModelMismatch', () => {
  it.each([
    ['A', 'B', true],
    ['B', 'B', false],
    ['B', 'A', true],
    ['GPT-4O', 'gpt-4o', false],
  ])('compares only final sent %s with response %s', (sent, response, want) => {
    expect(
      isResponseModelMismatch({
        requested_model: 'A',
        upstream_model: sent,
        returned_model: response,
      })
    ).toBe(want)
  })

  it('does not warn when the upstream omitted the response model', () => {
    expect(
      isResponseModelMismatch({
        requested_model: 'A',
        upstream_model: 'B',
        returned_model: '',
      })
    ).toBe(false)
  })
})
