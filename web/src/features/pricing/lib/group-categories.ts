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
import type { PricingGroup, PricingGroupType, PricingModel } from '../types'
import { withPluginPricing } from './plugin-pricing'

// Navigation values are distinct from administrator-controlled type IDs.
export const ALL_CATEGORIES = 'all'
export const UNCLASSIFIED_CATEGORY = 'unclassified'

export function categoryValue(typeID: string): string {
  return `type:${typeID}`
}

export function categoryTypeID(value: string): string | undefined {
  return value.startsWith('type:') ? value.slice(5) : undefined
}

export function getGroupCategoryID(
  groupName: string,
  groups: PricingGroup[]
): string | undefined {
  const group = groups.find((candidate) => candidate.name === groupName)
  if (!group) return undefined
  return group.type_id === null
    ? UNCLASSIFIED_CATEGORY
    : categoryValue(group.type_id)
}

export function getCategoryGroupNames(
  category: string,
  groups: PricingGroup[]
): string[] {
  if (category === ALL_CATEGORIES) return groups.map((group) => group.name)
  if (category === UNCLASSIFIED_CATEGORY) {
    return groups
      .filter((group) => group.type_id === null)
      .map((group) => group.name)
  }
  const typeID = categoryTypeID(category)
  return groups
    .filter((group) => group.type_id === typeID)
    .map((group) => group.name)
}

export function modelBelongsToCategory(
  model: PricingModel,
  category: string,
  groups: PricingGroup[] = []
): boolean {
  if (category === ALL_CATEGORIES) return true
  if (category === UNCLASSIFIED_CATEGORY) {
    const names = getCategoryGroupNames(category, groups)
    return names.some(
      (name) =>
        model.enable_groups.includes(name) ||
        model.enable_groups.includes('all')
    )
  }
  const typeID = categoryTypeID(category)
  return typeID !== undefined && model.group_type_ids?.includes(typeID) === true
}

export type PricingSelection = {
  category?: string
  unclassified?: boolean
  group?: string
}

export function normalizePricingSelection(
  selection: PricingSelection,
  types: PricingGroupType[],
  groups: PricingGroup[],
  hasCatalog: boolean
): PricingSelection {
  let category = selection.category
  let unclassified = selection.unclassified === true || undefined
  let group = selection.group
  if (!hasCatalog || !types.some((type) => type.id === category)) {
    category = undefined
  }
  if (!hasCatalog || !groups.some((item) => item.type_id === null)) {
    unclassified = undefined
  }
  if (category) unclassified = undefined
  if (
    group === 'auto' ||
    (group && !groups.some((item) => item.name === group))
  ) {
    group = undefined
  }
  if (group && hasCatalog) {
    const assigned = groups.find((item) => item.name === group)
    if (
      (category && assigned?.type_id !== category) ||
      (unclassified && assigned?.type_id !== null)
    ) {
      group = undefined
    } else if (!category && !unclassified && assigned) {
      category = assigned.type_id ?? undefined
      unclassified = assigned.type_id === null || undefined
    }
  }
  return { category, unclassified, group }
}

// This projection is display-only. Preserve the original model for the
// detail comparison; never mutate the shared query result.
export function scopePricingModel(
  model: PricingModel,
  groupNames?: string[]
): PricingModel {
  if (groupNames === undefined && !model.group_plugin_keys) return model
  const candidates =
    groupNames ??
    (model.enable_groups.includes('all')
      ? Object.keys(model.group_ratio ?? {})
      : model.enable_groups)
  const enableGroups = candidates.filter(
    (name) =>
      model.enable_groups.includes(name) || model.enable_groups.includes('all')
  )
  let result: PricingModel = { ...model, enable_groups: enableGroups }
  if (model.group_plugin_keys) {
    const allowed = new Set(
      enableGroups.flatMap((name) => [
        ...(model.group_plugin_keys?.[name] ?? []),
        ...(model.group_plugin_keys?.all ?? []),
      ])
    )
    const variants =
      model.billing_plugin_variants
        ?.filter((variant) => allowed.has(variant.plugin_key))
        .map((variant) => {
          if (!model.group_ratio) return variant
          const ratios = enableGroups
            .filter(
              (name) =>
                model.group_plugin_keys?.[name]?.includes(variant.plugin_key) ||
                model.group_plugin_keys?.all?.includes(variant.plugin_key)
            )
            .map((name) => model.group_ratio?.[name])
            .filter(
              (ratio): ratio is number =>
                typeof ratio === 'number' &&
                Number.isFinite(ratio) &&
                ratio >= 0
            )
          return {
            ...variant,
            display_group_ratio: ratios.length
              ? Math.min(...ratios)
              : Number.NaN,
          }
        }) ?? []
    if (variants.length) {
      result = {
        ...withPluginPricing(result, variants[0]),
        quota_type: model.quota_type,
        billing_plugin_variants: variants,
      }
    } else if (!allowed.size || model.billing_plugin_variants?.length) {
      result = {
        ...result,
        billing_plugin_variants: [],
        billing_usage_schema: undefined,
        billing_usage_examples: undefined,
      }
    }
  }
  if (model.group_endpoint_types && groupNames !== undefined) {
    result.supported_endpoint_types = [
      ...new Set(
        enableGroups.flatMap((name) => [
          ...(model.group_endpoint_types?.[name] ?? []),
          ...(model.group_endpoint_types?.all ?? []),
        ])
      ),
    ]
    if (enableGroups.length === 1) {
      result.image_capabilities =
        model.group_image_capabilities?.[enableGroups[0]] ??
        model.group_image_capabilities?.all
    }
  }
  return result
}
