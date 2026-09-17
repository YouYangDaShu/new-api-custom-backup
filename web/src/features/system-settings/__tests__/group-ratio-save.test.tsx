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
  act,
  fireEvent,
  render,
  renderHook,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { describe, expect, test, vi } from 'vitest'

import { updateGroupRatioOptions } from '../api'
import { SettingsPageProvider } from '../components/settings-page-context'
import { useUpdateGroupRatioOptions } from '../hooks/use-update-option'
import { RatioSettingsCard } from '../models/ratio-settings-card'

vi.mock('@/features/model-pricing/api', () => ({
  buildPricingChanges: vi.fn(() => []),
  useModelPricing: vi.fn(() => ({
    data: null,
    error: null,
    isError: false,
    refetch: vi.fn(),
  })),
  useSaveModelPricing: vi.fn(() => ({
    isError: false,
    isPending: false,
    mutateAsync: vi.fn(),
    reset: vi.fn(),
  })),
}))

vi.mock('../api', () => ({
  updateGroupRatioOptions: vi.fn(),
  updateSystemOption: vi.fn(),
}))

const request = {
  options: [{ key: 'GroupRatio', value: '{"default":1}' }],
}

function createWrapper() {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>
  }
}

function createDeferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

const modelDefaults = {
  ModelPrice: '{}',
  ModelRatio: '{}',
  CacheRatio: '{}',
  CreateCacheRatio: '{}',
  CompletionRatio: '{}',
  ImageRatio: '{}',
  AudioRatio: '{}',
  AudioCompletionRatio: '{}',
  ExposeRatioEnabled: false,
  BillingMode: '{}',
  BillingExpr: '{}',
}

const groupDefaults = {
  GroupRatio: '{"grok":1}',
  TopupGroupRatio: '{"grok":1}',
  UserUsableGroups: '{"grok":"Grok route"}',
  GroupGroupRatio: '{}',
  AutoGroups: '["grok"]',
  MaxTokenAutoGroups: 1,
  DefaultUseAutoGroup: false,
  GroupSpecialUsableGroup: '{}',
  MembershipGroups: '{}',
}

function renderRatioSettingsCard(actionsContainer: HTMLDivElement) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <SettingsPageProvider actionsContainer={actionsContainer}>
        <RatioSettingsCard
          modelDefaults={modelDefaults}
          groupDefaults={groupDefaults}
          toolPricesDefault='{}'
          visibleTabs={['groups']}
        />
      </SettingsPageProvider>
    </QueryClientProvider>
  )
}

describe('RatioSettingsCard group save', () => {
  test('disables the real save button while the bulk mutation is pending and prevents duplicate submit', async () => {
    const deferred = createDeferred<{ success: boolean; message: string }>()
    vi.mocked(updateGroupRatioOptions).mockReturnValue(deferred.promise)
    const actionsContainer = document.createElement('div')
    document.body.appendChild(actionsContainer)
    const rendered = renderRatioSettingsCard(actionsContainer)

    const routeCard = screen
      .getByText('📦 Model route groups')
      .closest('[data-slot="card"]') as HTMLElement
    fireEvent.change(within(routeCard).getAllByDisplayValue('1')[0], {
      target: { value: '1.25' },
    })

    const saveButton = screen.getByRole('button', {
      name: 'Save group ratios',
    })
    await userEvent.click(saveButton)
    await waitFor(() =>
      expect(updateGroupRatioOptions).toHaveBeenCalledTimes(1)
    )
    await waitFor(() => expect(saveButton).toBeDisabled())

    await userEvent.click(saveButton)
    expect(updateGroupRatioOptions).toHaveBeenCalledTimes(1)

    deferred.resolve({ success: true, message: '' })
    await waitFor(() => expect(saveButton).toBeEnabled())

    rendered.unmount()
    actionsContainer.remove()
  })
})

describe('useUpdateGroupRatioOptions', () => {
  test('rejects business failures so callers keep the previous saved baseline', async () => {
    vi.mocked(updateGroupRatioOptions).mockResolvedValue({
      success: false,
      message: 'save failed',
    })
    const { result } = renderHook(() => useUpdateGroupRatioOptions(), {
      wrapper: createWrapper(),
    })

    await expect(
      act(async () => result.current.mutateAsync(request))
    ).rejects.toThrow('save failed')
  })

  test('can retry after a business failure', async () => {
    vi.mocked(updateGroupRatioOptions)
      .mockResolvedValueOnce({ success: false, message: 'save failed' })
      .mockResolvedValueOnce({ success: true, message: '' })
    const { result } = renderHook(() => useUpdateGroupRatioOptions(), {
      wrapper: createWrapper(),
    })

    await expect(
      act(async () => result.current.mutateAsync(request))
    ).rejects.toThrow('save failed')
    await expect(
      act(async () => result.current.mutateAsync(request))
    ).resolves.toEqual({ success: true, message: '' })
    expect(updateGroupRatioOptions).toHaveBeenCalledTimes(2)
  })
})
