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

export type CCSwitchApp =
  | 'agy'
  | 'claude'
  | 'claude-desktop'
  | 'codex'
  | 'gemini'
  | 'grokbuild'
  | 'opencode'
  | 'openclaw'
  | 'hermes'
  | 'pi'
  | 'mcode'
  | 'kimi'
  | 'dsh'
  | 'omp'
  | 'copilot'
  | 'workbuddy'

type CCSwitchApiFormat =
  | 'anthropic'
  | 'openai_chat'
  | 'openai_responses'
  | 'gemini_native'

export type CCSwitchModelField = {
  key: string
  labelKey: string
  required: boolean
}

export type CCSwitchSource = {
  appId: CCSwitchApp
  labelKey: string
  defaultName: string
  endpointSuffix: string
  modelFields: readonly CCSwitchModelField[]
  apiFormat?: CCSwitchApiFormat
}

const PRIMARY_MODEL_FIELD: CCSwitchModelField = {
  key: 'model',
  labelKey: 'Primary Model',
  required: true,
}

const SINGLE_MODEL_FIELDS = [PRIMARY_MODEL_FIELD] as const

const CLAUDE_MODEL_FIELDS = [
  PRIMARY_MODEL_FIELD,
  { key: 'haikuModel', labelKey: 'Haiku Model', required: false },
  { key: 'sonnetModel', labelKey: 'Sonnet Model', required: false },
  { key: 'opusModel', labelKey: 'Opus Model', required: false },
] as const

export const CCSWITCH_SOURCES: readonly CCSwitchSource[] = [
  {
    appId: 'claude',
    labelKey: 'Claude Code',
    defaultName: 'coder',
    endpointSuffix: '',
    modelFields: CLAUDE_MODEL_FIELDS,
  },
  {
    appId: 'claude-desktop',
    labelKey: 'Claude Desktop',
    defaultName: 'coder',
    endpointSuffix: '',
    modelFields: CLAUDE_MODEL_FIELDS,
    apiFormat: 'anthropic',
  },
  {
    appId: 'codex',
    labelKey: 'Codex',
    defaultName: 'coder',
    endpointSuffix: '/v1',
    modelFields: SINGLE_MODEL_FIELDS,
  },
  {
    appId: 'gemini',
    labelKey: 'Gemini',
    defaultName: 'coder',
    endpointSuffix: '',
    modelFields: SINGLE_MODEL_FIELDS,
  },
  {
    appId: 'agy',
    labelKey: 'Antigravity CLI',
    defaultName: 'coder',
    endpointSuffix: '',
    modelFields: SINGLE_MODEL_FIELDS,
    apiFormat: 'gemini_native',
  },
  {
    appId: 'grokbuild',
    labelKey: 'Grok Build',
    defaultName: 'coder',
    endpointSuffix: '/v1',
    modelFields: SINGLE_MODEL_FIELDS,
  },
  {
    appId: 'opencode',
    labelKey: 'OpenCode',
    defaultName: 'coder',
    endpointSuffix: '/v1',
    modelFields: SINGLE_MODEL_FIELDS,
  },
  {
    appId: 'openclaw',
    labelKey: 'OpenClaw',
    defaultName: 'coder',
    endpointSuffix: '/v1',
    modelFields: SINGLE_MODEL_FIELDS,
  },
  {
    appId: 'hermes',
    labelKey: 'Hermes',
    defaultName: 'coder',
    endpointSuffix: '/v1',
    modelFields: SINGLE_MODEL_FIELDS,
  },
  {
    appId: 'pi',
    labelKey: 'Pi',
    defaultName: 'coder',
    endpointSuffix: '/v1',
    modelFields: SINGLE_MODEL_FIELDS,
    apiFormat: 'openai_chat',
  },
  {
    appId: 'mcode',
    labelKey: 'MiniMax Code',
    defaultName: 'coder',
    endpointSuffix: '/v1',
    modelFields: SINGLE_MODEL_FIELDS,
    apiFormat: 'openai_chat',
  },
  {
    appId: 'kimi',
    labelKey: 'Kimi Code',
    defaultName: 'coder',
    endpointSuffix: '/v1',
    modelFields: SINGLE_MODEL_FIELDS,
    apiFormat: 'openai_chat',
  },
  {
    appId: 'dsh',
    labelKey: 'DeepSeek Harness',
    defaultName: 'coder',
    endpointSuffix: '/v1',
    modelFields: SINGLE_MODEL_FIELDS,
    apiFormat: 'openai_chat',
  },
  {
    appId: 'omp',
    labelKey: 'Oh My Pi',
    defaultName: 'coder',
    endpointSuffix: '/v1',
    modelFields: SINGLE_MODEL_FIELDS,
    apiFormat: 'openai_chat',
  },
  {
    appId: 'copilot',
    labelKey: 'GitHub Copilot CLI',
    defaultName: 'coder',
    endpointSuffix: '/v1',
    modelFields: SINGLE_MODEL_FIELDS,
    apiFormat: 'openai_chat',
  },
  {
    appId: 'workbuddy',
    labelKey: 'WorkBuddy',
    defaultName: 'coder',
    endpointSuffix: '/v1/chat/completions',
    modelFields: SINGLE_MODEL_FIELDS,
    apiFormat: 'openai_chat',
  },
] as const

export function normalizeServerAddress(address: string): string {
  const trimmed = address.trim().replace(/\/+$/, '')
  return trimmed.replace(/(?:\/+v1)+$/i, '')
}

export function buildCCSwitchURL(
  source: CCSwitchSource,
  name: string,
  models: Record<string, string>,
  apiKey: string,
  serverAddress: string
): string | null {
  const normalizedAddress = normalizeServerAddress(serverAddress)
  const endpoint = normalizedAddress + source.endpointSuffix
  const params = new URLSearchParams()
  params.set('resource', 'provider')
  params.set('app', source.appId)
  params.set('name', name)
  params.set('endpoint', endpoint)
  params.set('apiKey', apiKey)
  if (source.apiFormat) params.set('apiFormat', source.apiFormat)
  for (const field of source.modelFields) {
    const value = models[field.key]?.trim()
    if (field.required && !value) return null
    if (value) params.set(field.key, value)
  }
  params.set('homepage', normalizedAddress)
  params.set('enabled', 'true')
  return `ccswitch://v1/import?${params.toString()}`
}
