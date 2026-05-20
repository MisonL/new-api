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

export const TEMPLATE_CHAT_TO_RESPONSES = 'chat_to_responses'
export const TEMPLATE_RESPONSES_TO_CHAT = 'responses_to_chat'
export const TEMPLATE_BIDIRECTIONAL = 'bidirectional'

export type ProtocolRuleTemplate =
  | typeof TEMPLATE_CHAT_TO_RESPONSES
  | typeof TEMPLATE_RESPONSES_TO_CHAT
  | typeof TEMPLATE_BIDIRECTIONAL

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

function createParseError(message: string): Error {
  return new Error(message)
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

function ruleFieldPath(fieldName: string, ruleIndex?: number): string {
  return ruleIndex == null ? fieldName : `rules[${ruleIndex}].${fieldName}`
}

function toPositiveIntegers(
  value: unknown,
  fieldName: string,
  ruleIndex?: number
): number[] {
  const path = ruleFieldPath(fieldName, ruleIndex)
  if (value === undefined) return []
  if (!Array.isArray(value)) {
    throw createParseError(`${path} must be an array`)
  }
  for (const item of value) {
    if (!Number.isInteger(item) || item <= 0) {
      throw createParseError(`${path} must contain positive integers`)
    }
  }
  return Array.from(new Set(value))
}

function toModelPatterns(value: unknown, ruleIndex?: number): string[] {
  const path = ruleFieldPath('model_patterns', ruleIndex)
  if (value === undefined) return []
  if (!Array.isArray(value)) {
    throw createParseError(`${path} must be an array`)
  }
  const patterns: string[] = []
  for (const item of value) {
    if (typeof item !== 'string') {
      throw createParseError(`${path} must contain strings`)
    }
    const pattern = item.trim()
    if (!pattern) continue
    try {
      new RegExp(pattern)
    } catch (error) {
      const detail = error instanceof Error ? `: ${error.message}` : ''
      throw createParseError(`${path} contains an invalid regex${detail}`)
    }
    patterns.push(pattern)
  }
  return patterns
}

function normalizeEndpoint(
  value: unknown,
  fieldName: string,
  ruleIndex: number
): ProtocolEndpoint {
  const endpoint = String(value ?? '')
    .trim()
    .toLowerCase()
  if (!endpoint) {
    throw createParseError(`rules[${ruleIndex}].${fieldName} is required`)
  }
  if (
    endpoint === 'openai' ||
    endpoint === 'chat' ||
    endpoint === ENDPOINT_CHAT ||
    endpoint === 'chat-completions' ||
    endpoint === '/v1/chat/completions'
  ) {
    return ENDPOINT_CHAT
  }
  if (
    endpoint === ENDPOINT_RESPONSES ||
    endpoint === 'response' ||
    endpoint === 'openai-response' ||
    endpoint === 'openai-responses' ||
    endpoint === '/v1/responses'
  ) {
    return ENDPOINT_RESPONSES
  }
  throw createParseError(`rules[${ruleIndex}].${fieldName} is unsupported`)
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

  const channelId = parseStrictPositiveInteger(preview.channelId)
  const channelType = parseStrictPositiveInteger(preview.channelType)

  if (!rule.all_channels) {
    const idMatched = channelId != null && rule.channel_ids.includes(channelId)
    const typeMatched =
      channelType != null && rule.channel_types.includes(channelType)
    if (!idMatched && !typeMatched) {
      return { matched: false, reason: 'Channel scope does not match.' }
    }
  }

  const model = preview.model.trim()
  if (!model)
    return { matched: false, reason: 'Model is required for preview.' }

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

export function createProtocolRuleFromTemplate(
  template: ProtocolRuleTemplate,
  existingRules: ProtocolRule[] = []
): ProtocolRule[] {
  const templates: ProtocolRuleTemplate[] =
    template === TEMPLATE_BIDIRECTIONAL
      ? [TEMPLATE_RESPONSES_TO_CHAT, TEMPLATE_CHAT_TO_RESPONSES]
      : [template]
  return templates.map((item) =>
    createProtocolRule({
      name: nextTemplateRuleName(item, existingRules),
      source_endpoint:
        item === TEMPLATE_RESPONSES_TO_CHAT
          ? ENDPOINT_RESPONSES
          : ENDPOINT_CHAT,
      target_endpoint:
        item === TEMPLATE_RESPONSES_TO_CHAT
          ? ENDPOINT_CHAT
          : ENDPOINT_RESPONSES,
      all_channels: false,
      channel_ids: [],
      channel_types: [1],
      model_patterns:
        item === TEMPLATE_RESPONSES_TO_CHAT
          ? ['^gpt-5.*$', '^o[13].*$']
          : ['^gpt-4o.*$', '^gpt-5.*$'],
    })
  )
}

function nextTemplateRuleName(
  template: ProtocolRuleTemplate,
  existingRules: ProtocolRule[]
) {
  const baseName =
    template === TEMPLATE_RESPONSES_TO_CHAT
      ? 'responses-to-chat'
      : 'chat-to-responses'
  const existingNames = new Set(existingRules.map((rule) => rule.name))
  let name = baseName
  let index = 2
  while (existingNames.has(name)) {
    name = `${baseName}-${index}`
    index += 1
  }
  return name
}

function ruleFromRecord(
  value: Record<string, unknown>,
  fallbackName: string,
  ruleIndex: number
): ProtocolRule {
  if (value.options !== undefined && !isRecord(value.options)) {
    throw createParseError(`rules[${ruleIndex}].options must be an object`)
  }
  const options = isRecord(value.options) ? value.options : {}
  const source = normalizeEndpoint(
    value.source_endpoint,
    'source_endpoint',
    ruleIndex
  )
  const target = normalizeEndpoint(
    value.target_endpoint,
    'target_endpoint',
    ruleIndex
  )
  if (target === source) {
    throw createParseError(
      `rules[${ruleIndex}].source_endpoint and target_endpoint must be different`
    )
  }
  const name = String(value.name || fallbackName)
  const enableCustomToolBridge =
    options.enable_custom_tool_bridge === true ||
    value.enable_custom_tool_bridge === true
  if (
    enableCustomToolBridge &&
    (source !== ENDPOINT_RESPONSES || target !== ENDPOINT_CHAT)
  ) {
    throw createParseError(
      `rules[${ruleIndex}].enable_custom_tool_bridge only supports Responses to Chat Completions`
    )
  }

  return createProtocolRule({
    name,
    enabled: value.enabled !== false,
    source_endpoint: source,
    target_endpoint: target,
    all_channels: value.all_channels !== false,
    channel_ids: toPositiveIntegers(
      value.channel_ids,
      'channel_ids',
      ruleIndex
    ),
    channel_types: toPositiveIntegers(
      value.channel_types,
      'channel_types',
      ruleIndex
    ),
    model_patterns: toModelPatterns(value.model_patterns, ruleIndex),
    enable_custom_tool_bridge: enableCustomToolBridge,
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
      channel_ids: toPositiveIntegers(policy.channel_ids, 'channel_ids'),
      channel_types: toPositiveIntegers(policy.channel_types, 'channel_types'),
      model_patterns: toModelPatterns(policy.model_patterns),
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
    if (policy.rules !== undefined && !Array.isArray(policy.rules)) {
      return { ok: false, error: 'rules must be an array' }
    }
    const ruleValues = Array.isArray(policy.rules) ? policy.rules : []
    const rules = ruleValues.map((rule, index) => {
      if (!isRecord(rule)) {
        throw createParseError(`rules[${index}] must be an object`)
      }
      return ruleFromRecord(rule, `Rule ${index + 1}`, index)
    })

    return {
      ok: true,
      policyExtra,
      rules: Array.isArray(policy.rules) ? rules : legacyRuleFromPolicy(policy),
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

function parseStrictPositiveInteger(value: string) {
  const token = value.trim()
  if (!/^\d+$/.test(token)) return null
  const parsed = Number.parseInt(token, 10)
  return parsed > 0 ? parsed : null
}

export function parseIntegerText(value: string) {
  return value
    .split(/[\s,，、]+/)
    .map(parseStrictPositiveInteger)
    .filter((item): item is number => item != null)
}

export function parseLines(value: string) {
  return value
    .split('\n')
    .map((item) => item.trim())
    .filter((item) => item.length > 0)
}
