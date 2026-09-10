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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import type { LogStatistics } from '../../types'
import { CommonLogsStats } from '../common-logs-stats'
import { UsageLogsProvider } from '../usage-logs-provider'

function StatsFixture() {
  return (
    <UsageLogsProvider>
      <CommonLogsStats />
    </UsageLogsProvider>
  )
}

function renderStats(data: Partial<LogStatistics>): void {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data },
  })
  const root = createRootRoute()
  const authenticated = createRoute({
    getParentRoute: () => root,
    id: '_authenticated',
  })
  const logs = createRoute({
    getParentRoute: () => authenticated,
    path: '/usage-logs/$section',
    component: StatsFixture,
    validateSearch: (search: Record<string, unknown>) => search,
  })
  const router = createRouter({
    routeTree: root.addChildren([authenticated.addChildren([logs])]),
    history: createMemoryHistory({
      initialEntries: ['/usage-logs/common'],
    }),
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useAuthStore.getState().auth.setUser(null)
})

test('shows zero totals for statistics omitted by an older API', async () => {
  renderStats({ quota: 0, rpm: 2, tpm: 120 })

  const cacheRead = (await screen.findByText('Cache Read TPM')).parentElement
  const cacheCreate = screen.getByText('Cache Create TPM').parentElement
  const output = screen.getByText('Output TPM').parentElement
  expect(cacheRead).toHaveTextContent('Cache Read TPM0Total 0')
  expect(cacheCreate).toHaveTextContent('Cache Create TPM0Total 0')
  expect(output).toHaveTextContent('Output TPM0Total 0')
  expect(screen.getByText('Cache Rate').parentElement).toHaveTextContent(
    'Cache Rate0%'
  )
})

test('shows recent rates beside filtered-range totals', async () => {
  renderStats({
    quota: 500,
    rpm: 2,
    tpm: 120,
    request_count: 7,
    completion_tokens: 50,
    total_tokens: 350,
    cache_read_tokens: 30,
    cache_create_tokens: 12,
    cache_rate: 10,
    cache_read_tpm: 3,
    cache_create_tpm: 4,
    completion_tpm: 5,
  })

  expect((await screen.findByText('RPM')).parentElement).toHaveTextContent(
    'RPM2Total 7'
  )
  expect(screen.getByText('TPM').parentElement).toHaveTextContent(
    'TPM120Total 350'
  )
  expect(screen.getByText('Cache Read TPM').parentElement).toHaveTextContent(
    'Cache Read TPM3Total 30'
  )
  expect(screen.getByText('Cache Create TPM').parentElement).toHaveTextContent(
    'Cache Create TPM4Total 12'
  )
  expect(screen.getByText('Output TPM').parentElement).toHaveTextContent(
    'Output TPM5Total 50'
  )
  expect(screen.getByText('Cache Rate').parentElement).toHaveTextContent(
    'Cache Rate10%'
  )
})
