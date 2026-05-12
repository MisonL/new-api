import type { AffinityRule } from './types'

const CODEX_CLI_HEADER_PASSTHROUGH_HEADERS = [
  'Originator',
  'Session_id',
  'User-Agent',
  'X-Codex-Beta-Features',
  'X-Codex-Turn-Metadata',
  'X-Codex-Window-Id',
  'X-Client-Request-Id',
]

const CLAUDE_CLI_HEADER_PASSTHROUGH_HEADERS = [
  'X-Claude-Code-Session-Id',
  'X-Stainless-Arch',
  'X-Stainless-Lang',
  'X-Stainless-Os',
  'X-Stainless-Package-Version',
  'X-Stainless-Retry-Count',
  'X-Stainless-Runtime',
  'X-Stainless-Runtime-Version',
  'X-Stainless-Timeout',
  'User-Agent',
  'X-App',
  'Anthropic-Beta',
  'Anthropic-Dangerous-Direct-Browser-Access',
  'Anthropic-Version',
]

const QWEN_CODE_CLI_HEADER_PASSTHROUGH_HEADERS = [
  'User-Agent',
  'X-Stainless-Arch',
  'X-Stainless-Lang',
  'X-Stainless-Os',
  'X-Stainless-Package-Version',
  'X-Stainless-Retry-Count',
  'X-Stainless-Runtime',
  'X-Stainless-Runtime-Version',
]

const DROID_CLI_HEADER_PASSTHROUGH_HEADERS = [
  'User-Agent',
  'X-Stainless-Arch',
  'X-Stainless-Lang',
  'X-Stainless-Os',
  'X-Stainless-Package-Version',
  'X-Stainless-Retry-Count',
  'X-Stainless-Runtime',
  'X-Stainless-Runtime-Version',
]

const GEMINI_CLI_HEADER_PASSTHROUGH_HEADERS = [
  'User-Agent',
  'X-Goog-Api-Client',
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

function combineParamOverrideTemplates(
  ...templates: Array<Record<string, unknown>>
) {
  return {
    operations: templates.flatMap((template) =>
      Array.isArray(template.operations) ? template.operations : []
    ),
  }
}

export type RuleTemplate = Omit<AffinityRule, 'id'>

export type ParamOverrideTemplate = {
  label: string
  payload: Record<string, unknown>
}

export const PARAM_OVERRIDE_TEMPLATES: Record<string, ParamOverrideTemplate> = {
  codexHeaders: {
    label: 'Codex Desktop Header Passthrough',
    payload: buildPassHeadersTemplate(CODEX_CLI_HEADER_PASSTHROUGH_HEADERS),
  },
  codexWithoutImageTool: {
    label: 'Codex Desktop Compat: Remove Image Generation Tool',
    payload: PRUNE_IMAGE_GENERATION_TOOL_TEMPLATE,
  },
  codexHeadersWithoutImageTool: {
    label: 'Codex Desktop Compat: Headers + Remove Image Tool',
    payload: combineParamOverrideTemplates(
      buildPassHeadersTemplate(CODEX_CLI_HEADER_PASSTHROUGH_HEADERS),
      PRUNE_IMAGE_GENERATION_TOOL_TEMPLATE
    ),
  },
  claudeHeaders: {
    label: 'Claude Code Header Passthrough',
    payload: buildPassHeadersTemplate(CLAUDE_CLI_HEADER_PASSTHROUGH_HEADERS),
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

export const RULE_TEMPLATES: Record<string, RuleTemplate> = {
  codexCli: {
    name: 'codex cli trace',
    model_regex: ['^gpt-.*$'],
    path_regex: ['/v1/responses'],
    key_sources: [{ type: 'gjson', path: 'prompt_cache_key' }],
    param_override_template: buildPassHeadersTemplate(
      CODEX_CLI_HEADER_PASSTHROUGH_HEADERS
    ),
    value_regex: '',
    ttl_seconds: 0,
    skip_retry_on_failure: true,
    include_using_group: true,
    include_model_name: false,
    include_rule_name: true,
  },
  claudeCli: {
    name: 'claude cli trace',
    model_regex: ['^claude-.*$'],
    path_regex: ['/v1/messages'],
    key_sources: [{ type: 'gjson', path: 'metadata.user_id' }],
    param_override_template: buildPassHeadersTemplate(
      CLAUDE_CLI_HEADER_PASSTHROUGH_HEADERS
    ),
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
