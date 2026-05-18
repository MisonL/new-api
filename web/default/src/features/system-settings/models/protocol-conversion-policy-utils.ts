export const ENDPOINT_CHAT = 'chat_completions'
export const ENDPOINT_RESPONSES = 'responses'

export type ProtocolEndpoint = typeof ENDPOINT_CHAT | typeof ENDPOINT_RESPONSES

export type ProtocolRule = {
  clientKey: string
  name: string
  enabled: boolean
  source_endpoint: ProtocolEndpoint
  target_endpoint: ProtocolEndpoint
  all_channels: boolean
  channel_ids: number[]
  channel_types: number[]
  model_patterns: string[]
  enable_custom_tool_bridge: boolean
  extra: Record<string, unknown>
  optionsExtra: Record<string, unknown>
}

export type ParsedProtocolPolicy =
  | {
      ok: true
      policyExtra: Record<string, unknown>
      rules: ProtocolRule[]
    }
  | {
      ok: false
      error: string
    }

export type ProtocolPreviewState = {
  channelId: string
  channelType: string
  model: string
}

export type ProtocolPreviewResult = {
  matched: boolean
  reason: string
}

export type DraftTextState = {
  source: string
  value: string
}

const POLICY_FIELDS = new Set([
  'enabled',
  'all_channels',
  'channel_ids',
  'channel_types',
  'model_patterns',
  'rules',
])

const RULE_FIELDS = new Set([
  'name',
  'enabled',
  'source_endpoint',
  'target_endpoint',
  'all_channels',
  'channel_ids',
  'channel_types',
  'model_patterns',
  'options',
])

const OPTION_FIELDS = new Set(['enable_custom_tool_bridge'])

let keyCounter = 0

