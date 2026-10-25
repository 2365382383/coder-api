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
import type { GroupTypeInfo } from '@/lib/api'
import { AUTO_GROUP_TYPE_ID } from '@/lib/group-types'

import type { ApiKeyGroupOption } from '../components/api-key-group-combobox'

export { AUTO_GROUP_TYPE_ID } from '@/lib/group-types'

export type ApiKeyGroupTypeOption = GroupTypeInfo & {
  id: string
  name: string
  order: number
  groups: ApiKeyGroupOption[]
}

function groupRatioValue(group: ApiKeyGroupOption): number | null {
  const ratio = group.ratio
  if (typeof ratio === 'number') {
    return Number.isFinite(ratio) ? ratio : null
  }
  if (typeof ratio !== 'string' || !ratio.trim()) return null
  const value = Number(ratio)
  return Number.isFinite(value) ? value : null
}

export function sortGroupOptionsByRatioAscending(
  groups: ApiKeyGroupOption[]
): ApiKeyGroupOption[] {
  return [...groups].sort((a, b) => {
    if (a.value === 'auto' && b.value !== 'auto') return -1
    if (b.value === 'auto' && a.value !== 'auto') return 1
    const aRatio = groupRatioValue(a)
    const bRatio = groupRatioValue(b)
    if (aRatio === null && bRatio === null) {
      return a.value.localeCompare(b.value)
    }
    if (aRatio === null) return 1
    if (bRatio === null) return -1
    if (aRatio !== bRatio) return aRatio - bRatio
    return a.value.localeCompare(b.value)
  })
}

export function buildGroupTypeOptions(
  configuredTypes: GroupTypeInfo[],
  groups: ApiKeyGroupOption[]
): ApiKeyGroupTypeOption[] {
  const groupsByType = new Map<string, ApiKeyGroupOption[]>()
  for (const group of groups) {
    const typeID = group.type?.id
    if (!typeID) continue
    const typeGroups = groupsByType.get(typeID) ?? []
    typeGroups.push(group)
    groupsByType.set(typeID, typeGroups)
  }

  return configuredTypes
    .filter((type) => type.id !== AUTO_GROUP_TYPE_ID)
    .map((type) => ({
      ...type,
      order: type.order ?? 0,
      groups: groupsByType.get(type.id) ?? [],
    }))
    .filter((type) => type.groups.length > 0)
    .sort((a, b) => {
      if (a.order !== b.order) return a.order - b.order
      const nameOrder = a.name.localeCompare(b.name)
      return nameOrder !== 0 ? nameOrder : a.id.localeCompare(b.id)
    })
}

export function pickDefaultGroupForType(
  type: ApiKeyGroupTypeOption
): ApiKeyGroupOption | undefined {
  return (
    type.groups.find((group) => group.value === 'default') ?? type.groups[0]
  )
}
