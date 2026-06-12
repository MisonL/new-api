import {
  type DragEvent,
  type KeyboardEvent,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from 'react'
import {
  ChevronDown,
  ChevronUp,
  Copy,
  FileSliders,
  GripVertical,
  Plus,
  Search,
  Trash2,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type ParamOverrideCondition = {
  id: string
  path: string
  mode: string
  value_text: string
  invert: boolean
  pass_missing_key: boolean
}

type ParamOverrideOperation = {
  id: string
  description: string
  path: string
  mode: string
  from: string
  to: string
  value_text: string
  keep_origin: boolean
  logic: string
  conditions: ParamOverrideCondition[]
}

type LegacyOverrideEntry = {
  id: string
  key: string
  value_text: string
}

type LegacyOverrideBuildResult = {
  value: Record<string, unknown>
  count: number
}

export type ParamOverrideEditorDialogProps = {
  open: boolean
  value: string
  onOpenChange: (open: boolean) => void
  onSave: (value: string) => void
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const OPERATION_MODE_OPTIONS = [
  { label: 'Set Field', value: 'set' },
  { label: 'Delete Field', value: 'delete' },
  { label: 'Append to End', value: 'append' },
  { label: 'Prepend to Start', value: 'prepend' },
  { label: 'Copy Field', value: 'copy' },
  { label: 'Move Field', value: 'move' },
  { label: 'String Replace', value: 'replace' },
  { label: 'Regex Replace', value: 'regex_replace' },
  { label: 'Trim Prefix', value: 'trim_prefix' },
  { label: 'Trim Suffix', value: 'trim_suffix' },
  { label: 'Ensure Prefix', value: 'ensure_prefix' },
  { label: 'Ensure Suffix', value: 'ensure_suffix' },
  { label: 'Trim Space', value: 'trim_space' },
  { label: 'To Lowercase', value: 'to_lower' },
  { label: 'To Uppercase', value: 'to_upper' },
  { label: 'Return Custom Error', value: 'return_error' },
  { label: 'Prune Object Items', value: 'prune_objects' },
  { label: 'Pass Through Headers', value: 'pass_headers' },
  { label: 'Sync Fields', value: 'sync_fields' },
  { label: 'Set Request Header', value: 'set_header' },
  { label: 'Delete Request Header', value: 'delete_header' },
  { label: 'Copy Request Header', value: 'copy_header' },
  { label: 'Move Request Header', value: 'move_header' },
]

const OPERATION_MODE_VALUES = new Set(
  OPERATION_MODE_OPTIONS.map((o) => o.value)
)

const OPERATION_MODE_LABEL_MAP = OPERATION_MODE_OPTIONS.reduce<
  Record<string, string>
>((acc, item) => {
  acc[item.value] = item.label
  return acc
}, {})

const CONDITION_MODE_OPTIONS = [
  { label: 'Exact Match', value: 'full' },
  { label: 'Prefix', value: 'prefix' },
  { label: 'Suffix', value: 'suffix' },
  { label: 'Contains', value: 'contains' },
  { label: 'Greater Than', value: 'gt' },
  { label: 'Greater Than or Equal', value: 'gte' },
  { label: 'Less Than', value: 'lt' },
  { label: 'Less Than or Equal', value: 'lte' },
]

const CONDITION_MODE_VALUES = new Set(
  CONDITION_MODE_OPTIONS.map((o) => o.value)
)

const MODE_META: Record<
  string,
  {
    path?: boolean
    pathOptional?: boolean
    value?: boolean
    from?: boolean
    to?: boolean
    keepOrigin?: boolean
    pathAlias?: boolean
  }
> = {
  delete: { path: true },
  set: { path: true, value: true, keepOrigin: true },
  append: { path: true, value: true, keepOrigin: true },
  prepend: { path: true, value: true, keepOrigin: true },
  copy: { from: true, to: true },
  move: { from: true, to: true },
  replace: { path: true, from: true, to: false },
  regex_replace: { path: true, from: true, to: false },
  trim_prefix: { path: true, value: true },
  trim_suffix: { path: true, value: true },
  ensure_prefix: { path: true, value: true },
  ensure_suffix: { path: true, value: true },
  trim_space: { path: true },
  to_lower: { path: true },
  to_upper: { path: true },
  return_error: { value: true },
  prune_objects: { pathOptional: true, value: true },
  pass_headers: { value: true, keepOrigin: true },
  sync_fields: { from: true, to: true },
  set_header: { path: true, value: true, keepOrigin: true },
  delete_header: { path: true },
  copy_header: { from: true, to: true, keepOrigin: true, pathAlias: true },
  move_header: { from: true, to: true, keepOrigin: true, pathAlias: true },
}

const VALUE_REQUIRED_MODES = new Set([
  'trim_prefix',
  'trim_suffix',
  'ensure_prefix',
  'ensure_suffix',
  'set_header',
  'return_error',
  'prune_objects',
  'pass_headers',
])

const FROM_REQUIRED_MODES = new Set([
  'copy',
  'move',
  'replace',
  'regex_replace',
  'copy_header',
  'move_header',
  'sync_fields',
])

const TO_REQUIRED_MODES = new Set([
  'copy',
  'move',
  'copy_header',
  'move_header',
  'sync_fields',
])

const MODE_DESCRIPTIONS: Record<string, string> = {
  set: 'Write value to the target field',
  delete: 'Remove the target field',
  append: 'Append value to array / string / object end',
  prepend: 'Prepend value to array / string / object start',
  copy: 'Copy source field to target field',
  move: 'Move source field to target field',
  replace: 'Do string replacement in the target field',
  regex_replace: 'Do regex replacement in the target field',
  trim_prefix: 'Remove string prefix',
  trim_suffix: 'Remove string suffix',
  ensure_prefix: 'Ensure the string has a specified prefix',
  ensure_suffix: 'Ensure the string has a specified suffix',
  trim_space: 'Trim leading/trailing whitespace',
  to_lower: 'Convert string to lowercase',
  to_upper: 'Convert string to uppercase',
  return_error: 'Return a custom error immediately',
  prune_objects: 'Prune object items by conditions',
  pass_headers: 'Pass specified request headers to upstream',
  sync_fields: 'Auto-fill when one field exists and another is missing',
  set_header:
    'Set runtime request header: override entire value, or manipulate comma-separated tokens',
  delete_header: 'Delete a runtime request header',
  copy_header: 'Copy a request header',
  move_header: 'Move a request header',
}

const SYNC_TARGET_TYPE_OPTIONS = [
  { label: 'Request Body Field', value: 'json' },
  { label: 'Request Header Field', value: 'header' },
]

const STRUCTURED_VALUE_TYPE_OPTIONS = [
  { label: 'Text', value: 'string' },
  { label: 'Number', value: 'number' },
  { label: 'Boolean', value: 'boolean' },
  { label: 'Null', value: 'null' },
  { label: 'Object', value: 'object' },
  { label: 'Array', value: 'array' },
]

const HEADER_VALUE_MODE_OPTIONS = [
  { label: 'Whole Header Value', value: 'direct' },
  { label: 'Token Mapping', value: 'mapping' },
]

const HEADER_TOKEN_ACTION_OPTIONS = [
  { label: 'Replace', value: 'replace' },
  { label: 'Delete', value: 'delete' },
  { label: 'Keep', value: 'keep' },
]

// Templates

const LEGACY_TEMPLATE = { temperature: 0, max_tokens: 1000 }

const OPERATION_TEMPLATE = {
  operations: [
    {
      description: 'Set default temperature for openai/* models.',
      path: 'temperature',
      mode: 'set',
      value: 0.7,
      conditions: [{ path: 'model', mode: 'prefix', value: 'openai/' }],
      logic: 'AND',
    },
  ],
}

const HEADER_PASSTHROUGH_TEMPLATE = {
  operations: [
    {
      description: 'Pass through common tracing headers to upstream.',
      mode: 'pass_headers',
      value: ['X-Request-Id', 'X-Trace-Id', 'X-Correlation-Id', 'Traceparent'],
      keep_origin: true,
    },
  ],
}

const OPENAI_SDK_HEADER_PASSTHROUGH_TEMPLATE = {
  operations: [
    {
      description:
        'Pass through OpenAI SDK organization, project and Stainless metadata headers.',
      mode: 'pass_headers',
      value: [
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
      ],
      keep_origin: true,
    },
  ],
}

const ANTHROPIC_RUNTIME_HEADER_PASSTHROUGH_TEMPLATE = {
  operations: [
    {
      description:
        'Pass through Anthropic runtime beta/version headers from the original client request.',
      mode: 'pass_headers',
      value: [
        'Anthropic-Beta',
        'Anthropic-Version',
        'Anthropic-Dangerous-Direct-Browser-Access',
        'X-App',
      ],
      keep_origin: true,
    },
  ],
}

const GEMINI_IMAGE_4K_TEMPLATE = {
  operations: [
    {
      description:
        'Set imageSize to 4K when model contains gemini/image and ends with 4k.',
      mode: 'set',
      path: 'generationConfig.imageConfig.imageSize',
      value: '4K',
      conditions: [
        { path: 'original_model', mode: 'contains', value: 'gemini' },
        { path: 'original_model', mode: 'contains', value: 'image' },
        { path: 'original_model', mode: 'suffix', value: '4k' },
      ],
      logic: 'AND',
    },
  ],
}

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

const buildPassHeadersTemplate = (headers: string[]) => ({
  operations: [
    { mode: 'pass_headers', value: [...headers], keep_origin: true },
  ],
})

const CODEX_SESSION_ID_FALLBACK_OPERATION = {
  mode: 'copy_header',
  from: 'X-Client-Request-Id',
  to: 'Session_id',
  keep_origin: true,
}

const buildCodexHeaderPassthroughTemplate = (headers: string[]) => ({
  operations: [
    { mode: 'pass_headers', value: [...headers], keep_origin: true },
    { ...CODEX_SESSION_ID_FALLBACK_OPERATION },
  ],
})

const CODEX_CLI_HEADER_PASSTHROUGH_TEMPLATE =
  buildCodexHeaderPassthroughTemplate(CODEX_CLI_HEADER_PASSTHROUGH_HEADERS)
const CODEX_DESKTOP_HEADER_PASSTHROUGH_TEMPLATE =
  buildCodexHeaderPassthroughTemplate(CODEX_DESKTOP_HEADER_PASSTHROUGH_HEADERS)
const CLAUDE_CLI_HEADER_PASSTHROUGH_TEMPLATE = buildPassHeadersTemplate(
  CLAUDE_CLI_HEADER_PASSTHROUGH_HEADERS
)
const GEMINI_CLI_HEADER_PASSTHROUGH_TEMPLATE = buildPassHeadersTemplate(
  GEMINI_CLI_HEADER_PASSTHROUGH_HEADERS
)
const QWEN_CODE_CLI_HEADER_PASSTHROUGH_TEMPLATE = buildPassHeadersTemplate(
  QWEN_CODE_CLI_HEADER_PASSTHROUGH_HEADERS
)
const DROID_CLI_HEADER_PASSTHROUGH_TEMPLATE = buildPassHeadersTemplate(
  DROID_CLI_HEADER_PASSTHROUGH_HEADERS
)

const AWS_BEDROCK_ANTHROPIC_BETA_TEMPLATE = {
  operations: [
    {
      description:
        'Normalize anthropic-beta header tokens for Bedrock compatibility.',
      mode: 'set_header',
      path: 'anthropic-beta',
      value: {
        'advanced-tool-use-2025-11-20': 'tool-search-tool-2025-10-19',
        bash_20241022: null,
        bash_20250124: null,
        'code-execution-2025-08-25': null,
        'compact-2026-01-12': 'compact-2026-01-12',
        'computer-use-2025-01-24': 'computer-use-2025-01-24',
        'computer-use-2025-11-24': 'computer-use-2025-11-24',
        'context-1m-2025-08-07': 'context-1m-2025-08-07',
        'context-management-2025-06-27': 'context-management-2025-06-27',
        'effort-2025-11-24': null,
        'fast-mode-2026-02-01': null,
        'files-api-2025-04-14': null,
        'fine-grained-tool-streaming-2025-05-14': null,
        'interleaved-thinking-2025-05-14': 'interleaved-thinking-2025-05-14',
        'mcp-client-2025-11-20': null,
        'mcp-client-2025-04-04': null,
        'mcp-servers-2025-12-04': null,
        'output-128k-2025-02-19': null,
        'structured-output-2024-03-01': null,
        'prompt-caching-scope-2026-01-05': null,
        'skills-2025-10-02': null,
        'structured-outputs-2025-11-13': null,
        text_editor_20241022: null,
        text_editor_20250124: null,
        'token-efficient-tools-2025-02-19': null,
        'tool-search-tool-2025-10-19': 'tool-search-tool-2025-10-19',
        'web-fetch-2025-09-10': null,
        'web-search-2025-03-05': null,
        'oauth-2025-04-20': null,
      },
    },
  ],
}

const AWS_BEDROCK_REMOVE_INPUT_EXAMPLES_TEMPLATE = {
  operations: [
    {
      description:
        'Remove all tools[*].custom.input_examples before upstream relay.',
      mode: 'delete',
      path: 'tools.*.custom.input_examples',
    },
  ],
}

const CODEX_REMOVE_IMAGE_GENERATION_TOOL_TEMPLATE = {
  operations: [
    {
      description:
        'Remove image_generation tool objects before upstream relay.',
      path: 'tools',
      mode: 'prune_objects',
      value: {
        type: 'image_generation',
        recursive: false,
      },
    },
  ],
}

type TemplatePresetConfig = {
  label: string
  group: 'recommended' | 'advanced' | 'examples'
  description?: string
  kind: 'operations' | 'legacy'
  payload: Record<string, unknown>
}

const TEMPLATE_GROUPS = [
  { label: 'Recommended Scenarios', value: 'recommended' },
  { label: 'Advanced Compatibility', value: 'advanced' },
  { label: 'Examples and Starting Points', value: 'examples' },
] as const

const TEMPLATE_PRESET_CONFIG: Record<string, TemplatePresetConfig> = {
  codex_cli_headers_passthrough: {
    label: 'Codex CLI Dynamic Headers Passthrough',
    group: 'recommended',
    description:
      'Pass through Codex CLI session, window, turn metadata and request id headers. User-Agent is managed by Header Profile.',
    kind: 'operations',
    payload: CODEX_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  },
  codex_desktop_headers_passthrough: {
    label: 'Codex Desktop Dynamic Headers Passthrough',
    group: 'recommended',
    description:
      'Pass through Codex Desktop session, window, turn metadata and request id headers. User-Agent is managed by Header Profile.',
    kind: 'operations',
    payload: CODEX_DESKTOP_HEADER_PASSTHROUGH_TEMPLATE,
  },
  claude_cli_headers_passthrough: {
    label: 'Claude Code Header Passthrough',
    group: 'recommended',
    description:
      'Pass through Claude Code session, Anthropic beta/version and Stainless runtime headers.',
    kind: 'operations',
    payload: CLAUDE_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  },
  openai_sdk_headers_passthrough: {
    label: 'OpenAI SDK Metadata Passthrough',
    group: 'recommended',
    description:
      'Pass through OpenAI organization, project and Stainless client metadata headers.',
    kind: 'operations',
    payload: OPENAI_SDK_HEADER_PASSTHROUGH_TEMPLATE,
  },
  aws_bedrock_anthropic_beta_override: {
    label: 'AWS Bedrock Claude Beta Header',
    group: 'recommended',
    description:
      'Normalize anthropic-beta header tokens for Bedrock compatibility.',
    kind: 'operations',
    payload: AWS_BEDROCK_ANTHROPIC_BETA_TEMPLATE,
  },
  remove_image_generation_tool: {
    label: 'Upstream Compat: Remove Image Generation Tool',
    group: 'recommended',
    description:
      'Remove image_generation tool objects when an upstream rejects that tool type.',
    kind: 'operations',
    payload: CODEX_REMOVE_IMAGE_GENERATION_TOOL_TEMPLATE,
  },
  aws_bedrock_remove_input_examples: {
    label: 'AWS Bedrock Remove Input Examples',
    group: 'advanced',
    description:
      'Remove tools.*.custom.input_examples before sending requests to Bedrock.',
    kind: 'operations',
    payload: AWS_BEDROCK_REMOVE_INPUT_EXAMPLES_TEMPLATE,
  },
  anthropic_runtime_headers_passthrough: {
    label: 'Anthropic Beta/Version Passthrough',
    group: 'advanced',
    description:
      'Pass through Anthropic runtime beta/version headers from the original request.',
    kind: 'operations',
    payload: ANTHROPIC_RUNTIME_HEADER_PASSTHROUGH_TEMPLATE,
  },
  gemini_cli_headers_passthrough: {
    label: 'Gemini CLI Header Passthrough',
    group: 'advanced',
    description: 'Pass through Gemini CLI x-goog-api-client metadata.',
    kind: 'operations',
    payload: GEMINI_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  },
  qwen_code_headers_passthrough: {
    label: 'Qwen Code Header Passthrough',
    group: 'advanced',
    description: 'Pass through Qwen Code Stainless client metadata headers.',
    kind: 'operations',
    payload: QWEN_CODE_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  },
  droid_cli_headers_passthrough: {
    label: 'Droid CLI Header Passthrough',
    group: 'advanced',
    description: 'Pass through Droid CLI Stainless client metadata headers.',
    kind: 'operations',
    payload: DROID_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  },
  pass_headers_auth: {
    label: 'Trace Headers Passthrough',
    group: 'advanced',
    description:
      'Pass through common tracing headers such as X-Request-Id and Traceparent.',
    kind: 'operations',
    payload: HEADER_PASSTHROUGH_TEMPLATE,
  },
  gemini_image_4k: {
    label: 'Gemini Image 4K',
    group: 'advanced',
    description:
      'Set generationConfig.imageConfig.imageSize to 4K for matching Gemini image models.',
    kind: 'operations',
    payload: GEMINI_IMAGE_4K_TEMPLATE,
  },
  operations_default: {
    label: 'Example: Set Temperature by Model',
    group: 'examples',
    description:
      'Example rule that sets temperature when the model name starts with openai/.',
    kind: 'operations',
    payload: OPERATION_TEMPLATE,
  },
  legacy_default: {
    label: 'Example: Legacy Field Object',
    group: 'examples',
    description:
      'Legacy top-level field object example for simple field overrides.',
    kind: 'legacy',
    payload: LEGACY_TEMPLATE,
  },
}

const QUICK_TEMPLATE_PRESETS = [
  'codex_cli_headers_passthrough',
  'codex_desktop_headers_passthrough',
  'claude_cli_headers_passthrough',
  'openai_sdk_headers_passthrough',
  'aws_bedrock_anthropic_beta_override',
  'remove_image_generation_tool',
]

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let localIdSeed = 0
const nextLocalId = () => `po_${Date.now()}_${localIdSeed++}`

const toValueText = (value: unknown): string => {
  if (value === undefined) return ''
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

const parseLooseValue = (valueText: string): unknown => {
  const raw = String(valueText ?? '').trim()
  if (raw === '') return ''
  try {
    return JSON.parse(raw)
  } catch {
    return raw
  }
}

const verifyJSON = (text: string): boolean => {
  try {
    JSON.parse(text)
    return true
  } catch {
    return false
  }
}

const normalizeCondition = (
  condition: Record<string, unknown> = {}
): ParamOverrideCondition => ({
  id: nextLocalId(),
  path: typeof condition.path === 'string' ? condition.path : '',
  mode: CONDITION_MODE_VALUES.has(condition.mode as string)
    ? (condition.mode as string)
    : 'full',
  value_text: toValueText(condition.value),
  invert: condition.invert === true,
  pass_missing_key: condition.pass_missing_key === true,
})

const normalizeConditionList = (
  rawConditions: unknown
): ParamOverrideCondition[] => {
  if (Array.isArray(rawConditions)) {
    return rawConditions
      .filter(
        (condition): condition is Record<string, unknown> =>
          condition !== null &&
          typeof condition === 'object' &&
          !Array.isArray(condition)
      )
      .map(normalizeCondition)
  }
  if (
    rawConditions &&
    typeof rawConditions === 'object' &&
    !Array.isArray(rawConditions)
  ) {
    return Object.entries(rawConditions as Record<string, unknown>).map(
      ([path, value]) => normalizeCondition({ path, mode: 'full', value })
    )
  }
  return []
}

const createDefaultCondition = (): ParamOverrideCondition =>
  normalizeCondition({})

const normalizeLegacyEntry = (
  key: string,
  value: unknown
): LegacyOverrideEntry => ({
  id: nextLocalId(),
  key,
  value_text: toValueText(value),
})

const createDefaultLegacyEntry = (): LegacyOverrideEntry =>
  normalizeLegacyEntry('', '')

const getLegacyEntriesFromObject = (
  source: Record<string, unknown>,
  options: { excludeOperations?: boolean } = {}
): LegacyOverrideEntry[] => {
  const entries = Object.entries(source)
    .filter(([key]) => !(options.excludeOperations && key === 'operations'))
    .map(([key, value]) => normalizeLegacyEntry(key, value))
  return entries.length > 0 ? entries : [createDefaultLegacyEntry()]
}

const normalizeOperation = (
  operation: Record<string, unknown> = {}
): ParamOverrideOperation => ({
  id: nextLocalId(),
  description:
    typeof operation.description === 'string' ? operation.description : '',
  path: typeof operation.path === 'string' ? operation.path : '',
  mode: OPERATION_MODE_VALUES.has(operation.mode as string)
    ? (operation.mode as string)
    : 'set',
  value_text: toValueText(operation.value),
  keep_origin: operation.keep_origin === true,
  from: typeof operation.from === 'string' ? operation.from : '',
  to: typeof operation.to === 'string' ? operation.to : '',
  logic: String(operation.logic || 'OR').toUpperCase() === 'AND' ? 'AND' : 'OR',
  conditions: normalizeConditionList(operation.conditions),
})

const createDefaultOperation = (): ParamOverrideOperation =>
  normalizeOperation({ mode: 'set' })

const reorderOperations = (
  ops: ParamOverrideOperation[],
  sourceId: string,
  targetId: string,
  position: 'before' | 'after' = 'before'
): ParamOverrideOperation[] => {
  if (!sourceId || !targetId || sourceId === targetId) return ops
  const srcIdx = ops.findIndex((o) => o.id === sourceId)
  if (srcIdx < 0) return ops
  const next = [...ops]
  const [moved] = next.splice(srcIdx, 1)
  let insertIdx = next.findIndex((o) => o.id === targetId)
  if (insertIdx < 0) return ops
  if (position === 'after') insertIdx += 1
  next.splice(insertIdx, 0, moved)
  return next
}

const isOperationBlank = (operation: ParamOverrideOperation): boolean => {
  const hasCondition = operation.conditions.some(
    (c) =>
      c.path.trim() ||
      c.value_text.trim() ||
      c.mode !== 'full' ||
      c.invert ||
      c.pass_missing_key
  )
  return (
    operation.mode === 'set' &&
    !operation.path.trim() &&
    !operation.from.trim() &&
    !operation.to.trim() &&
    operation.value_text.trim() === '' &&
    !operation.keep_origin &&
    !hasCondition
  )
}

const getOperationSummary = (
  operation: ParamOverrideOperation,
  index: number
): string => {
  const mode = operation.mode || 'set'
  const modeLabel = OPERATION_MODE_LABEL_MAP[mode] || mode
  if (mode === 'sync_fields') {
    const from = operation.from.trim()
    const to = operation.to.trim()
    return `${index + 1}. ${modeLabel} · ${from || to || '-'}`
  }
  const path = operation.path.trim()
  const from = operation.from.trim()
  const to = operation.to.trim()
  return `${index + 1}. ${modeLabel} · ${path || from || to || '-'}`
}

const getModeTagTailwind = (mode: string): string => {
  if (mode.includes('header'))
    return 'bg-cyan-500/15 text-cyan-700 dark:text-cyan-300 border-cyan-500/20'
  if (mode.includes('replace') || mode.includes('trim'))
    return 'bg-violet-500/15 text-violet-700 dark:text-violet-300 border-violet-500/20'
  if (mode.includes('copy') || mode.includes('move'))
    return 'bg-blue-500/15 text-blue-700 dark:text-blue-300 border-blue-500/20'
  if (mode.includes('error') || mode.includes('prune'))
    return 'bg-red-500/15 text-red-700 dark:text-red-300 border-red-500/20'
  if (mode.includes('sync'))
    return 'bg-green-500/15 text-green-700 dark:text-green-300 border-green-500/20'
  return 'bg-muted text-muted-foreground'
}

const getModePathLabel = (mode: string): string => {
  if (mode === 'set_header' || mode === 'delete_header') return 'Header Name'
  if (mode === 'prune_objects') return 'Target Path (optional)'
  return 'Target Field Path'
}

const getModePathPlaceholder = (mode: string): string => {
  if (mode === 'set_header') return 'Authorization'
  if (mode === 'delete_header') return 'X-Debug-Mode'
  if (mode === 'prune_objects') return 'messages'
  return 'temperature'
}

const getModeFromLabel = (mode: string): string => {
  if (mode === 'replace') return 'Match Text'
  if (mode === 'regex_replace') return 'Regex Pattern'
  if (mode === 'copy_header' || mode === 'move_header') return 'Source Header'
  return 'Source Field'
}

const getModeFromPlaceholder = (mode: string): string => {
  if (mode === 'replace') return 'openai/'
  if (mode === 'regex_replace') return '^gpt-'
  if (mode === 'copy_header' || mode === 'move_header') return 'Authorization'
  return 'model'
}

const getModeToLabel = (mode: string): string => {
  if (mode === 'replace' || mode === 'regex_replace') return 'Replace With'
  if (mode === 'copy_header' || mode === 'move_header') return 'Target Header'
  return 'Target Field'
}

const getModeToPlaceholder = (mode: string): string => {
  if (mode === 'replace') return '(leave empty to delete)'
  if (mode === 'regex_replace') return 'openai/gpt-'
  if (mode === 'copy_header' || mode === 'move_header') return 'X-Upstream-Auth'
  return 'original_model'
}

const getModeValueLabel = (mode: string): string => {
  if (mode === 'set_header')
    return 'Header Value (supports string or JSON mapping)'
  if (mode === 'pass_headers')
    return 'Pass-through Headers (comma-separated or JSON array)'
  if (
    mode === 'trim_prefix' ||
    mode === 'trim_suffix' ||
    mode === 'ensure_prefix' ||
    mode === 'ensure_suffix'
  )
    return 'Prefix/Suffix Text'
  if (mode === 'prune_objects') return 'Prune Rule (string or JSON object)'
  return 'Value (supports JSON or plain text)'
}

const getModeValuePlaceholder = (mode: string): string => {
  if (mode === 'set_header') return 'Bearer sk-xxx'
  if (mode === 'pass_headers') return 'Authorization, X-Request-Id'
  if (
    mode === 'trim_prefix' ||
    mode === 'trim_suffix' ||
    mode === 'ensure_prefix' ||
    mode === 'ensure_suffix'
  )
    return 'openai/'
  if (mode === 'prune_objects') return '{"type":"redacted_thinking"}'
  return '0.7'
}

const parseSyncTargetSpec = (spec: string): { type: string; key: string } => {
  const raw = String(spec ?? '').trim()
  if (!raw) return { type: 'json', key: '' }
  const idx = raw.indexOf(':')
  if (idx < 0) return { type: 'json', key: raw }
  const prefix = raw.slice(0, idx).trim().toLowerCase()
  const key = raw.slice(idx + 1).trim()
  return prefix === 'header' ? { type: 'header', key } : { type: 'json', key }
}

const buildSyncTargetSpec = (type: string, key: string): string => {
  const normalizedType = type === 'header' ? 'header' : 'json'
  const normalizedKey = String(key ?? '').trim()
  if (!normalizedKey) return ''
  return `${normalizedType}:${normalizedKey}`
}

// return_error helpers

type ReturnErrorDraft = {
  message: string
  statusCode: number
  code: string
  type: string
  skipRetry: boolean
  simpleMode: boolean
}

const parseReturnErrorDraft = (valueText: string): ReturnErrorDraft => {
  const defaults: ReturnErrorDraft = {
    message: '',
    statusCode: 400,
    code: '',
    type: '',
    skipRetry: true,
    simpleMode: true,
  }
  const raw = String(valueText ?? '').trim()
  if (!raw) return defaults
  try {
    const parsed = JSON.parse(raw) as Record<string, unknown>
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const statusRaw =
        parsed.status_code !== undefined ? parsed.status_code : parsed.status
      const statusValue = Number(statusRaw)
      return {
        ...defaults,
        message: String(
          (parsed.message as string) || (parsed.msg as string) || ''
        ).trim(),
        statusCode:
          Number.isInteger(statusValue) &&
          statusValue >= 100 &&
          statusValue <= 599
            ? statusValue
            : 400,
        code: String((parsed.code as string) || '').trim(),
        type: String((parsed.type as string) || '').trim(),
        skipRetry: parsed.skip_retry !== false,
        simpleMode: false,
      }
    }
  } catch {
    /* treat as plain text */
  }
  return { ...defaults, message: raw, simpleMode: true }
}

const buildReturnErrorValueText = (
  draft: Partial<ReturnErrorDraft>
): string => {
  const message = String(draft.message || '').trim()
  if (draft.simpleMode) return message
  const statusCode = Number(draft.statusCode)
  const payload: Record<string, unknown> = {
    message,
    status_code:
      Number.isInteger(statusCode) && statusCode >= 100 && statusCode <= 599
        ? statusCode
        : 400,
  }
  const code = String(draft.code || '').trim()
  const type = String(draft.type || '').trim()
  if (code) payload.code = code
  if (type) payload.type = type
  if (draft.skipRetry === false) payload.skip_retry = false
  return JSON.stringify(payload)
}

// prune_objects helpers

type PruneRule = {
  id: string
  path: string
  mode: string
  value_text: string
  invert: boolean
  pass_missing_key: boolean
}

type PruneObjectsDraft = {
  simpleMode: boolean
  typeText: string
  logic: string
  recursive: boolean
  rules: PruneRule[]
}

const normalizePruneRule = (rule: Record<string, unknown> = {}): PruneRule => ({
  id: nextLocalId(),
  path: typeof rule.path === 'string' ? rule.path : '',
  mode: CONDITION_MODE_VALUES.has(rule.mode as string)
    ? (rule.mode as string)
    : 'full',
  value_text: toValueText(rule.value),
  invert: rule.invert === true,
  pass_missing_key: rule.pass_missing_key === true,
})

const parsePruneObjectsDraft = (valueText: string): PruneObjectsDraft => {
  const defaults: PruneObjectsDraft = {
    simpleMode: true,
    typeText: '',
    logic: 'AND',
    recursive: true,
    rules: [],
  }
  const raw = String(valueText ?? '').trim()
  if (!raw) return defaults
  try {
    const parsed = JSON.parse(raw)
    if (typeof parsed === 'string')
      return { ...defaults, typeText: parsed.trim() }
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const rules: PruneRule[] = []
      if (
        parsed.where &&
        typeof parsed.where === 'object' &&
        !Array.isArray(parsed.where)
      ) {
        for (const [path, value] of Object.entries(
          parsed.where as Record<string, unknown>
        )) {
          rules.push(normalizePruneRule({ path, mode: 'full', value }))
        }
      }
      if (Array.isArray(parsed.conditions)) {
        for (const item of parsed.conditions) {
          if (item && typeof item === 'object')
            rules.push(normalizePruneRule(item))
        }
      } else if (
        parsed.conditions &&
        typeof parsed.conditions === 'object' &&
        !Array.isArray(parsed.conditions)
      ) {
        for (const [path, value] of Object.entries(
          parsed.conditions as Record<string, unknown>
        )) {
          rules.push(normalizePruneRule({ path, mode: 'full', value }))
        }
      }
      const typeText =
        parsed.type === undefined ? '' : String(parsed.type).trim()
      const logic =
        String(parsed.logic || 'AND').toUpperCase() === 'OR' ? 'OR' : 'AND'
      const recursive = parsed.recursive !== false
      const hasAdvancedFields =
        parsed.logic !== undefined ||
        parsed.recursive !== undefined ||
        parsed.where !== undefined ||
        parsed.conditions !== undefined
      return {
        ...defaults,
        simpleMode: !hasAdvancedFields,
        typeText,
        logic,
        recursive,
        rules,
      }
    }
    return { ...defaults, typeText: String(parsed ?? '').trim() }
  } catch {
    return { ...defaults, typeText: raw }
  }
}

const buildPruneObjectsValueText = (draft: PruneObjectsDraft): string => {
  const typeText = String(draft.typeText || '').trim()
  if (draft.simpleMode) return typeText
  const payload: Record<string, unknown> = {}
  if (typeText) payload.type = typeText
  if (String(draft.logic || 'AND').toUpperCase() === 'OR') payload.logic = 'OR'
  payload.recursive = draft.recursive !== false
  const conditions = (draft.rules || [])
    .filter((rule) => String(rule.path || '').trim())
    .map((rule) => {
      const conditionPayload: Record<string, unknown> = {
        path: String(rule.path || '').trim(),
        mode: CONDITION_MODE_VALUES.has(rule.mode) ? rule.mode : 'full',
      }
      const valueRaw = String(rule.value_text || '').trim()
      if (valueRaw !== '') {
        conditionPayload.value = parseStructuredValueText(valueRaw)
      }
      if (rule.invert) conditionPayload.invert = true
      if (rule.pass_missing_key) conditionPayload.pass_missing_key = true
      return conditionPayload
    })
  if (conditions.length > 0) payload.conditions = conditions
  if (!payload.type && !payload.conditions)
    return JSON.stringify({
      logic: String(draft.logic || 'AND').toUpperCase() === 'OR' ? 'OR' : 'AND',
      recursive: draft.recursive !== false,
    })
  return JSON.stringify(payload)
}

const getPruneAdvancedSummaryParts = (draft: PruneObjectsDraft): string[] => {
  const parts = [
    draft.recursive ? 'Recursive' : 'Current Level Only',
    String(draft.logic || 'AND').toUpperCase() === 'OR'
      ? 'Any Match (OR)'
      : 'All Must Match (AND)',
  ]
  const extraRules = draft.rules.filter((rule) =>
    String(rule.path || '').trim()
  ).length
  if (extraRules > 0) parts.push('Additional Conditions: {{count}}')
  return parts
}

// pass_headers helpers

const parsePassHeaderNames = (rawValue: unknown): string[] => {
  if (Array.isArray(rawValue))
    return rawValue.map((i) => String(i ?? '').trim()).filter(Boolean)
  if (rawValue && typeof rawValue === 'object') {
    const obj = rawValue as Record<string, unknown>
    if (obj.names !== undefined) return parsePassHeaderNames(obj.names)
    if (Array.isArray(obj.headers))
      return obj.headers.map((i) => String(i ?? '').trim()).filter(Boolean)
    if (obj.header !== undefined) {
      const single = String(obj.header ?? '').trim()
      return single ? [single] : []
    }
    return []
  }
  if (typeof rawValue === 'string')
    return rawValue
      .split(',')
      .map((i) => i.trim())
      .filter(Boolean)
  return []
}

type PassHeadersDraft = {
  sourceKey: 'headers' | 'names' | 'header'
  headers: string[]
}

type PassHeaderRow = {
  id: string
  value: string
}

const MAX_STRUCTURED_VALUE_DEPTH = 8
const STRUCTURED_VALUE_DEPTH_ERROR = `Structured value nesting depth exceeds ${MAX_STRUCTURED_VALUE_DEPTH}`
const isCompleteStructuredNumberText = (text: string): boolean => {
  const trimmed = text.trim()
  return (
    trimmed !== '' &&
    trimmed !== '-' &&
    trimmed !== '.' &&
    trimmed !== '-.' &&
    Number.isFinite(Number(trimmed))
  )
}

const parsePassHeadersDraft = (valueText: string): PassHeadersDraft => {
  const parsed = parseLooseValue(valueText)
  const headers = parsePassHeaderNames(parsed)
  if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
    const obj = parsed as Record<string, unknown>
    if (obj.names !== undefined) return { sourceKey: 'names', headers }
    if (obj.header !== undefined) return { sourceKey: 'header', headers }
  }
  return { sourceKey: 'headers', headers }
}

const buildPassHeadersValueText = (draft: PassHeadersDraft): string => {
  const cleanHeaders = Array.from(
    new Set(draft.headers.map((item) => item.trim()).filter(Boolean))
  )
  if (draft.sourceKey === 'names') {
    return JSON.stringify({ names: cleanHeaders })
  }
  if (draft.sourceKey === 'header') {
    return JSON.stringify({ header: cleanHeaders[0] || '' })
  }
  return JSON.stringify(cleanHeaders)
}

type StructuredValueNodeKind =
  | 'string'
  | 'number'
  | 'boolean'
  | 'null'
  | 'object'
  | 'array'

type StructuredObjectEntry = {
  id: string
  key: string
  value: StructuredValueNode
}

type StructuredArrayItem = {
  id: string
  value: StructuredValueNode
}

type StructuredValueNode = {
  id: string
  kind: StructuredValueNodeKind
  text: string
  boolValue: boolean
  objectEntries: StructuredObjectEntry[]
  arrayItems: StructuredArrayItem[]
}

const createStructuredValueNode = (
  kind: StructuredValueNodeKind = 'string'
): StructuredValueNode => ({
  id: nextLocalId(),
  kind,
  text: kind === 'number' ? '0' : '',
  boolValue: true,
  objectEntries: [],
  arrayItems: [],
})

const isJsonLikeStructuredValueText = (valueText: string): boolean => {
  const trimmed = valueText.trim()
  if (trimmed === '') return false
  if ('[{'.includes(trimmed[0])) return true
  if (trimmed[0] === '"') return true
  if (trimmed === 'true' || trimmed === 'false' || trimmed === 'null') {
    return true
  }
  return /^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:e[+-]?\d+)?$/i.test(trimmed)
}

