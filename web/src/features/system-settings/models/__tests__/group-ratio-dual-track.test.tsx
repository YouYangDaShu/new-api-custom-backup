import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
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
import { useState } from 'react'
import { describe, expect, test, vi } from 'vitest'

import { GroupRatioVisualEditor } from '../group-ratio-visual-editor'

const props = {
  groupRatio: JSON.stringify({ default: 0.9, vip: 0.8, grok: 1.2 }),
  topupGroupRatio: JSON.stringify({ default: 1.1, vip: 1.2, grok: 1 }),
  userUsableGroups: JSON.stringify({
    default: 'Default membership',
    vip: 'VIP membership',
    grok: 'Grok route',
  }),
  groupGroupRatio: '{}',
  autoGroups: JSON.stringify(['grok']),
  maxTokenAutoGroupsField: <div>Maximum custom groups per token</div>,
  groupSpecialUsableGroup: '{}',
  membershipGroups: JSON.stringify({ default: true, vip: true }),
}

const groupFieldToProp = (
  field: string
):
  | 'groupRatio'
  | 'topupGroupRatio'
  | 'userUsableGroups'
  | 'membershipGroups'
  | null => {
  if (field === 'GroupRatio') return 'groupRatio'
  if (field === 'TopupGroupRatio') return 'topupGroupRatio'
  if (field === 'UserUsableGroups') return 'userUsableGroups'
  if (field === 'MembershipGroups') return 'membershipGroups'
  return null
}

const DualTrackHarness = () => {
  const [state, setState] = useState(props)
  return (
    <GroupRatioVisualEditor
      {...state}
      onChange={(field, value) => {
        const key = groupFieldToProp(field)
        if (key) setState((current) => ({ ...current, [key]: value }))
      }}
    />
  )
}

describe('dual-track group editor', () => {
  test('renders route and membership groups in separate cards with the complete fields', () => {
    render(<GroupRatioVisualEditor {...props} onChange={vi.fn()} />)

    const routeCard = screen
      .getByText('📦 Model route groups')
      .closest('[data-slot="card"]')
    const membershipCard = screen
      .getByText('👤 Membership tier groups')
      .closest('[data-slot="card"]')

    expect(routeCard).not.toBeNull()
    expect(membershipCard).not.toBeNull()
    expect(
      within(routeCard as HTMLElement).getByDisplayValue('grok')
    ).toBeVisible()
    expect(
      within(routeCard as HTMLElement).queryByDisplayValue('vip')
    ).not.toBeInTheDocument()
    expect(
      within(membershipCard as HTMLElement).getByDisplayValue('vip')
    ).toBeVisible()
    expect(
      within(membershipCard as HTMLElement).queryByDisplayValue('grok')
    ).not.toBeInTheDocument()

    for (const card of [routeCard, membershipCard] as HTMLElement[]) {
      expect(within(card).getByText('Ratio')).toBeVisible()
      expect(within(card).getByText('Top-up ratio')).toBeVisible()
      expect(within(card).getByText('User selectable')).toBeVisible()
      expect(within(card).getByText('Description')).toBeVisible()
    }
  })

  test('editing one track preserves the hidden track and emits the membership map', () => {
    const onChange = vi.fn()
    render(<GroupRatioVisualEditor {...props} onChange={onChange} />)

    const routeCard = screen
      .getByText('📦 Model route groups')
      .closest('[data-slot="card"]') as HTMLElement
    fireEvent.change(within(routeCard).getByDisplayValue('1.2'), {
      target: { value: '1.3' },
    })

    const groupRatioCall = onChange.mock.calls.find(
      ([field]) => field === 'GroupRatio'
    )
    const membershipCall = onChange.mock.calls.find(
      ([field]) => field === 'MembershipGroups'
    )

    expect(JSON.parse(groupRatioCall?.[1] as string)).toEqual({
      default: 0.9,
      vip: 0.8,
      grok: 1.3,
    })
    expect(JSON.parse(membershipCall?.[1] as string)).toEqual({
      default: true,
      vip: true,
    })
  })

  test('cross-track duplicate names are rejected before serialization', () => {
    const onChange = vi.fn()
    render(<GroupRatioVisualEditor {...props} onChange={onChange} />)

    const routeCard = screen
      .getByText('📦 Model route groups')
      .closest('[data-slot="card"]') as HTMLElement
    fireEvent.change(within(routeCard).getByDisplayValue('grok'), {
      target: { value: 'vip' },
    })

    expect(
      within(routeCard).getByText('Duplicate group names: vip')
    ).toBeVisible()
    expect(onChange).not.toHaveBeenCalled()
  })

  test('new group names are unique across both cards', async () => {
    render(<DualTrackHarness />)

    const routeCard = screen
      .getByText('📦 Model route groups')
      .closest('[data-slot="card"]') as HTMLElement
    const membershipCard = screen
      .getByText('👤 Membership tier groups')
      .closest('[data-slot="card"]') as HTMLElement
    fireEvent.click(within(routeCard).getByText('Add group'))
    expect(within(routeCard).getByDisplayValue('group_1')).toBeVisible()

    await waitFor(() =>
      expect(within(routeCard).getByDisplayValue('group_1')).toBeVisible()
    )
    fireEvent.click(within(membershipCard).getByText('Add group'))
    expect(within(membershipCard).getByDisplayValue('group_2')).toBeVisible()
  })

  test('membership selectable and description values survive route edits', () => {
    const onChange = vi.fn()
    render(<GroupRatioVisualEditor {...props} onChange={onChange} />)

    const routeCard = screen
      .getByText('📦 Model route groups')
      .closest('[data-slot="card"]') as HTMLElement
    fireEvent.change(within(routeCard).getByDisplayValue('1.2'), {
      target: { value: '1.4' },
    })

    const usableGroupsCall = onChange.mock.calls.find(
      ([field]) => field === 'UserUsableGroups'
    )
    const topupRatioCall = onChange.mock.calls.find(
      ([field]) => field === 'TopupGroupRatio'
    )

    expect(JSON.parse(usableGroupsCall?.[1] as string)).toEqual({
      default: 'Default membership',
      vip: 'VIP membership',
      grok: 'Grok route',
    })
    expect(JSON.parse(topupRatioCall?.[1] as string)).toEqual({
      default: 1.1,
      vip: 1.2,
      grok: 1,
    })
  })
})
