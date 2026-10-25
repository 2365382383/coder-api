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

import type { PricingGroup, PricingGroupType, PricingModel } from '../../types'
import { getDynamicPricingSummary } from '../dynamic-price'
import { filterAndSortModels } from '../filters'
import {
  getCategoryGroupNames,
  getGroupCategoryID,
  modelBelongsToCategory,
  normalizePricingSelection,
  scopePricingModel,
} from '../group-categories'
import { getDisplayGroupRatio } from '../model-helpers'
import { formatRequestPrice } from '../price'

const types: PricingGroupType[] = [
  { id: 'type-a', name: 'Custom A', order: 0, group_count: 1, model_count: 1 },
  { id: 'type-b', name: 'Custom B', order: 1, group_count: 1, model_count: 1 },
]

const groups: PricingGroup[] = [
  {
    name: 'group-a',
    type_id: 'type-a',
    ratio: 1,
    description: '',
    model_count: 1,
  },
  {
    name: 'group-b',
    type_id: 'type-b',
    ratio: 1,
    description: '',
    model_count: 1,
  },
]

const models: PricingModel[] = [
  {
    id: 1,
    model_name: 'shared-model',
    quota_type: 0,
    model_ratio: 1,
    completion_ratio: 1,
    enable_groups: ['group-a', 'group-b'],
    group_type_ids: ['type-a', 'type-b'],
  },
]

