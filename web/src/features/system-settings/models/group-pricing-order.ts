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
import { safeJsonParse } from '../utils/json-parser'
import {
  parseGroupTypeMap,
  parseGroupTypes,
  serializeGroupTypeMap,
  type GroupTypeDefinition,
  type GroupTypeMap,
} from './group-type'

export type GroupPricingRow = {
  _id: string
  name: string
  ratio: string
  topupRatio: string
  selectable: boolean
  description: string
  typeId: string
}

let groupPricingIdCounter = 0

export function createGroupPricingId() {
  groupPricingIdCounter += 1
  return `gpr_${groupPricingIdCounter}`
}

export function normalizeRatio(value: unknown): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : 1
}

function groupPricingRatioValue(row: GroupPricingRow): number | null {
  const value = row.ratio.trim()
  if (!value) return null
  const ratio = Number(value)
  return Number.isFinite(ratio) ? ratio : null
}

function sortGroupPricingRows(
  rows: GroupPricingRow[],
  typeDefinitions: GroupTypeDefinition[]
): GroupPricingRow[] {
  const typeOrder = new Map(
    typeDefinitions.map((type, index) => [type.id, index])
  )

  return [...rows].sort((a, b) => {
    const aIsAuto = a.name.trim() === 'auto'
    const bIsAuto = b.name.trim() === 'auto'
    if (aIsAuto !== bIsAuto) return aIsAuto ? 1 : -1

    const aTypeOrder = typeOrder.get(a.typeId)
    const bTypeOrder = typeOrder.get(b.typeId)
    if (aTypeOrder === undefined && bTypeOrder !== undefined) return 1
    if (aTypeOrder !== undefined && bTypeOrder === undefined) return -1
    if (aTypeOrder !== bTypeOrder) {
      return (aTypeOrder ?? 0) - (bTypeOrder ?? 0)
    }

    const aRatio = groupPricingRatioValue(a)
    const bRatio = groupPricingRatioValue(b)
    if (aRatio === null && bRatio !== null) return 1
    if (aRatio !== null && bRatio === null) return -1
    if (aRatio !== bRatio) {
      return (aRatio ?? 0) - (bRatio ?? 0)
    }

    const nameOrder = a.name.localeCompare(b.name)
    return nameOrder !== 0 ? nameOrder : a._id.localeCompare(b._id)
  })
}

export function parseRatioMap(value: string): Record<string, number> {
  return safeJsonParse<Record<string, number>>(value, {
    fallback: {},
    silent: true,
  })
}

export function parseUsableMap(value: string): Record<string, string> {
  return safeJsonParse<Record<string, string>>(value, {
    fallback: {},
    silent: true,
  })
}

export function normalizeGroupTypeAssignments(
  value: string,
  validTypeIDs: ReadonlySet<string>
): GroupTypeMap {
  const assignments = parseGroupTypeMap(value)
  const normalized: GroupTypeMap = {}
  for (const [groupName, typeID] of Object.entries(assignments)) {
    if (!validTypeIDs.has(typeID)) continue
    normalized[groupName] = typeID
  }
  return normalized
}

export function buildGroupPricingRows(
  groupRatio: string,
  userUsableGroups: string,
  topupGroupRatio: string,
  groupTypeMap: string,
  validTypeIDs: ReadonlySet<string>
): GroupPricingRow[] {
  const ratioMap = parseRatioMap(groupRatio)
  const usableMap = parseUsableMap(userUsableGroups)
  const topupMap = parseRatioMap(topupGroupRatio)
  const typeAssignments = normalizeGroupTypeAssignments(
    groupTypeMap,
    validTypeIDs
  )
  const names = new Set([
    ...Object.keys(ratioMap),
    ...Object.keys(usableMap),
    ...Object.keys(topupMap),
  ])

  return [...names].map((name) => ({
    _id: createGroupPricingId(),
    name,
    ratio: String(normalizeRatio(ratioMap[name])),
    topupRatio: Object.hasOwn(topupMap, name) ? String(topupMap[name]) : '',
    selectable: Object.hasOwn(usableMap, name),
    description: String(usableMap[name] ?? ''),
    typeId: typeAssignments[name] ?? '',
  }))
}

export function serializeGroupPricingRows(
  rows: GroupPricingRow[],
  validTypeIDs: ReadonlySet<string>
) {
  const groupRatio: Record<string, number> = {}
  const userUsableGroups: Record<string, string> = {}
  const topupGroupRatio: Record<string, number> = {}
  const groupTypeMap: GroupTypeMap = {}

  for (const row of rows) {
    const name = row.name.trim()
    if (!name) continue
    groupRatio[name] = normalizeRatio(row.ratio)
    if (row.selectable) {
      userUsableGroups[name] = row.description
    }
    const topup = row.topupRatio.trim()
    if (topup !== '' && Number.isFinite(Number(topup))) {
      topupGroupRatio[name] = Number(topup)
    }
    if (row.typeId && validTypeIDs.has(row.typeId)) {
      groupTypeMap[name] = row.typeId
    }
  }

  return {
    GroupRatio: JSON.stringify(groupRatio, null, 2),
    UserUsableGroups: JSON.stringify(userUsableGroups, null, 2),
    TopupGroupRatio: JSON.stringify(topupGroupRatio, null, 2),
    GroupTypeMap: serializeGroupTypeMap(groupTypeMap, validTypeIDs),
  }
}

export function sortGroupPricingSettings(settings: {
  GroupRatio: string
  UserUsableGroups: string
  TopupGroupRatio: string
  GroupTypes: string
  GroupTypeMap: string
}) {
  const typeDefinitions = parseGroupTypes(settings.GroupTypes)
  const validTypeIDs = new Set(typeDefinitions.map((type) => type.id))
  const rows = buildGroupPricingRows(
    settings.GroupRatio,
    settings.UserUsableGroups,
    settings.TopupGroupRatio,
    settings.GroupTypeMap,
    validTypeIDs
  )

  return serializeGroupPricingRows(
    sortGroupPricingRows(rows, typeDefinitions),
    validTypeIDs
  )
}
