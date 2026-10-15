import type { Channel, ModelTestRuntimeConfig } from '../types'

export const DEFAULT_MODEL_TEST_ENDPOINT_TYPE = 'openai-response'

export const DEFAULT_MODEL_TEST_RUNTIME_CONFIG: ModelTestRuntimeConfig = {
  enabled: true,
  headerConfig: true,
  paramOverride: true,
  proxy: true,
  modelMapping: true,
  responseProtocol: 'native',
  testPrompt: 'hi',
  maxTokens: 16,
}

export function normalizeModelTestRuntimeConfig(
  config?: Partial<ModelTestRuntimeConfig> | null
): ModelTestRuntimeConfig {
  const normalized: ModelTestRuntimeConfig = {
    ...DEFAULT_MODEL_TEST_RUNTIME_CONFIG,
    ...(config ?? {}),
  }

  normalized.enabled = normalized.enabled !== false
  normalized.headerConfig = normalized.headerConfig !== false
  normalized.paramOverride = normalized.paramOverride !== false
  normalized.proxy = normalized.proxy !== false
  normalized.modelMapping = normalized.modelMapping !== false

  if (
    normalized.responseProtocol !== 'native' &&
    normalized.responseProtocol !== 'chat_completions'
  ) {
    normalized.responseProtocol =
      DEFAULT_MODEL_TEST_RUNTIME_CONFIG.responseProtocol
  }

  const prompt =
    typeof normalized.testPrompt === 'string'
      ? normalized.testPrompt.trim()
      : ''
  normalized.testPrompt = prompt || DEFAULT_MODEL_TEST_RUNTIME_CONFIG.testPrompt

  const maxTokens = Number(normalized.maxTokens)
  normalized.maxTokens =
    Number.isFinite(maxTokens) && maxTokens > 0
      ? Math.min(Math.floor(maxTokens), 8192)
      : DEFAULT_MODEL_TEST_RUNTIME_CONFIG.maxTokens

  return normalized
}

export function buildModelTestRuntimeParams(
  config?: Partial<ModelTestRuntimeConfig> | null
): Record<string, string> {
  const normalized = normalizeModelTestRuntimeConfig(config)
  return {
    runtime_config: String(normalized.enabled),
    header_config: String(normalized.headerConfig),
    param_override: String(normalized.paramOverride),
    proxy: String(normalized.proxy),
    model_mapping: String(normalized.modelMapping),
    response_protocol: normalized.responseProtocol,
    test_prompt: normalized.testPrompt,
    max_tokens: String(normalized.maxTokens),
  }
}

type ParsedObject = Record<string, unknown>

function parseObject(value: unknown): ParsedObject {
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    return value as ParsedObject
  }
  if (typeof value !== 'string' || !value.trim()) return {}
  try {
    const parsed: unknown = JSON.parse(value)
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed)
      ? (parsed as ParsedObject)
      : {}
  } catch {
    return {}
  }
}

function hasConfigText(value: unknown): boolean {
  return (
    typeof value === 'string' && value.trim() !== '' && value.trim() !== '{}'
  )
}

export type ModelTestRuntimeSnapshot = {
  headerConfigured: boolean
  headerValue: string
  paramConfigured: boolean
  paramValue: string
  proxyConfigured: boolean
  proxyValue: string
  modelMappingConfigured: boolean
  modelMappingValue: string
}

function compactList(values: string[], limit = 3): string {
  const items = values.map((value) => value.trim()).filter(Boolean)
  if (items.length <= limit) return items.join(', ')
  return `${items.slice(0, limit).join(', ')} +${items.length - limit}`
}

function maskProxyValue(value: string): string {
  const proxy = value.trim()
  if (!proxy) return ''
  try {
    const parsed = new URL(proxy)
    return `${parsed.protocol}//${parsed.host}`
  } catch {
    return 'configured'
  }
}

function summarizeParamOverride(value: unknown): string {
  if (!hasConfigText(value)) return ''
  const config = parseObject(value)
  const operations = Array.isArray(config.operations)
    ? config.operations.filter((operation): operation is ParsedObject =>
        Boolean(
          operation &&
          typeof operation === 'object' &&
          !Array.isArray(operation)
        )
      )
    : []
  if (operations.length > 0) {
    const modes = compactList(
      operations.map((operation) => String(operation.mode ?? '')),
      4
    )
    return modes
      ? `${operations.length} rules | ${modes}`
      : `${operations.length} rules`
  }
  return compactList(Object.keys(config))
}

function summarizeModelMapping(value: unknown): string {
  if (!hasConfigText(value)) return ''
  const config = parseObject(value)
  const entries = Object.entries(config).filter(
    ([from, to]) => from.trim() !== '' && String(to).trim() !== ''
  )
  if (entries.length === 0) return 'configured'
  const values = entries
    .slice(0, 2)
    .map(([from, to]) => `${from} -> ${String(to)}`)
  if (entries.length > 2) values.push(`+${entries.length - 2}`)
  return values.join(', ')
}

export function getModelTestRuntimeSnapshot(
  channel: Channel | null | undefined
): ModelTestRuntimeSnapshot {
  const setting = parseObject(channel?.setting)
  const settings = parseObject(channel?.settings)
  const headerProfileStrategy = parseObject(settings.header_profile_strategy)
  const selectedProfileIds = Array.isArray(
    headerProfileStrategy.selected_profile_ids
  )
    ? headerProfileStrategy.selected_profile_ids.map(String)
    : []
  const headerPolicyConfigured =
    typeof settings.header_policy_mode === 'string' &&
    settings.header_policy_mode.trim() !== ''
  const legacyHeaderPolicyConfigured =
    settings.override_header_user_agent === true ||
    (typeof settings.ua_strategy === 'string'
      ? settings.ua_strategy.trim() !== ''
      : settings.ua_strategy != null && settings.ua_strategy !== false)
  const headerProfileConfigured =
    headerProfileStrategy.enabled === true && selectedProfileIds.length > 0

  return {
    headerConfigured:
      headerProfileConfigured ||
      hasConfigText(channel?.header_override) ||
      headerPolicyConfigured ||
      legacyHeaderPolicyConfigured,
    headerValue: headerProfileConfigured
      ? `Header Profile: ${compactList(selectedProfileIds)}`
      : hasConfigText(channel?.header_override)
        ? 'header_override'
        : headerPolicyConfigured
          ? `header policy: ${String(settings.header_policy_mode)}`
          : legacyHeaderPolicyConfigured
            ? 'legacy UA policy'
            : '',
    paramConfigured: hasConfigText(channel?.param_override),
    paramValue: summarizeParamOverride(channel?.param_override),
    proxyConfigured:
      typeof setting.proxy === 'string' && setting.proxy.trim() !== '',
    proxyValue: maskProxyValue(
      typeof setting.proxy === 'string' ? setting.proxy : ''
    ),
    modelMappingConfigured: hasConfigText(channel?.model_mapping),
    modelMappingValue: summarizeModelMapping(channel?.model_mapping),
  }
}
