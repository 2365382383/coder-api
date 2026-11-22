/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.
*/
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

const { getApiKeyModelsMock } = vi.hoisted(() => ({
  getApiKeyModelsMock: vi.fn(),
}))

vi.mock('../../../api', () => ({ getApiKeyModels: getApiKeyModelsMock }))

const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { CCSwitchDialog } = await import('../cc-switch-dialog')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        Application: 'Application',
        'Antigravity CLI': 'Antigravity CLI',
        Cancel: 'Cancel',
        'Claude Code': 'Claude Code',
        'Claude Desktop': 'Claude Desktop',
        Codex: 'Codex',
        'DeepSeek Harness': 'DeepSeek Harness',
        'GitHub Copilot CLI': 'GitHub Copilot CLI',
        'Grok Build': 'Grok Build',
        Gemini: 'Gemini',
        Hermes: 'Hermes',
        'Import to CC Switch': 'Import to CC Switch',
        'Kimi Code': 'Kimi Code',
        'MiniMax Code': 'MiniMax Code',
        Name: 'Name',
        'No models found': 'No models found',
        'Open CC Switch': 'Open CC Switch',
        OpenClaw: 'OpenClaw',
        OpenCode: 'OpenCode',
        'Oh My Pi': 'Oh My Pi',
        Pi: 'Pi',
        'Primary Model': 'Primary Model',
        'Select or enter model name': 'Select or enter model name',
        'Haiku Model': 'Haiku Model',
        'Sonnet Model': 'Sonnet Model',
        'Opus Model': 'Opus Model',
        'Use the latest version of CC Switch':
          'Use the latest version of CC Switch',
        WorkBuddy: 'WorkBuddy',
      },
    },
  },
})

afterEach(() => {
  localStorage.clear()
  vi.clearAllMocks()
})

beforeEach(() => {
  getApiKeyModelsMock.mockResolvedValue(['gpt-5.4', 'claude-sonnet-4-6'])
})

function renderDialog(
  apiKey: { id: number } = { id: 42 },
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  })
) {
  render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <CCSwitchDialog
          open
          onOpenChange={() => undefined}
          tokenKey='token-value'
          apiKey={apiKey}
        />
      </I18nextProvider>
    </QueryClientProvider>
  )
  return queryClient
}

