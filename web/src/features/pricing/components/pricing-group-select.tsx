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
import { useTranslation } from 'react-i18next'

import { Combobox } from '@/components/ui/combobox'
import { toIntlLocale } from '@/i18n/languages'

import type { PricingGroup } from '../types'

export function PricingGroupSelect(props: {
  groups: PricingGroup[]
  value: string
  onChange: (group: string) => void
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const ratioFormatter = new Intl.NumberFormat(locale, {
    maximumFractionDigits: 12,
  })
  const current = props.groups.find((group) => group.name === props.value)

  return (
    <div className='bg-card min-w-0 space-y-2 rounded-xl border p-3'>
      <div className='flex min-w-0 flex-col gap-2 sm:flex-row sm:items-center'>
        <span className='text-muted-foreground shrink-0 text-sm'>
          {t('Group')}
        </span>
        <Combobox
          aria-label={t('Group')}
          className='w-full min-w-0 sm:max-w-lg'
          value={props.value}
          onValueChange={(value) => {
            if (value !== null) props.onChange(value)
          }}
          options={[
            { value: 'all', label: t('All Groups') },
            ...props.groups.map((group) => ({
              value: group.name,
              label: Number.isFinite(group.ratio)
                ? `${group.name} · ×${ratioFormatter.format(group.ratio)}`
                : group.name,
              description: group.description,
            })),
          ]}
        />
      </div>
      {current?.description && (
        <p className='text-muted-foreground text-sm break-words whitespace-pre-wrap'>
          {current.description}
        </p>
      )}
    </div>
  )
}
