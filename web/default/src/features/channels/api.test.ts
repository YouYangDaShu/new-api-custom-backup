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
// @ts-expect-error Vitest is provided by bunx for this isolated test.
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { createChannel, getChannels, updateChannel } from './api'

vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
  },
}))

vi.mock('@/features/users/api', () => ({
  getGroups: vi.fn(),
}))

describe('channel collection API paths', () => {
  beforeEach(() => {
    vi.mocked(api.get).mockReset().mockResolvedValue({ data: {} })
    vi.mocked(api.post).mockReset().mockResolvedValue({ data: {} })
    vi.mocked(api.put).mockReset().mockResolvedValue({ data: {} })
  })

  test('uses the trailing-slash collection path for GET', async () => {
    await getChannels()

    expect(api.get).toHaveBeenCalledWith('/api/channel/', { params: {} })
  })

  test('uses the trailing-slash collection path for POST', async () => {
    const request = { mode: 'single' as const, channel: {} }

    await createChannel(request)

    expect(api.post).toHaveBeenCalledWith(
      '/api/channel/',
      request,
      expect.any(Object)
    )
  })

  test('keeps the trailing-slash collection path for PUT', async () => {
    await updateChannel(7, { name: 'channel' })

    expect(api.put).toHaveBeenCalledWith(
      '/api/channel/',
      { id: 7, name: 'channel' },
      expect.any(Object)
    )
  })
})
