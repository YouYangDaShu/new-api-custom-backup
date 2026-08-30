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

import { createDraftPrize } from './draft-prize.ts'

describe('lottery draft prize ids', () => {
  test('numbers fresh tiers sequentially', () => {
    assert.equal(createDraftPrize([]).id, 'tier-1')
    assert.equal(createDraftPrize([{ id: 'tier-1' }]).id, 'tier-2')
  })

  test('never reuses an id that survived a middle-tier deletion', () => {
    // Two tiers, tier-1 deleted: length + 1 would produce a duplicate tier-2.
    const next = createDraftPrize([{ id: 'tier-2' }])
    assert.equal(next.id, 'tier-3')
    assert.equal(next.name, 'Tier 3')
  })

  test('skips over any occupied ids until a free one is found', () => {
    const next = createDraftPrize([
      { id: 'tier-2' },
      { id: 'tier-3' },
      { id: 'custom' },
    ])
    assert.equal(next.id, 'tier-4')
  })
})