describe('CC Switch dialog', () => {
  test('shows every CC Switch application supported by the current protocol', () => {
    renderDialog()

    for (const app of [
      'Antigravity CLI',
      'Claude Code',
      'Claude Desktop',
      'Codex',
      'DeepSeek Harness',
      'Gemini',
      'GitHub Copilot CLI',
      'Grok Build',
      'Hermes',
      'Kimi Code',
      'MiniMax Code',
      'Oh My Pi',
      'OpenClaw',
      'OpenCode',
      'Pi',
      'WorkBuddy',
    ]) {
      expect(screen.getByRole('radio', { name: app })).toBeInTheDocument()
    }
  })

  test.each(['Claude Code', 'Claude Desktop', 'Codex', 'Gemini'])(
    'opens %s models outside the clipping dialog and keeps the dialog open after selection',
    async (app) => {
      renderDialog()
      const user = userEvent.setup()
      await user.click(screen.getByRole('radio', { name: app }))
      const input = screen.getAllByRole('combobox')[0]

      await user.click(input)

      expect(input).toHaveAttribute('aria-expanded', 'true')
      const list = await screen.findByRole('listbox')
      const dialog = screen.getByRole('dialog', { name: 'Import to CC Switch' })
      expect(dialog).not.toContainElement(list)
      await user.click(screen.getByRole('option', { name: 'gpt-5.4' }))
      await waitFor(() => expect(input).toHaveValue('gpt-5.4'))
      expect(input).toHaveAttribute('aria-expanded', 'false')
      expect(dialog).toBeVisible()
    }
  )

  test('filters model names and supports keyboard selection and Escape without closing the dialog', async () => {
    renderDialog()
    const user = userEvent.setup()
    await user.click(screen.getByRole('radio', { name: 'Codex' }))
    const input = screen.getAllByRole('combobox')[0]
    await user.click(input)
    await user.type(input, 'sonnet')

    expect(
      screen.queryByRole('option', { name: 'gpt-5.4' })
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('option', { name: 'claude-sonnet-4-6' })
    ).toBeVisible()
    await user.keyboard('{ArrowDown}{Enter}')
    await waitFor(() => expect(input).toHaveValue('claude-sonnet-4-6'))
    await user.click(input)
    await user.keyboard('{Escape}')

    expect(input).toHaveValue('claude-sonnet-4-6')
    expect(input).toHaveAttribute('aria-expanded', 'false')
    expect(screen.getByRole('dialog')).toBeVisible()
  })

  test('shows an empty result when no models are available and updates an open dropdown when models arrive', async () => {
    getApiKeyModelsMock.mockResolvedValue([])
    const queryClient = renderDialog()
    const user = userEvent.setup()
    await user.click(screen.getByRole('radio', { name: 'Codex' }))
    const input = screen.getAllByRole('combobox')[0]
    await user.click(input)

    expect(await screen.findByText('No models found')).toBeVisible()
    queryClient.setQueryData(['cc-switch-models', 42], ['gpt-5.4'])
    await user.click(await screen.findByRole('option', { name: 'gpt-5.4' }))
    await waitFor(() => expect(input).toHaveValue('gpt-5.4'))
  })

  test('lets users edit the provider name without opening a model dropdown', async () => {
    renderDialog()
    const user = userEvent.setup()
    const input = screen.getByRole('textbox', { name: 'Name' })

    await user.clear(input)
    await user.type(input, 'Development')

    expect(input).toHaveValue('Development')
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
  })

  test('shows the selected API key models rather than a global cached catalog', async () => {
    getApiKeyModelsMock.mockResolvedValue(['key-group-model', 'model-b'])
    renderDialog({ id: 42 })

    const input = await screen.findByRole('combobox', { name: 'Primary Model' })
    await waitFor(() => expect(input).not.toBeDisabled())
    const group = input.parentElement
    if (!group) throw new Error('Expected a model input group')
    fireEvent.click(within(group).getByRole('button'))

    expect(getApiKeyModelsMock).toHaveBeenCalledWith('token-value')
    expect(await screen.findByText('key-group-model')).toBeInTheDocument()
    expect(screen.queryByText('gpt-5.4')).not.toBeInTheDocument()
  })

  test('hides stale selected-key models while refreshing and after load failure', async () => {
    let resolveModels!: (models: string[]) => void
    getApiKeyModelsMock.mockImplementation(
      () => new Promise<string[]>((resolve) => (resolveModels = resolve))
    )
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: Infinity } },
    })
    queryClient.setQueryData(['cc-switch-models', 42], ['wrong-group-model'])
    renderDialog({ id: 42 }, queryClient)

    const input = await screen.findByRole('combobox', { name: 'Primary Model' })
    expect(input).toBeDisabled()
    expect(
      screen.getByRole('button', { name: 'Open CC Switch' })
    ).toBeDisabled()
    resolveModels(['key-group-model'])

    await waitFor(() => expect(input).not.toBeDisabled())
    const group = input.parentElement
    if (!group) throw new Error('Expected a model input group')
    fireEvent.click(within(group).getByRole('button'))
    expect(await screen.findByText('key-group-model')).toBeInTheDocument()
    expect(screen.queryByText('wrong-group-model')).not.toBeInTheDocument()

    getApiKeyModelsMock.mockRejectedValue(new Error('models unavailable'))
    queryClient.invalidateQueries({ queryKey: ['cc-switch-models', 42] })
    await waitFor(() => expect(input).toBeDisabled())
    expect(
      screen.getByRole('button', { name: 'Open CC Switch' })
    ).toBeDisabled()
  })

  test('builds a Claude Desktop link with scoped models', async () => {
    renderDialog()
    const user = userEvent.setup()
    await user.click(screen.getByRole('radio', { name: 'Claude Desktop' }))

    expect(
      screen.getByText('Use the latest version of CC Switch')
    ).toBeVisible()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(
      screen.getByRole('combobox', { name: 'Primary Model' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('combobox', { name: 'Haiku Model' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('combobox', { name: 'Sonnet Model' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('combobox', { name: 'Opus Model' })
    ).toBeInTheDocument()

    const primaryModel = screen.getByRole('combobox', {
      name: 'Primary Model',
    })
    await waitFor(() => expect(primaryModel).not.toBeDisabled())
    await user.click(primaryModel)
    await user.click(
      await screen.findByRole('option', { name: 'claude-sonnet-4-6' })
    )

    const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)
    await user.click(screen.getByRole('button', { name: 'Open CC Switch' }))

    await waitFor(() => expect(openSpy).toHaveBeenCalledOnce())
    const openedURL = openSpy.mock.calls[0]?.[0]
    if (typeof openedURL !== 'string') {
      throw new Error('Expected window.open to receive a CC Switch URL')
    }
    const params = new URL(openedURL).searchParams
    expect(params.get('app')).toBe('claude-desktop')
    expect(params.get('endpoint')).toBe(window.location.origin)
    expect(params.get('apiFormat')).toBe('anthropic')
    expect(params.get('model')).toBe('claude-sonnet-4-6')
    expect(params.get('enabled')).toBe('true')
  })
})
