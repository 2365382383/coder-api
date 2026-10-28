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
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'
import { z } from 'zod'

import { PricingCategoryNav } from '../components/pricing-category-nav'
import { PricingGroupSelect } from '../components/pricing-group-select'
import { useFilters } from '../hooks/use-filters'
import type { PricingGroup, PricingGroupType, PricingModel } from '../types'

const types: PricingGroupType[] = [
  { id: 'lab-a', name: 'Research A', order: 0, group_count: 1, model_count: 1 },
  { id: 'lab-b', name: 'Research B', order: 1, group_count: 1, model_count: 1 },
]
const groups: PricingGroup[] = [
  {
    name: 'group-a',
    type_id: 'lab-a',
    ratio: 1,
    description: 'First group',
    model_count: 1,
  },
  {
    name: 'group-b',
    type_id: 'lab-b',
    ratio: 3,
    description: 'Second group',
    model_count: 1,
  },
]
const models: PricingModel[] = [
  {
    id: 1,
    model_name: 'model-a',
    quota_type: 0,
    model_ratio: 1,
    completion_ratio: 1,
    enable_groups: ['group-a'],
    group_type_ids: ['lab-a'],
  },
  {
    id: 2,
    model_name: 'model-b',
    quota_type: 0,
    model_ratio: 1,
    completion_ratio: 1,
    enable_groups: ['group-b'],
    group_type_ids: ['lab-b'],
  },
]

function NavigationFixture() {
  const [ready, setReady] = useState(false)
  const filters = useFilters(
    ready ? models : [],
    ready ? types : [],
    ready ? groups : [],
    ready,
    ready
  )
  return (
    <div>
      <button type='button' onClick={() => setReady(true)}>
        Load catalog
      </button>
      <button type='button' onClick={filters.clearAll}>
        Clear everything
      </button>
      <PricingCategoryNav
        types={ready ? types : []}
        selectedCategory={filters.categoryFilter}
        modelCount={models.length}
        onCategoryChange={filters.setCategoryFilter}
      />
      <PricingGroupSelect
        groups={groups}
        value={filters.groupFilter}
        onChange={filters.setGroupFilter}
      />
      <output aria-label='Visible models'>
        {filters.filteredModels.map((model) => model.model_name).join(',')}
      </output>
    </div>
  )
}

async function openNavigation(path: string) {
  const root = createRootRoute({ component: Outlet })
  const route = createRoute({
    getParentRoute: () => root,
    path: '/pricing/',
    validateSearch: z.object({
      search: z.string().optional(),
      category: z.string().optional(),
      unclassified: z.boolean().optional(),
      group: z.string().optional(),
    }),
    component: NavigationFixture,
  })
  const router = createRouter({
    routeTree: root.addChildren([route]),
    history: createMemoryHistory({ initialEntries: [path] }),
  })
  await router.load()
  render(<RouterProvider router={router} />)
  return router
}

describe('pricing URL selection', () => {
  it('orders group options by ascending ratio', async () => {
    const user = userEvent.setup()
    render(
      <PricingGroupSelect
        groups={[
          {
            name: 'high',
            type_id: null,
            ratio: 3,
            description: 'High group',
            model_count: 1,
          },
          {
            name: 'same-b',
            type_id: null,
            ratio: 1,
            description: 'Same ratio B',
            model_count: 1,
          },
          {
            name: 'low',
            type_id: null,
            ratio: 0.2,
            description: 'Low group',
            model_count: 1,
          },
          {
            name: 'same-a',
            type_id: null,
            ratio: 1,
            description: 'Same ratio A',
            model_count: 1,
          },
          {
            name: 'unpriced',
            type_id: null,
            ratio: Number.NaN,
            description: 'Unavailable ratio',
            model_count: 1,
          },
        ]}
        value='all'
        onChange={() => {}}
      />
    )

    await user.click(screen.getByRole('combobox', { name: 'Group' }))
    expect(
      (await screen.findAllByRole('option')).map((option) => option.textContent)
    ).toEqual([
      'All Groups',
      'low · ×0.2Low group',
      'same-a · ×1Same ratio A',
      'same-b · ×1Same ratio B',
      'high · ×3High group',
      'unpricedUnavailable ratio',
    ])
  })

  it('clears category, group and search in one navigation', async () => {
    const router = await openNavigation(
      '/pricing/?category=lab-a&group=group-a&search=missing'
    )
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Load catalog' }))
    await user.click(screen.getByRole('button', { name: 'Clear everything' }))
    await waitFor(() => expect(router.state.location.search).toEqual({}))
    await waitFor(() =>
      expect(screen.getByLabelText('Visible models')).toHaveTextContent(
        'model-a,model-b'
      )
    )
  })
  it('keeps a shared category URL while loading, then removes an incompatible group', async () => {
    const router = await openNavigation(
      '/pricing/?category=lab-a&group=group-b'
    )
    expect(router.state.location.search).toMatchObject({
      category: 'lab-a',
      group: 'group-b',
    })
    await userEvent
      .setup()
      .click(screen.getByRole('button', { name: 'Load catalog' }))
    await waitFor(() =>
      expect(router.state.location.search).toMatchObject({ category: 'lab-a' })
    )
    await waitFor(() =>
      expect(router.state.location.search.group).toBeUndefined()
    )
    expect(screen.getByLabelText('Visible models')).toHaveTextContent('model-a')
  })

  it('updates the category and group in the URL and follows browser history', async () => {
    const router = await openNavigation('/pricing/')
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Load catalog' }))
    await user.click(screen.getByRole('button', { name: /Research B/ }))
    await waitFor(() =>
      expect(router.state.location.search.category).toBe('lab-b')
    )
    expect(screen.getByLabelText('Visible models')).toHaveTextContent('model-b')
    await user.click(screen.getByRole('combobox', { name: 'Group' }))
    await user.click(await screen.findByRole('option', { name: /group-b/ }))
    await waitFor(() =>
      expect(router.state.location.search.group).toBe('group-b')
    )
    await act(async () => {
      router.history.back()
    })
    await waitFor(() =>
      expect(router.state.location.search.group).toBeUndefined()
    )
    expect(router.state.location.search.category).toBe('lab-b')
    await act(async () => {
      router.history.back()
    })
    await waitFor(() =>
      expect(router.state.location.search.category).toBeUndefined()
    )
    expect(screen.getByLabelText('Visible models')).toHaveTextContent(
      'model-a,model-b'
    )
  })
})
