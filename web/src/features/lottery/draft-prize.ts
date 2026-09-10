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

export interface DraftPrize {
  id: string
  name: string
  amount: string
  probability: string
}

/**
 * Create a new draft prize tier whose id never collides with an existing tier.
 * The backend rejects duplicate prize ids, and deleting a middle tier used to
 * make the next `tier-${length + 1}` id repeat an id still in the list.
 */
export function createDraftPrize(
  existing: ReadonlyArray<{ id: string }>
): DraftPrize {
  const usedIds = new Set(existing.map((prize) => prize.id))
  let index = existing.length + 1
  while (usedIds.has(`tier-${index}`)) {
    index += 1
  }
  return {
    id: `tier-${index}`,
    name: `Tier ${index}`,
    amount: '',
    probability: '',
  }
}
