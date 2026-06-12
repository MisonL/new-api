import type { AffinityRule } from './types'

const CODEX_CLI_HEADER_PASSTHROUGH_HEADERS = [
  'Originator',
  'Session_id',
  'Session-Id',
  'Thread-Id',
  'X-Codex-Beta-Features',
  'X-Codex-Turn-Metadata',
  'X-Codex-Window-Id',
  'X-Client-Request-Id',
]

const CODEX_DESKTOP_HEADER_PASSTHROUGH_HEADERS = [
  ...CODEX_CLI_HEADER_PASSTHROUGH_HEADERS,
]

const CLAUDE_CLI_HEADER_PASSTHROUGH_HEADERS = [
  'X-Claude-Code-Session-Id',
  'X-Stainless-Arch',
  'X-Stainless-Lang',
  'X-Stainless-OS',
  'X-Stainless-Package-Version',
  'X-Stainless-Retry-Count',
  'X-Stainless-Runtime',
  'X-Stainless-Runtime-Version',
  'X-Stainless-Timeout',
  'X-App',
  'Anthropic-Beta',
  'Anthropic-Dangerous-Direct-Browser-Access',
  'Anthropic-Version',
]

const QWEN_CODE_CLI_HEADER_PASSTHROUGH_HEADERS = [
  'X-Stainless-Arch',
  'X-Stainless-Lang',
  'X-Stainless-OS',
  'X-Stainless-Package-Version',
  'X-Stainless-Retry-Count',
  'X-Stainless-Runtime',
  'X-Stainless-Runtime-Version',
]

const DROID_CLI_HEADER_PASSTHROUGH_HEADERS = [
  'X-Stainless-Arch',
  'X-Stainless-Lang',
  'X-Stainless-OS',
  'X-Stainless-Package-Version',
  'X-Stainless-Retry-Count',
  'X-Stainless-Runtime',
  'X-Stainless-Runtime-Version',
]

const GEMINI_CLI_HEADER_PASSTHROUGH_HEADERS = ['X-Goog-Api-Client']

const OPENAI_SDK_HEADER_PASSTHROUGH_HEADERS = [
  'OpenAI-Organization',
  'OpenAI-Project',
  'X-Stainless-Arch',
  'X-Stainless-Lang',
  'X-Stainless-OS',
  'X-Stainless-Package-Version',
  'X-Stainless-Retry-Count',
  'X-Stainless-Runtime',
  'X-Stainless-Runtime-Version',
  'X-Stainless-Timeout',
]

function buildPassHeadersTemplate(headers: string[]) {
  return {
    operations: [
      {
        mode: 'pass_headers',
        value: [...headers],
        keep_origin: true,
      },
    ],
  }
}

const CODEX_SESSION_ID_FALLBACK_OPERATION = {
  mode: 'copy_header',
  from: 'X-Client-Request-Id',
  to: 'Session_id',
  keep_origin: true,
}

function buildCodexHeaderPassthroughTemplate(headers: string[]) {
  return {
    operations: [
      {
        mode: 'pass_headers',
        value: [...headers],
        keep_origin: true,
      },
      { ...CODEX_SESSION_ID_FALLBACK_OPERATION },
    ],
  }
}

const PRUNE_IMAGE_GENERATION_TOOL_TEMPLATE = {
  operations: [
    {
      path: 'tools',
      mode: 'prune_objects',
      value: {
        type: 'image_generation',
        recursive: false,
      },
    },
  ],
}

export type RuleTemplate = Omit<AffinityRule, 'id'>

export type ParamOverrideTemplate = {
  label: string
  payload: Record<string, unknown>
}