function nextClientKey() {
  keyCounter += 1
  return `protocol-rule-${Date.now()}-${keyCounter}`
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

function pickExtra(
  value: unknown,
  knownFields: Set<string>
): Record<string, unknown> {
  if (!isRecord(value)) return {}
  return Object.fromEntries(
    Object.entries(value).filter(([key]) => !knownFields.has(key))
  )
}

function toPositiveIntegers(value: unknown): number[] {
  if (!Array.isArray(value)) return []
  return value
    .map((item) =>
      typeof item === 'number' ? item : Number.parseInt(String(item), 10)
    )
    .filter((item) => Number.isInteger(item) && item > 0)
}

function toTextList(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value
    .map((item) => String(item ?? '').trim())
    .filter((item) => item.length > 0)
}

function normalizeEndpoint(value: unknown, fallback: ProtocolEndpoint) {
  return value === ENDPOINT_RESPONSES || value === ENDPOINT_CHAT
    ? value
    : fallback
}

export function isResponsesToChatRule(rule: ProtocolRule) {
  return (
    rule.source_endpoint === ENDPOINT_RESPONSES &&
    rule.target_endpoint === ENDPOINT_CHAT
  )
}

export function getProtocolRuleWarningKeys(rule: ProtocolRule) {
  const warnings: string[] = []
  if (!rule.enabled) warnings.push('Rule is disabled.')
  if (
    !rule.all_channels &&
    rule.channel_ids.length === 0 &&
    rule.channel_types.length === 0
  ) {
    warnings.push('Channel scope is empty. This rule will not match.')
  }
  if (rule.model_patterns.length === 0) {
    warnings.push('Model patterns are empty. This rule will not match.')
  }
  return warnings
}

export function getProtocolPreviewResult(
  rule: ProtocolRule,
  preview: ProtocolPreviewState,
  passThroughEnabled: boolean
): ProtocolPreviewResult {
  if (!rule.enabled) return { matched: false, reason: 'Rule is disabled.' }

  if (
    !rule.all_channels &&
    rule.channel_ids.length === 0 &&
    rule.channel_types.length === 0
  ) {
    return {
      matched: false,
      reason: 'Channel scope is empty. This rule will not match.',
    }
  }

  if (rule.model_patterns.length === 0) {
    return {
      matched: false,
      reason: 'Model patterns are empty. This rule will not match.',
    }
  }

  const channelId = Number.parseInt(preview.channelId, 10)
  const channelType = Number.parseInt(preview.channelType, 10)

  if (!rule.all_channels) {
    const idMatched =
      Number.isInteger(channelId) && rule.channel_ids.includes(channelId)
    const typeMatched =
      Number.isInteger(channelType) && rule.channel_types.includes(channelType)
    if (!idMatched && !typeMatched) {
      return { matched: false, reason: 'Channel scope does not match.' }
    }
  }

  const model = preview.model.trim()
  if (!model) return { matched: false, reason: 'Model is required for preview.' }

  const matched = rule.model_patterns.some((pattern) => {
    try {
      return new RegExp(pattern).test(model)
    } catch {
      return false
    }
  })
  if (!matched) {
    return { matched: false, reason: 'Model pattern does not match.' }
  }

  if (passThroughEnabled) {
    return {
      matched: true,
      reason:
        'Sample request matches this rule, but passthrough will skip conversion.',
    }
  }

  return { matched: true, reason: 'Sample request matches this rule.' }
}

export function createCommittedDraftText(value: string): DraftTextState {
  return { source: value, value }
}

export function createDraftTextChange(
  value: string,
  parsedSource: string
): DraftTextState {
  return { source: parsedSource, value }
}

export function getDraftTextValue(
  draft: DraftTextState,
  source: string
): string {
  return draft.source === source ? draft.value : source
}

export function createProtocolRule(
  overrides: Partial<ProtocolRule> = {}
): ProtocolRule {
  return {
    clientKey: nextClientKey(),
    name: '',
    enabled: true,
    source_endpoint: ENDPOINT_CHAT,
    target_endpoint: ENDPOINT_RESPONSES,
    all_channels: true,
    channel_ids: [],
    channel_types: [],
    model_patterns: [],
    enable_custom_tool_bridge: false,
    extra: {},
    optionsExtra: {},
    ...overrides,
  }
}

function ruleFromRecord(
  value: Record<string, unknown>,
  fallbackName: string
): ProtocolRule {
  const options = isRecord(value.options) ? value.options : {}
  const source = normalizeEndpoint(value.source_endpoint, ENDPOINT_CHAT)
  const targetFallback =
    source === ENDPOINT_CHAT ? ENDPOINT_RESPONSES : ENDPOINT_CHAT
  const target = normalizeEndpoint(value.target_endpoint, targetFallback)
  const normalizedTarget = target === source ? targetFallback : target
  const name = String(value.name || fallbackName)

  return createProtocolRule({
    name,
    enabled: value.enabled !== false,
    source_endpoint: source,
    target_endpoint: normalizedTarget,
    all_channels: value.all_channels !== false,
    channel_ids: toPositiveIntegers(value.channel_ids),
    channel_types: toPositiveIntegers(value.channel_types),
    model_patterns: toTextList(value.model_patterns),
    enable_custom_tool_bridge:
      options.enable_custom_tool_bridge === true ||
      value.enable_custom_tool_bridge === true,
    extra: pickExtra(value, RULE_FIELDS),
    optionsExtra: pickExtra(options, OPTION_FIELDS),
  })
}

function legacyRuleFromPolicy(policy: Record<string, unknown>): ProtocolRule[] {
  if (
    policy.enabled === undefined &&
    policy.all_channels === undefined &&
    policy.channel_ids === undefined &&
    policy.channel_types === undefined &&
    policy.model_patterns === undefined
  ) {
    return []
  }

  return [
    createProtocolRule({
      name: 'chat-completions-to-responses',
      enabled: policy.enabled !== false,
      source_endpoint: ENDPOINT_CHAT,
      target_endpoint: ENDPOINT_RESPONSES,
      all_channels: policy.all_channels === true,
      channel_ids: toPositiveIntegers(policy.channel_ids),
      channel_types: toPositiveIntegers(policy.channel_types),
      model_patterns: toTextList(policy.model_patterns),
    }),
  ]
}

export function parseProtocolPolicy(rawValue: string): ParsedProtocolPolicy {
  const raw = String(rawValue || '').trim()
  if (!raw) {
    return { ok: true, policyExtra: {}, rules: [] }
  }

  try {
    const policy = JSON.parse(raw) as unknown
    if (!isRecord(policy)) {
      return { ok: false, error: 'Policy must be a JSON object' }
    }

    const policyExtra = pickExtra(policy, POLICY_FIELDS)
    const ruleValues = Array.isArray(policy.rules) ? policy.rules : []
    const rules = ruleValues
      .filter(isRecord)
      .map((rule, index) => ruleFromRecord(rule, `Rule ${index + 1}`))

    return {
      ok: true,
      policyExtra,
      rules: rules.length > 0 ? rules : legacyRuleFromPolicy(policy),
    }
  } catch (error) {
    return {
      ok: false,
      error: error instanceof Error ? error.message : 'Invalid JSON format',
    }
  }
}

export function serializeProtocolPolicy(
  rules: ProtocolRule[],
  policyExtra: Record<string, unknown> = {}
) {
  const cleanedRules = rules.map((rule, index) => {
    const payload: Record<string, unknown> = {
      ...rule.extra,
      name: rule.name.trim() || `Rule ${index + 1}`,
      enabled: rule.enabled,
      source_endpoint: rule.source_endpoint,
      target_endpoint: rule.target_endpoint,
      all_channels: rule.all_channels,
    }

    if (!rule.all_channels && rule.channel_ids.length > 0) {
      payload.channel_ids = rule.channel_ids
    }
    if (!rule.all_channels && rule.channel_types.length > 0) {
      payload.channel_types = rule.channel_types
    }
    if (rule.model_patterns.length > 0) {
      payload.model_patterns = rule.model_patterns
    }

    const options = { ...rule.optionsExtra }
    if (rule.enable_custom_tool_bridge && isResponsesToChatRule(rule)) {
      options.enable_custom_tool_bridge = true
    } else {
      delete options.enable_custom_tool_bridge
    }
    if (Object.keys(options).length > 0) {
      payload.options = options
    }

    return payload
  })

  const policy =
    cleanedRules.length > 0
      ? { ...policyExtra, rules: cleanedRules }
      : { ...policyExtra }

  return JSON.stringify(policy, null, 2)
}

export function parseIntegerText(value: string) {
  return value
    .split(',')
    .map((item) => Number.parseInt(item.trim(), 10))
    .filter((item) => Number.isInteger(item) && item > 0)
}

export function parseLines(value: string) {
  return value
    .split('\n')
    .map((item) => item.trim())
    .filter((item) => item.length > 0)
}