const normalizeStructuredValueNode = (
  value: unknown,
  depth = 0
): StructuredValueNode => {
  if (depth > MAX_STRUCTURED_VALUE_DEPTH) {
    throw new Error(STRUCTURED_VALUE_DEPTH_ERROR)
  }
  if (value === null) return createStructuredValueNode('null')
  if (Array.isArray(value)) {
    return {
      ...createStructuredValueNode('array'),
      arrayItems: value.map((item) => ({
        id: nextLocalId(),
        value: normalizeStructuredValueNode(item, depth + 1),
      })),
    }
  }
  if (typeof value === 'object' && value !== null) {
    return {
      ...createStructuredValueNode('object'),
      objectEntries: Object.entries(value as Record<string, unknown>).map(
        ([key, item]) => ({
          id: nextLocalId(),
          key,
          value: normalizeStructuredValueNode(item, depth + 1),
        })
      ),
    }
  }
  if (typeof value === 'number' && Number.isFinite(value)) {
    return { ...createStructuredValueNode('number'), text: String(value) }
  }
  if (typeof value === 'boolean') {
    return { ...createStructuredValueNode('boolean'), boolValue: value }
  }
  return { ...createStructuredValueNode('string'), text: String(value ?? '') }
}

const parseStructuredValueNode = (valueText: string): StructuredValueNode => {
  const raw = String(valueText ?? '')
  if (raw.trim() === '') return createStructuredValueNode('string')
  if (!isJsonLikeStructuredValueText(raw)) {
    return normalizeStructuredValueNode(raw)
  }
  return normalizeStructuredValueNode(JSON.parse(raw))
}

