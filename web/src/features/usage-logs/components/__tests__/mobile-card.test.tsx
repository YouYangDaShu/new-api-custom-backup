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
  flexRender,
  getCoreRowModel,
  useReactTable,
  type VisibilityState,
} from '@tanstack/react-table'
import { render, screen, within, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'

import { usageLogSchema, type UsageLog } from '../../data/schema'
import { useCommonLogsColumns } from '../columns/common-logs-columns'
import { UsageLogsMobileList } from '../usage-logs-mobile-card'
import { UsageLogsProvider, useUsageLogsContext } from '../usage-logs-provider'

const longName = 'enterprise-production-failover-2026-without-any-short-alias'
const log = usageLogSchema.parse({
  id: 1,
  user_id: 2,
  created_at: 1788840000,
  type: 2,
  content: '',
  model_name: longName,
  username: 'production-admin',
  channel: 372,
  channel_name: longName,
  token_name: 'backend-production-token',
  group: 'enterprise-production',
  quota: 123456789,
  use_time: 1.5,
  prompt_tokens: 1200,
  completion_tokens: 800,
  other: JSON.stringify({
    cache_tokens: 300,
    cache_creation_tokens_5m: 200,
    model_ratio: 1,
  }),
})

function Fixture(props: {
  admin?: boolean
  visibility?: VisibilityState
  logs?: UsageLog[]
  loading?: boolean
  desktopTokens?: boolean
}) {
  const columns = useCommonLogsColumns(props.admin ?? true, false)
  const context = useUsageLogsContext()
  const table = useReactTable({
    data: props.logs ?? [log],
    columns,
    getCoreRowModel: getCoreRowModel(),
    state: { columnVisibility: props.visibility ?? {} },
  })
  const tokenCell = table
    .getRowModel()
    .rows[0]?.getAllCells()
    .find((cell) => cell.column.id === 'prompt_tokens')
  return (
    <>
      <button type='button' onClick={() => context.setSensitiveVisible(false)}>
        Hide sensitive data
      </button>
      <UsageLogsMobileList
        table={table}
        logCategory='common'
        isLoading={props.loading}
      />
      {props.desktopTokens && tokenCell
        ? flexRender(tokenCell.column.columnDef.cell, tokenCell.getContext())
        : null}
    </>
  )
}

function renderLogs(props: Parameters<typeof Fixture>[0] = {}) {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <UsageLogsProvider>
        <Fixture {...props} />
      </UsageLogsProvider>
    </QueryClientProvider>
  )
}

it('opens long channel text on tap and copies the complete value', async () => {
  const user = userEvent.setup()
  const copy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
  renderLogs()
  await user.click(
    screen.getByRole('button', { name: `Channel: ${longName} #372` })
  )
  const dialog = await screen.findByRole('dialog', { name: 'Channel' })
  expect(within(dialog).getByText(`${longName} #372`)).toHaveClass(
    '[overflow-wrap:anywhere]'
  )
  await user.click(
    within(dialog).getByRole('button', { name: 'Copy to clipboard' })
  )
  expect(copy).toHaveBeenCalledWith(`${longName} #372`)
  await user.keyboard('{Escape}')
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(
    screen.getByRole('button', { name: `Channel: ${longName} #372` })
  ).toHaveFocus()
})

it('keeps the model clamped to two lines and exposes its full value with keyboard input', async () => {
  const user = userEvent.setup()
  renderLogs()
  const button = screen.getByRole('button', { name: `Model: ${longName}` })
  expect(within(button).getByText(longName)).toHaveClass(
    'line-clamp-2',
    '[overflow-wrap:anywhere]'
  )
  button.focus()
  await user.keyboard('{Enter}')
  expect(
    await screen.findByRole('dialog', { name: 'Model' })
  ).toHaveTextContent(longName)
})

it('hides sensitive names and disables full-text inspection when privacy is enabled', async () => {
  const user = userEvent.setup()
  renderLogs()
  await user.click(screen.getByRole('button', { name: 'Hide sensitive data' }))
  expect(screen.queryByText('production-admin')).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: /Channel:/ })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: /Token:/ })
  ).not.toBeInTheDocument()
})