export const PARAM_OVERRIDE_TEMPLATES: Record<string, ParamOverrideTemplate> = {
  codexCliHeaders: {
    label: 'Codex CLI Dynamic Headers Passthrough',
    payload: buildCodexHeaderPassthroughTemplate(
      CODEX_CLI_HEADER_PASSTHROUGH_HEADERS
    ),
  },
  codexHeaders: {
    label: 'Codex Desktop Dynamic Headers Passthrough',
    payload: buildCodexHeaderPassthroughTemplate(
      CODEX_DESKTOP_HEADER_PASSTHROUGH_HEADERS
    ),
  },
  codexWithoutImageTool: {
    label: 'Upstream Compat: Remove Image Generation Tool',
    payload: PRUNE_IMAGE_GENERATION_TOOL_TEMPLATE,
  },
  claudeHeaders: {
    label: 'Claude Code Header Passthrough',
    payload: buildPassHeadersTemplate(CLAUDE_CLI_HEADER_PASSTHROUGH_HEADERS),
  },
  openaiSdkHeaders: {
    label: 'OpenAI SDK Metadata Passthrough',
    payload: buildPassHeadersTemplate(OPENAI_SDK_HEADER_PASSTHROUGH_HEADERS),
  },
  geminiHeaders: {
    label: 'Gemini CLI Header Passthrough',
    payload: buildPassHeadersTemplate(GEMINI_CLI_HEADER_PASSTHROUGH_HEADERS),
  },
  qwenCodeHeaders: {
    label: 'Qwen Code Header Passthrough',
    payload: buildPassHeadersTemplate(QWEN_CODE_CLI_HEADER_PASSTHROUGH_HEADERS),
  },
  droidHeaders: {
    label: 'Droid CLI Header Passthrough',
    payload: buildPassHeadersTemplate(DROID_CLI_HEADER_PASSTHROUGH_HEADERS),
  },
}

function isPlainRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export function stringifyParamOverrideTemplatePayload(
  payload: Record<string, unknown>
): string {
  return JSON.stringify(cloneTemplate(payload), null, 2)
}

export function appendParamOverrideTemplatePayload(
  currentJson: string,
  payload: Record<string, unknown>
): string {
  const nextPayload = cloneTemplate(payload)
  const raw = String(currentJson || '').trim()
  if (!raw) {
    return stringifyParamOverrideTemplatePayload(nextPayload)
  }

  const current = JSON.parse(raw)
  if (!isPlainRecord(current)) {
    throw new Error('Parameter override template must be a JSON object')
  }

  if (
    Array.isArray(current.operations) &&
    Array.isArray(nextPayload.operations)
  ) {
    return JSON.stringify(
      {
        ...current,
        operations: [...current.operations, ...nextPayload.operations],
      },
      null,
      2
    )
  }

  return JSON.stringify(
    {
      ...current,
      ...nextPayload,
    },
    null,
    2
  )
}

export const RULE_TEMPLATES: Record<string, RuleTemplate> = {
  codexCli: {
    name: 'codex cli trace',
    model_regex: ['^gpt-.*$'],
    path_regex: ['/v1/responses'],
    key_sources: [{ type: 'gjson', path: 'prompt_cache_key' }],
    value_regex: '',
    ttl_seconds: 0,
    skip_retry_on_failure: true,
    include_using_group: true,
    include_model_name: false,
    include_rule_name: true,
  },
  claudeCli: {
    name: 'claude code trace',
    model_regex: ['^claude-.*$'],
    path_regex: ['/v1/messages'],
    key_sources: [{ type: 'gjson', path: 'metadata.user_id' }],
    value_regex: '',
    ttl_seconds: 0,
    skip_retry_on_failure: true,
    include_using_group: true,
    include_model_name: false,
    include_rule_name: true,
  },
}

export function makeUniqueName(
  existingNames: Set<string>,
  baseName: string
): string {
  const base = (baseName || '').trim() || 'rule'
  if (!existingNames.has(base)) return base
  for (let i = 2; i < 1000; i++) {
    const n = `${base}-${i}`
    if (!existingNames.has(n)) return n
  }
  return `${base}-${Date.now()}`
}

export function cloneTemplate<T>(template: T): T {
  return JSON.parse(JSON.stringify(template))
}
