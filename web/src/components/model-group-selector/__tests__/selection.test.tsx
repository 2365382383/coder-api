/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or (at your
option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

import { ModelGroupSelector } from '../../model-group-selector'

const modelLabel = 'claude-opus-4-8-with-a-long-provider-suffix'
const groupLabel = 'Claude-Kiro-with-a-long-group-name'

test('keeps complete model and group labels available from the desktop selector', async () => {
  const user = userEvent.setup()

  render(
    <ModelGroupSelector
      groups={[{ label: groupLabel, value: groupLabel }]}
      models={[{ label: modelLabel, value: modelLabel }]}
      onGroupChange={vi.fn()}
      onModelChange={vi.fn()}
      selectedGroup={groupLabel}
      selectedModel={modelLabel}
    />
  )

  await user.click(screen.getByRole('combobox'))

  const popover = document.querySelector('[data-slot="popover-content"]')
  expect(popover).toHaveClass('w-[42rem]')

  const modelOption = screen.getByRole('option', { name: modelLabel })
  const modelLabelElement = within(modelOption).getByTitle(modelLabel)
  expect(modelLabelElement).toBeInTheDocument()
  await user.hover(modelLabelElement)
  expect(
    await screen.findByText(modelLabel, {
      selector: '[data-slot="tooltip-content"]',
    })
  ).toBeVisible()

  const groupButton = screen
    .getAllByRole('button', { name: groupLabel })
    .find((button) => popover?.contains(button))
  expect(groupButton).toBeDefined()
  expect(groupButton).toHaveAttribute('title', groupLabel)
})