it('respects hidden columns and omits admin fields in the self view', () => {
  renderLogs({
    admin: false,
    visibility: { token_name: false, model_name: false },
  })
  expect(
    screen.queryByRole('button', {
      name: /Channel:|User:|Token:|Group:|Model:/,
    })
  ).not.toBeInTheDocument()
  expect(screen.queryByText('enterprise-production')).not.toBeInTheDocument()
})

it('shows separated cached tokens and cache rate on mobile', () => {
  renderLogs()
  expect(screen.getByTitle('Input')).toHaveTextContent('900')
  expect(screen.getByTitle('Output')).toHaveTextContent('800')
  expect(screen.getByTitle('Cached input')).toHaveTextContent('300')
  expect(screen.getByTitle('Cache Rate')).toHaveTextContent('25%')
  expect(screen.getByTestId('cache-rate-icon')).toHaveClass('lucide-activity')
  expect(screen.getByText('Cache Write 200')).toBeVisible()
})

it('shows the same cache metrics in the desktop token cell', () => {
  renderLogs({ desktopTokens: true })
  const cached = screen.getAllByTitle('Cached input')
  expect(cached).toHaveLength(2)
  expect(cached[1]).toHaveTextContent('300')
  const cacheRateIcons = screen.getAllByTestId('cache-rate-icon')
  expect(cacheRateIcons).toHaveLength(2)
  expect(
    cacheRateIcons.every((icon) => icon.classList.contains('lucide-activity'))
  ).toBe(true)
  expect(screen.getAllByTitle('Cache Rate')[1]).toHaveTextContent('25%')
  expect(screen.getAllByTitle('Input')[1]).toHaveTextContent('900')
  expect(screen.getAllByTitle('Output')[1]).toHaveTextContent('800')
})

it('shows the exact cached token count without compact rounding in both views', () => {
  renderLogs({
    desktopTokens: true,
    logs: [
      {
        ...log,
        prompt_tokens: 1234567,
        other: JSON.stringify({ cache_tokens: 1234567 }),
      },
    ],
  })
  const cached = screen.getAllByLabelText('Cached input: 1,234,567')
  expect(cached).toHaveLength(2)
  for (const metric of cached) {
    expect(metric).toHaveTextContent('1,234,567')
    expect(metric).not.toHaveTextContent('1.23M')
  }
  expect(screen.getAllByLabelText('Cache Rate: 100%')).toHaveLength(2)
})

it('does not invent cache metrics when cache_tokens is absent', () => {
  renderLogs({
    desktopTokens: true,
    logs: [
      {
        ...log,
        other: JSON.stringify({ model_ratio: 1 }),
      },
    ],
  })
  expect(screen.getAllByTitle('Input')).toHaveLength(2)
  expect(screen.getAllByTitle('Input')[0]).toHaveTextContent('1,200')
  expect(screen.queryByTitle('Cached input')).not.toBeInTheDocument()
})

it('keeps cache-only metrics but hides zero cache read and rate', () => {
  const { unmount } = renderLogs({
    desktopTokens: true,
    logs: [
      {
        ...log,
        prompt_tokens: 0,
        completion_tokens: 0,
        other: JSON.stringify({ cache_tokens: 150 }),
      },
    ],
  })
  expect(screen.getAllByLabelText('Input: 0')).toHaveLength(2)
  expect(screen.getAllByLabelText('Output: 0')).toHaveLength(2)
  expect(screen.getAllByLabelText('Cached input: 150')).toHaveLength(2)
  expect(screen.getAllByLabelText('Cache Rate: 0%')).toHaveLength(2)

  unmount()
  renderLogs({
    desktopTokens: true,
    logs: [
      {
        ...log,
        other: JSON.stringify({ cache_tokens: 0 }),
      },
    ],
  })
  expect(screen.getAllByTitle('Input')).toHaveLength(2)
  expect(screen.getAllByTitle('Output')).toHaveLength(2)
  expect(screen.queryByTitle('Cached input')).not.toBeInTheDocument()
  expect(screen.queryByTitle('Cache Rate')).not.toBeInTheDocument()
})

