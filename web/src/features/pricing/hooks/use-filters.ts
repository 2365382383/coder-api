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
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useMemo, useCallback, useEffect, useState } from 'react'

import { useDebounce } from '@/hooks/use-debounce'

import {
  FILTER_ALL,
  SORT_OPTIONS,
  QUOTA_TYPES,
  ENDPOINT_TYPES,
  DEFAULT_TOKEN_UNIT,
  VIEW_MODES,
  type ViewMode,
} from '../constants'
import { filterAndSortModels, extractAllTags } from '../lib/filters'
import {
  ALL_CATEGORIES,
  UNCLASSIFIED_CATEGORY,
  categoryTypeID,
  categoryValue,
  getGroupCategoryID,
  normalizePricingSelection,
} from '../lib/group-categories'
import type {
  PricingGroup,
  PricingGroupType,
  PricingModel,
  TokenUnit,
} from '../types'

export function useFilters(
  models: PricingModel[],
  groupTypes: PricingGroupType[],
  groups: PricingGroup[],
  catalogReady: boolean,
  hasCatalog: boolean
) {
  const search = useSearch({ from: '/pricing/' })
  const navigate = useNavigate({ from: '/pricing/' })
  const [filtersAdjusted, setFiltersAdjusted] = useState(false)
  const selection = useMemo(
    () =>
      catalogReady
        ? normalizePricingSelection(search, groupTypes, groups, hasCatalog)
        : search,
    [catalogReady, search, groupTypes, groups, hasCatalog]
  )
  let categoryFilter = ALL_CATEGORIES
  if (selection.unclassified) categoryFilter = UNCLASSIFIED_CATEGORY
  if (selection.category) categoryFilter = categoryValue(selection.category)
  const searchInput = search.search || ''
  const debouncedSearchInput = useDebounce(searchInput, 200)
  const sortBy = search.sort || SORT_OPTIONS.NAME
  const vendorFilter = search.vendor || FILTER_ALL
  const groupFilter = selection.group || FILTER_ALL
  const quotaTypeFilter = search.quotaType || QUOTA_TYPES.ALL
  const endpointTypeFilter = search.endpointType || ENDPOINT_TYPES.ALL
  const tagFilter = search.tag || FILTER_ALL
  const tokenUnit: TokenUnit =
    search.tokenUnit === 'K' ? 'K' : DEFAULT_TOKEN_UNIT
  const viewMode: ViewMode =
    search.view === VIEW_MODES.TABLE ? VIEW_MODES.TABLE : VIEW_MODES.CARD
  const showRechargePrice = search.rechargePrice === true

  useEffect(() => {
    if (!catalogReady) return
    if (
      selection.category === search.category &&
      selection.group === search.group &&
      selection.unclassified === search.unclassified
    ) {
      return
    }
    setFiltersAdjusted(true)
    void navigate({
      replace: true,
      search: (previous) => ({ ...previous, ...selection }),
    })
  }, [catalogReady, selection, search, navigate])

  const updateFilters = useCallback(
    (updates: Partial<typeof search>, replace = false) => {
      setFiltersAdjusted(false)
      void navigate({
        replace,
        search: (previous) => ({ ...previous, ...updates }),
      })
    },
    [navigate]
  )
  const setSearchInput = useCallback(
    (value: string) => updateFilters({ search: value || undefined }, true),
    [updateFilters]
  )
  const setSortBy = useCallback(
    (value: string) =>
      updateFilters({ sort: value === SORT_OPTIONS.NAME ? undefined : value }),
    [updateFilters]
  )
  const setCategoryFilter = useCallback(
    (value: string) => {
      const category = categoryTypeID(value)
      const unclassified = value === UNCLASSIFIED_CATEGORY || undefined
      const group =
        search.group && getGroupCategoryID(search.group, groups) === value
          ? search.group
          : undefined
      updateFilters({ category, unclassified, group })
    },
    [search.group, groups, updateFilters]
  )
  const setGroupFilter = useCallback(
    (value: string) => {
      if (value === FILTER_ALL) {
        updateFilters({ group: undefined })
        return
      }
      const groupCategory = hasCatalog
        ? getGroupCategoryID(value, groups)
        : undefined
      updateFilters({
        group: value,
        category: groupCategory ? categoryTypeID(groupCategory) : undefined,
        unclassified: groupCategory === UNCLASSIFIED_CATEGORY || undefined,
      })
    },
    [groups, hasCatalog, updateFilters]
  )
  const setVendorFilter = useCallback(
    (value: string) =>
      updateFilters({ vendor: value === FILTER_ALL ? undefined : value }),
    [updateFilters]
  )
  const setQuotaTypeFilter = useCallback(
    (value: string) =>
      updateFilters({
        quotaType: value === QUOTA_TYPES.ALL ? undefined : value,
      }),
    [updateFilters]
  )
  const setEndpointTypeFilter = useCallback(
    (value: string) =>
      updateFilters({
        endpointType: value === ENDPOINT_TYPES.ALL ? undefined : value,
      }),
    [updateFilters]
  )
  const setTagFilter = useCallback(
    (value: string) =>
      updateFilters({ tag: value === FILTER_ALL ? undefined : value }),
    [updateFilters]
  )
  const setTokenUnit = useCallback(
    (value: TokenUnit) =>
      updateFilters({
        tokenUnit: value === DEFAULT_TOKEN_UNIT ? undefined : value,
      }),
    [updateFilters]
  )
  const setViewMode = useCallback(
    (value: ViewMode) =>
      updateFilters({ view: value === VIEW_MODES.CARD ? undefined : value }),
    [updateFilters]
  )
  const setShowRechargePrice = useCallback(
    (value: boolean) => updateFilters({ rechargePrice: value || undefined }),
    [updateFilters]
  )
  const filteredModels = useMemo(
    () =>
      filterAndSortModels(
        models,
        {
          search: debouncedSearchInput,
          category: categoryFilter,
          vendor: vendorFilter,
          group: groupFilter,
          quotaType: quotaTypeFilter,
          endpointType: endpointTypeFilter,
          tag: tagFilter,
          sortBy,
        },
        groups
      ),
    [
      models,
      groups,
      debouncedSearchInput,
      categoryFilter,
      vendorFilter,
      groupFilter,
      quotaTypeFilter,
      endpointTypeFilter,
      tagFilter,
      sortBy,
    ]
  )
  const activeFilterCount =
    Number(categoryFilter !== ALL_CATEGORIES) +
    Number(groupFilter !== FILTER_ALL) +
    Number(vendorFilter !== FILTER_ALL) +
    Number(quotaTypeFilter !== QUOTA_TYPES.ALL) +
    Number(endpointTypeFilter !== ENDPOINT_TYPES.ALL) +
    Number(tagFilter !== FILTER_ALL)
  const availableTags = useMemo(() => extractAllTags(models), [models])
  const clearFilters = useCallback(
    () =>
      updateFilters({
        category: undefined,
        unclassified: undefined,
        group: undefined,
        vendor: undefined,
        quotaType: undefined,
        endpointType: undefined,
        tag: undefined,
      }),
    [updateFilters]
  )
  const clearSearch = useCallback(
    () => updateFilters({ search: undefined }, true),
    [updateFilters]
  )
  const clearAll = useCallback(
    () =>
      updateFilters({
        search: undefined,
        category: undefined,
        unclassified: undefined,
        group: undefined,
        vendor: undefined,
        quotaType: undefined,
        endpointType: undefined,
        tag: undefined,
      }),
    [updateFilters]
  )

  return {
    searchInput,
    sortBy,
    categoryFilter,
    vendorFilter,
    groupFilter,
    quotaTypeFilter,
    endpointTypeFilter,
    tagFilter,
    tokenUnit,
    viewMode,
    showRechargePrice,
    setSearchInput,
    setSortBy,
    setCategoryFilter,
    setVendorFilter,
    setGroupFilter,
    setQuotaTypeFilter,
    setEndpointTypeFilter,
    setTagFilter,
    setTokenUnit,
    setViewMode,
    setShowRechargePrice,
    filteredModels,
    hasActiveFilters: activeFilterCount > 0,
    activeFilterCount,
    availableTags,
    clearFilters,
    clearSearch,
    clearAll,
    filtersAdjusted,
  }
}