const parseStructuredValueNodeForDisplay = (
  valueText: string
): StructuredValueNode => {
  try {
    return parseStructuredValueNode(valueText)
  } catch {
    return normalizeStructuredValueNode(valueText)
  }
}

function assertStructuredValueInvariant(
  condition: unknown,
  message: string
): asserts condition {
  if (!condition) throw new Error(message)
}

const getStructuredText = (node: StructuredValueNode): string => {
  assertStructuredValueInvariant(
    typeof node.text === 'string',
    'Invalid structured value node text'
  )
  return node.text
}

const getStructuredBooleanValue = (node: StructuredValueNode): boolean => {
  assertStructuredValueInvariant(
    typeof node.boolValue === 'boolean',
    'Invalid structured value boolean'
  )
  return node.boolValue
}

const getStructuredObjectEntries = (
  node: StructuredValueNode
): StructuredObjectEntry[] => {
  assertStructuredValueInvariant(
    Array.isArray(node.objectEntries),
    'Invalid structured value object entries'
  )
  return node.objectEntries
}

const getStructuredArrayItems = (
  node: StructuredValueNode
): StructuredArrayItem[] => {
  assertStructuredValueInvariant(
    Array.isArray(node.arrayItems),
    'Invalid structured value array items'
  )
  return node.arrayItems
}

const buildStructuredValue = (node: StructuredValueNode): unknown => {
  switch (node.kind) {
    case 'number': {
      const text = getStructuredText(node)
      const numberValue = Number(text)
      if (!isCompleteStructuredNumberText(text)) {
        throw new Error('Invalid number value')
      }
      return numberValue
    }
    case 'boolean':
      return getStructuredBooleanValue(node)
    case 'null':
      return null
    case 'object': {
      const payload: Record<string, unknown> = {}
      for (const entry of getStructuredObjectEntries(node)) {
        assertStructuredValueInvariant(
          typeof entry.key === 'string',
          'Invalid structured value object key'
        )
        const key = entry.key.trim()
        if (key) payload[key] = buildStructuredValue(entry.value)
      }
      return payload
    }
    case 'array':
      return getStructuredArrayItems(node).map((item) =>
        buildStructuredValue(item.value)
      )
    case 'string':
      return getStructuredText(node)
    default:
      throw new Error('Invalid structured value kind')
  }
}

const shouldQuoteStructuredString = (value: string): boolean => {
  if (value !== value.trim()) return true
  if (value.trim() === '') return false
  try {
    JSON.parse(value)
    return true
  } catch {
    return false
  }
}

const buildStructuredValueText = (node: StructuredValueNode): string => {
  const value = buildStructuredValue(node)
  if (node.kind === 'string') {
    const text = String(value ?? '')
    return shouldQuoteStructuredString(text) ? JSON.stringify(text) : text
  }
  return JSON.stringify(value)
}

const canSerializeStructuredValueNode = (
  node: StructuredValueNode
): boolean => {
  switch (node.kind) {
    case 'number':
      return isCompleteStructuredNumberText(getStructuredText(node))
    case 'boolean':
      getStructuredBooleanValue(node)
      return true
    case 'object':
      return getStructuredObjectEntries(node).every((entry) =>
        canSerializeStructuredValueNode(entry.value)
      )
    case 'array':
      return getStructuredArrayItems(node).every((item) =>
        canSerializeStructuredValueNode(item.value)
      )
    case 'string':
      getStructuredText(node)
      return true
    case 'null':
      return true
    default:
      throw new Error('Invalid structured value kind')
  }
}

const parseStructuredValueText = (valueText: string): unknown => {
  const node = parseStructuredValueNode(valueText)
  return buildStructuredValue(node)
}

type HeaderValueMappingRow = {
  id: string
  token: string
  action: string
  replacement: string
}

type HeaderValueDraft = {
  mode: 'direct' | 'mapping'
  directText: string
  keepOnlyDeclared: boolean
  appendText: string
  wildcardAction: string
  wildcardReplacement: string
  rows: HeaderValueMappingRow[]
}

const splitHeaderTokenText = (value: unknown): string[] => {
  if (Array.isArray(value)) {
    return value.flatMap(splitHeaderTokenText).filter(Boolean)
  }
  return String(value ?? '')
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

const parseHeaderValueDraft = (valueText: string): HeaderValueDraft => {
  const defaults: HeaderValueDraft = {
    mode: 'direct',
    directText: '',
    keepOnlyDeclared: false,
    appendText: '',
    wildcardAction: 'none',
    wildcardReplacement: '',
    rows: [],
  }
  const raw = String(valueText ?? '').trim()
  if (!raw) return defaults
  const parsed = parseLooseValue(raw)
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return { ...defaults, directText: String(parsed ?? '') }
  }
  const mapping = parsed as Record<string, unknown>
  const rows: HeaderValueMappingRow[] = []
  for (const [token, replacement] of Object.entries(mapping)) {
    if (token === '$append' || token === '$keep_only_declared') continue
    if (token === '*') continue
    if (replacement === null) {
      rows.push({ id: nextLocalId(), token, action: 'delete', replacement: '' })
      continue
    }
    const replacementText = splitHeaderTokenText(replacement).join(', ')
    rows.push({
      id: nextLocalId(),
      token,
      action: replacementText === token ? 'keep' : 'replace',
      replacement: replacementText,
    })
  }
  return {
    mode: 'mapping',
    directText: '',
    keepOnlyDeclared: mapping.$keep_only_declared === true,
    appendText: splitHeaderTokenText(mapping.$append).join(', '),
    wildcardAction: Object.prototype.hasOwnProperty.call(mapping, '*')
      ? mapping['*'] === null
        ? 'delete'
        : 'replace'
      : 'none',
    wildcardReplacement:
      Object.prototype.hasOwnProperty.call(mapping, '*') &&
      mapping['*'] !== null
        ? splitHeaderTokenText(mapping['*']).join(', ')
        : '',
    rows,
  }
}

const buildHeaderValueText = (draft: HeaderValueDraft): string => {
  if (draft.mode === 'direct') return JSON.stringify(draft.directText)
  const payload: Record<string, unknown> = {}
  if (draft.keepOnlyDeclared) payload.$keep_only_declared = true
  const appendTokens = splitHeaderTokenText(draft.appendText)
  if (appendTokens.length > 0) payload.$append = appendTokens
  if (draft.wildcardAction === 'delete') {
    payload['*'] = null
  } else if (draft.wildcardAction === 'replace') {
    const wildcardTokens = splitHeaderTokenText(draft.wildcardReplacement)
    payload['*'] =
      wildcardTokens.length > 1 ? wildcardTokens : wildcardTokens[0] || ''
  }
  for (const row of draft.rows) {
    const token = row.token.trim()
    if (!token) continue
    if (row.action === 'delete') {
      payload[token] = null
    } else if (row.action === 'keep') {
      payload[token] = token
    } else {
      const tokens = splitHeaderTokenText(row.replacement)
      payload[token] = tokens.length > 1 ? tokens : tokens[0] || ''
    }
  }
  return JSON.stringify(payload)
}

// Condition payload builder
const buildConditionPayload = (
  condition: ParamOverrideCondition
): Record<string, unknown> | null => {
  const path = condition.path.trim()
  if (!path) return null
  const payload: Record<string, unknown> = {
    path,
    mode: condition.mode || 'full',
    value: parseStructuredValueText(condition.value_text),
  }
  if (condition.invert) payload.invert = true
  if (condition.pass_missing_key) payload.pass_missing_key = true
  return payload
}

const buildLegacyOverridePayload = (
  entries: LegacyOverrideEntry[],
  t: (key: string, options?: Record<string, unknown>) => string
): LegacyOverrideBuildResult => {
  const payload: Record<string, unknown> = {}
  let count = 0
  for (const entry of entries) {
    const key = entry.key.trim()
    const valueText = entry.value_text.trim()
    if (!key && !valueText) continue
    if (!key) throw new Error(t('Legacy override field name is required'))
    if (key === 'operations') {
      throw new Error(t('Legacy override field name cannot be operations'))
    }
    if (Object.prototype.hasOwnProperty.call(payload, key)) {
      throw new Error(t('Legacy override field names must be unique'))
    }
    payload[key] = parseStructuredValueText(entry.value_text)
    count += 1
  }
  return { value: payload, count }
}

const buildLegacyPreviewPayload = (
  entries: LegacyOverrideEntry[]
): Record<string, unknown> => {
  const payload: Record<string, unknown> = {}
  for (const entry of entries) {
    const key = entry.key.trim()
    if (!key) continue
    payload[key] = parseStructuredValueText(entry.value_text)
  }
  return payload
}

// Validation

const validateOperations = (
  operations: ParamOverrideOperation[],
  t: (key: string, options?: Record<string, unknown>) => string
): string => {
  for (let i = 0; i < operations.length; i++) {
    const op = operations[i]
    const mode = op.mode || 'set'
    const meta = MODE_META[mode] || MODE_META.set
    const line = i + 1
    const pathValue = op.path.trim()
    const fromValue = op.from.trim()
    const toValue = op.to.trim()

    if (meta.path && !pathValue)
      return t('Rule {{line}} is missing target path', { line })
    if (FROM_REQUIRED_MODES.has(mode) && !fromValue) {
      if (!(meta.pathAlias && pathValue))
        return t('Rule {{line}} is missing source field', { line })
    }
    if (TO_REQUIRED_MODES.has(mode) && !toValue) {
      if (!(meta.pathAlias && pathValue))
        return t('Rule {{line}} is missing target field', { line })
    }
    if (VALUE_REQUIRED_MODES.has(mode) && op.value_text.trim() === '')
      return t('Rule {{line}} is missing value', { line })

    if (mode === 'return_error') {
      const raw = op.value_text.trim()
      if (!raw) return t('Rule {{line}} is missing value', { line })
      try {
        const parsed = JSON.parse(raw)
        if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
          const parsedObject = parsed as Record<string, unknown>
          if (
            !String(
              parsedObject.message !== undefined
                ? parsedObject.message
                : parsedObject.msg || ''
            ).trim()
          )
            return t('Rule {{line}} return_error requires a message field', {
              line,
            })
        }
      } catch {
        /* plain string is allowed */
      }
    }

    if (mode === 'prune_objects') {
      const raw = op.value_text.trim()
      if (!raw)
        return t('Rule {{line}} prune_objects is missing conditions', { line })
      const draft = parsePruneObjectsDraft(raw)
      if (
        !draft.typeText.trim() &&
        !draft.rules.some((rule) => rule.path.trim())
      ) {
        return t('Rule {{line}} prune_objects is missing conditions', { line })
      }
    }

    if (mode === 'set_header') {
      const parsed = parseLooseValue(op.value_text)
      if (parsed === null || parsed === undefined) {
        return t('Rule {{line}} is missing value', { line })
      }
      if (typeof parsed === 'string' && !parsed.trim()) {
        return t('Rule {{line}} is missing value', { line })
      }
      if (
        parsed &&
        typeof parsed === 'object' &&
        !Array.isArray(parsed) &&
        Object.keys(parsed as Record<string, unknown>).length === 0
      ) {
        return t('Rule {{line}} is missing value', { line })
      }
    }

    if (mode === 'pass_headers') {
      const raw = op.value_text.trim()
      if (!raw)
        return t('Rule {{line}} pass_headers is missing header names', { line })
      const parsed = parseLooseValue(raw)
      const headers = parsePassHeaderNames(parsed)
      if (headers.length === 0)
        return t('Rule {{line}} pass_headers format is invalid', { line })
    }
  }
  return ''
}

// Parse initial state

type EditorState = {
  editMode: 'visual' | 'json'
  visualMode: 'operations' | 'legacy'
  legacyEntries: LegacyOverrideEntry[]
  operations: ParamOverrideOperation[]
  jsonText: string
  jsonError: string
}

const parseInitialState = (rawValue: string): EditorState => {
  const text = typeof rawValue === 'string' ? rawValue : ''
  const trimmed = text.trim()
  if (!trimmed) {
    return {
      editMode: 'visual',
      visualMode: 'operations',
      legacyEntries: [createDefaultLegacyEntry()],
      operations: [createDefaultOperation()],
      jsonText: '',
      jsonError: '',
    }
  }

  if (!verifyJSON(trimmed)) {
    return {
      editMode: 'json',
      visualMode: 'operations',
      legacyEntries: [createDefaultLegacyEntry()],
      operations: [createDefaultOperation()],
      jsonText: text,
      jsonError: 'Invalid JSON format',
    }
  }

  const parsed = JSON.parse(trimmed) as Record<string, unknown>
  const pretty = JSON.stringify(parsed, null, 2)

  if (
    parsed &&
    typeof parsed === 'object' &&
    !Array.isArray(parsed) &&
    Array.isArray(parsed.operations)
  ) {
    return {
      editMode: 'visual',
      visualMode: 'operations',
      legacyEntries: getLegacyEntriesFromObject(
        parsed as Record<string, unknown>,
        { excludeOperations: true }
      ),
      operations:
        (parsed.operations as Record<string, unknown>[]).length > 0
          ? (parsed.operations as Record<string, unknown>[]).map(
              normalizeOperation
            )
          : [createDefaultOperation()],
      jsonText: pretty,
      jsonError: '',
    }
  }

  if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
    return {
      editMode: 'visual',
      visualMode: 'legacy',
      legacyEntries: getLegacyEntriesFromObject(
        parsed as Record<string, unknown>
      ),
      operations: [createDefaultOperation()],
      jsonText: pretty,
      jsonError: '',
    }
  }

  return {
    editMode: 'json',
    visualMode: 'operations',
    legacyEntries: [createDefaultLegacyEntry()],
    operations: [createDefaultOperation()],
    jsonText: pretty,
    jsonError: '',
  }
}

// Build operations JSON

const buildOperationsJson = (
  sourceOperations: ParamOverrideOperation[],
  options: { validate: boolean },
  t: (key: string, options?: Record<string, unknown>) => string
): string => {
  const filteredOps = sourceOperations.filter((o) => !isOperationBlank(o))
  if (filteredOps.length === 0) return ''

  if (options.validate) {
    const message = validateOperations(filteredOps, t)
    if (message) throw new Error(message)
  }

  const payloadOps = filteredOps.map((operation) => {
    const mode = operation.mode || 'set'
    const meta = MODE_META[mode] || MODE_META.set
    const descriptionValue = String(operation.description || '').trim()
    const pathValue = operation.path.trim()
    const fromValue = operation.from.trim()
    const toValue = operation.to.trim()
    const payload: Record<string, unknown> = { mode }
    if (descriptionValue) payload.description = descriptionValue
    if (meta.path) payload.path = pathValue
    if (meta.pathOptional && pathValue) payload.path = pathValue
    if (meta.value) {
      if (mode === 'pass_headers') {
        payload.value = parseLooseValue(operation.value_text)
      } else if (mode === 'set_header') {
        payload.value = parseLooseValue(operation.value_text)
      } else if (mode === 'return_error' || mode === 'prune_objects') {
        payload.value = parseLooseValue(operation.value_text)
      } else {
        payload.value = parseStructuredValueText(operation.value_text)
      }
    }
    if (meta.keepOrigin && operation.keep_origin) payload.keep_origin = true
    if (meta.from) payload.from = fromValue
    if (!meta.to && operation.to.trim()) payload.to = toValue
    if (meta.to) payload.to = toValue
    if (meta.pathAlias) {
      if (!payload.from && pathValue) payload.from = pathValue
      if (!payload.to && pathValue) payload.to = pathValue
    }
    const conditions = operation.conditions
      .map(buildConditionPayload)
      .filter(Boolean)
    if (conditions.length > 0) {
      payload.conditions = conditions
      payload.logic = operation.logic === 'AND' ? 'AND' : 'OR'
    }
    return payload
  })

  return JSON.stringify({ operations: payloadOps }, null, 2)
}