it('keeps uncached input green when the long-context warning is active', () => {
  renderLogs({
    desktopTokens: true,
    logs: [
      {
        ...log,
        other: JSON.stringify({
          cache_tokens: 300,
          billing_mode: 'tiered_expr',
          expr_b64: Buffer.from(
            'len <= 100 ? tier("short", p) : tier("long", p)'
          ).toString('base64'),
        }),
      },
    ],
  })
  const inputs = screen.getAllByLabelText('Input: 900')
  expect(inputs).toHaveLength(2)
  for (const input of inputs) {
    expect(input).toHaveClass('text-emerald-600')
    expect(input).not.toHaveClass('text-red-600')
  }
})

it('hides non-finite cache-write values in both views', () => {
  renderLogs({
    desktopTokens: true,
    logs: [
      {
        ...log,
        prompt_tokens: 0,
        completion_tokens: 0,
        other: '{"cache_creation_tokens_5m":1e400}',
      },
    ],
  })
  expect(screen.queryByText(/Cache Write/)).not.toBeInTheDocument()
  expect(screen.queryByText('∞')).not.toBeInTheDocument()
})

it('shows the established empty state when no logs exist', () => {
  renderLogs({ logs: [] })
  expect(screen.getByText('No Logs Found')).toBeVisible()
})

it.each([false, true])(
  'keeps timing in the right column for streaming=%s',
  (streaming) => {
    renderLogs({
      logs: [
        {
          ...log,
          is_stream: streaming,
          use_time: 58,
          other: JSON.stringify({ frt: 58000 }),
        },
      ],
    })
    const row = screen
      .getByRole('button', { name: /^Time:/ })
      .closest('[data-slot="log-time-and-timing"]')
    expect(row).toHaveClass('grid', 'grid-cols-2')
    expect(
      screen.getByRole('button', { name: /^Time:/ }).parentElement
    ).toHaveClass('flex-col', 'justify-between')
    expect(
      within(row as HTMLElement)
        .getByText('Duration')
        .closest('.col-start-2')
    ).not.toBeNull()
    if (streaming) {
      expect(within(row as HTMLElement).getByText('First token')).toBeVisible()
    }
  }
)

it('retains the user avatar and model badge in the mobile summary', () => {
  renderLogs()
  expect(screen.getByText('P')).toBeVisible()
  const modelButton = screen.getByRole('button', { name: `Model: ${longName}` })
  expect(modelButton.querySelector('[data-slot="status-badge"]')).not.toBeNull()
})

it('omits unused token and throughput placeholders for async jobs', () => {
  renderLogs({
    logs: [
      {
        ...log,
        prompt_tokens: 0,
        completion_tokens: 0,
        other: JSON.stringify({ is_task: true }),
      },
    ],
  })
  expect(screen.getByText('Async')).toBeVisible()
  expect(screen.queryByText('Input')).not.toBeInTheDocument()
  const timing = screen
    .getByRole('button', { name: /^Time:/ })
    .closest('[data-slot="log-time-and-timing"]')
  expect(within(timing as HTMLElement).queryByText('—')).not.toBeInTheDocument()
})

it('shows mapped model names in full when inspecting a mobile model badge', async () => {
  const user = userEvent.setup()
  renderLogs({
    logs: [
      {
        ...log,
        other: JSON.stringify({
          is_model_mapped: true,
          upstream_model_name:
            'provider-production-mapped-model-with-a-long-name',
        }),
      },
    ],
  })
  await user.click(screen.getByRole('button', { name: `Model: ${longName}` }))
  const dialog = await screen.findByRole('dialog', { name: 'Model' })
  expect(within(dialog).getByText('Actual Model')).toBeVisible()
  expect(
    within(dialog).getByText(
      'provider-production-mapped-model-with-a-long-name'
    )
  ).toBeVisible()
})

it('shows loading placeholders without displaying stale log fields', () => {
  renderLogs({ loading: true })
  expect(screen.getByRole('status', { name: 'Loading' })).toHaveAttribute(
    'aria-busy',
    'true'
  )
  expect(
    screen.queryByRole('button', { name: /^Model:/ })
  ).not.toBeInTheDocument()
})