describe('dynamic pricing group categories', () => {
  it('uses only task providers bound to the selected group', () => {
    const schema = { seconds: { type: 'number', unit: 'second' } } as const
    const original: PricingModel = {
      ...models[0],
      group_plugin_keys: {
        'group-a': ['provider-a'],
        'group-b': ['provider-b'],
      },
      billing_mode: 'tiered_expr',
      billing_expr: 'tier("base", u("seconds") * 9)',
      billing_usage_schema: schema,
      billing_plugin_variants: [
        {
          plugin_key: 'provider-a',
          plugin_name: 'Provider A',
          billing_mode: 'tiered_expr',
          billing_expr: 'tier("a", u("seconds") * 9)',
          billing_usage_schema: schema,
        },
        {
          plugin_key: 'provider-b',
          plugin_name: 'Provider B',
          billing_mode: 'tiered_expr',
          billing_expr: 'tier("b", u("seconds") * 1)',
          billing_usage_schema: schema,
        },
      ],
    }
    const projected = scopePricingModel(original, ['group-b'])
    expect(
      projected.billing_plugin_variants?.map((variant) => variant.plugin_key)
    ).toEqual(['provider-b'])
    expect(projected.billing_expr).toBe('tier("b", u("seconds") * 1)')
    expect(original.billing_plugin_variants).toHaveLength(2)
    const paired = scopePricingModel({
      ...original,
      group_ratio: { 'group-a': 0.1, 'group-b': 3 },
    })
    const summary = getDynamicPricingSummary(paired, {
      tokenUnit: 'M',
      groupRatioMultiplier: 0.1,
    })
    expect(summary?.entries[0].minValue).toBeCloseTo(0.9)
    expect(summary?.entries[0].maxValue).toBe(3)
    expect(summary?.groupRatiosApplied).toBe(true)
    const nonPlugin = scopePricingModel(
      { ...original, group_plugin_keys: { 'group-b': [] } },
      ['group-b']
    )
    expect(nonPlugin.billing_plugin_variants).toEqual([])
    expect(nonPlugin.billing_usage_schema).toBeUndefined()
  })
  it('uses stable type IDs instead of display names for model membership', () => {
    expect(modelBelongsToCategory(models[0], 'type:type-a')).toBe(true)
    expect(modelBelongsToCategory(models[0], 'type:type-b')).toBe(true)
    expect(modelBelongsToCategory(models[0], 'Custom A')).toBe(false)
  })

  it('resolves a group to its configured category and group list', () => {
    expect(getGroupCategoryID('group-a', groups)).toBe('type:type-a')
    expect(getCategoryGroupNames('type:type-b', groups)).toEqual(['group-b'])
    expect(getGroupCategoryID('missing', groups)).toBeUndefined()
  })

  it('limits the displayed ratio to the selected category groups', () => {
    expect(
      getDisplayGroupRatio(
        { ...models[0], group_ratio: { 'group-a': 1, 'group-b': 3 } },
        undefined,
        ['group-b']
      )
    ).toBe(3)
    expect(
      getDisplayGroupRatio(
        { ...models[0], group_ratio: { 'group-a': 1, 'group-b': 3 } },
        undefined,
        ['group-a']
      )
    ).toBe(1)
  })

  it('normalizes deleted categories, moved groups, and the legacy response without inventing a type', () => {
    expect(
      normalizePricingSelection({ category: 'missing' }, types, groups, true)
    ).toEqual({
      category: undefined,
      group: undefined,
      unclassified: undefined,
    })
    expect(
      normalizePricingSelection(
        { category: 'type-a', group: 'group-b' },
        types,
        groups,
        true
      ).group
    ).toBeUndefined()
    expect(
      normalizePricingSelection({ group: 'group-b' }, types, groups, true)
        .category
    ).toBe('type-b')
    expect(
      normalizePricingSelection(
        { category: 'type-a', group: 'group-a' },
        [],
        groups,
        false
      )
    ).toEqual({
      category: undefined,
      group: 'group-a',
      unclassified: undefined,
    })
    expect(
      normalizePricingSelection({ group: 'group-a' }, types, [], true).group
    ).toBeUndefined()
    expect(
      normalizePricingSelection(
        { category: 'type-a' },
        [{ ...types[0], name: 'Renamed' }],
        groups,
        true
      ).category
    ).toBe('type-a')
  })

  it('keeps an administrator ID named all distinct from the all and unclassified controls', () => {
    const custom = { ...models[0], group_type_ids: ['all'] }
    expect(modelBelongsToCategory(custom, 'type:all')).toBe(true)
    expect(modelBelongsToCategory(custom, 'unclassified', groups)).toBe(false)
    const unclassified = [{ ...groups[0], type_id: null }]
    expect(modelBelongsToCategory(custom, 'unclassified', unclassified)).toBe(
      true
    )
    expect(
      normalizePricingSelection({ group: 'group-a' }, [], unclassified, true)
        .unclassified
    ).toBe(true)
  })

  it('projects actual group endpoints without changing the shared model', () => {
    const original = {
      ...models[0],
      supported_endpoint_types: ['openai', 'openai-video'],
      group_endpoint_types: {
        'group-a': ['openai'],
        'group-b': ['openai-video'],
      },
    }
    expect(
      scopePricingModel(original, ['group-b']).supported_endpoint_types
    ).toEqual(['openai-video'])
    expect(original.supported_endpoint_types).toEqual([
      'openai',
      'openai-video',
    ])
    expect(
      scopePricingModel({ ...original, enable_groups: ['all'] }, ['group-a'])
        .enable_groups
    ).toEqual(['group-a'])
  })

  it('sorts using the selected group price for legacy, fixed, expression, and task prices', () => {
    const options = {
      search: '',
      category: 'type:type-b',
      vendor: 'all',
      group: 'group-b',
      quotaType: 'all',
      endpointType: 'all',
      tag: 'all',
      sortBy: 'input-price-low',
    }
    const expensive = {
      ...models[0],
      model_name: 'expensive',
      model_ratio: 1,
      group_ratio: { 'group-a': 0.01, 'group-b': 5 },
    }
    const cheap = {
      ...models[0],
      model_name: 'cheap',
      model_ratio: 2,
      group_ratio: { 'group-a': 10, 'group-b': 1 },
    }
    expect(
      filterAndSortModels([expensive, cheap], options, groups).map(
        (model) => model.model_name
      )
    ).toEqual(['cheap', 'expensive'])
    const expression = {
      ...expensive,
      billing_mode: 'tiered_expr',
      billing_expr: 'tier("base", p * 2 + c * 2)',
    }
    expect(
      filterAndSortModels([expression, cheap], options, groups).map(
        (model) => model.model_name
      )
    ).toEqual(['cheap', 'expensive'])
    for (const overrides of [
      { quota_type: 1, model_price: 1 },
      { billing_mode: 'tiered_expr', billing_expr: 'tier("fixed", fixed(1))' },
      {
        billing_mode: 'tiered_expr',
        billing_expr: 'tier("task", u("seconds") * 1)',
        billing_usage_schema: { seconds: { type: 'number', unit: 'second' } },
        billing_usage_examples: [
          { label: 'One second', facts: { seconds: 1 } },
        ],
      },
    ] satisfies Partial<PricingModel>[]) {
      const a = { ...expensive, ...overrides }
      const b = { ...cheap, ...overrides }
      expect(
        filterAndSortModels(
          [a, b],
          { ...options, sortBy: 'request-price-low' },
          groups
        ).map((model) => model.model_name)
      ).toEqual(['cheap', 'expensive'])
    }
  })

  it('preserves free ratios and does not turn a missing group price into a configured price', () => {
    expect(formatRequestPrice({ ...models[0], quota_type: 1 })).toBe('-')
    expect(
      getDisplayGroupRatio(
        { ...models[0], group_ratio: { 'group-a': 0 } },
        'group-a'
      )
    ).toBe(0)
    expect(
      getDisplayGroupRatio({ ...models[0], group_ratio: {} }, 'group-a')
    ).toBeNaN()
  })
})