const getOperationDedupKey = (operation: ParamOverrideOperation): string =>
  buildOperationsJson([operation], { validate: false }, (key) => key)

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function ParamOverrideEditorDialog(
  props: ParamOverrideEditorDialogProps
) {
  const { t } = useTranslation()

  const [editMode, setEditMode] = useState<'visual' | 'json'>('visual')
  const [visualMode, setVisualMode] = useState<'operations' | 'legacy'>(
    'operations'
  )
  const [legacyEntries, setLegacyEntries] = useState<LegacyOverrideEntry[]>([
    createDefaultLegacyEntry(),
  ])
  const [operations, setOperations] = useState<ParamOverrideOperation[]>([
    createDefaultOperation(),
  ])
  const [jsonText, setJsonText] = useState('')
  const [jsonError, setJsonError] = useState('')
  const [operationSearch, setOperationSearch] = useState('')
  const [selectedOperationId, setSelectedOperationId] = useState('')
  const [expandedConditions, setExpandedConditions] = useState<
    Record<string, boolean>
  >({})
  const [draggedOperationId, setDraggedOperationId] = useState('')
  const [dragOverOperationId, setDragOverOperationId] = useState('')
  const [dragOverPosition, setDragOverPosition] = useState<'before' | 'after'>(
    'before'
  )
  const [templatePresetKey, setTemplatePresetKey] = useState(
    'codex_cli_headers_passthrough'
  )

  // Initialize state when dialog opens
  useEffect(() => {
    if (!props.open) return
    const state = parseInitialState(props.value)
    setEditMode(state.editMode)
    setVisualMode(state.visualMode)
    setLegacyEntries(state.legacyEntries)
    setOperations(state.operations)
    setJsonText(state.jsonText)
    setJsonError(state.jsonError)
    setOperationSearch('')
    setSelectedOperationId(state.operations[0]?.id || '')
    setExpandedConditions({})
    setDraggedOperationId('')
    setDragOverOperationId('')
    setDragOverPosition('before')
    setTemplatePresetKey('codex_cli_headers_passthrough')
  }, [props.open, props.value])

  // Keep selectedOperationId valid
  useEffect(() => {
    if (operations.length === 0) {
      setSelectedOperationId('')
      return
    }
    if (!operations.some((o) => o.id === selectedOperationId)) {
      setSelectedOperationId(operations[0].id)
    }
  }, [operations, selectedOperationId])

  // Template presets
  const quickTemplatePresets = useMemo(
    () =>
      QUICK_TEMPLATE_PRESETS.map((key) => ({
        key,
        config: TEMPLATE_PRESET_CONFIG[key],
      })).filter((item) => item.config),
    []
  )

  const templatePresetOptions = useMemo(
    () =>
      TEMPLATE_GROUPS.map((group) => ({
        ...group,
        options: Object.entries(TEMPLATE_PRESET_CONFIG)
          .filter(([, config]) => config.group === group.value)
          .map(([value, config]) => ({
            value,
            label: config.label,
          })),
      })).filter((group) => group.options.length > 0),
    []
  )

  const operationCount = useMemo(
    () => operations.filter((o) => !isOperationBlank(o)).length,
    [operations]
  )

  const filteredOperations = useMemo(() => {
    const keyword = operationSearch.trim().toLowerCase()
    if (!keyword) return operations
    return operations.filter((op) => {
      const searchableText = [
        op.description,
        op.mode,
        op.path,
        op.from,
        op.to,
        op.value_text,
      ]
        .filter(Boolean)
        .join(' ')
        .toLowerCase()
      return searchableText.includes(keyword)
    })
  }, [operationSearch, operations])

  const selectedOperation = useMemo(
    () => operations.find((o) => o.id === selectedOperationId),
    [operations, selectedOperationId]
  )

  const selectedOperationIndex = useMemo(
    () => operations.findIndex((o) => o.id === selectedOperationId),
    [operations, selectedOperationId]
  )

  const returnErrorDraft = useMemo(() => {
    if (!selectedOperation || selectedOperation.mode !== 'return_error')
      return null
    return parseReturnErrorDraft(selectedOperation.value_text)
  }, [selectedOperation])

  const pruneObjectsDraft = useMemo(() => {
    if (!selectedOperation || selectedOperation.mode !== 'prune_objects')
      return null
    return parsePruneObjectsDraft(selectedOperation.value_text)
  }, [selectedOperation])

  const topOperationModes = useMemo(() => {
    const counts: Record<string, number> = {}
    for (const op of operations) {
      const mode = op.mode || 'set'
      counts[mode] = (counts[mode] || 0) + 1
    }
    return Object.entries(counts)
      .sort((a, b) => b[1] - a[1])
      .slice(0, 4)
  }, [operations])

  const selectedTemplatePreset =
    TEMPLATE_PRESET_CONFIG[templatePresetKey] ||
    TEMPLATE_PRESET_CONFIG.codex_cli_headers_passthrough

  // ---------------------------------------------------------------------------
  // Operations
  // ---------------------------------------------------------------------------

  const updateOperation = useCallback(
    (operationId: string, patch: Partial<ParamOverrideOperation>) => {
      setOperations((prev) =>
        prev.map((o) => (o.id === operationId ? { ...o, ...patch } : o))
      )
    },
    []
  )

  const addOperation = useCallback(() => {
    const created = createDefaultOperation()
    setOperations((prev) => [...prev, created])
    setSelectedOperationId(created.id)
  }, [])

  const duplicateOperation = useCallback((operationId: string) => {
    let insertedId = ''
    setOperations((prev) => {
      const idx = prev.findIndex((o) => o.id === operationId)
      if (idx < 0) return prev
      const source = prev[idx]
      const cloned = normalizeOperation({
        description: source.description,
        path: source.path,
        mode: source.mode,
        value:
          source.mode === 'set_header' ||
          source.mode === 'pass_headers' ||
          source.mode === 'return_error' ||
          source.mode === 'prune_objects'
            ? parseLooseValue(source.value_text)
            : parseStructuredValueText(source.value_text),
        keep_origin: source.keep_origin,
        from: source.from,
        to: source.to,
        logic: source.logic,
        conditions: source.conditions.map((c) => ({
          path: c.path,
          mode: c.mode,
          value: parseStructuredValueText(c.value_text),
          invert: c.invert,
          pass_missing_key: c.pass_missing_key,
        })),
      })
      insertedId = cloned.id
      const next = [...prev]
      next.splice(idx + 1, 0, cloned)
      return next
    })
    if (insertedId) setSelectedOperationId(insertedId)
  }, [])

  const removeOperation = useCallback((operationId: string) => {
    setOperations((prev) => {
      if (prev.length <= 1) return [createDefaultOperation()]
      return prev.filter((o) => o.id !== operationId)
    })
  }, [])

  // Conditions
  const addCondition = useCallback((operationId: string) => {
    const created = createDefaultCondition()
    setOperations((prev) =>
      prev.map((op) =>
        op.id === operationId
          ? { ...op, conditions: [...op.conditions, created] }
          : op
      )
    )
    setExpandedConditions((prev) => ({ ...prev, [created.id]: true }))
  }, [])

  const updateCondition = useCallback(
    (
      operationId: string,
      conditionId: string,
      patch: Partial<ParamOverrideCondition>
    ) => {
      setOperations((prev) =>
        prev.map((op) =>
          op.id === operationId
            ? {
                ...op,
                conditions: op.conditions.map((c) =>
                  c.id === conditionId ? { ...c, ...patch } : c
                ),
              }
            : op
        )
      )
    },
    []
  )

  const removeCondition = useCallback(
    (operationId: string, conditionId: string) => {
      setOperations((prev) =>
        prev.map((op) =>
          op.id === operationId
            ? {
                ...op,
                conditions: op.conditions.filter((c) => c.id !== conditionId),
              }
            : op
        )
      )
    },
    []
  )

  const updateLegacyEntry = useCallback(
    (entryId: string, patch: Partial<LegacyOverrideEntry>) => {
      setLegacyEntries((prev) =>
        prev.map((entry) =>
          entry.id === entryId ? { ...entry, ...patch } : entry
        )
      )
    },
    []
  )

  const addLegacyEntry = useCallback(() => {
    setLegacyEntries((prev) => [...prev, createDefaultLegacyEntry()])
  }, [])

  const removeLegacyEntry = useCallback((entryId: string) => {
    setLegacyEntries((prev) => {
      if (prev.length <= 1) return [createDefaultLegacyEntry()]
      return prev.filter((entry) => entry.id !== entryId)
    })
  }, [])

  // return_error draft
  const updateReturnErrorDraft = useCallback(
    (operationId: string, draftPatch: Partial<ReturnErrorDraft>) => {
      setOperations((prev) =>
        prev.map((op) => {
          if (op.id !== operationId) return op
          const draft = parseReturnErrorDraft(op.value_text)
          const nextDraft = { ...draft, ...draftPatch }
          return {
            ...op,
            value_text: buildReturnErrorValueText(nextDraft),
          }
        })
      )
    },
    []
  )

  // prune_objects draft
  const updatePruneObjectsDraft = useCallback(
    (
      operationId: string,
      updater:
        | Partial<PruneObjectsDraft>
        | ((draft: PruneObjectsDraft) => PruneObjectsDraft)
    ) => {
      setOperations((prev) =>
        prev.map((op) => {
          if (op.id !== operationId) return op
          const draft = parsePruneObjectsDraft(op.value_text)
          const nextDraft =
            typeof updater === 'function'
              ? updater(draft)
              : { ...draft, ...updater }
          return {
            ...op,
            value_text: buildPruneObjectsValueText(nextDraft),
          }
        })
      )
    },
    []
  )

  const addPruneRule = useCallback(
    (operationId: string) => {
      updatePruneObjectsDraft(operationId, (draft) => ({
        ...draft,
        simpleMode: false,
        rules: [...draft.rules, normalizePruneRule({})],
      }))
    },
    [updatePruneObjectsDraft]
  )

  const updatePruneRule = useCallback(
    (operationId: string, ruleId: string, patch: Partial<PruneRule>) => {
      updatePruneObjectsDraft(operationId, (draft) => ({
        ...draft,
        rules: draft.rules.map((r) =>
          r.id === ruleId ? { ...r, ...patch } : r
        ),
      }))
    },
    [updatePruneObjectsDraft]
  )

  const removePruneRule = useCallback(
    (operationId: string, ruleId: string) => {
      updatePruneObjectsDraft(operationId, (draft) => ({
        ...draft,
        rules: draft.rules.filter((r) => r.id !== ruleId),
      }))
    },
    [updatePruneObjectsDraft]
  )

  // Drag and drop
  const resetDragState = useCallback(() => {
    setDraggedOperationId('')
    setDragOverOperationId('')
    setDragOverPosition('before')
  }, [])

  const handleDragStart = useCallback(
    (event: DragEvent, operationId: string) => {
      setDraggedOperationId(operationId)
      setSelectedOperationId(operationId)
      event.dataTransfer.effectAllowed = 'move'
      event.dataTransfer.setData('text/plain', operationId)
    },
    []
  )

  const handleDragOver = useCallback(
    (event: DragEvent, operationId: string) => {
      event.preventDefault()
      if (!draggedOperationId || draggedOperationId === operationId) return
      const rect = event.currentTarget.getBoundingClientRect()
      const position: 'before' | 'after' =
        event.clientY - rect.top > rect.height / 2 ? 'after' : 'before'
      setDragOverOperationId(operationId)
      setDragOverPosition(position)
      event.dataTransfer.dropEffect = 'move'
    },
    [draggedOperationId]
  )

  const handleDrop = useCallback(
    (event: DragEvent, operationId: string) => {
      event.preventDefault()
      const sourceId =
        draggedOperationId || event.dataTransfer.getData('text/plain')
      const position =
        dragOverOperationId === operationId ? dragOverPosition : 'before'
      if (sourceId && operationId && sourceId !== operationId) {
        setOperations((prev) =>
          reorderOperations(prev, sourceId, operationId, position)
        )
        setSelectedOperationId(sourceId)
      }
      resetDragState()
    },
    [draggedOperationId, dragOverOperationId, dragOverPosition, resetDragState]
  )

  // ---------------------------------------------------------------------------
  // Mode switching & templates
  // ---------------------------------------------------------------------------

  const buildVisualJson = useCallback((): string => {
    const legacyPayload = buildLegacyOverridePayload(legacyEntries, t)
    if (visualMode === 'legacy') {
      if (legacyPayload.count === 0) return ''
      return JSON.stringify(legacyPayload.value, null, 2)
    }
    const operationsJson = buildOperationsJson(
      operations,
      { validate: true },
      t
    )
    if (!operationsJson) {
      return legacyPayload.count > 0
        ? JSON.stringify(legacyPayload.value, null, 2)
        : ''
    }
    const operationsPayload = JSON.parse(operationsJson) as Record<
      string,
      unknown
    >
    return JSON.stringify(
      { ...legacyPayload.value, ...operationsPayload },
      null,
      2
    )
  }, [legacyEntries, operations, t, visualMode])

  const buildVisualJsonPreview = useCallback((): string => {
    if (visualMode === 'legacy') {
      return JSON.stringify(buildLegacyPreviewPayload(legacyEntries), null, 2)
    }
    const legacyPayload = buildLegacyPreviewPayload(legacyEntries)
    const operationsJson = buildOperationsJson(
      operations,
      { validate: false },
      t
    )
    if (!operationsJson) {
      return Object.keys(legacyPayload).length > 0
        ? JSON.stringify(legacyPayload, null, 2)
        : ''
    }
    const operationsPayload = JSON.parse(operationsJson) as Record<
      string,
      unknown
    >
    return JSON.stringify({ ...legacyPayload, ...operationsPayload }, null, 2)
  }, [legacyEntries, operations, t, visualMode])

  const switchToJsonMode = useCallback(() => {
    if (editMode === 'json') return
    try {
      setJsonText(buildVisualJson())
      setJsonError('')
    } catch (error) {
      toast.error((error as Error).message)
      setJsonText(buildVisualJsonPreview())
      setJsonError(
        (error as Error).message || t('Parameter configuration error')
      )
    }
    setEditMode('json')
  }, [buildVisualJson, buildVisualJsonPreview, editMode, t])

  const switchToVisualMode = useCallback(() => {
    if (editMode === 'visual') return
    const trimmed = jsonText.trim()
    if (!trimmed) {
      const fallback = createDefaultOperation()
      setVisualMode('operations')
      setOperations([fallback])
      setSelectedOperationId(fallback.id)
      setLegacyEntries([createDefaultLegacyEntry()])
      setJsonError('')
      setEditMode('visual')
      return
    }
    if (!verifyJSON(trimmed)) {
      toast.error(t('Parameter override must be valid JSON format'))
      return
    }
    const parsed = JSON.parse(trimmed) as Record<string, unknown>
    if (
      parsed &&
      typeof parsed === 'object' &&
      !Array.isArray(parsed) &&
      Array.isArray(parsed.operations)
    ) {
      const nextOps =
        (parsed.operations as Record<string, unknown>[]).length > 0
          ? (parsed.operations as Record<string, unknown>[]).map(
              normalizeOperation
            )
          : [createDefaultOperation()]
      setVisualMode('operations')
      setOperations(nextOps)
      setSelectedOperationId(nextOps[0]?.id || '')
      setLegacyEntries(
        getLegacyEntriesFromObject(parsed as Record<string, unknown>, {
          excludeOperations: true,
        })
      )
      setJsonError('')
      setEditMode('visual')
      setTemplatePresetKey('codex_cli_headers_passthrough')
      return
    }
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const fallback = createDefaultOperation()
      setVisualMode('legacy')
      const entries = Object.entries(parsed as Record<string, unknown>).map(
        ([key, value]) => normalizeLegacyEntry(key, value)
      )
      setLegacyEntries(
        entries.length > 0 ? entries : [createDefaultLegacyEntry()]
      )
      setOperations([fallback])
      setSelectedOperationId(fallback.id)
      setJsonError('')
      setEditMode('visual')
      setTemplatePresetKey('codex_cli_headers_passthrough')
      return
    }
    toast.error(t('Parameter override must be a valid JSON object'))
  }, [editMode, jsonText, t])

  const applyTemplate = useCallback(
    (action: 'replace' | 'add') => {
      const preset =
        TEMPLATE_PRESET_CONFIG[templatePresetKey] ||
        TEMPLATE_PRESET_CONFIG.codex_cli_headers_passthrough
      const payload = preset.payload as Record<string, unknown>

      if (preset.kind === 'legacy') {
        if (action === 'add') {
          const current: Record<string, unknown> = {}
          for (const entry of legacyEntries) {
            const key = entry.key.trim()
            if (!key) continue
            current[key] = parseStructuredValueText(entry.value_text)
          }
          const merged = { ...(payload || {}), ...current }
          const entries = Object.entries(merged).map(([key, value]) =>
            normalizeLegacyEntry(key, value)
          )
          setLegacyEntries(
            entries.length > 0 ? entries : [createDefaultLegacyEntry()]
          )
          setJsonText(JSON.stringify(merged, null, 2))
          setJsonError('')
          setEditMode('visual')
        } else {
          const entries = Object.entries(payload || {}).map(([key, value]) =>
            normalizeLegacyEntry(key, value)
          )
          setVisualMode('legacy')
          setLegacyEntries(
            entries.length > 0 ? entries : [createDefaultLegacyEntry()]
          )
          setOperations([createDefaultOperation()])
          setJsonText(JSON.stringify(payload, null, 2))
          setJsonError('')
          setEditMode('visual')
        }
        return
      }

      const operationsPayload = ((payload as Record<string, unknown>)
        .operations || []) as Record<string, unknown>[]

      if (action === 'add') {
        const appended = operationsPayload.map(normalizeOperation)
        const existing =
          visualMode === 'operations'
            ? operations.filter((o) => !isOperationBlank(o))
            : []
        const existingKeys = new Set(existing.map(getOperationDedupKey))
        const uniqueAppended = appended.filter((operation) => {
          const key = getOperationDedupKey(operation)
          if (existingKeys.has(key)) return false
          existingKeys.add(key)
          return true
        })
        const nextOps = [...existing, ...uniqueAppended]
        setVisualMode('operations')
        setOperations(nextOps.length > 0 ? nextOps : appended)
        setSelectedOperationId(
          uniqueAppended[0]?.id || nextOps[0]?.id || appended[0]?.id || ''
        )
        setJsonError('')
        setEditMode('visual')
        setJsonText('')
      } else {
        const nextOps = operationsPayload.map(normalizeOperation)
        const finalOps =
          nextOps.length > 0 ? nextOps : [createDefaultOperation()]
        setVisualMode('operations')
        setOperations(finalOps)
        setSelectedOperationId(finalOps[0]?.id || '')
        setJsonText(JSON.stringify({ operations: operationsPayload }, null, 2))
        setLegacyEntries([createDefaultLegacyEntry()])
        setJsonError('')
        setEditMode('visual')
      }
    },
    [legacyEntries, operations, templatePresetKey, visualMode]
  )

  const resetEditorState = useCallback(() => {
    const fallback = createDefaultOperation()
    setVisualMode('operations')
    setLegacyEntries([createDefaultLegacyEntry()])
    setOperations([fallback])
    setSelectedOperationId(fallback.id)
    setJsonText('')
    setJsonError('')
    setTemplatePresetKey('codex_cli_headers_passthrough')
    setEditMode('visual')
  }, [])

  // JSON mode
  const handleJsonChange = useCallback(
    (nextValue: string) => {
      setJsonText(nextValue)
      const trimmed = nextValue.trim()
      if (!trimmed) {
        setJsonError('')
        return
      }
      setJsonError(verifyJSON(trimmed) ? '' : t('JSON format error'))
    },
    [t]
  )

  const formatJson = useCallback(() => {
    const trimmed = jsonText.trim()
    if (!trimmed) return
    if (!verifyJSON(trimmed)) {
      toast.error(t('Parameter override must be valid JSON format'))
      return
    }
    setJsonText(JSON.stringify(JSON.parse(trimmed), null, 2))
    setJsonError('')
  }, [jsonText, t])

  const visualValidationError = useMemo(() => {
    if (editMode !== 'visual') return ''
    try {
      buildVisualJson()
      return ''
    } catch (error) {
      return (error as Error)?.message || t('Parameter configuration error')
    }
  }, [buildVisualJson, editMode, t])

  // Save
  const handleSave = useCallback(() => {
    try {
      let result = ''
      if (editMode === 'json') {
        const trimmed = jsonText.trim()
        if (trimmed) {
          if (!verifyJSON(trimmed))
            throw new Error(t('Parameter override must be valid JSON format'))
          result = JSON.stringify(JSON.parse(trimmed), null, 2)
        }
      } else {
        result = buildVisualJson()
      }
      props.onSave(result)
      props.onOpenChange(false)
    } catch (error) {
      toast.error((error as Error).message)
    }
  }, [buildVisualJson, editMode, jsonText, props, t])

  // Expand/collapse all conditions
  const expandAllConditions = useCallback(() => {
    if (!selectedOperation) return
    const map: Record<string, boolean> = {}
    for (const c of selectedOperation.conditions) map[c.id] = true
    setExpandedConditions((prev) => ({ ...prev, ...map }))
  }, [selectedOperation])

  const collapseAllConditions = useCallback(() => {
    if (!selectedOperation) return
    const map: Record<string, boolean> = {}
    for (const c of selectedOperation.conditions) map[c.id] = false
    setExpandedConditions((prev) => ({ ...prev, ...map }))
  }, [selectedOperation])

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='flex max-h-[90vh] flex-col gap-0 p-0 sm:max-w-5xl'>
        <DialogHeader className='border-b px-6 py-4'>
          <DialogTitle>{t('Parameter Override')}</DialogTitle>
          <DialogDescription>
            {t(
              'Create request parameter override rules with a visual editor or raw JSON.'
            )}
          </DialogDescription>
        </DialogHeader>

        {/* Toolbar */}
        <div className='bg-muted/30 border-b px-4 py-3'>
          <div className='flex flex-col gap-3'>
            <div className='flex flex-wrap items-center justify-between gap-2'>
              <div className='flex flex-wrap items-center gap-2'>
                <span className='text-muted-foreground text-xs font-medium'>
                  {t('Edit Mode')}
                </span>
                <Button
                  type='button'
                  variant={editMode === 'visual' ? 'default' : 'secondary'}
                  size='sm'
                  aria-pressed={editMode === 'visual'}
                  className='h-8 rounded-full px-4 text-sm font-semibold'
                  onClick={switchToVisualMode}
                >
                  {t('Visual')}
                </Button>
                <Button
                  type='button'
                  variant={editMode === 'json' ? 'default' : 'secondary'}
                  size='sm'
                  aria-pressed={editMode === 'json'}
                  className='h-8 rounded-full px-4 text-sm font-semibold'
                  onClick={switchToJsonMode}
                >
                  {t('JSON Text')}
                </Button>
              </div>
              <Button
                type='button'
                variant='ghost'
                size='sm'
                onClick={resetEditorState}
              >
                {t('Reset')}
              </Button>
            </div>

            <div className='bg-background rounded-lg border p-3'>
              <div className='flex flex-wrap items-start justify-between gap-2'>
                <div className='min-w-0'>
                  <p className='text-sm font-medium'>
                    {t('Preset Rule Library')}
                  </p>
                  <p className='text-muted-foreground mt-1 text-xs'>
                    {t(
                      'Pick a scenario first. It will not change this channel until you apply it.'
                    )}
                  </p>
                </div>
                <Badge variant='secondary' className='max-w-full truncate'>
                  {t(selectedTemplatePreset.label)}
                </Badge>
              </div>

              <div className='mt-3 flex flex-wrap gap-2'>
                {quickTemplatePresets.map(({ key, config }) => (
                  <Button
                    key={key}
                    type='button'
                    variant={key === templatePresetKey ? 'default' : 'outline'}
                    size='sm'
                    className='h-8 max-w-full justify-start truncate text-xs'
                    onClick={() => setTemplatePresetKey(key)}
                  >
                    {t(config.label)}
                  </Button>
                ))}
              </div>

              <div className='mt-3 grid gap-2 lg:grid-cols-[minmax(220px,1fr)_auto_auto]'>
                <Select
                  value={templatePresetKey}
                  onValueChange={(v) =>
                    setTemplatePresetKey(v || 'codex_cli_headers_passthrough')
                  }
                >
                  <SelectTrigger className='h-8 w-full'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {templatePresetOptions.map((group) => (
                      <SelectGroup key={group.value}>
                        <SelectLabel>{t(group.label)}</SelectLabel>
                        {group.options.map((o) => (
                          <SelectItem key={o.value} value={o.value}>
                            {t(o.label)}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    ))}
                  </SelectContent>
                </Select>
                <Button
                  type='button'
                  variant='default'
                  size='sm'
                  className='whitespace-nowrap'
                  onClick={() => applyTemplate('replace')}
                >
                  {t('Replace Current Rules')}
                </Button>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  className='whitespace-nowrap'
                  onClick={() => applyTemplate('add')}
                >
                  <Plus className='mr-1 h-3.5 w-3.5' />
                  {t('Append to Existing Rules')}
                </Button>
              </div>
              {selectedTemplatePreset.description ? (
                <p className='text-muted-foreground mt-2 text-xs'>
                  {t(selectedTemplatePreset.description)}
                </p>
              ) : null}
              <p className='text-muted-foreground mt-2 text-xs'>
                {t(
                  'Replace Current Rules removes existing rules first. Append keeps existing rules and adds the selected preset after them.'
                )}
              </p>
            </div>
          </div>
        </div>

        {/* Content */}
        <div className='min-h-0 flex-1 overflow-hidden'>
          {editMode === 'visual' ? (
            visualMode === 'legacy' ? (
              <div className='p-4'>
                <LegacyOverrideEditor
                  entries={legacyEntries}
                  updateEntry={updateLegacyEntry}
                  addEntry={addLegacyEntry}
                  removeEntry={removeLegacyEntry}
                />
              </div>
            ) : (
              <div className='flex h-full'>
                {/* Left sidebar */}
                <div className='flex w-[280px] flex-shrink-0 flex-col border-r'>
                  <div className='flex items-center justify-between border-b px-3 py-2'>
                    <div className='flex items-center gap-2'>
                      <span className='text-sm font-medium'>{t('Rules')}</span>
                      <Badge variant='secondary'>
                        {operationCount}/{operations.length}
                      </Badge>
                    </div>
                    <Button
                      type='button'
                      variant='ghost'
                      size='sm'
                      onClick={addOperation}
                    >
                      <Plus className='h-4 w-4' />
                    </Button>
                  </div>

                  {topOperationModes.length > 0 && (
                    <div className='flex flex-wrap gap-1 border-b px-3 py-2'>
                      {topOperationModes.map(([mode, count]) => (
                        <span
                          key={`mode_stat_${mode}`}
                          className={cn(
                            'inline-flex items-center rounded-md border px-1.5 py-0.5 text-[10px] font-medium',
                            getModeTagTailwind(mode)
                          )}
                        >
                          {t(OPERATION_MODE_LABEL_MAP[mode] || mode)} · {count}
                        </span>
                      ))}
                    </div>
                  )}

                  <div className='px-3 py-2'>
                    <div className='relative'>
                      <Search className='text-muted-foreground absolute top-2.5 left-2.5 h-3.5 w-3.5' />
                      <Input
                        value={operationSearch}
                        onChange={(e) => setOperationSearch(e.target.value)}
                        placeholder={t('Search rules...')}
                        className='h-8 pl-8 text-xs'
                      />
                    </div>
                  </div>

                  <ScrollArea className='flex-1'>
                    <div className='flex flex-col gap-1 px-3 pb-3'>
                      {filteredOperations.length === 0 ? (
                        <p className='text-muted-foreground py-4 text-center text-xs'>
                          {t('No matching rules')}
                        </p>
                      ) : (
                        filteredOperations.map((operation) => {
                          const index = operations.findIndex(
                            (o) => o.id === operation.id
                          )
                          const isActive = operation.id === selectedOperationId
                          const isDragging = operation.id === draggedOperationId
                          const isDropTarget =
                            operation.id === dragOverOperationId &&
                            draggedOperationId !== '' &&
                            draggedOperationId !== operation.id
                          return (
                            <div
                              key={operation.id}
                              role='button'
                              tabIndex={0}
                              draggable={operations.length > 1}
                              onClick={() =>
                                setSelectedOperationId(operation.id)
                              }
                              onDragStart={(e) =>
                                handleDragStart(e, operation.id)
                              }
                              onDragOver={(e) =>
                                handleDragOver(e, operation.id)
                              }
                              onDrop={(e) => handleDrop(e, operation.id)}
                              onDragEnd={resetDragState}
                              onKeyDown={(e: KeyboardEvent) => {
                                if (e.key === 'Enter' || e.key === ' ') {
                                  e.preventDefault()
                                  setSelectedOperationId(operation.id)
                                }
                              }}
                              className={cn(
                                'cursor-pointer rounded-lg border p-2.5 transition-colors',
                                isActive
                                  ? 'border-primary bg-primary/5'
                                  : 'hover:bg-muted/50',
                                isDragging && 'opacity-50',
                                isDropTarget &&
                                  dragOverPosition === 'before' &&
                                  'border-t-primary border-t-2',
                                isDropTarget &&
                                  dragOverPosition === 'after' &&
                                  'border-b-primary border-b-2'
                              )}
                            >
                              <div className='flex items-start gap-2'>
                                <GripVertical
                                  className={cn(
                                    'text-muted-foreground mt-0.5 h-3.5 w-3.5 flex-shrink-0',
                                    operations.length > 1
                                      ? 'cursor-grab'
                                      : 'cursor-default'
                                  )}
                                />
                                <div className='min-w-0 flex-1'>
                                  <div className='flex items-center justify-between gap-1'>
                                    <span className='text-xs font-semibold'>
                                      #{index + 1}
                                    </span>
                                    <Badge
                                      variant='outline'
                                      className='text-[10px]'
                                    >
                                      {operation.conditions.length}
                                    </Badge>
                                  </div>
                                  <p className='text-muted-foreground mt-0.5 line-clamp-1 text-[11px]'>
                                    {getOperationSummary(operation, index)}
                                  </p>
                                  {operation.description.trim() && (
                                    <p className='text-muted-foreground mt-0.5 line-clamp-2 text-[10px]'>
                                      {operation.description}
                                    </p>
                                  )}
                                  <span
                                    className={cn(
                                      'mt-1 inline-flex items-center rounded-md border px-1.5 py-0.5 text-[10px] font-medium',
                                      getModeTagTailwind(
                                        operation.mode || 'set'
                                      )
                                    )}
                                  >
                                    {t(
                                      OPERATION_MODE_LABEL_MAP[
                                        operation.mode || 'set'
                                      ] ||
                                        operation.mode ||
                                        'set'
                                    )}
                                  </span>
                                </div>
                              </div>
                            </div>
                          )
                        })
                      )}
                    </div>
                  </ScrollArea>
                </div>

                {/* Right panel - Rule editor */}
                <div className='flex min-w-0 flex-1 flex-col overflow-y-auto'>
                  <div className='border-b p-3'>
                    <Collapsible>
                      <CollapsibleTrigger className='hover:bg-muted/50 flex w-full items-center justify-between rounded-md px-2 py-1.5 text-left'>
                        <div className='flex items-center gap-2'>
                          <FileSliders className='text-muted-foreground h-3.5 w-3.5' />
                          <span className='text-xs font-medium'>
                            {t('Top-level Field Overrides')}
                          </span>
                          <Badge variant='secondary' className='text-[10px]'>
                            {
                              legacyEntries.filter(
                                (entry) =>
                                  entry.key.trim() || entry.value_text.trim()
                              ).length
                            }
                          </Badge>
                        </div>
                        <ChevronDown className='text-muted-foreground h-3.5 w-3.5' />
                      </CollapsibleTrigger>
                      <CollapsibleContent className='pt-2'>
                        <LegacyOverrideEditor
                          compact
                          entries={legacyEntries}
                          updateEntry={updateLegacyEntry}
                          addEntry={addLegacyEntry}
                          removeEntry={removeLegacyEntry}
                        />
                      </CollapsibleContent>
                    </Collapsible>
                  </div>
                  {selectedOperation ? (
                    <RuleEditor
                      operation={selectedOperation}
                      operationIndex={selectedOperationIndex}
                      operations={operations}
                      returnErrorDraft={returnErrorDraft}
                      pruneObjectsDraft={pruneObjectsDraft}
                      expandedConditions={expandedConditions}
                      setExpandedConditions={setExpandedConditions}
                      updateOperation={updateOperation}
                      duplicateOperation={duplicateOperation}
                      removeOperation={removeOperation}
                      addCondition={addCondition}
                      updateCondition={updateCondition}
                      removeCondition={removeCondition}
                      updateReturnErrorDraft={updateReturnErrorDraft}
                      updatePruneObjectsDraft={updatePruneObjectsDraft}
                      addPruneRule={addPruneRule}
                      updatePruneRule={updatePruneRule}
                      removePruneRule={removePruneRule}
                      expandAllConditions={expandAllConditions}
                      collapseAllConditions={collapseAllConditions}
                    />
                  ) : (
                    <div className='flex flex-1 items-center justify-center'>
                      <p className='text-muted-foreground text-sm'>
                        {t('Select a rule to edit.')}
                      </p>
                    </div>
                  )}

                  {visualValidationError && (
                    <div className='border-t px-4 py-2'>
                      <p className='text-destructive text-xs'>
                        {visualValidationError}
                      </p>
                    </div>
                  )}
                </div>
              </div>
            )
          ) : (
            /* JSON mode */
            <div className='p-4'>
              <div className='mb-2 flex items-center gap-2'>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={formatJson}
                >
                  {t('Format')}
                </Button>
                <span className='text-muted-foreground text-xs'>
                  {t('Advanced text editing')}
                </span>
              </div>
              <Textarea
                value={jsonText}
                onChange={(e) => handleJsonChange(e.target.value)}
                placeholder={JSON.stringify(OPERATION_TEMPLATE, null, 2)}
                rows={20}
                className='font-mono text-xs'
              />
              <p className='text-muted-foreground mt-2 text-xs'>
                {t(
                  'Edit JSON text directly. Format will be validated on save.'
                )}
              </p>
              {jsonError && (
                <p className='text-destructive mt-1 text-xs'>{jsonError}</p>
              )}
            </div>
          )}
        </div>

        {/* Footer */}
        <DialogFooter className='border-t px-6 py-4'>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='button' onClick={handleSave}>
            {t('Save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ---------------------------------------------------------------------------
// RuleEditor sub-component
// ---------------------------------------------------------------------------

type LegacyOverrideEditorProps = {
  entries: LegacyOverrideEntry[]
  updateEntry: (entryId: string, patch: Partial<LegacyOverrideEntry>) => void
  addEntry: () => void
  removeEntry: (entryId: string) => void
  compact?: boolean
}

function LegacyOverrideEditor(props: LegacyOverrideEditorProps) {
  const { t } = useTranslation()

  return (
    <div
      className={cn('rounded-lg border p-3', props.compact && 'border-dashed')}
    >
      <div className='mb-3 flex flex-wrap items-center justify-between gap-2'>
        <div>
          <p className='text-sm font-medium'>
            {props.compact
              ? t('Top-level Field Overrides')
              : t('Legacy Field Overrides')}
          </p>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t(
              props.compact
                ? 'These fields are saved next to operations in the same param_override object.'
                : 'Set top-level request fields with typed values. This matches the legacy JSON object format.'
            )}
          </p>
        </div>
        <Button
          type='button'
          variant='outline'
          size='sm'
          className='h-8 text-xs'
          onClick={props.addEntry}
        >
          <Plus className='mr-1 h-3 w-3' />
          {t('Add Field')}
        </Button>
      </div>

      <div className='space-y-3'>
        {props.entries.map((entry, index) => (
          <div key={entry.id} className='rounded-md border p-3'>
            <div className='mb-2 flex items-center justify-between gap-2'>
              <Badge variant='outline' className='text-[10px]'>
                #{index + 1}
              </Badge>
              <Button
                type='button'
                variant='ghost'
                size='sm'
                className='text-destructive hover:text-destructive h-7 text-xs'
                onClick={() => props.removeEntry(entry.id)}
              >
                <Trash2 className='mr-1 h-3 w-3' />
                {t('Delete')}
              </Button>
            </div>
            <div
              className={cn(
                'grid gap-3',
                props.compact ? 'grid-cols-1' : 'sm:grid-cols-[220px_1fr]'
              )}
            >
              <div className='space-y-1.5'>
                <label className='text-xs font-medium'>{t('Field Name')}</label>
                <Input
                  value={entry.key}
                  onChange={(event) =>
                    props.updateEntry(entry.id, { key: event.target.value })
                  }
                  placeholder='temperature'
                  className='h-8 text-xs'
                />
              </div>
              <div className='space-y-1.5'>
                <label className='text-xs font-medium'>
                  {t('Field Value')}
                </label>
                <StructuredValueNodeEditor
                  node={parseStructuredValueNodeForDisplay(entry.value_text)}
                  sourceKey={entry.value_text}
                  placeholder='0.7'
                  onChange={(node) =>
                    props.updateEntry(entry.id, {
                      value_text: buildStructuredValueText(node),
                    })
                  }
                />
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

type RuleEditorProps = {
  operation: ParamOverrideOperation
  operationIndex: number
  operations: ParamOverrideOperation[]
  returnErrorDraft: ReturnErrorDraft | null
  pruneObjectsDraft: PruneObjectsDraft | null
  expandedConditions: Record<string, boolean>
  setExpandedConditions: React.Dispatch<
    React.SetStateAction<Record<string, boolean>>
  >
  updateOperation: (
    operationId: string,
    patch: Partial<ParamOverrideOperation>
  ) => void
  duplicateOperation: (operationId: string) => void
  removeOperation: (operationId: string) => void
  addCondition: (operationId: string) => void
  updateCondition: (
    operationId: string,
    conditionId: string,
    patch: Partial<ParamOverrideCondition>
  ) => void
  removeCondition: (operationId: string, conditionId: string) => void
  updateReturnErrorDraft: (
    operationId: string,
    draftPatch: Partial<ReturnErrorDraft>
  ) => void
  updatePruneObjectsDraft: (
    operationId: string,
    updater:
      | Partial<PruneObjectsDraft>
      | ((draft: PruneObjectsDraft) => PruneObjectsDraft)
  ) => void
  addPruneRule: (operationId: string) => void
  updatePruneRule: (
    operationId: string,
    ruleId: string,
    patch: Partial<PruneRule>
  ) => void
  removePruneRule: (operationId: string, ruleId: string) => void
  expandAllConditions: () => void
  collapseAllConditions: () => void
}

function RuleEditor(ruleEditorProps: RuleEditorProps) {
  const { t } = useTranslation()
  const operation = ruleEditorProps.operation
  const mode = operation.mode || 'set'
  const meta = MODE_META[mode] || MODE_META.set
  const conditions = operation.conditions
  const syncFromTarget =
    mode === 'sync_fields' ? parseSyncTargetSpec(operation.from) : null
  const syncToTarget =
    mode === 'sync_fields' ? parseSyncTargetSpec(operation.to) : null

  return (
    <ScrollArea className='flex-1'>
      <div className='space-y-4 p-4'>
        {/* Header */}
        <div className='flex items-center justify-between'>
          <div className='flex items-center gap-2'>
            <Badge variant='outline'>
              #{ruleEditorProps.operationIndex + 1}
            </Badge>
            <span className='text-muted-foreground line-clamp-1 text-xs'>
              {getOperationSummary(operation, ruleEditorProps.operationIndex)}
            </span>
          </div>
          <div className='flex items-center gap-1'>
            <Button
              type='button'
              variant='ghost'
              size='sm'
              onClick={() => ruleEditorProps.duplicateOperation(operation.id)}
            >
              <Copy className='mr-1 h-3.5 w-3.5' />
              {t('Duplicate')}
            </Button>
            <Button
              type='button'
              variant='ghost'
              size='sm'
              className='text-destructive hover:text-destructive'
              onClick={() => ruleEditorProps.removeOperation(operation.id)}
            >
              <Trash2 className='mr-1 h-3.5 w-3.5' />
              {t('Delete')}
            </Button>
          </div>
        </div>

        {/* Operation type + path */}
        <div className='grid gap-3 sm:grid-cols-2'>
          <div className='space-y-1.5'>
            <label className='text-xs font-medium'>{t('Operation Type')}</label>
            <Select
              value={mode}
              onValueChange={(nextMode) =>
                ruleEditorProps.updateOperation(operation.id, {
                  mode: nextMode,
                })
              }
            >
              <SelectTrigger className='h-9'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {OPERATION_MODE_OPTIONS.map((o) => (
                  <SelectItem key={o.value} value={o.value}>
                    {t(o.label)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {(meta.path || meta.pathOptional) && (
            <div className='space-y-1.5'>
              <label className='text-xs font-medium'>
                {t(getModePathLabel(mode))}
              </label>
              <Input
                value={operation.path}
                onChange={(e) =>
                  ruleEditorProps.updateOperation(operation.id, {
                    path: e.target.value,
                  })
                }
                placeholder={getModePathPlaceholder(mode)}
                className='h-9'
              />
            </div>
          )}
        </div>

        {/* Mode description */}
        {MODE_DESCRIPTIONS[mode] && (
          <p className='text-muted-foreground text-xs'>
            {t(MODE_DESCRIPTIONS[mode])}
          </p>
        )}

        {/* Description */}
        <div className='space-y-1.5'>
          <div className='flex items-center justify-between'>
            <label className='text-xs font-medium'>
              {t('Rule Description (optional)')}
            </label>
            <span className='text-muted-foreground text-[10px]'>
              {operation.description.length}/180
            </span>
          </div>
          <Input
            value={operation.description}
            onChange={(e) =>
              ruleEditorProps.updateOperation(operation.id, {
                description: e.target.value,
              })
            }
            placeholder={t(
              'e.g. Clean tool parameters to avoid upstream validation errors'
            )}
            maxLength={180}
            className='h-9'
          />
        </div>

        {/* Value section */}
        {meta.value &&
          (mode === 'return_error' && ruleEditorProps.returnErrorDraft ? (
            <ReturnErrorEditor
              operationId={operation.id}
              draft={ruleEditorProps.returnErrorDraft}
              updateDraft={ruleEditorProps.updateReturnErrorDraft}
            />
          ) : mode === 'prune_objects' && ruleEditorProps.pruneObjectsDraft ? (
            <PruneObjectsEditor
              operationId={operation.id}
              draft={ruleEditorProps.pruneObjectsDraft}
              updateDraft={ruleEditorProps.updatePruneObjectsDraft}
              addRule={ruleEditorProps.addPruneRule}
              updateRule={ruleEditorProps.updatePruneRule}
              removeRule={ruleEditorProps.removePruneRule}
            />
          ) : mode === 'pass_headers' ? (
            <PassHeadersEditor
              operationId={operation.id}
              valueText={operation.value_text}
              updateOperation={ruleEditorProps.updateOperation}
            />
          ) : mode === 'set_header' ? (
            <HeaderValueEditor
              operationId={operation.id}
              valueText={operation.value_text}
              updateOperation={ruleEditorProps.updateOperation}
            />
          ) : (
            <StructuredValueEditor
              operationId={operation.id}
              label={getModeValueLabel(mode)}
              valueText={operation.value_text}
              placeholder={getModeValuePlaceholder(mode)}
              updateOperation={ruleEditorProps.updateOperation}
            />
          ))}

        {/* keep_origin */}
        {meta.keepOrigin && (
          <div className='flex items-center justify-between rounded-lg border px-3 py-2'>
            <p className='text-sm font-medium'>
              {t('Keep original value (skip if target exists)')}
            </p>
            <Switch
              checked={operation.keep_origin}
              onCheckedChange={(checked) =>
                ruleEditorProps.updateOperation(operation.id, {
                  keep_origin: checked,
                })
              }
            />
          </div>
        )}

        {/* sync_fields */}
        {mode === 'sync_fields' && syncFromTarget && syncToTarget ? (
          <SyncFieldsEditor
            operationId={operation.id}
            syncFromTarget={syncFromTarget}
            syncToTarget={syncToTarget}
            updateOperation={ruleEditorProps.updateOperation}
          />
        ) : (meta.from || meta.to !== undefined) && mode !== 'sync_fields' ? (
          <div className='grid gap-3 sm:grid-cols-2'>
            {(meta.from || meta.to === false) && (
              <div className='space-y-1.5'>
                <label className='text-xs font-medium'>
                  {t(getModeFromLabel(mode))}
                </label>
                <Input
                  value={operation.from}
                  onChange={(e) =>
                    ruleEditorProps.updateOperation(operation.id, {
                      from: e.target.value,
                    })
                  }
                  placeholder={getModeFromPlaceholder(mode)}
                  className='h-9'
                />
              </div>
            )}
            {(meta.to || meta.to === false) && (
              <div className='space-y-1.5'>
                <label className='text-xs font-medium'>
                  {t(getModeToLabel(mode))}
                </label>
                <Input
                  value={operation.to}
                  onChange={(e) =>
                    ruleEditorProps.updateOperation(operation.id, {
                      to: e.target.value,
                    })
                  }
                  placeholder={getModeToPlaceholder(mode)}
                  className='h-9'
                />
              </div>
            )}
          </div>
        ) : null}

        {/* Conditions */}
        <div className='rounded-lg border p-3'>
          <div className='mb-2 flex items-center justify-between'>
            <div className='flex items-center gap-2'>
              <span className='text-sm font-medium'>{t('Conditions')}</span>
              <Select
                value={operation.logic || 'OR'}
                onValueChange={(v) =>
                  ruleEditorProps.updateOperation(operation.id, {
                    logic: v,
                  })
                }
              >
                <SelectTrigger className='h-7 w-[120px] text-xs'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='OR'>{t('Match Any (OR)')}</SelectItem>
                  <SelectItem value='AND'>{t('Match All (AND)')}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className='flex items-center gap-1'>
              {conditions.length > 0 && (
                <>
                  <Button
                    type='button'
                    variant='ghost'
                    size='sm'
                    className='h-7 text-xs'
                    onClick={ruleEditorProps.expandAllConditions}
                  >
                    <ChevronDown className='mr-1 h-3 w-3' />
                    {t('Expand All')}
                  </Button>
                  <Button
                    type='button'
                    variant='ghost'
                    size='sm'
                    className='h-7 text-xs'
                    onClick={ruleEditorProps.collapseAllConditions}
                  >
                    <ChevronUp className='mr-1 h-3 w-3' />
                    {t('Collapse All')}
                  </Button>
                </>
              )}
              <Button
                type='button'
                variant='outline'
                size='sm'
                className='h-7 text-xs'
                onClick={() => ruleEditorProps.addCondition(operation.id)}
              >
                <Plus className='mr-1 h-3 w-3' />
                {t('Add Condition')}
              </Button>
            </div>
          </div>

          {conditions.length === 0 ? (
            <p className='text-muted-foreground text-xs'>
              {t('When no conditions are set, the operation always executes.')}
            </p>
          ) : (
            <div className='space-y-2'>
              {conditions.map((condition, conditionIndex) => (
                <ConditionEditor
                  key={condition.id}
                  condition={condition}
                  conditionIndex={conditionIndex}
                  operationId={operation.id}
                  expanded={
                    ruleEditorProps.expandedConditions[condition.id] ?? false
                  }
                  onExpandedChange={(expanded) =>
                    ruleEditorProps.setExpandedConditions((prev) => ({
                      ...prev,
                      [condition.id]: expanded,
                    }))
                  }
                  updateCondition={ruleEditorProps.updateCondition}
                  removeCondition={ruleEditorProps.removeCondition}
                />
              ))}
            </div>
          )}
        </div>
      </div>
    </ScrollArea>
  )
}

// ---------------------------------------------------------------------------
// ConditionEditor
// ---------------------------------------------------------------------------

type ConditionEditorProps = {
  condition: ParamOverrideCondition
  conditionIndex: number
  operationId: string
  expanded: boolean
  onExpandedChange: (expanded: boolean) => void
  updateCondition: (
    operationId: string,
    conditionId: string,
    patch: Partial<ParamOverrideCondition>
  ) => void
  removeCondition: (operationId: string, conditionId: string) => void
}

function ConditionEditor(conditionEditorProps: ConditionEditorProps) {
  const { t } = useTranslation()
  const condition = conditionEditorProps.condition

  return (
    <Collapsible
      open={conditionEditorProps.expanded}
      onOpenChange={conditionEditorProps.onExpandedChange}
    >
      <div className='rounded-md border'>
        <CollapsibleTrigger className='hover:bg-muted/50 flex w-full items-center justify-between px-3 py-2'>
          <div className='flex items-center gap-2'>
            <Badge variant='outline' className='text-[10px]'>
              C{conditionEditorProps.conditionIndex + 1}
            </Badge>
            <span className='text-muted-foreground text-xs'>
              {condition.path || t('Path not set')}
            </span>
          </div>
          {conditionEditorProps.expanded ? (
            <ChevronUp className='text-muted-foreground h-3.5 w-3.5' />
          ) : (
            <ChevronDown className='text-muted-foreground h-3.5 w-3.5' />
          )}
        </CollapsibleTrigger>
        <CollapsibleContent>
          <div className='space-y-3 border-t px-3 py-3'>
            <div className='flex items-center justify-between'>
              <span className='text-muted-foreground text-xs'>
                {t('Condition Settings')}
              </span>
              <Button
                type='button'
                variant='ghost'
                size='sm'
                className='text-destructive hover:text-destructive h-7 text-xs'
                onClick={() =>
                  conditionEditorProps.removeCondition(
                    conditionEditorProps.operationId,
                    condition.id
                  )
                }
              >
                <Trash2 className='mr-1 h-3 w-3' />
                {t('Delete Condition')}
              </Button>
            </div>
            <div className='grid gap-2 sm:grid-cols-[1fr_150px_1.4fr]'>
              <div className='space-y-1'>
                <label className='text-[10px] font-medium'>
                  {t('Field Path')}
                </label>
                <Input
                  value={condition.path}
                  onChange={(e) =>
                    conditionEditorProps.updateCondition(
                      conditionEditorProps.operationId,
                      condition.id,
                      { path: e.target.value }
                    )
                  }
                  placeholder='model'
                  className='h-8 text-xs'
                />
              </div>
              <div className='space-y-1'>
                <label className='text-[10px] font-medium'>
                  {t('Match Mode')}
                </label>
                <Select
                  value={condition.mode}
                  onValueChange={(v) =>
                    conditionEditorProps.updateCondition(
                      conditionEditorProps.operationId,
                      condition.id,
                      { mode: v }
                    )
                  }
                >
                  <SelectTrigger className='h-8 text-xs'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {CONDITION_MODE_OPTIONS.map((o) => (
                      <SelectItem key={o.value} value={o.value}>
                        {t(o.label)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className='space-y-1'>
                <label className='text-[10px] font-medium'>
                  {t('Match Value')}
                </label>
                <StructuredValueNodeEditor
                  node={parseStructuredValueNodeForDisplay(
                    condition.value_text
                  )}
                  sourceKey={condition.value_text}
                  placeholder='gpt'
                  onChange={(node) =>
                    conditionEditorProps.updateCondition(
                      conditionEditorProps.operationId,
                      condition.id,
                      { value_text: buildStructuredValueText(node) }
                    )
                  }
                />
              </div>
            </div>
            <div className='flex flex-wrap gap-4'>
              <label className='flex items-center gap-2 text-xs'>
                <Switch
                  checked={condition.invert}
                  onCheckedChange={(checked) =>
                    conditionEditorProps.updateCondition(
                      conditionEditorProps.operationId,
                      condition.id,
                      { invert: checked }
                    )
                  }
                />
                {t('Invert match')}
              </label>
              <label className='flex items-center gap-2 text-xs'>
                <Switch
                  checked={condition.pass_missing_key}
                  onCheckedChange={(checked) =>
                    conditionEditorProps.updateCondition(
                      conditionEditorProps.operationId,
                      condition.id,
                      { pass_missing_key: checked }
                    )
                  }
                />
                {t('Pass when key is missing')}
              </label>
            </div>
          </div>
        </CollapsibleContent>
      </div>
    </Collapsible>
  )
}

// ---------------------------------------------------------------------------
// ReturnErrorEditor
// ---------------------------------------------------------------------------

type ReturnErrorEditorProps = {
  operationId: string
  draft: ReturnErrorDraft
  updateDraft: (
    operationId: string,
    draftPatch: Partial<ReturnErrorDraft>
  ) => void
}

function ReturnErrorEditor(returnErrorEditorProps: ReturnErrorEditorProps) {
  const { t } = useTranslation()
  const draft = returnErrorEditorProps.draft

  return (
    <div className='rounded-lg border p-3'>
      <div className='mb-2 flex items-center justify-between'>
        <span className='text-sm font-medium'>
          {t('Custom Error Response')}
        </span>
        <div
          className='bg-muted/60 flex items-center gap-0.5 rounded-md p-0.5'
          role='group'
          aria-label={t('Mode')}
          onPointerDown={(event) => event.stopPropagation()}
        >
          <span className='text-muted-foreground text-xs'>{t('Mode')}</span>
          <Button
            type='button'
            variant={draft.simpleMode ? 'default' : 'ghost'}
            size='sm'
            aria-pressed={draft.simpleMode}
            className='h-7 rounded-sm px-2 text-xs'
            onClick={(event) => {
              event.stopPropagation()
              returnErrorEditorProps.updateDraft(
                returnErrorEditorProps.operationId,
                { simpleMode: true }
              )
            }}
          >
            {t('Simple')}
          </Button>
          <Button
            type='button'
            variant={draft.simpleMode ? 'ghost' : 'default'}
            size='sm'
            aria-pressed={!draft.simpleMode}
            className='h-7 rounded-sm px-2 text-xs'
            onClick={(event) => {
              event.stopPropagation()
              returnErrorEditorProps.updateDraft(
                returnErrorEditorProps.operationId,
                { simpleMode: false }
              )
            }}
          >
            {t('Condition Mode')}
          </Button>
        </div>
      </div>

      <div className='space-y-1.5'>
        <label className='text-xs font-medium'>
          {t('Error Message (required)')}
        </label>
        <Textarea
          value={draft.message}
          onChange={(e) =>
            returnErrorEditorProps.updateDraft(
              returnErrorEditorProps.operationId,
              { message: e.target.value }
            )
          }
          placeholder={t('e.g. This request does not meet access policy')}
          rows={2}
          className='text-xs'
        />
      </div>

      {draft.simpleMode ? (
        <p className='text-muted-foreground mt-2 text-xs'>
          {t(
            'Simple mode only returns message; status code and error type use system defaults.'
          )}
        </p>
      ) : (
        <>
          <div className='mt-3 grid gap-3 sm:grid-cols-3'>
            <div className='space-y-1'>
              <label className='text-xs font-medium'>{t('Status Code')}</label>
              <Input
                value={String(draft.statusCode ?? '')}
                onChange={(e) =>
                  returnErrorEditorProps.updateDraft(
                    returnErrorEditorProps.operationId,
                    { statusCode: parseInt(e.target.value, 10) || 400 }
                  )
                }
                placeholder='400'
                className='h-8 text-xs'
              />
            </div>
            <div className='space-y-1'>
              <label className='text-xs font-medium'>
                {t('Error Code (optional)')}
              </label>
              <Input
                value={draft.code}
                onChange={(e) =>
                  returnErrorEditorProps.updateDraft(
                    returnErrorEditorProps.operationId,
                    { code: e.target.value }
                  )
                }
                placeholder='forced_bad_request'
                className='h-8 text-xs'
              />
            </div>
            <div className='space-y-1'>
              <label className='text-xs font-medium'>
                {t('Error Type (optional)')}
              </label>
              <Input
                value={draft.type}
                onChange={(e) =>
                  returnErrorEditorProps.updateDraft(
                    returnErrorEditorProps.operationId,
                    { type: e.target.value }
                  )
                }
                placeholder='invalid_request_error'
                className='h-8 text-xs'
              />
            </div>
          </div>
          <div className='mt-2 flex items-center gap-2'>
            <span className='text-muted-foreground text-xs'>
              {t('Retry Suggestion')}
            </span>
            <Button
              type='button'
              variant={draft.skipRetry ? 'default' : 'outline'}
              size='sm'
              className='h-7 text-xs'
              onClick={() =>
                returnErrorEditorProps.updateDraft(
                  returnErrorEditorProps.operationId,
                  { skipRetry: true }
                )
              }
            >
              {t('Stop Retry')}
            </Button>
            <Button
              type='button'
              variant={draft.skipRetry ? 'outline' : 'default'}
              size='sm'
              className='h-7 text-xs'
              onClick={() =>
                returnErrorEditorProps.updateDraft(
                  returnErrorEditorProps.operationId,
                  { skipRetry: false }
                )
              }
            >
              {t('Allow Retry')}
            </Button>
          </div>
          <div className='mt-2 flex flex-wrap gap-1'>
            {[
              {
                label: 'Bad Request',
                statusCode: 400,
                code: 'invalid_request',
                type: 'invalid_request_error',
              },
              {
                label: 'Unauthorized',
                statusCode: 401,
                code: 'unauthorized',
                type: 'authentication_error',
              },
              {
                label: 'Rate Limited',
                statusCode: 429,
                code: 'rate_limited',
                type: 'rate_limit_error',
              },
            ].map((preset) => (
              <Button
                key={preset.code}
                type='button'
                variant='outline'
                size='sm'
                className='h-6 text-[10px]'
                onClick={() =>
                  returnErrorEditorProps.updateDraft(
                    returnErrorEditorProps.operationId,
                    {
                      statusCode: preset.statusCode,
                      code: preset.code,
                      type: preset.type,
                    }
                  )
                }
              >
                {t(preset.label)}
              </Button>
            ))}
          </div>
        </>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// PruneObjectsEditor
// ---------------------------------------------------------------------------

type PruneObjectsEditorProps = {
  operationId: string
  draft: PruneObjectsDraft
  updateDraft: (
    operationId: string,
    updater:
      | Partial<PruneObjectsDraft>
      | ((draft: PruneObjectsDraft) => PruneObjectsDraft)
  ) => void
  addRule: (operationId: string) => void
  updateRule: (
    operationId: string,
    ruleId: string,
    patch: Partial<PruneRule>
  ) => void
  removeRule: (operationId: string, ruleId: string) => void
}

function PruneObjectsEditor(pruneObjectsEditorProps: PruneObjectsEditorProps) {
  const { t } = useTranslation()
  const draft = pruneObjectsEditorProps.draft
  const advancedSummaryParts = getPruneAdvancedSummaryParts(draft)
  const advancedConditionCount = draft.rules.filter((rule) =>
    String(rule.path || '').trim()
  ).length

  return (
    <div className='rounded-lg border p-3'>
      <div className='mb-2 flex flex-wrap items-center justify-between gap-2'>
        <div>
          <span className='text-sm font-medium'>{t('Object Prune Rules')}</span>
          <p className='text-muted-foreground mt-1 text-xs'>
            {draft.simpleMode
              ? t('Simple mode only matches the object type field.')
              : advancedSummaryParts
                  .map((part) =>
                    part === 'Additional Conditions: {{count}}'
                      ? t(part, { count: advancedConditionCount })
                      : t(part)
                  )
                  .join(' / ')}
          </p>
        </div>
        <div
          className='bg-muted/60 flex items-center gap-0.5 rounded-md p-0.5'
          role='group'
          aria-label={t('Mode')}
          onPointerDown={(event) => event.stopPropagation()}
        >
          <span className='text-muted-foreground text-xs'>{t('Mode')}</span>
          <Button
            type='button'
            variant={draft.simpleMode ? 'default' : 'ghost'}
            size='sm'
            aria-pressed={draft.simpleMode}
            className='h-7 rounded-sm px-2 text-xs'
            onClick={(event) => {
              event.stopPropagation()
              pruneObjectsEditorProps.updateDraft(
                pruneObjectsEditorProps.operationId,
                { simpleMode: true }
              )
            }}
          >
            {t('Simple')}
          </Button>
          <Button
            type='button'
            variant={draft.simpleMode ? 'ghost' : 'default'}
            size='sm'
            aria-pressed={!draft.simpleMode}
            className='h-7 rounded-sm px-2 text-xs'
            onClick={(event) => {
              event.stopPropagation()
              pruneObjectsEditorProps.updateDraft(
                pruneObjectsEditorProps.operationId,
                { simpleMode: false }
              )
            }}
          >
            {t('Advanced')}
          </Button>
        </div>
      </div>

      {draft.simpleMode ? (
        <div className='space-y-1.5'>
          <label className='text-xs font-medium'>{t('Type (common)')}</label>
          <Input
            value={draft.typeText}
            onChange={(e) =>
              pruneObjectsEditorProps.updateDraft(
                pruneObjectsEditorProps.operationId,
                { typeText: e.target.value }
              )
            }
            placeholder='redacted_thinking'
            className='h-8 text-xs'
          />
        </div>
      ) : (
        <div className='space-y-3'>
          <div className='bg-muted/40 rounded-md border px-3 py-2 text-xs'>
            {t(
              'Condition mode is active. Configure recursion, match logic, and extra object field conditions below.'
            )}
          </div>

          <div className='grid gap-3 sm:grid-cols-2'>
            <div className='space-y-1'>
              <label className='text-xs font-medium'>
                {t('Type (common)')}
              </label>
              <Input
                value={draft.typeText}
                onChange={(e) =>
                  pruneObjectsEditorProps.updateDraft(
                    pruneObjectsEditorProps.operationId,
                    { simpleMode: false, typeText: e.target.value }
                  )
                }
                placeholder='redacted_thinking'
                className='h-8 text-xs'
              />
            </div>
            <div className='space-y-1'>
              <label className='text-xs font-medium'>{t('Logic')}</label>
              <Select
                value={draft.logic}
                onValueChange={(v) =>
                  pruneObjectsEditorProps.updateDraft(
                    pruneObjectsEditorProps.operationId,
                    { simpleMode: false, logic: v || 'AND' }
                  )
                }
              >
                <SelectTrigger className='h-8 text-xs'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='AND'>
                    {t('All Must Match (AND)')}
                  </SelectItem>
                  <SelectItem value='OR'>{t('Any Match (OR)')}</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className='space-y-1'>
            <label className='text-xs font-medium'>
              {t('Recursion Strategy')}
            </label>
            <div className='flex flex-wrap gap-1'>
              <Button
                type='button'
                variant={draft.recursive ? 'default' : 'outline'}
                size='sm'
                className='h-8 text-xs'
                onClick={() =>
                  pruneObjectsEditorProps.updateDraft(
                    pruneObjectsEditorProps.operationId,
                    { simpleMode: false, recursive: true }
                  )
                }
              >
                {t('Recursive')}
              </Button>
              <Button
                type='button'
                variant={draft.recursive ? 'outline' : 'default'}
                size='sm'
                className='h-8 text-xs'
                onClick={() =>
                  pruneObjectsEditorProps.updateDraft(
                    pruneObjectsEditorProps.operationId,
                    { simpleMode: false, recursive: false }
                  )
                }
              >
                {t('Current Level Only')}
              </Button>
            </div>
          </div>

          <div className='bg-muted/30 rounded-md border p-2'>
            <div className='mb-2 flex items-center justify-between'>
              <span className='text-xs font-medium'>
                {t('Additional Conditions')}
              </span>
              <Button
                type='button'
                variant='outline'
                size='sm'
                className='h-7 text-xs'
                onClick={() =>
                  pruneObjectsEditorProps.addRule(
                    pruneObjectsEditorProps.operationId
                  )
                }
              >
                <Plus className='mr-1 h-3 w-3' />
                {t('Add Condition')}
              </Button>
            </div>
            {draft.rules.length === 0 ? (
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Without additional conditions, only the type above is used for pruning.'
                )}
              </p>
            ) : (
              <div className='space-y-2'>
                {draft.rules.map((rule, ruleIndex) => (
                  <div
                    key={rule.id}
                    className='bg-background rounded-md border p-2'
                  >
                    <div className='mb-1 flex items-center justify-between'>
                      <Badge variant='outline' className='text-[10px]'>
                        R{ruleIndex + 1}
                      </Badge>
                      <Button
                        type='button'
                        variant='ghost'
                        size='sm'
                        className='text-destructive hover:text-destructive h-6 text-[10px]'
                        onClick={() =>
                          pruneObjectsEditorProps.removeRule(
                            pruneObjectsEditorProps.operationId,
                            rule.id
                          )
                        }
                      >
                        <Trash2 className='mr-1 h-3 w-3' />
                        {t('Delete')}
                      </Button>
                    </div>
                    <div className='grid gap-2 sm:grid-cols-[1fr_150px_1.4fr]'>
                      <div className='space-y-0.5'>
                        <label className='text-[10px] font-medium'>
                          {t('Field Path')}
                        </label>
                        <Input
                          value={rule.path}
                          onChange={(e) =>
                            pruneObjectsEditorProps.updateRule(
                              pruneObjectsEditorProps.operationId,
                              rule.id,
                              { path: e.target.value }
                            )
                          }
                          placeholder='type'
                          className='h-7 text-xs'
                        />
                      </div>
                      <div className='space-y-0.5'>
                        <label className='text-[10px] font-medium'>
                          {t('Match Mode')}
                        </label>
                        <Select
                          value={rule.mode}
                          onValueChange={(v) =>
                            pruneObjectsEditorProps.updateRule(
                              pruneObjectsEditorProps.operationId,
                              rule.id,
                              { mode: v }
                            )
                          }
                        >
                          <SelectTrigger className='h-7 text-xs'>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            {CONDITION_MODE_OPTIONS.map((o) => (
                              <SelectItem key={o.value} value={o.value}>
                                {t(o.label)}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      </div>
                      <div className='space-y-0.5'>
                        <label className='text-[10px] font-medium'>
                          {t('Match Value (optional)')}
                        </label>
                        <StructuredValueNodeEditor
                          node={parseStructuredValueNodeForDisplay(
                            rule.value_text
                          )}
                          sourceKey={rule.value_text}
                          placeholder='redacted_thinking'
                          onChange={(node) =>
                            pruneObjectsEditorProps.updateRule(
                              pruneObjectsEditorProps.operationId,
                              rule.id,
                              { value_text: buildStructuredValueText(node) }
                            )
                          }
                        />
                      </div>
                    </div>
                    <div className='mt-1.5 flex flex-wrap gap-3'>
                      <label className='flex items-center gap-1.5 text-[10px]'>
                        <Switch
                          checked={rule.invert}
                          onCheckedChange={(checked) =>
                            pruneObjectsEditorProps.updateRule(
                              pruneObjectsEditorProps.operationId,
                              rule.id,
                              { invert: checked }
                            )
                          }
                        />
                        {t('Invert match')}
                      </label>
                      <label className='flex items-center gap-1.5 text-[10px]'>
                        <Switch
                          checked={rule.pass_missing_key}
                          onCheckedChange={(checked) =>
                            pruneObjectsEditorProps.updateRule(
                              pruneObjectsEditorProps.operationId,
                              rule.id,
                              { pass_missing_key: checked }
                            )
                          }
                        />
                        {t('Pass when key is missing')}
                      </label>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// PassHeadersEditor
// ---------------------------------------------------------------------------

type PassHeadersEditorProps = {
  operationId: string
  valueText: string
  updateOperation: (
    operationId: string,
    patch: Partial<ParamOverrideOperation>
  ) => void
}

function PassHeadersEditor(passHeadersEditorProps: PassHeadersEditorProps) {
  const { t } = useTranslation()
  const draft = useMemo(
    () => parsePassHeadersDraft(passHeadersEditorProps.valueText),
    [passHeadersEditorProps.valueText]
  )
  const headers = draft.headers
  const headerRows = useMemo<PassHeaderRow[]>(
    () =>
      headers.map((header, index) => ({
        id: `pass-header-${index}`,
        value: header,
      })),
    [headers]
  )

  const commitDraft = useCallback(
    (nextDraft: PassHeadersDraft) => {
      passHeadersEditorProps.updateOperation(
        passHeadersEditorProps.operationId,
        {
          value_text: buildPassHeadersValueText(nextDraft),
        }
      )
    },
    [passHeadersEditorProps]
  )

  const commitHeaders = useCallback(
    (nextHeaders: string[]) => {
      commitDraft({ ...draft, headers: nextHeaders })
    },
    [commitDraft, draft]
  )

  return (
    <div className='rounded-lg border p-3'>
      <div className='mb-2 flex items-center justify-between gap-2'>
        <div>
          <span className='text-sm font-medium'>
            {t('Pass-through Headers')}
          </span>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t(
              'Only headers that exist on the original client request are forwarded.'
            )}
          </p>
        </div>
        <Button
          type='button'
          variant='outline'
          size='sm'
          className='h-7 text-xs'
          disabled={draft.sourceKey === 'header' && headers.length > 0}
          onClick={() =>
            commitHeaders(
              draft.sourceKey === 'header' && headers.length > 0
                ? headers
                : Array.from(new Set([...headers, 'X-Header-Name']))
            )
          }
        >
          <Plus className='mr-1 h-3 w-3' />
          {t('Add Header')}
        </Button>
      </div>
      <div className='mb-2 grid gap-2 sm:grid-cols-[180px_1fr]'>
        <div className='space-y-1'>
          <label className='text-xs font-medium'>{t('Value Shape')}</label>
          <Select
            value={draft.sourceKey}
            onValueChange={(value) =>
              commitDraft({
                ...draft,
                sourceKey: value as PassHeadersDraft['sourceKey'],
                headers:
                  value === 'header'
                    ? draft.headers.slice(0, 1)
                    : draft.headers,
              })
            }
          >
            <SelectTrigger className='h-8 text-xs'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='headers'>{t('Array')}</SelectItem>
              <SelectItem value='names'>{t('Object Names')}</SelectItem>
              <SelectItem value='header'>{t('Single Header')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <p className='text-muted-foreground self-end pb-1 text-xs'>
          {t(
            'Choose the saved value shape for compatibility with existing configurations.'
          )}
        </p>
      </div>
      {headerRows.length === 0 ? (
        <div className='text-muted-foreground rounded-md border border-dashed px-3 py-4 text-center text-xs'>
          {t('No pass-through headers configured.')}
        </div>
      ) : (
        <div className='space-y-2'>
          {headerRows.map((headerRow, index) => (
            <div key={headerRow.id} className='grid grid-cols-[1fr_auto] gap-2'>
              <Input
                value={headerRow.value}
                onChange={(event) => {
                  const nextHeaders = [...headers]
                  nextHeaders[index] = event.target.value
                  commitHeaders(nextHeaders)
                }}
                placeholder='X-Client-Request-Id'
                className='h-8 text-xs'
              />
              <Button
                type='button'
                variant='ghost'
                size='sm'
                className='text-destructive hover:text-destructive h-8 px-2'
                onClick={() =>
                  commitHeaders(
                    headers.filter((_, itemIndex) => itemIndex !== index)
                  )
                }
              >
                <Trash2 className='h-3.5 w-3.5' />
              </Button>
            </div>
          ))}
        </div>
      )}
      <div className='mt-2 flex flex-wrap gap-1'>
        {[
          'User-Agent',
          'Session_id',
          'X-Client-Request-Id',
          'X-Codex-Turn-Metadata',
          'Anthropic-Beta',
          'X-Stainless-Runtime',
        ].map((header) => (
          <Button
            key={header}
            type='button'
            variant='outline'
            size='sm'
            className='h-6 text-[10px]'
            onClick={() =>
              commitHeaders(
                draft.sourceKey === 'header'
                  ? [header]
                  : Array.from(new Set([...headers, header]))
              )
            }
          >
            {header}
          </Button>
        ))}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// HeaderValueEditor
// ---------------------------------------------------------------------------

type HeaderValueEditorProps = {
  operationId: string
  valueText: string
  updateOperation: (
    operationId: string,
    patch: Partial<ParamOverrideOperation>
  ) => void
}

function HeaderValueEditor(headerValueEditorProps: HeaderValueEditorProps) {
  const { t } = useTranslation()
  const draft = useMemo(
    () => parseHeaderValueDraft(headerValueEditorProps.valueText),
    [headerValueEditorProps.valueText]
  )

  const updateDraft = useCallback(
    (patch: Partial<HeaderValueDraft>) => {
      const nextDraft = { ...draft, ...patch }
      headerValueEditorProps.updateOperation(
        headerValueEditorProps.operationId,
        {
          value_text: buildHeaderValueText(nextDraft),
        }
      )
    },
    [draft, headerValueEditorProps]
  )

  const updateRow = useCallback(
    (rowId: string, patch: Partial<HeaderValueMappingRow>) => {
      updateDraft({
        rows: draft.rows.map((row) =>
          row.id === rowId ? { ...row, ...patch } : row
        ),
      })
    },
    [draft.rows, updateDraft]
  )

  return (
    <div className='rounded-lg border p-3'>
      <div className='mb-2 flex flex-wrap items-center justify-between gap-2'>
        <div>
          <span className='text-sm font-medium'>{t('Header Value')}</span>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t(
              'Use direct mode for a whole value, or mapping mode for comma-separated header tokens.'
            )}
          </p>
        </div>
        <Select
          value={draft.mode}
          onValueChange={(value) =>
            updateDraft({ mode: value as HeaderValueDraft['mode'] })
          }
        >
          <SelectTrigger className='h-8 w-[180px] text-xs'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {HEADER_VALUE_MODE_OPTIONS.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {t(option.label)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {draft.mode === 'direct' ? (
        <Input
          value={draft.directText}
          onChange={(event) => updateDraft({ directText: event.target.value })}
          placeholder='Bearer sk-xxx'
          className='h-8 text-xs'
        />
      ) : (
        <div className='space-y-3'>
          <label className='flex items-center gap-2 text-xs'>
            <Switch
              checked={draft.keepOnlyDeclared}
              onCheckedChange={(checked) =>
                updateDraft({ keepOnlyDeclared: checked })
              }
            />
            {t('Keep only declared tokens')}
          </label>
          <div className='space-y-1'>
            <label className='text-xs font-medium'>{t('Append Tokens')}</label>
            <Input
              value={draft.appendText}
              onChange={(event) =>
                updateDraft({ appendText: event.target.value })
              }
              placeholder='context-1m-2025-08-07, interleaved-thinking-2025-05-14'
              className='h-8 text-xs'
            />
          </div>
          <div className='grid gap-2 sm:grid-cols-[180px_1fr]'>
            <div className='space-y-1'>
              <label className='text-xs font-medium'>
                {t('Wildcard Rule')}
              </label>
              <Select
                value={draft.wildcardAction}
                onValueChange={(value) =>
                  updateDraft({ wildcardAction: value })
                }
              >
                <SelectTrigger className='h-8 text-xs'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='none'>{t('No wildcard rule')}</SelectItem>
                  <SelectItem value='replace'>
                    {t('Replace undeclared tokens')}
                  </SelectItem>
                  <SelectItem value='delete'>
                    {t('Delete undeclared tokens')}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className='space-y-1'>
              <label className='text-xs font-medium'>
                {t('Wildcard Replacement')}
              </label>
              <Input
                value={draft.wildcardReplacement}
                disabled={draft.wildcardAction !== 'replace'}
                onChange={(event) =>
                  updateDraft({ wildcardReplacement: event.target.value })
                }
                placeholder='tool-search-tool-2025-10-19'
                className='h-8 text-xs'
              />
            </div>
          </div>
          <div className='space-y-2'>
            <div className='flex items-center justify-between'>
              <span className='text-xs font-medium'>{t('Token Rules')}</span>
              <Button
                type='button'
                variant='outline'
                size='sm'
                className='h-7 text-xs'
                onClick={() =>
                  updateDraft({
                    rows: [
                      ...draft.rows,
                      {
                        id: nextLocalId(),
                        token: '',
                        action: 'replace',
                        replacement: '',
                      },
                    ],
                  })
                }
              >
                <Plus className='mr-1 h-3 w-3' />
                {t('Add Rule')}
              </Button>
            </div>
            {draft.rows.length === 0 ? (
              <div className='text-muted-foreground rounded-md border border-dashed px-3 py-4 text-center text-xs'>
                {t('No token rules configured.')}
              </div>
            ) : (
              draft.rows.map((row) => (
                <div
                  key={row.id}
                  className='grid gap-2 rounded-md border p-2 sm:grid-cols-[1fr_120px_1fr_auto]'
                >
                  <Input
                    value={row.token}
                    onChange={(event) =>
                      updateRow(row.id, { token: event.target.value })
                    }
                    placeholder='advanced-tool-use-2025-11-20'
                    className='h-8 text-xs'
                  />
                  <Select
                    value={row.action}
                    onValueChange={(value) =>
                      updateRow(row.id, { action: value })
                    }
                  >
                    <SelectTrigger className='h-8 text-xs'>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {HEADER_TOKEN_ACTION_OPTIONS.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {t(option.label)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Input
                    value={row.replacement}
                    disabled={row.action !== 'replace'}
                    onChange={(event) =>
                      updateRow(row.id, { replacement: event.target.value })
                    }
                    placeholder='tool-search-tool-2025-10-19'
                    className='h-8 text-xs'
                  />
                  <Button
                    type='button'
                    variant='ghost'
                    size='sm'
                    className='text-destructive hover:text-destructive h-8 px-2'
                    onClick={() =>
                      updateDraft({
                        rows: draft.rows.filter((item) => item.id !== row.id),
                      })
                    }
                  >
                    <Trash2 className='h-3.5 w-3.5' />
                  </Button>
                </div>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// StructuredValueEditor
// ---------------------------------------------------------------------------

type StructuredValueEditorProps = {
  operationId: string
  label: string
  valueText: string
  placeholder: string
  updateOperation: (
    operationId: string,
    patch: Partial<ParamOverrideOperation>
  ) => void
}

function StructuredValueEditor({
  operationId,
  label,
  valueText,
  placeholder,
  updateOperation,
}: StructuredValueEditorProps) {
  const { t } = useTranslation()
  const node = useMemo(
    () => parseStructuredValueNodeForDisplay(valueText),
    [valueText]
  )

  const commitNode = useCallback(
    (nextNode: StructuredValueNode) => {
      updateOperation(operationId, {
        value_text: buildStructuredValueText(nextNode),
      })
    },
    [operationId, updateOperation]
  )

  return (
    <div className='rounded-lg border p-3'>
      <div className='mb-2 flex items-center justify-between'>
        <span className='text-sm font-medium'>{t(label)}</span>
        <span className='text-muted-foreground text-[10px]'>
          {t('Value Type')}
        </span>
      </div>
      <StructuredValueNodeEditor
        node={node}
        sourceKey={valueText}
        placeholder={placeholder}
        onChange={commitNode}
      />
    </div>
  )
}

type StructuredValueNodeEditorProps = {
  node: StructuredValueNode
  sourceKey?: string
  placeholder?: string
  depth?: number
  onChange: (node: StructuredValueNode) => void
}

function StructuredValueNodeEditor({
  node: propNode,
  sourceKey,
  placeholder,
  depth = 0,
  onChange,
}: StructuredValueNodeEditorProps) {
  const { t } = useTranslation()
  const source = sourceKey ?? propNode
  const [draftState, setDraftState] = useState<{
    source: string | StructuredValueNode
    draft: StructuredValueNode
  }>(() => ({
    source,
    draft: propNode,
  }))
  const node = Object.is(draftState.source, source)
    ? draftState.draft
    : propNode
  const compact = depth > 1
  const emitNode = useCallback(
    (nextNode: StructuredValueNode) => {
      setDraftState({
        source,
        draft: nextNode,
      })
      if (!canSerializeStructuredValueNode(nextNode)) {
        return
      }
      onChange(nextNode)
    },
    [onChange, source]
  )
  const updateNode = useCallback(
    (patch: Partial<StructuredValueNode>) => emitNode({ ...node, ...patch }),
    [emitNode, node]
  )
  const canAddChild = depth < MAX_STRUCTURED_VALUE_DEPTH
  const numberInvalid =
    node.kind === 'number' && !isCompleteStructuredNumberText(node.text)

  return (
    <div className='space-y-2'>
      <Select
        value={node.kind}
        onValueChange={(value) => {
          const nextNode = createStructuredValueNode(
            value as StructuredValueNodeKind
          )
          emitNode({
            ...nextNode,
            id: node.id,
          })
        }}
      >
        <SelectTrigger className='h-8 w-[160px] text-xs'>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {STRUCTURED_VALUE_TYPE_OPTIONS.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              {t(option.label)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      {node.kind === 'string' || node.kind === 'number' ? (
        <div className='space-y-1'>
          <Input
            value={node.text}
            onChange={(event) => {
              const nextText = event.target.value
              updateNode({ text: nextText })
            }}
            placeholder={
              node.kind === 'number' ? '0.7' : placeholder || 'value'
            }
            aria-invalid={numberInvalid}
            className={cn(
              'h-8 text-xs',
              numberInvalid ? 'border-destructive' : ''
            )}
          />
          {numberInvalid ? (
            <p className='text-destructive text-xs'>{t('Invalid number')}</p>
          ) : null}
        </div>
      ) : null}

      {node.kind === 'boolean' ? (
        <div className='flex gap-1'>
          <Button
            type='button'
            variant={node.boolValue ? 'default' : 'outline'}
            size='sm'
            className='h-7 text-xs'
            onClick={() => updateNode({ boolValue: true })}
          >
            true
          </Button>
          <Button
            type='button'
            variant={node.boolValue ? 'outline' : 'default'}
            size='sm'
            className='h-7 text-xs'
            onClick={() => updateNode({ boolValue: false })}
          >
            false
          </Button>
        </div>
      ) : null}

      {node.kind === 'null' ? (
        <p className='text-muted-foreground text-xs'>
          {t('This value will be saved as null.')}
        </p>
      ) : null}

      {node.kind === 'object' ? (
        <div className='space-y-2 rounded-md border p-2'>
          <div className='flex items-center justify-between'>
            <span className='text-xs font-medium'>{t('Object Fields')}</span>
            <Button
              type='button'
              variant='outline'
              size='sm'
              className='h-7 text-xs'
              disabled={!canAddChild}
              onClick={() =>
                updateNode({
                  objectEntries: [
                    ...node.objectEntries,
                    {
                      id: nextLocalId(),
                      key: '',
                      value: createStructuredValueNode('string'),
                    },
                  ],
                })
              }
            >
              <Plus className='mr-1 h-3 w-3' />
              {t('Add Field')}
            </Button>
          </div>
          {!canAddChild ? (
            <p className='text-muted-foreground text-xs'>
              {t('Maximum nesting depth reached')}
            </p>
          ) : null}
          {node.objectEntries.length === 0 ? (
            <p className='text-muted-foreground text-xs'>
              {t('No object fields configured.')}
            </p>
          ) : (
            node.objectEntries.map((entry) => (
              <div
                key={entry.id}
                className='grid gap-2 rounded-md border p-2 sm:grid-cols-[160px_1fr_auto]'
              >
                <Input
                  value={entry.key}
                  onChange={(event) =>
                    updateNode({
                      objectEntries: node.objectEntries.map((item) =>
                        item.id === entry.id
                          ? { ...item, key: event.target.value }
                          : item
                      ),
                    })
                  }
                  placeholder='key'
                  className='h-8 text-xs'
                />
                <StructuredValueNodeEditor
                  node={entry.value}
                  depth={depth + 1}
                  onChange={(value) =>
                    updateNode({
                      objectEntries: node.objectEntries.map((item) =>
                        item.id === entry.id ? { ...item, value } : item
                      ),
                    })
                  }
                />
                <Button
                  type='button'
                  variant='ghost'
                  size='sm'
                  className='text-destructive hover:text-destructive h-8 px-2'
                  onClick={() =>
                    updateNode({
                      objectEntries: node.objectEntries.filter(
                        (item) => item.id !== entry.id
                      ),
                    })
                  }
                >
                  <Trash2 className='h-3.5 w-3.5' />
                </Button>
              </div>
            ))
          )}
        </div>
      ) : null}

      {node.kind === 'array' ? (
        <div className='space-y-2 rounded-md border p-2'>
          <div className='flex items-center justify-between'>
            <span className='text-xs font-medium'>{t('Array Items')}</span>
            <Button
              type='button'
              variant='outline'
              size='sm'
              className='h-7 text-xs'
              disabled={!canAddChild}
              onClick={() =>
                updateNode({
                  arrayItems: [
                    ...node.arrayItems,
                    { id: nextLocalId(), value: createStructuredValueNode() },
                  ],
                })
              }
            >
              <Plus className='mr-1 h-3 w-3' />
              {t('Add Item')}
            </Button>
          </div>
          {!canAddChild ? (
            <p className='text-muted-foreground text-xs'>
              {t('Maximum nesting depth reached')}
            </p>
          ) : null}
          {node.arrayItems.length === 0 ? (
            <p className='text-muted-foreground text-xs'>
              {t('No array items configured.')}
            </p>
          ) : (
            node.arrayItems.map((item, index) => (
              <div
                key={item.id}
                className='grid gap-2 rounded-md border p-2 sm:grid-cols-[40px_1fr_auto]'
              >
                <span className='text-muted-foreground pt-2 text-xs'>
                  #{index + 1}
                </span>
                <StructuredValueNodeEditor
                  node={item.value}
                  depth={depth + 1}
                  onChange={(value) =>
                    updateNode({
                      arrayItems: node.arrayItems.map((entry) =>
                        entry.id === item.id ? { ...entry, value } : entry
                      ),
                    })
                  }
                />
                <Button
                  type='button'
                  variant='ghost'
                  size='sm'
                  className='text-destructive hover:text-destructive h-8 px-2'
                  onClick={() =>
                    updateNode({
                      arrayItems: node.arrayItems.filter(
                        (entry) => entry.id !== item.id
                      ),
                    })
                  }
                >
                  <Trash2 className='h-3.5 w-3.5' />
                </Button>
              </div>
            ))
          )}
        </div>
      ) : null}

      {compact ? null : (
        <p className='text-muted-foreground text-xs'>
          {t('Preview')}:{' '}
          {canSerializeStructuredValueNode(node)
            ? buildStructuredValueText(node) || '-'
            : '-'}
        </p>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// SyncFieldsEditor
// ---------------------------------------------------------------------------

type SyncFieldsEditorProps = {
  operationId: string
  syncFromTarget: { type: string; key: string }
  syncToTarget: { type: string; key: string }
  updateOperation: (
    operationId: string,
    patch: Partial<ParamOverrideOperation>
  ) => void
}

function SyncFieldsEditor(syncFieldsEditorProps: SyncFieldsEditorProps) {
  const { t } = useTranslation()
  return (
    <div className='space-y-3'>
      <label className='text-xs font-medium'>{t('Sync Endpoints')}</label>
      <div className='grid gap-3 sm:grid-cols-2'>
        <div className='space-y-1.5'>
          <label className='text-[10px] font-medium'>
            {t('Source Endpoint')}
          </label>
          <div className='flex gap-2'>
            <Select
              value={syncFieldsEditorProps.syncFromTarget.type || 'json'}
              onValueChange={(v) =>
                syncFieldsEditorProps.updateOperation(
                  syncFieldsEditorProps.operationId,
                  {
                    from: buildSyncTargetSpec(
                      v,
                      syncFieldsEditorProps.syncFromTarget.key
                    ),
                  }
                )
              }
            >
              <SelectTrigger className='h-8 w-[110px] text-xs'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {SYNC_TARGET_TYPE_OPTIONS.map((o) => (
                  <SelectItem key={o.value} value={o.value}>
                    {t(o.label)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input
              value={syncFieldsEditorProps.syncFromTarget.key}
              onChange={(e) =>
                syncFieldsEditorProps.updateOperation(
                  syncFieldsEditorProps.operationId,
                  {
                    from: buildSyncTargetSpec(
                      syncFieldsEditorProps.syncFromTarget.type,
                      e.target.value
                    ),
                  }
                )
              }
              placeholder='session_id'
              className='h-8 text-xs'
            />
          </div>
        </div>
        <div className='space-y-1.5'>
          <label className='text-[10px] font-medium'>
            {t('Target Endpoint')}
          </label>
          <div className='flex gap-2'>
            <Select
              value={syncFieldsEditorProps.syncToTarget.type || 'json'}
              onValueChange={(v) =>
                syncFieldsEditorProps.updateOperation(
                  syncFieldsEditorProps.operationId,
                  {
                    to: buildSyncTargetSpec(
                      v,
                      syncFieldsEditorProps.syncToTarget.key
                    ),
                  }
                )
              }
            >
              <SelectTrigger className='h-8 w-[110px] text-xs'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {SYNC_TARGET_TYPE_OPTIONS.map((o) => (
                  <SelectItem key={o.value} value={o.value}>
                    {t(o.label)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input
              value={syncFieldsEditorProps.syncToTarget.key}
              onChange={(e) =>
                syncFieldsEditorProps.updateOperation(
                  syncFieldsEditorProps.operationId,
                  {
                    to: buildSyncTargetSpec(
                      syncFieldsEditorProps.syncToTarget.type,
                      e.target.value
                    ),
                  }
                )
              }
              placeholder='prompt_cache_key'
              className='h-8 text-xs'
            />
          </div>
        </div>
      </div>
      <div className='flex flex-wrap gap-1'>
        {[
          {
            label: 'header:session_id -> json:prompt_cache_key',
            from: 'header:session_id',
            to: 'json:prompt_cache_key',
          },
          {
            label: 'json:prompt_cache_key -> header:session_id',
            from: 'json:prompt_cache_key',
            to: 'header:session_id',
          },
        ].map((preset) => (
          <Button
            key={preset.label}
            type='button'
            variant='outline'
            size='sm'
            className='h-6 text-[10px]'
            onClick={() =>
              syncFieldsEditorProps.updateOperation(
                syncFieldsEditorProps.operationId,
                { from: preset.from, to: preset.to }
              )
            }
          >
            {preset.label}
          </Button>
        ))}
      </div>
    </div>
  )
}
