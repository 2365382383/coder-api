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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { PricingCategoryNav } from '../components/pricing-category-nav'

describe('pricing category navigation', () => {
  it('renders configured category names and emits stable IDs', async () => {
    const user = userEvent.setup()
    const onCategoryChange = vi.fn()
    render(
      <PricingCategoryNav
        types={[
          {
            id: 'custom-a-id',
            name: '自定义分类 A',
            order: 0,
            group_count: 2,
            model_count: 3,
          },
          {
            id: 'custom-b-id',
            name: '自定义分类 B',
            order: 1,
            group_count: 1,
            model_count: 1,
          },
        ]}
        selectedCategory='all'
        modelCount={4}
        onCategoryChange={onCategoryChange}
      />
    )

    expect(screen.getByRole('button', { name: /自定义分类 A/ })).toBeVisible()
    expect(screen.getByRole('button', { name: /自定义分类 B/ })).toBeVisible()
    expect(
      screen.queryByRole('button', { name: /OpenAI/ })
    ).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /自定义分类 B/ }))
    expect(onCategoryChange).toHaveBeenCalledWith('type:custom-b-id')
  })

  it('does not render a category bar when the server has no configured types', () => {
    const { container } = render(
      <PricingCategoryNav
        types={[]}
        selectedCategory='all'
        modelCount={2}
        onCategoryChange={vi.fn()}
      />
    )
    expect(container).toBeEmptyDOMElement()
  })

  it('keeps the stable selection when a configured category is renamed and reordered', () => {
    const props = {
      selectedCategory: 'type:stable-id',
      modelCount: 1,
      onCategoryChange: vi.fn(),
    }
    const original = {
      id: 'stable-id',
      name: 'Original',
      order: 0,
      group_count: 1,
      model_count: 1,
    }
    const view = render(<PricingCategoryNav {...props} types={[original]} />)
    expect(screen.getByRole('button', { name: /Original/ })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    view.rerender(
      <PricingCategoryNav
        {...props}
        types={[{ ...original, name: 'Renamed', order: 9 }]}
      />
    )
    expect(
      screen.queryByRole('button', { name: /Original/ })
    ).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Renamed/ })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    view.rerender(
      <PricingCategoryNav {...props} types={[]} unclassifiedCount={1} />
    )
    expect(
      screen.queryByRole('button', { name: /Renamed/ })
    ).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Unclassified/ })).toBeVisible()
  })
})
