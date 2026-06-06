/*
Copyright (C) 2025 QuantumNous

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

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Card,
  Col,
  Collapse,
  Input,
  Modal,
  Row,
  Select,
  Space,
  Switch,
  Tag,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { IconDelete, IconMenu, IconPlus } from '@douyinfe/semi-icons';
import { copy, showError, showSuccess, verifyJSON } from '../../../../helpers';
import {
  CLAUDE_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  CODEX_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  CODEX_DESKTOP_HEADER_PASSTHROUGH_TEMPLATE,
  DROID_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  GEMINI_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  QWEN_CODE_CLI_HEADER_PASSTHROUGH_TEMPLATE,
} from '../../../../constants/channel-affinity-template.constants';
import { useIsMobile } from '../../../../hooks/common/useIsMobile';

const { Text } = Typography;

const FIELD_NAME_PLACEHOLDER = 'temperature';
const HEADER_NAME_PLACEHOLDER = 'X-Client-Request-Id';
const HEADER_DIRECT_VALUE_PLACEHOLDER = 'Bearer sk-xxx';
const HEADER_APPEND_TOKENS_PLACEHOLDER =
  'context-1m-2025-08-07, interleaved-thinking-2025-05-14';
const HEADER_TOKEN_PLACEHOLDER = 'advanced-tool-use-2025-11-20';
const HEADER_REPLACEMENT_PLACEHOLDER = 'tool-search-tool-2025-10-19';
const OBJECT_KEY_PLACEHOLDER = 'key';
const BOOLEAN_TRUE_LABEL = 'true';
const BOOLEAN_FALSE_LABEL = 'false';
const MAX_STRUCTURED_VALUE_DEPTH = 8;
const STRUCTURED_VALUE_DEPTH_ERROR = `Structured value nesting depth exceeds ${MAX_STRUCTURED_VALUE_DEPTH}`;

const isCompleteStructuredNumberText = (text) => {
  const trimmed = String(text ?? '').trim();
  return (
    trimmed !== '' &&
    trimmed !== '-' &&
    trimmed !== '.' &&
    trimmed !== '-.' &&
    Number.isFinite(Number(trimmed))
  );
};

const OPERATION_MODE_OPTIONS = [
  { label: '设置字段', value: 'set' },
  { label: '删除字段', value: 'delete' },
  { label: '追加到末尾', value: 'append' },
  { label: '追加到开头', value: 'prepend' },
  { label: '复制字段', value: 'copy' },
  { label: '移动字段', value: 'move' },
  { label: '字符串替换', value: 'replace' },
  { label: '正则替换', value: 'regex_replace' },
  { label: '裁剪前缀', value: 'trim_prefix' },
  { label: '裁剪后缀', value: 'trim_suffix' },
  { label: '确保前缀', value: 'ensure_prefix' },
  { label: '确保后缀', value: 'ensure_suffix' },
  { label: '去掉空白', value: 'trim_space' },
  { label: '转小写', value: 'to_lower' },
  { label: '转大写', value: 'to_upper' },
  { label: '返回自定义错误', value: 'return_error' },
  { label: '清理对象项', value: 'prune_objects' },
  { label: '请求头透传', value: 'pass_headers' },
  { label: '字段同步', value: 'sync_fields' },
  { label: '设置请求头', value: 'set_header' },
  { label: '删除请求头', value: 'delete_header' },
  { label: '复制请求头', value: 'copy_header' },
  { label: '移动请求头', value: 'move_header' },
];

const OPERATION_MODE_VALUES = new Set(
  OPERATION_MODE_OPTIONS.map((item) => item.value),
);

const CONDITION_MODE_OPTIONS = [
  { label: '完全匹配', value: 'full' },
  { label: '前缀匹配', value: 'prefix' },
  { label: '后缀匹配', value: 'suffix' },
  { label: '包含', value: 'contains' },
  { label: '大于', value: 'gt' },
  { label: '大于等于', value: 'gte' },
  { label: '小于', value: 'lt' },
  { label: '小于等于', value: 'lte' },
];

const CONDITION_MODE_VALUES = new Set(
  CONDITION_MODE_OPTIONS.map((item) => item.value),
);

const MODE_META = {
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
};

const VALUE_REQUIRED_MODES = new Set([
  'trim_prefix',
  'trim_suffix',
  'ensure_prefix',
  'ensure_suffix',
  'set_header',
  'return_error',
  'prune_objects',
  'pass_headers',
]);

const FROM_REQUIRED_MODES = new Set([
  'copy',
  'move',
  'replace',
  'regex_replace',
  'copy_header',
  'move_header',
  'sync_fields',
]);

const TO_REQUIRED_MODES = new Set([
  'copy',
  'move',
  'copy_header',
  'move_header',
  'sync_fields',
]);

const MODE_DESCRIPTIONS = {
  set: '把值写入目标字段',
  delete: '删除目标字段',
  append: '把值追加到数组 / 字符串 / 对象末尾',
  prepend: '把值追加到数组 / 字符串 / 对象开头',
  copy: '把来源字段复制到目标字段',
  move: '把来源字段移动到目标字段',
  replace: '在目标字段里做字符串替换',
  regex_replace: '在目标字段里做正则替换',
  trim_prefix: '去掉字符串前缀',
  trim_suffix: '去掉字符串后缀',
  ensure_prefix: '确保字符串有指定前缀',
  ensure_suffix: '确保字符串有指定后缀',
  trim_space: '去掉字符串头尾空白',
  to_lower: '把字符串转成小写',
  to_upper: '把字符串转成大写',
  return_error: '立即返回自定义错误',
  prune_objects: '按条件清理对象中的子项',
  pass_headers:
    '把客户端原始请求里的指定请求头透传到上游；适合 Codex CLI / Claude Code 等要求真实客户端动态头的渠道',
  sync_fields: '在一个字段有值、另一个缺失时自动补齐',
  set_header:
    '设置运行期请求头：可直接覆盖整条值，也可对逗号分隔的 token 做删除、替换、追加或白名单保留',
  delete_header: '删除运行期请求头',
  copy_header: '复制请求头',
  move_header: '移动请求头',
};

const getModePathLabel = (mode) => {
  if (mode === 'set_header' || mode === 'delete_header') {
    return '请求头名称';
  }
  if (mode === 'prune_objects') {
    return '目标路径（可选）';
  }
  return '目标字段路径';
};

const getModePathPlaceholder = (mode) => {
  if (mode === 'set_header') return 'Authorization';
  if (mode === 'delete_header') return 'X-Debug-Mode';
  if (mode === 'prune_objects') return 'messages';
  return 'temperature';
};

const getModeFromLabel = (mode) => {
  if (mode === 'replace') return '匹配文本';
  if (mode === 'regex_replace') return '正则表达式';
  if (mode === 'copy_header' || mode === 'move_header') return '来源请求头';
  return '来源字段';
};

const getModeFromPlaceholder = (mode) => {
  if (mode === 'replace') return 'openai/';
  if (mode === 'regex_replace') return '^gpt-';
  if (mode === 'copy_header' || mode === 'move_header') return 'Authorization';
  return 'model';
};

const getModeToLabel = (mode) => {
  if (mode === 'replace' || mode === 'regex_replace') return '替换为';
  if (mode === 'copy_header' || mode === 'move_header') return '目标请求头';
  return '目标字段';
};

const getModeToPlaceholder = (mode) => {
  if (mode === 'replace') return '（可留空）';
  if (mode === 'regex_replace') return 'openai/gpt-';
  if (mode === 'copy_header' || mode === 'move_header')
    return 'X-Upstream-Auth';
  return 'original_model';
};

const getModeValueLabel = (mode) => {
  if (mode === 'set_header') return '请求头值（支持字符串或 JSON 映射）';
  if (mode === 'pass_headers')
    return '透传请求头（来自客户端原始请求，支持逗号分隔或 JSON 数组）';
  if (
    mode === 'trim_prefix' ||
    mode === 'trim_suffix' ||
    mode === 'ensure_prefix' ||
    mode === 'ensure_suffix'
  ) {
    return '前后缀文本';
  }
  if (mode === 'prune_objects') {
    return '清理规则（字符串或 JSON 对象）';
  }
  return '值（支持 JSON 或普通文本）';
};

const HEADER_VALUE_JSONC_EXAMPLE = `{
  // 置空：删除 Bedrock 不支持的 beta特性
  "files-api-2025-04-14": null,

  // 替换：把旧特性改成兼容特性
  "advanced-tool-use-2025-11-20": "tool-search-tool-2025-10-19",

  // 追加：在末尾补一个需要的特性
  "$append": ["context-1m-2025-08-07"]
}`;

const getModeValuePlaceholder = (mode) => {
  if (mode === 'set_header') {
    return [
      '纯字符串（整条覆盖）：',
      'Bearer sk-xxx',
      '',
      '或使用 JSON 规则：',
      '{',
      '  "files-api-2025-04-14": null,',
      '  "advanced-tool-use-2025-11-20": "tool-search-tool-2025-10-19",',
      '  "$append": ["context-1m-2025-08-07"]',
      '}',
    ].join('\n');
  }
  if (mode === 'pass_headers') return 'Session_id, X-Client-Request-Id';
  if (
    mode === 'trim_prefix' ||
    mode === 'trim_suffix' ||
    mode === 'ensure_prefix' ||
    mode === 'ensure_suffix'
  ) {
    return 'openai/';
  }
  if (mode === 'prune_objects') {
    return '{"type":"redacted_thinking"}';
  }
  return '0.7';
};

const SYNC_TARGET_TYPE_OPTIONS = [
  { label: '请求体字段', value: 'json' },
  { label: '请求头字段', value: 'header' },
];

const STRUCTURED_VALUE_TYPE_OPTIONS = [
  { label: '文本', value: 'string' },
  { label: '数字', value: 'number' },
  { label: '布尔值', value: 'boolean' },
  { label: '空值', value: 'null' },
  { label: '对象', value: 'object' },
  { label: '数组', value: 'array' },
];

const HEADER_VALUE_MODE_OPTIONS = [
  { label: '整条请求头值', value: 'direct' },
  { label: 'Token 映射', value: 'mapping' },
];

const HEADER_TOKEN_ACTION_OPTIONS = [
  { label: '替换', value: 'replace' },
  { label: '删除', value: 'delete' },
  { label: '保留', value: 'keep' },
];

const LEGACY_TEMPLATE = {
  temperature: 0,
  max_tokens: 1000,
};

const OPERATION_TEMPLATE = {
  operations: [
    {
      description: 'Set default temperature for openai/* models.',
      path: 'temperature',
      mode: 'set',
      value: 0.7,
      conditions: [
        {
          path: 'model',
          mode: 'prefix',
          value: 'openai/',
        },
      ],
      logic: 'AND',
    },
  ],
};

const HEADER_PASSTHROUGH_TEMPLATE = {
  operations: [
    {
      description: 'Pass through common tracing headers to upstream.',
      mode: 'pass_headers',
      value: ['X-Request-Id', 'X-Trace-Id', 'X-Correlation-Id', 'Traceparent'],
      keep_origin: true,
    },
  ],
};

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
};

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
};

const GEMINI_IMAGE_4K_TEMPLATE = {
  operations: [
    {
      description:
        'Set imageSize to 4K when model contains gemini/image and ends with 4k.',
      mode: 'set',
      path: 'generationConfig.imageConfig.imageSize',
      value: '4K',
      conditions: [
        {
          path: 'original_model',
          mode: 'contains',
          value: 'gemini',
        },
        {
          path: 'original_model',
          mode: 'contains',
          value: 'image',
        },
        {
          path: 'original_model',
          mode: 'suffix',
          value: '4k',
        },
      ],
      logic: 'AND',
    },
  ],
};

const AWS_BEDROCK_ANTHROPIC_BETA_TEMPLATE = {
  operations: [
    {
      description:
        'Normalize anthropic-beta header tokens for Bedrock compatibility.',
      mode: 'set_header',
      path: 'anthropic-beta',
      // https://github.com/BerriAI/litellm/blob/main/litellm/anthropic_beta_headers_config.json
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
};

const AWS_BEDROCK_REMOVE_INPUT_EXAMPLES_TEMPLATE = {
  operations: [
    {
      description:
        'Remove all tools[*].custom.input_examples before upstream relay.',
      mode: 'delete',
      path: 'tools.*.custom.input_examples',
    },
  ],
};

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
};

const TEMPLATE_GROUP_OPTIONS = [
  { label: '推荐场景', value: 'recommended' },
  { label: '高级兼容', value: 'advanced' },
  { label: '示例起点', value: 'examples' },
];

const TEMPLATE_PRESET_CONFIG = {
  codex_cli_headers_passthrough: {
    group: 'recommended',
    label: 'Codex CLI 真实请求头透传',
    description:
      '透传 Codex 会话、窗口、turn metadata 和客户端请求 ID；缺少 Session_id 时用 X-Client-Request-Id 补齐。',
    kind: 'operations',
    payload: CODEX_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  },
  codex_desktop_headers_passthrough: {
    group: 'recommended',
    label: 'Codex Desktop 真实请求头透传',
    description:
      '透传 Codex Desktop 会话、窗口、turn metadata 和客户端请求 ID；缺少 Session_id 时用 X-Client-Request-Id 补齐。',
    kind: 'operations',
    payload: CODEX_DESKTOP_HEADER_PASSTHROUGH_TEMPLATE,
  },
  claude_cli_headers_passthrough: {
    group: 'recommended',
    label: 'Claude Code 真实请求头透传',
    description: '透传 Claude Code 会话、Anthropic Beta 和 Stainless 动态头。',
    kind: 'operations',
    payload: CLAUDE_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  },
  openai_sdk_headers_passthrough: {
    group: 'recommended',
    label: 'OpenAI SDK 元数据透传',
    description:
      '透传 OpenAI-Organization、OpenAI-Project 和 X-Stainless-* 客户端元数据。',
    kind: 'operations',
    payload: OPENAI_SDK_HEADER_PASSTHROUGH_TEMPLATE,
  },
  aws_bedrock_anthropic_beta_override: {
    group: 'recommended',
    label: 'AWS Bedrock Claude Beta 头规范化',
    description:
      '规范化 anthropic-beta 请求头，适配 Bedrock 支持的 beta token。',
    kind: 'operations',
    payload: AWS_BEDROCK_ANTHROPIC_BETA_TEMPLATE,
  },
  remove_image_generation_tool: {
    group: 'recommended',
    label: '上游兼容：移除图片生成工具',
    description: '移除上游不接受的 image_generation 工具对象。',
    kind: 'operations',
    payload: CODEX_REMOVE_IMAGE_GENERATION_TOOL_TEMPLATE,
  },
  aws_bedrock_remove_input_examples: {
    group: 'advanced',
    label: 'AWS Bedrock 删除工具示例字段',
    description: '移除 Bedrock 不兼容的 tools.*.custom.input_examples 字段。',
    kind: 'operations',
    payload: AWS_BEDROCK_REMOVE_INPUT_EXAMPLES_TEMPLATE,
  },
  anthropic_runtime_headers_passthrough: {
    group: 'advanced',
    label: 'Anthropic Beta/Version 透传',
    description:
      '透传 Anthropic-Beta、Anthropic-Version 和 X-App 等运行时请求头。',
    kind: 'operations',
    payload: ANTHROPIC_RUNTIME_HEADER_PASSTHROUGH_TEMPLATE,
  },
  gemini_cli_headers_passthrough: {
    group: 'advanced',
    label: 'Gemini CLI 真实请求头透传',
    description: '透传 Gemini CLI 的 x-goog-api-client 动态头。',
    kind: 'operations',
    payload: GEMINI_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  },
  qwen_code_headers_passthrough: {
    group: 'advanced',
    label: 'Qwen Code 真实请求头透传',
    description: '透传 Qwen Code 使用的 Stainless 客户端动态头。',
    kind: 'operations',
    payload: QWEN_CODE_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  },
  droid_cli_headers_passthrough: {
    group: 'advanced',
    label: 'Droid CLI 真实请求头透传',
    description: '透传 Droid CLI 使用的 Stainless 客户端动态头。',
    kind: 'operations',
    payload: DROID_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  },
  pass_headers_auth: {
    group: 'advanced',
    label: 'Trace 请求头透传',
    description:
      '透传 X-Request-Id、X-Trace-Id、X-Correlation-Id、Traceparent。',
    kind: 'operations',
    payload: HEADER_PASSTHROUGH_TEMPLATE,
  },
  gemini_image_4k: {
    group: 'advanced',
    label: 'Gemini 图片 4K',
    description: '当模型名包含 gemini/image 并以 4k 结尾时写入 imageSize=4K。',
    kind: 'operations',
    payload: GEMINI_IMAGE_4K_TEMPLATE,
  },
  operations_default: {
    group: 'examples',
    label: '示例：按模型设置 temperature',
    description: '示例规则：当模型名以 openai/ 开头时设置 temperature。',
    kind: 'operations',
    payload: OPERATION_TEMPLATE,
  },
  legacy_default: {
    group: 'examples',
    label: '示例：旧格式字段对象',
    description: '旧格式顶层字段对象示例，适合简单字段覆盖。',
    kind: 'legacy',
    payload: LEGACY_TEMPLATE,
  },
};

const QUICK_TEMPLATE_PRESETS = [
  'codex_cli_headers_passthrough',
  'codex_desktop_headers_passthrough',
  'claude_cli_headers_passthrough',
  'openai_sdk_headers_passthrough',
  'aws_bedrock_anthropic_beta_override',
  'remove_image_generation_tool',
];

const FIELD_GUIDE_TARGET_OPTIONS = [
  { label: '填入目标路径', value: 'path' },
  { label: '填入来源字段', value: 'from' },
  { label: '填入目标字段', value: 'to' },
];

const BUILTIN_FIELD_SECTIONS = [
  {
    title: '常用请求字段',
    fields: [
      {
        key: 'model',
        label: '模型名称',
        tip: '支持多级模型名，例如 openai/gpt-4o-mini',
      },
      { key: 'temperature', label: '采样温度', tip: '控制输出随机性' },
      { key: 'max_tokens', label: '最大输出 Token', tip: '控制输出长度上限' },
      {
        key: 'messages.-1.content',
        label: '最后一条消息内容',
        tip: '常用于重写用户输入',
      },
    ],
  },
  {
    title: '上下文字段',
    fields: [
      { key: 'retry.is_retry', label: '是否重试', tip: 'true 表示重试请求' },
      { key: 'last_error.code', label: '上次错误码', tip: '配合重试策略使用' },
      {
        key: 'metadata.conversation_id',
        label: '会话 ID',
        tip: '可用于路由或缓存命中',
      },
    ],
  },
  {
    title: '请求头映射字段',
    fields: [
      {
        key: 'header_override_normalized.authorization',
        label: '标准化 Authorization',
        tip: '统一小写后可稳定匹配',
      },
      {
        key: 'header_override_normalized.x_debug_mode',
        label: '标准化 X-Debug-Mode',
        tip: '适合灰度 / 调试开关判断',
      },
    ],
  },
];

const OPERATION_MODE_LABEL_MAP = OPERATION_MODE_OPTIONS.reduce((acc, item) => {
  acc[item.value] = item.label;
  return acc;
}, {});

let localIdSeed = 0;
const nextLocalId = () => `param_override_${Date.now()}_${localIdSeed++}`;

const toValueText = (value) => {
  if (value === undefined) return '';
  if (typeof value === 'string') return value;
  try {
    return JSON.stringify(value);
  } catch (error) {
    return String(value);
  }
};

const parseLooseValue = (valueText) => {
  const raw = String(valueText ?? '');
  if (raw.trim() === '') return '';
  try {
    return JSON.parse(raw);
  } catch (error) {
    return raw;
  }
};

const parsePassHeaderNames = (rawValue) => {
  if (Array.isArray(rawValue)) {
    return rawValue.map((item) => String(item ?? '').trim()).filter(Boolean);
  }
  if (rawValue && typeof rawValue === 'object') {
    if (rawValue.names !== undefined) {
      return parsePassHeaderNames(rawValue.names);
    }
    if (Array.isArray(rawValue.headers)) {
      return rawValue.headers
        .map((item) => String(item ?? '').trim())
        .filter(Boolean);
    }
    if (rawValue.header !== undefined) {
      const single = String(rawValue.header ?? '').trim();
      return single ? [single] : [];
    }
    return [];
  }
  if (typeof rawValue === 'string') {
    return rawValue
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean);
  }
  return [];
};

const parsePassHeadersDraft = (valueText) => {
  const parsed = parseLooseValue(valueText);
  const headers = parsePassHeaderNames(parsed);
  if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
    if (parsed.names !== undefined) return { sourceKey: 'names', headers };
    if (parsed.header !== undefined) return { sourceKey: 'header', headers };
  }
  return { sourceKey: 'headers', headers };
};

const buildPassHeadersValueText = (draft = {}) => {
  const cleanHeaders = Array.from(
    new Set((draft.headers || []).map((item) => item.trim()).filter(Boolean)),
  );
  if (draft.sourceKey === 'names') {
    return JSON.stringify({ names: cleanHeaders });
  }
  if (draft.sourceKey === 'header') {
    return JSON.stringify({ header: cleanHeaders[0] || '' });
  }
  return JSON.stringify(cleanHeaders);
};

const createStructuredValueNode = (kind = 'string') => ({
  id: nextLocalId(),
  kind,
  text: kind === 'number' ? '0' : '',
  boolValue: true,
  objectEntries: [],
  arrayItems: [],
});

const isJsonLikeStructuredValueText = (valueText) => {
  const trimmed = String(valueText ?? '').trim();
  if (trimmed === '') return false;
  if ('[{'.includes(trimmed[0])) return true;
  if (trimmed[0] === '"') return true;
  if (trimmed === 'true' || trimmed === 'false' || trimmed === 'null') {
    return true;
  }
  return /^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:e[+-]?\d+)?$/i.test(trimmed);
};

const normalizeStructuredValueNode = (value, depth = 0) => {
  if (depth > MAX_STRUCTURED_VALUE_DEPTH) {
    throw new Error(STRUCTURED_VALUE_DEPTH_ERROR);
  }
  if (value === null) {
    return createStructuredValueNode('null');
  }
  if (Array.isArray(value)) {
    return {
      ...createStructuredValueNode('array'),
      arrayItems: value.map((item) => ({
        id: nextLocalId(),
        value: normalizeStructuredValueNode(item, depth + 1),
      })),
    };
  }
  if (value && typeof value === 'object') {
    return {
      ...createStructuredValueNode('object'),
      objectEntries: Object.entries(value).map(([key, item]) => ({
        id: nextLocalId(),
        key,
        value: normalizeStructuredValueNode(item, depth + 1),
      })),
    };
  }
  if (typeof value === 'number' && Number.isFinite(value)) {
    return { ...createStructuredValueNode('number'), text: String(value) };
  }
  if (typeof value === 'boolean') {
    return { ...createStructuredValueNode('boolean'), boolValue: value };
  }
  return { ...createStructuredValueNode('string'), text: String(value ?? '') };
};

const parseStructuredValueNode = (valueText) => {
  const raw = String(valueText ?? '');
  if (raw.trim() === '') {
    return createStructuredValueNode('string');
  }
  if (!isJsonLikeStructuredValueText(raw)) {
    return normalizeStructuredValueNode(raw);
  }
  return normalizeStructuredValueNode(JSON.parse(raw));
};

const parseStructuredValueNodeForDisplay = (valueText) => {
  try {
    return parseStructuredValueNode(valueText);
  } catch (error) {
    return normalizeStructuredValueNode(valueText);
  }
};

const assertStructuredValueInvariant = (condition, message) => {
  if (!condition) {
    throw new Error(message);
  }
};

const getStructuredText = (node) => {
  assertStructuredValueInvariant(
    typeof node.text === 'string',
    'Invalid structured value node text',
  );
  return node.text;
};

const getStructuredBooleanValue = (node) => {
  assertStructuredValueInvariant(
    typeof node.boolValue === 'boolean',
    'Invalid structured value boolean',
  );
  return node.boolValue;
};

const getStructuredObjectEntries = (node) => {
  assertStructuredValueInvariant(
    Array.isArray(node.objectEntries),
    'Invalid structured value object entries',
  );
  return node.objectEntries;
};

const getStructuredArrayItems = (node) => {
  assertStructuredValueInvariant(
    Array.isArray(node.arrayItems),
    'Invalid structured value array items',
  );
  return node.arrayItems;
};

const buildStructuredValue = (node) => {
  switch (node.kind) {
    case 'number': {
      const text = getStructuredText(node);
      const numberValue = Number(text);
      if (!isCompleteStructuredNumberText(text)) {
        throw new Error('Invalid number value');
      }
      return numberValue;
    }
    case 'boolean':
      return getStructuredBooleanValue(node);
    case 'null':
      return null;
    case 'object': {
      const payload = {};
      getStructuredObjectEntries(node).forEach((entry) => {
        assertStructuredValueInvariant(
          typeof entry.key === 'string',
          'Invalid structured value object key',
        );
        const key = entry.key.trim();
        if (key) {
          payload[key] = buildStructuredValue(entry.value);
        }
      });
      return payload;
    }
    case 'array':
      return getStructuredArrayItems(node).map((item) =>
        buildStructuredValue(item.value),
      );
    case 'string':
      return getStructuredText(node);
    default:
      throw new Error('Invalid structured value kind');
  }
};

const shouldQuoteStructuredString = (value) => {
  if (value !== value.trim()) return true;
  if (value.trim() === '') return false;
  try {
    JSON.parse(value);
    return true;
  } catch (error) {
    return false;
  }
};

const buildStructuredValueText = (node) => {
  const value = buildStructuredValue(node);
  if (node.kind === 'string') {
    const text = String(value ?? '');
    return shouldQuoteStructuredString(text) ? JSON.stringify(text) : text;
  }
  return JSON.stringify(value);
};

const canSerializeStructuredValueNode = (node) => {
  switch (node.kind) {
    case 'number':
      return isCompleteStructuredNumberText(getStructuredText(node));
    case 'boolean':
      getStructuredBooleanValue(node);
      return true;
    case 'object':
      return getStructuredObjectEntries(node).every((entry) =>
        canSerializeStructuredValueNode(entry.value),
      );
    case 'array':
      return getStructuredArrayItems(node).every((item) =>
        canSerializeStructuredValueNode(item.value),
      );
    case 'string':
      getStructuredText(node);
      return true;
    case 'null':
      return true;
    default:
      throw new Error('Invalid structured value kind');
  }
};

const parseStructuredValueText = (valueText) =>
  buildStructuredValue(parseStructuredValueNode(valueText));

const normalizeLegacyEntry = (key, value) => ({
  id: nextLocalId(),
  key,
  value_text: toValueText(value),
});

const createDefaultLegacyEntry = () => normalizeLegacyEntry('', '');

const getLegacyEntriesFromObject = (source, options = {}) => {
  const entries = Object.entries(source || {})
    .filter(([key]) => !(options.excludeOperations && key === 'operations'))
    .map(([key, value]) => normalizeLegacyEntry(key, value));
  return entries.length > 0 ? entries : [createDefaultLegacyEntry()];
};

const parseLegacyEntries = (valueText, options = {}) => {
  const raw = String(valueText ?? '').trim();
  if (!raw) return [createDefaultLegacyEntry()];
  try {
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return [createDefaultLegacyEntry()];
    }
    return getLegacyEntriesFromObject(parsed, options);
  } catch (error) {
    return [createDefaultLegacyEntry()];
  }
};

const buildLegacyValueText = (entries = []) => {
  const payload = {};
  entries.forEach((entry) => {
    const key = String(entry.key || '').trim();
    if (!key) return;
    payload[key] = parseStructuredValueText(entry.value_text);
  });
  return Object.keys(payload).length > 0
    ? JSON.stringify(payload, null, 2)
    : '';
};

const buildLegacyPreviewPayload = (entries = []) => {
  const payload = {};
  entries.forEach((entry) => {
    const key = String(entry.key || '').trim();
    if (!key) return;
    payload[key] = parseStructuredValueText(entry.value_text);
  });
  return payload;
};

const buildLegacyOverridePayload = (entries = [], t) => {
  const payload = {};
  let count = 0;
  entries.forEach((entry) => {
    const key = String(entry.key || '').trim();
    const valueText = String(entry.value_text ?? '').trim();
    if (!key && !valueText) return;
    if (!key) {
      throw new Error(t('旧格式字段名不能为空'));
    }
    if (key === 'operations') {
      throw new Error(t('旧格式字段名不能为 operations'));
    }
    if (Object.prototype.hasOwnProperty.call(payload, key)) {
      throw new Error(t('旧格式字段名不能重复'));
    }
    payload[key] = parseStructuredValueText(entry.value_text);
    count += 1;
  });
  return { value: payload, count };
};

const splitHeaderTokenText = (value) => {
  if (Array.isArray(value)) {
    return value.flatMap(splitHeaderTokenText).filter(Boolean);
  }
  return String(value ?? '')
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean);
};

const parseHeaderValueDraft = (valueText) => {
  const defaults = {
    mode: 'direct',
    directText: '',
    keepOnlyDeclared: false,
    appendText: '',
    wildcardAction: 'none',
    wildcardReplacement: '',
    rows: [],
  };
  const raw = String(valueText ?? '').trim();
  if (!raw) return defaults;
  const parsed = parseLooseValue(raw);
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return { ...defaults, directText: String(parsed ?? '') };
  }
  const rows = [];
  Object.entries(parsed).forEach(([token, replacement]) => {
    if (token === '$append' || token === '$keep_only_declared') return;
    if (token === '*') return;
    if (replacement === null) {
      rows.push({
        id: nextLocalId(),
        token,
        action: 'delete',
        replacement: '',
      });
      return;
    }
    const replacementText = splitHeaderTokenText(replacement).join(', ');
    rows.push({
      id: nextLocalId(),
      token,
      action: replacementText === token ? 'keep' : 'replace',
      replacement: replacementText,
    });
  });
  return {
    mode: 'mapping',
    directText: '',
    keepOnlyDeclared: parsed.$keep_only_declared === true,
    appendText: splitHeaderTokenText(parsed.$append).join(', '),
    wildcardAction: Object.prototype.hasOwnProperty.call(parsed, '*')
      ? parsed['*'] === null
        ? 'delete'
        : 'replace'
      : 'none',
    wildcardReplacement:
      Object.prototype.hasOwnProperty.call(parsed, '*') && parsed['*'] !== null
        ? splitHeaderTokenText(parsed['*']).join(', ')
        : '',
    rows,
  };
};

const buildHeaderValueText = (draft = {}) => {
  if (draft.mode === 'direct') {
    return JSON.stringify(String(draft.directText ?? ''));
  }
  const payload = {};
  if (draft.keepOnlyDeclared) payload.$keep_only_declared = true;
  const appendTokens = splitHeaderTokenText(draft.appendText);
  if (appendTokens.length > 0) payload.$append = appendTokens;
  if (draft.wildcardAction === 'delete') {
    payload['*'] = null;
  } else if (draft.wildcardAction === 'replace') {
    const wildcardTokens = splitHeaderTokenText(draft.wildcardReplacement);
    payload['*'] =
      wildcardTokens.length > 1 ? wildcardTokens : wildcardTokens[0] || '';
  }
  (draft.rows || []).forEach((row) => {
    const token = String(row.token || '').trim();
    if (!token) return;
    if (row.action === 'delete') {
      payload[token] = null;
      return;
    }
    if (row.action === 'keep') {
      payload[token] = token;
      return;
    }
    const tokens = splitHeaderTokenText(row.replacement);
    payload[token] = tokens.length > 1 ? tokens : tokens[0] || '';
  });
  return JSON.stringify(payload);
};

const parseReturnErrorDraft = (valueText) => {
  const defaults = {
    message: '',
    statusCode: 400,
    code: '',
    type: '',
    skipRetry: true,
    simpleMode: true,
  };

  const raw = String(valueText ?? '').trim();
  if (!raw) {
    return defaults;
  }

  try {
    const parsed = JSON.parse(raw);
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const statusRaw =
        parsed.status_code !== undefined ? parsed.status_code : parsed.status;
      const statusValue = Number(statusRaw);
      return {
        ...defaults,
        message: String(parsed.message || parsed.msg || '').trim(),
        statusCode:
          Number.isInteger(statusValue) &&
          statusValue >= 100 &&
          statusValue <= 599
            ? statusValue
            : 400,
        code: String(parsed.code || '').trim(),
        type: String(parsed.type || '').trim(),
        skipRetry: parsed.skip_retry !== false,
        simpleMode: false,
      };
    }
  } catch (error) {
    // treat as plain text message
  }

  return {
    ...defaults,
    message: raw,
    simpleMode: true,
  };
};

const buildReturnErrorValueText = (draft = {}) => {
  const message = String(draft.message || '').trim();
  if (draft.simpleMode) {
    return message;
  }

  const statusCode = Number(draft.statusCode);
  const payload = {
    message,
    status_code:
      Number.isInteger(statusCode) && statusCode >= 100 && statusCode <= 599
        ? statusCode
        : 400,
  };
  const code = String(draft.code || '').trim();
  const type = String(draft.type || '').trim();
  if (code) payload.code = code;
  if (type) payload.type = type;
  if (draft.skipRetry === false) {
    payload.skip_retry = false;
  }
  return JSON.stringify(payload);
};

const normalizePruneRule = (rule = {}) => ({
  id: nextLocalId(),
  path: typeof rule.path === 'string' ? rule.path : '',
  mode: CONDITION_MODE_VALUES.has(rule.mode) ? rule.mode : 'full',
  value_text: toValueText(rule.value),
  invert: rule.invert === true,
  pass_missing_key: rule.pass_missing_key === true,
});

export const parsePruneObjectsDraft = (valueText) => {
  const defaults = {
    simpleMode: true,
    typeText: '',
    logic: 'AND',
    recursive: true,
    rules: [],
  };

  const raw = String(valueText ?? '').trim();
  if (!raw) {
    return defaults;
  }

  try {
    const parsed = JSON.parse(raw);
    if (typeof parsed === 'string') {
      return {
        ...defaults,
        simpleMode: true,
        typeText: parsed.trim(),
      };
    }
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const rules = [];
      if (
        parsed.where &&
        typeof parsed.where === 'object' &&
        !Array.isArray(parsed.where)
      ) {
        Object.entries(parsed.where).forEach(([path, value]) => {
          rules.push(
            normalizePruneRule({
              path,
              mode: 'full',
              value,
            }),
          );
        });
      }
      if (Array.isArray(parsed.conditions)) {
        parsed.conditions.forEach((item) => {
          if (item && typeof item === 'object') {
            rules.push(normalizePruneRule(item));
          }
        });
      } else if (
        parsed.conditions &&
        typeof parsed.conditions === 'object' &&
        !Array.isArray(parsed.conditions)
      ) {
        Object.entries(parsed.conditions).forEach(([path, value]) => {
          rules.push(
            normalizePruneRule({
              path,
              mode: 'full',
              value,
            }),
          );
        });
      }

      const typeText =
        parsed.type === undefined ? '' : String(parsed.type).trim();
      const logic =
        String(parsed.logic || 'AND').toUpperCase() === 'OR' ? 'OR' : 'AND';
      const recursive = parsed.recursive !== false;
      const hasAdvancedFields =
        parsed.logic !== undefined ||
        parsed.recursive !== undefined ||
        parsed.where !== undefined ||
        parsed.conditions !== undefined;

      return {
        ...defaults,
        simpleMode: !hasAdvancedFields,
        typeText,
        logic,
        recursive,
        rules,
      };
    }
    return {
      ...defaults,
      simpleMode: true,
      typeText: String(parsed ?? '').trim(),
    };
  } catch (error) {
    return {
      ...defaults,
      simpleMode: true,
      typeText: raw,
    };
  }
};

export const buildPruneObjectsValueText = (draft = {}) => {
  const typeText = String(draft.typeText || '').trim();
  if (draft.simpleMode) {
    return typeText;
  }

  const payload = {};
  if (typeText) {
    payload.type = typeText;
  }
  if (String(draft.logic || 'AND').toUpperCase() === 'OR') {
    payload.logic = 'OR';
  }
  payload.recursive = draft.recursive !== false;

  const conditions = (draft.rules || [])
    .filter((rule) => String(rule.path || '').trim())
    .map((rule) => {
      const conditionPayload = {
        path: String(rule.path || '').trim(),
        mode: CONDITION_MODE_VALUES.has(rule.mode) ? rule.mode : 'full',
      };
      const valueRaw = String(rule.value_text || '').trim();
      if (valueRaw !== '') {
        conditionPayload.value = parseStructuredValueText(valueRaw);
      }
      if (rule.invert) {
        conditionPayload.invert = true;
      }
      if (rule.pass_missing_key) {
        conditionPayload.pass_missing_key = true;
      }
      return conditionPayload;
    });

  if (conditions.length > 0) {
    payload.conditions = conditions;
  }

  if (!payload.type && !payload.conditions) {
    return JSON.stringify({
      logic: String(draft.logic || 'AND').toUpperCase() === 'OR' ? 'OR' : 'AND',
      recursive: draft.recursive !== false,
    });
  }
  return JSON.stringify(payload);
};

const parseSyncTargetSpec = (spec) => {
  const raw = String(spec ?? '').trim();
  if (!raw) return { type: 'json', key: '' };
  const idx = raw.indexOf(':');
  if (idx < 0) return { type: 'json', key: raw };
  const prefix = raw.slice(0, idx).trim().toLowerCase();
  const key = raw.slice(idx + 1).trim();
  if (prefix === 'header') {
    return { type: 'header', key };
  }
  return { type: 'json', key };
};

const buildSyncTargetSpec = (type, key) => {
  const normalizedType = type === 'header' ? 'header' : 'json';
  const normalizedKey = String(key ?? '').trim();
  if (!normalizedKey) return '';
  return `${normalizedType}:${normalizedKey}`;
};

const normalizeCondition = (condition = {}) => ({
  id: nextLocalId(),
  path: typeof condition.path === 'string' ? condition.path : '',
  mode: CONDITION_MODE_VALUES.has(condition.mode) ? condition.mode : 'full',
  value_text: toValueText(condition.value),
  invert: condition.invert === true,
  pass_missing_key: condition.pass_missing_key === true,
});

const normalizeConditionList = (rawConditions) => {
  if (Array.isArray(rawConditions)) {
    return rawConditions
      .filter(
        (condition) =>
          condition &&
          typeof condition === 'object' &&
          !Array.isArray(condition),
      )
      .map(normalizeCondition);
  }
  if (
    rawConditions &&
    typeof rawConditions === 'object' &&
    !Array.isArray(rawConditions)
  ) {
    return Object.entries(rawConditions).map(([path, value]) =>
      normalizeCondition({ path, mode: 'full', value }),
    );
  }
  return [];
};

const createDefaultCondition = () => normalizeCondition({});

const normalizeOperation = (operation = {}) => ({
  id: nextLocalId(),
  description:
    typeof operation.description === 'string' ? operation.description : '',
  path: typeof operation.path === 'string' ? operation.path : '',
  mode: OPERATION_MODE_VALUES.has(operation.mode) ? operation.mode : 'set',
  value_text: toValueText(operation.value),
  keep_origin: operation.keep_origin === true,
  from: typeof operation.from === 'string' ? operation.from : '',
  to: typeof operation.to === 'string' ? operation.to : '',
  logic: String(operation.logic || 'OR').toUpperCase() === 'AND' ? 'AND' : 'OR',
  conditions: normalizeConditionList(operation.conditions),
});

const createDefaultOperation = () => normalizeOperation({ mode: 'set' });

const reorderOperations = (
  sourceOperations = [],
  sourceId,
  targetId,
  position = 'before',
) => {
  if (!sourceId || !targetId || sourceId === targetId) {
    return sourceOperations;
  }

  const sourceIndex = sourceOperations.findIndex(
    (item) => item.id === sourceId,
  );

  if (sourceIndex < 0) {
    return sourceOperations;
  }

  const nextOperations = [...sourceOperations];
  const [moved] = nextOperations.splice(sourceIndex, 1);
  let insertIndex = nextOperations.findIndex((item) => item.id === targetId);

  if (insertIndex < 0) {
    return sourceOperations;
  }

  if (position === 'after') {
    insertIndex += 1;
  }

  nextOperations.splice(insertIndex, 0, moved);
  return nextOperations;
};

const getOperationSummary = (operation = {}, index = 0) => {
  const mode = operation.mode || 'set';
  const modeLabel = OPERATION_MODE_LABEL_MAP[mode] || mode;
  if (mode === 'sync_fields') {
    const from = String(operation.from || '').trim();
    const to = String(operation.to || '').trim();
    return `${index + 1}. ${modeLabel} - ${from || to || '-'}`;
  }
  const path = String(operation.path || '').trim();
  const from = String(operation.from || '').trim();
  const to = String(operation.to || '').trim();
  return `${index + 1}. ${modeLabel} - ${path || from || to || '-'}`;
};

const getOperationModeTagColor = (mode = 'set') => {
  if (mode.includes('header')) return 'cyan';
  if (mode.includes('replace') || mode.includes('trim')) return 'violet';
  if (mode.includes('copy') || mode.includes('move')) return 'blue';
  if (mode.includes('error') || mode.includes('prune')) return 'red';
  if (mode.includes('sync')) return 'green';
  return 'grey';
};

const parseInitialState = (rawValue) => {
  const text = typeof rawValue === 'string' ? rawValue : '';
  const trimmed = text.trim();
  if (!trimmed) {
    return {
      editMode: 'visual',
      visualMode: 'operations',
      legacyValue: '',
      legacyEntries: [createDefaultLegacyEntry()],
      operations: [createDefaultOperation()],
      jsonText: '',
      jsonError: '',
    };
  }

  if (!verifyJSON(trimmed)) {
    return {
      editMode: 'json',
      visualMode: 'operations',
      legacyValue: '',
      legacyEntries: [createDefaultLegacyEntry()],
      operations: [createDefaultOperation()],
      jsonText: text,
      jsonError: 'JSON 格式不正确',
    };
  }

  const parsed = JSON.parse(trimmed);
  const pretty = JSON.stringify(parsed, null, 2);

  if (
    parsed &&
    typeof parsed === 'object' &&
    !Array.isArray(parsed) &&
    Array.isArray(parsed.operations)
  ) {
    return {
      editMode: 'visual',
      visualMode: 'operations',
      legacyValue: '',
      legacyEntries: getLegacyEntriesFromObject(parsed, {
        excludeOperations: true,
      }),
      operations:
        parsed.operations.length > 0
          ? parsed.operations.map(normalizeOperation)
          : [createDefaultOperation()],
      jsonText: pretty,
      jsonError: '',
    };
  }

  if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
    return {
      editMode: 'visual',
      visualMode: 'legacy',
      legacyValue: pretty,
      legacyEntries: parseLegacyEntries(pretty),
      operations: [createDefaultOperation()],
      jsonText: pretty,
      jsonError: '',
    };
  }

  return {
    editMode: 'json',
    visualMode: 'operations',
    legacyValue: '',
    legacyEntries: [createDefaultLegacyEntry()],
    operations: [createDefaultOperation()],
    jsonText: pretty,
    jsonError: '',
  };
};

const isOperationBlank = (operation) => {
  const hasCondition = (operation.conditions || []).some(
    (condition) =>
      condition.path.trim() ||
      String(condition.value_text ?? '').trim() ||
      condition.mode !== 'full' ||
      condition.invert ||
      condition.pass_missing_key,
  );
  return (
    operation.mode === 'set' &&
    !operation.path.trim() &&
    !operation.from.trim() &&
    !operation.to.trim() &&
    String(operation.value_text ?? '').trim() === '' &&
    !operation.keep_origin &&
    !hasCondition
  );
};

const buildConditionPayload = (condition) => {
  const path = condition.path.trim();
  if (!path) return null;
  const payload = {
    path,
    mode: condition.mode || 'full',
    value: parseStructuredValueText(condition.value_text),
  };
  if (condition.invert) payload.invert = true;
  if (condition.pass_missing_key) payload.pass_missing_key = true;
  return payload;
};

const validateOperations = (operations, t) => {
  for (let i = 0; i < operations.length; i++) {
    const op = operations[i];
    const mode = op.mode || 'set';
    const meta = MODE_META[mode] || MODE_META.set;
    const line = i + 1;
    const pathValue = op.path.trim();
    const fromValue = op.from.trim();
    const toValue = op.to.trim();

    if (meta.path && !pathValue) {
      return t('第 {{line}} 条操作缺少目标路径', { line });
    }
    if (FROM_REQUIRED_MODES.has(mode) && !fromValue) {
      if (!(meta.pathAlias && pathValue)) {
        return t('第 {{line}} 条操作缺少来源字段', { line });
      }
    }
    if (TO_REQUIRED_MODES.has(mode) && !toValue) {
      if (!(meta.pathAlias && pathValue)) {
        return t('第 {{line}} 条操作缺少目标字段', { line });
      }
    }
    if (meta.from && !fromValue && !(meta.pathAlias && pathValue)) {
      return t('第 {{line}} 条操作缺少来源字段', { line });
    }
    if (meta.to && !toValue && !(meta.pathAlias && pathValue)) {
      return t('第 {{line}} 条操作缺少目标字段', { line });
    }
    if (
      VALUE_REQUIRED_MODES.has(mode) &&
      String(op.value_text ?? '').trim() === ''
    ) {
      return t('第 {{line}} 条操作缺少值', { line });
    }
    if (mode === 'return_error') {
      const raw = String(op.value_text ?? '').trim();
      if (!raw) {
        return t('第 {{line}} 条操作缺少值', { line });
      }
      try {
        const parsed = JSON.parse(raw);
        if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
          if (
            !String(
              parsed.message !== undefined ? parsed.message : parsed.msg || '',
            ).trim()
          ) {
            return t('第 {{line}} 条 return_error 需要 message 字段', { line });
          }
        }
      } catch (error) {
        // plain string value is allowed
      }
    }

    if (mode === 'prune_objects') {
      const raw = String(op.value_text ?? '').trim();
      if (!raw) {
        return t('第 {{line}} 条 prune_objects 缺少条件', { line });
      }
      try {
        const parsed = JSON.parse(raw);
        if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
          const hasType =
            parsed.type !== undefined && String(parsed.type).trim() !== '';
          const hasWhere =
            parsed.where &&
            typeof parsed.where === 'object' &&
            !Array.isArray(parsed.where) &&
            Object.keys(parsed.where).length > 0;
          const hasConditionsArray =
            Array.isArray(parsed.conditions) && parsed.conditions.length > 0;
          const hasConditionsObject =
            parsed.conditions &&
            typeof parsed.conditions === 'object' &&
            !Array.isArray(parsed.conditions) &&
            Object.keys(parsed.conditions).length > 0;
          if (
            !hasType &&
            !hasWhere &&
            !hasConditionsArray &&
            !hasConditionsObject
          ) {
            return t('第 {{line}} 条 prune_objects 需要至少一个匹配条件', {
              line,
            });
          }
        }
      } catch (error) {
        // non-JSON string is treated as type string
      }
    }

    if (mode === 'set_header') {
      const parsed = parseLooseValue(op.value_text);
      if (parsed === null || parsed === undefined) {
        return t('第 {{line}} 条操作缺少值', { line });
      }
      if (typeof parsed === 'string' && !parsed.trim()) {
        return t('第 {{line}} 条操作缺少值', { line });
      }
      if (
        parsed &&
        typeof parsed === 'object' &&
        !Array.isArray(parsed) &&
        Object.keys(parsed).length === 0
      ) {
        return t('第 {{line}} 条操作缺少值', { line });
      }
    }

    if (mode === 'pass_headers') {
      const raw = String(op.value_text ?? '').trim();
      if (!raw) {
        return t('第 {{line}} 条请求头透传缺少请求头名称', { line });
      }
      const parsed = parseLooseValue(raw);
      const headers = parsePassHeaderNames(parsed);
      if (headers.length === 0) {
        return t('第 {{line}} 条请求头透传格式无效', { line });
      }
    }
  }
  return '';
};

const ParamOverrideEditorModal = ({ visible, value, onSave, onCancel }) => {
  const { t } = useTranslation();
  const isMobile = useIsMobile();

  const [editMode, setEditMode] = useState('visual');
  const [visualMode, setVisualMode] = useState('operations');
  const [legacyValue, setLegacyValue] = useState('');
  const [legacyEntries, setLegacyEntries] = useState([
    createDefaultLegacyEntry(),
  ]);
  const [operations, setOperations] = useState([createDefaultOperation()]);
  const [jsonText, setJsonText] = useState('');
  const [jsonError, setJsonError] = useState('');
  const [operationSearch, setOperationSearch] = useState('');
  const [selectedOperationId, setSelectedOperationId] = useState('');
  const [expandedConditionMap, setExpandedConditionMap] = useState({});
  const [draggedOperationId, setDraggedOperationId] = useState('');
  const [dragOverOperationId, setDragOverOperationId] = useState('');
  const [dragOverPosition, setDragOverPosition] = useState('before');
  const [templateGroupKey, setTemplateGroupKey] = useState('recommended');
  const [templatePresetKey, setTemplatePresetKey] = useState(
    'codex_cli_headers_passthrough',
  );
  const [headerValueExampleVisible, setHeaderValueExampleVisible] =
    useState(false);
  const [fieldGuideVisible, setFieldGuideVisible] = useState(false);
  const [fieldGuideTarget, setFieldGuideTarget] = useState('path');
  const [fieldGuideKeyword, setFieldGuideKeyword] = useState('');
  const [operationEditorActive, setOperationEditorActive] = useState(false);

  useEffect(() => {
    if (!visible) return;
    const nextState = parseInitialState(value);
    setEditMode(nextState.editMode);
    setVisualMode(nextState.visualMode);
    setLegacyValue(nextState.legacyValue);
    setLegacyEntries(nextState.legacyEntries);
    setOperations(nextState.operations);
    setJsonText(nextState.jsonText);
    setJsonError(nextState.jsonError);
    setOperationSearch('');
    setSelectedOperationId(nextState.operations[0]?.id || '');
    setExpandedConditionMap({});
    setDraggedOperationId('');
    setDragOverOperationId('');
    setDragOverPosition('before');
    setTemplateGroupKey('recommended');
    setTemplatePresetKey('codex_cli_headers_passthrough');
    setHeaderValueExampleVisible(false);
    setFieldGuideVisible(false);
    setFieldGuideTarget('path');
    setFieldGuideKeyword('');
    setOperationEditorActive(
      nextState.visualMode !== 'operations' ||
        nextState.operations.some((item) => !isOperationBlank(item)),
    );
  }, [visible, value]);

  useEffect(() => {
    if (operations.length === 0) {
      setSelectedOperationId('');
      return;
    }
    if (!operations.some((item) => item.id === selectedOperationId)) {
      setSelectedOperationId(operations[0].id);
    }
  }, [operations, selectedOperationId]);

  const templatePresetOptions = useMemo(
    () =>
      Object.entries(TEMPLATE_PRESET_CONFIG)
        .filter(([, config]) => config.group === templateGroupKey)
        .map(([value, config]) => ({
          value,
          label: config.label,
        })),
    [templateGroupKey],
  );

  useEffect(() => {
    if (templatePresetOptions.length === 0) return;
    const exists = templatePresetOptions.some(
      (item) => item.value === templatePresetKey,
    );
    if (!exists) {
      setTemplatePresetKey(templatePresetOptions[0].value);
    }
  }, [templatePresetKey, templatePresetOptions]);

  const operationCount = useMemo(
    () => operations.filter((item) => !isOperationBlank(item)).length,
    [operations],
  );

  const filteredOperations = useMemo(() => {
    const keyword = operationSearch.trim().toLowerCase();
    if (!keyword) return operations;
    return operations.filter((operation) => {
      const searchableText = [
        operation.description,
        operation.mode,
        operation.path,
        operation.from,
        operation.to,
        operation.value_text,
      ]
        .filter(Boolean)
        .join(' ')
        .toLowerCase();
      return searchableText.includes(keyword);
    });
  }, [operationSearch, operations]);

  const selectedOperation = useMemo(
    () => operations.find((operation) => operation.id === selectedOperationId),
    [operations, selectedOperationId],
  );

  const selectedOperationIndex = useMemo(
    () =>
      operations.findIndex((operation) => operation.id === selectedOperationId),
    [operations, selectedOperationId],
  );

  const returnErrorDraft = useMemo(() => {
    if (
      !selectedOperation ||
      (selectedOperation.mode || '') !== 'return_error'
    ) {
      return null;
    }
    return parseReturnErrorDraft(selectedOperation.value_text);
  }, [selectedOperation]);

  const pruneObjectsDraft = useMemo(() => {
    if (
      !selectedOperation ||
      (selectedOperation.mode || '') !== 'prune_objects'
    ) {
      return null;
    }
    return parsePruneObjectsDraft(selectedOperation.value_text);
  }, [selectedOperation]);

  const topOperationModes = useMemo(() => {
    const counts = operations.reduce((acc, operation) => {
      const mode = operation.mode || 'set';
      acc[mode] = (acc[mode] || 0) + 1;
      return acc;
    }, {});
    return Object.entries(counts)
      .sort((a, b) => b[1] - a[1])
      .slice(0, 4);
  }, [operations]);

  const buildOperationsJson = useCallback(
    (sourceOperations, options = {}) => {
      const { validate = true } = options;
      const filteredOps = sourceOperations.filter(
        (item) => !isOperationBlank(item),
      );
      if (filteredOps.length === 0) return '';

      if (validate) {
        const message = validateOperations(filteredOps, t);
        if (message) {
          throw new Error(message);
        }
      }

      const payloadOps = filteredOps.map((operation) => {
        const mode = operation.mode || 'set';
        const meta = MODE_META[mode] || MODE_META.set;
        const descriptionValue = String(operation.description || '').trim();
        const pathValue = operation.path.trim();
        const fromValue = operation.from.trim();
        const toValue = operation.to.trim();
        const payload = { mode };
        if (descriptionValue) {
          payload.description = descriptionValue;
        }
        if (meta.path) {
          payload.path = pathValue;
        }
        if (meta.pathOptional && pathValue) {
          payload.path = pathValue;
        }
        if (meta.value) {
          if (
            mode === 'pass_headers' ||
            mode === 'set_header' ||
            mode === 'return_error' ||
            mode === 'prune_objects'
          ) {
            payload.value = parseLooseValue(operation.value_text);
          } else {
            payload.value = parseStructuredValueText(operation.value_text);
          }
        }
        if (meta.keepOrigin && operation.keep_origin) {
          payload.keep_origin = true;
        }
        if (meta.from) {
          payload.from = fromValue;
        }
        if (!meta.to && operation.to.trim()) {
          payload.to = toValue;
        }
        if (meta.to) {
          payload.to = toValue;
        }
        if (meta.pathAlias) {
          if (!payload.from && pathValue) {
            payload.from = pathValue;
          }
          if (!payload.to && pathValue) {
            payload.to = pathValue;
          }
        }

        const conditions = (operation.conditions || [])
          .map(buildConditionPayload)
          .filter(Boolean);

        if (conditions.length > 0) {
          payload.conditions = conditions;
          payload.logic = operation.logic === 'AND' ? 'AND' : 'OR';
        }

        return payload;
      });

      return JSON.stringify({ operations: payloadOps }, null, 2);
    },
    [t],
  );

  const getOperationDedupKey = useCallback(
    (operation) => buildOperationsJson([operation], { validate: false }),
    [buildOperationsJson],
  );

  const buildVisualJson = useCallback(() => {
    const legacyPayload = buildLegacyOverridePayload(legacyEntries, t);
    if (visualMode === 'legacy') {
      if (legacyPayload.count === 0) {
        return '';
      }
      return JSON.stringify(legacyPayload.value, null, 2);
    }
    const operationsJson = buildOperationsJson(operations, { validate: true });
    if (!operationsJson) {
      return legacyPayload.count > 0
        ? JSON.stringify(legacyPayload.value, null, 2)
        : '';
    }
    const operationsPayload = JSON.parse(operationsJson);
    return JSON.stringify(
      { ...legacyPayload.value, ...operationsPayload },
      null,
      2,
    );
  }, [buildOperationsJson, legacyEntries, operations, t, visualMode]);

  const buildVisualJsonPreview = useCallback(() => {
    if (visualMode === 'legacy') {
      return buildLegacyValueText(legacyEntries);
    }
    const legacyPayload = buildLegacyPreviewPayload(legacyEntries);
    const operationsJson = buildOperationsJson(operations, { validate: false });
    if (!operationsJson) {
      return Object.keys(legacyPayload).length > 0
        ? JSON.stringify(legacyPayload, null, 2)
        : '';
    }
    const operationsPayload = JSON.parse(operationsJson);
    return JSON.stringify({ ...legacyPayload, ...operationsPayload }, null, 2);
  }, [buildOperationsJson, legacyEntries, operations, visualMode]);

  const switchToJsonMode = () => {
    if (editMode === 'json') return;
    try {
      setJsonText(buildVisualJson());
      setJsonError('');
    } catch (error) {
      showError(error.message);
      setJsonText(buildVisualJsonPreview());
      setJsonError(error.message || t('参数配置有误'));
    }
    setEditMode('json');
  };

  const switchToVisualMode = () => {
    if (editMode === 'visual') return;
    const trimmed = jsonText.trim();
    if (!trimmed) {
      const fallback = createDefaultOperation();
      setVisualMode('operations');
      setOperations([fallback]);
      setSelectedOperationId(fallback.id);
      setLegacyValue('');
      setLegacyEntries([createDefaultLegacyEntry()]);
      setJsonError('');
      setEditMode('visual');
      setOperationEditorActive(false);
      return;
    }
    if (!verifyJSON(trimmed)) {
      showError(t('参数覆盖必须是合法的 JSON 格式！'));
      return;
    }
    const parsed = JSON.parse(trimmed);
    if (
      parsed &&
      typeof parsed === 'object' &&
      !Array.isArray(parsed) &&
      Array.isArray(parsed.operations)
    ) {
      const nextOperations =
        parsed.operations.length > 0
          ? parsed.operations.map(normalizeOperation)
          : [createDefaultOperation()];
      setVisualMode('operations');
      setOperations(nextOperations);
      setSelectedOperationId(nextOperations[0]?.id || '');
      setLegacyValue('');
      setLegacyEntries(
        getLegacyEntriesFromObject(parsed, { excludeOperations: true }),
      );
      setJsonError('');
      setEditMode('visual');
      setTemplateGroupKey('recommended');
      setTemplatePresetKey('codex_cli_headers_passthrough');
      setOperationEditorActive(
        nextOperations.some((item) => !isOperationBlank(item)),
      );
      return;
    }
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const fallback = createDefaultOperation();
      setVisualMode('legacy');
      const text = JSON.stringify(parsed, null, 2);
      setLegacyValue(text);
      setLegacyEntries(parseLegacyEntries(text));
      setOperations([fallback]);
      setSelectedOperationId(fallback.id);
      setJsonError('');
      setEditMode('visual');
      setTemplateGroupKey('recommended');
      setTemplatePresetKey('codex_cli_headers_passthrough');
      setOperationEditorActive(true);
      return;
    }
    showError(t('参数覆盖必须是合法的 JSON 对象'));
  };

  const fillLegacyTemplate = (legacyPayload) => {
    const text = JSON.stringify(legacyPayload, null, 2);
    const fallback = createDefaultOperation();
    setVisualMode('legacy');
    setLegacyValue(text);
    setLegacyEntries(parseLegacyEntries(text));
    setOperations([fallback]);
    setSelectedOperationId(fallback.id);
    setExpandedConditionMap({});
    setJsonText(text);
    setJsonError('');
    setEditMode('visual');
    setOperationEditorActive(true);
  };

  const fillOperationsTemplate = (operationsPayload) => {
    const nextOperations = (operationsPayload || []).map(normalizeOperation);
    const finalOperations =
      nextOperations.length > 0 ? nextOperations : [createDefaultOperation()];
    setVisualMode('operations');
    setOperations(finalOperations);
    setSelectedOperationId(finalOperations[0]?.id || '');
    setExpandedConditionMap({});
    setJsonText(
      JSON.stringify({ operations: operationsPayload || [] }, null, 2),
    );
    setJsonError('');
    setEditMode('visual');
    setOperationEditorActive(true);
  };

  const appendLegacyTemplate = (legacyPayload) => {
    let parsedCurrent = {};
    const trimmed = legacyValue.trim();
    if (trimmed) {
      if (!verifyJSON(trimmed)) {
        showError(t('当前旧格式 JSON 不合法，无法追加模板'));
        return;
      }
      const parsed = JSON.parse(trimmed);
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        showError(t('当前旧格式不是 JSON 对象，无法追加模板'));
        return;
      }
      parsedCurrent = parsed;
    }

    const merged = {
      ...(legacyPayload || {}),
      ...parsedCurrent,
    };
    const text = JSON.stringify(merged, null, 2);
    setLegacyValue(text);
    setLegacyEntries(parseLegacyEntries(text));
    setExpandedConditionMap({});
    setJsonText(text);
    setJsonError('');
    setEditMode('visual');
    setOperationEditorActive(true);
  };

  const appendOperationsTemplate = (operationsPayload) => {
    const appended = (operationsPayload || []).map(normalizeOperation);
    const existing =
      visualMode === 'operations'
        ? operations.filter((item) => !isOperationBlank(item))
        : [];
    const existingKeys = new Set(existing.map(getOperationDedupKey));
    const uniqueAppended = appended.filter((operation) => {
      const key = getOperationDedupKey(operation);
      if (existingKeys.has(key)) {
        return false;
      }
      existingKeys.add(key);
      return true;
    });
    const nextOperations = [...existing, ...uniqueAppended];
    setVisualMode('operations');
    setOperations(nextOperations.length > 0 ? nextOperations : appended);
    setSelectedOperationId(
      uniqueAppended[0]?.id || nextOperations[0]?.id || appended[0]?.id || '',
    );
    setExpandedConditionMap({});
    setJsonError('');
    setEditMode('visual');
    setJsonText('');
    setOperationEditorActive(true);
  };

  const clearValue = () => {
    const fallback = createDefaultOperation();
    setVisualMode('operations');
    setLegacyValue('');
    setLegacyEntries([createDefaultLegacyEntry()]);
    setOperations([fallback]);
    setSelectedOperationId(fallback.id);
    setExpandedConditionMap({});
    setJsonText('');
    setJsonError('');
    setTemplateGroupKey('recommended');
    setTemplatePresetKey('codex_cli_headers_passthrough');
    setOperationEditorActive(false);
  };

  const getSelectedTemplatePreset = () =>
    TEMPLATE_PRESET_CONFIG[templatePresetKey] ||
    TEMPLATE_PRESET_CONFIG.codex_cli_headers_passthrough;

  const selectedTemplatePreset = getSelectedTemplatePreset();

  const selectTemplatePreset = (presetKey) => {
    const preset =
      TEMPLATE_PRESET_CONFIG[presetKey] ||
      TEMPLATE_PRESET_CONFIG.codex_cli_headers_passthrough;
    setTemplateGroupKey(preset.group || 'recommended');
    setTemplatePresetKey(presetKey);
  };

  const applyTemplatePreset = (presetKey, action = 'replace') => {
    const preset =
      TEMPLATE_PRESET_CONFIG[presetKey] ||
      TEMPLATE_PRESET_CONFIG.codex_cli_headers_passthrough;
    setTemplateGroupKey(preset.group || 'recommended');
    setTemplatePresetKey(presetKey);
    if (preset.kind === 'legacy') {
      if (action === 'add') {
        appendLegacyTemplate(preset.payload || {});
      } else {
        fillLegacyTemplate(preset.payload || {});
      }
      return;
    }
    if (action === 'add') {
      appendOperationsTemplate(preset.payload?.operations || []);
    } else {
      fillOperationsTemplate(preset.payload?.operations || []);
    }
  };

  const replaceWithSelectedTemplate = () => {
    applyTemplatePreset(templatePresetKey, 'replace');
  };

  const addSelectedTemplate = () => {
    applyTemplatePreset(templatePresetKey, 'add');
  };

  const resetEditorState = () => {
    clearValue();
    setEditMode('visual');
  };

  const applyBuiltinField = (fieldKey, target = 'path') => {
    if (!selectedOperation) {
      showError(t('请先选择一条规则'));
      return;
    }
    const mode = selectedOperation.mode || 'set';
    const meta = MODE_META[mode] || MODE_META.set;
    if (
      target === 'path' &&
      (meta.path || meta.pathOptional || meta.pathAlias)
    ) {
      updateOperation(selectedOperation.id, { path: fieldKey });
      return;
    }
    if (
      target === 'from' &&
      (meta.from || meta.pathAlias || mode === 'sync_fields')
    ) {
      updateOperation(selectedOperation.id, {
        from:
          mode === 'sync_fields'
            ? buildSyncTargetSpec('json', fieldKey)
            : fieldKey,
      });
      return;
    }
    if (target === 'to' && (meta.to || mode === 'sync_fields')) {
      updateOperation(selectedOperation.id, {
        to:
          mode === 'sync_fields'
            ? buildSyncTargetSpec('json', fieldKey)
            : fieldKey,
      });
      return;
    }
    showError(t('当前规则不支持写入到该位置'));
  };

  const openFieldGuide = (target = 'path') => {
    setFieldGuideTarget(target);
    setFieldGuideVisible(true);
  };

  const copyBuiltinField = async (fieldKey) => {
    const ok = await copy(fieldKey);
    if (ok) {
      showSuccess(t('已复制字段：{{name}}', { name: fieldKey }));
    } else {
      showError(t('复制失败'));
    }
  };

  const filteredFieldGuideSections = useMemo(() => {
    const keyword = fieldGuideKeyword.trim().toLowerCase();
    if (!keyword) {
      return BUILTIN_FIELD_SECTIONS;
    }
    return BUILTIN_FIELD_SECTIONS.map((section) => ({
      ...section,
      fields: section.fields.filter((field) =>
        [field.key, field.label, field.tip]
          .filter(Boolean)
          .join(' ')
          .toLowerCase()
          .includes(keyword),
      ),
    })).filter((section) => section.fields.length > 0);
  }, [fieldGuideKeyword]);

  const fieldGuideActionLabel = useMemo(() => {
    if (fieldGuideTarget === 'from') return t('填入来源');
    if (fieldGuideTarget === 'to') return t('填入目标');
    return t('填入路径');
  }, [fieldGuideTarget, t]);

  const fieldGuideFieldCount = useMemo(
    () =>
      filteredFieldGuideSections.reduce(
        (total, section) => total + section.fields.length,
        0,
      ),
    [filteredFieldGuideSections],
  );

  const updateOperation = (operationId, patch) => {
    setOperations((prev) =>
      prev.map((item) =>
        item.id === operationId ? { ...item, ...patch } : item,
      ),
    );
  };

  const updateLegacyEntry = (entryId, patch) => {
    setLegacyEntries((prev) => {
      const nextEntries = prev.map((entry) =>
        entry.id === entryId ? { ...entry, ...patch } : entry,
      );
      setLegacyValue(buildLegacyValueText(nextEntries));
      return nextEntries;
    });
  };

  const addLegacyEntry = () => {
    setLegacyEntries((prev) => [...prev, createDefaultLegacyEntry()]);
  };

  const removeLegacyEntry = (entryId) => {
    setLegacyEntries((prev) => {
      const nextEntries =
        prev.length <= 1
          ? [createDefaultLegacyEntry()]
          : prev.filter((entry) => entry.id !== entryId);
      setLegacyValue(buildLegacyValueText(nextEntries));
      return nextEntries;
    });
  };

  const updateReturnErrorDraft = (operationId, draftPatch = {}) => {
    const current = operations.find((item) => item.id === operationId);
    if (!current) return;
    const draft = parseReturnErrorDraft(current.value_text);
    const nextDraft = { ...draft, ...draftPatch };
    updateOperation(operationId, {
      value_text: buildReturnErrorValueText(nextDraft),
    });
  };

  const updatePruneObjectsDraft = (operationId, updater) => {
    const current = operations.find((item) => item.id === operationId);
    if (!current) return;
    const draft = parsePruneObjectsDraft(current.value_text);
    const nextDraft =
      typeof updater === 'function'
        ? updater(draft)
        : { ...draft, ...(updater || {}) };
    updateOperation(operationId, {
      value_text: buildPruneObjectsValueText(nextDraft),
    });
  };

  const addPruneRule = (operationId) => {
    updatePruneObjectsDraft(operationId, (draft) => ({
      ...draft,
      simpleMode: false,
      rules: [...(draft.rules || []), normalizePruneRule({})],
    }));
  };

  const updatePruneRule = (operationId, ruleId, patch) => {
    updatePruneObjectsDraft(operationId, (draft) => ({
      ...draft,
      rules: (draft.rules || []).map((rule) =>
        rule.id === ruleId ? { ...rule, ...patch } : rule,
      ),
    }));
  };

  const removePruneRule = (operationId, ruleId) => {
    updatePruneObjectsDraft(operationId, (draft) => ({
      ...draft,
      rules: (draft.rules || []).filter((rule) => rule.id !== ruleId),
    }));
  };

  const addOperation = () => {
    const created = createDefaultOperation();
    setOperations((prev) => [...prev, created]);
    setSelectedOperationId(created.id);
    setOperationEditorActive(true);
  };

  const startOperationEditor = () => {
    setOperationEditorActive(true);
    if (operations.length === 0) {
      const created = createDefaultOperation();
      setOperations([created]);
      setSelectedOperationId(created.id);
      return;
    }
    setSelectedOperationId(operations[0].id);
  };

  const resetOperationDragState = useCallback(() => {
    setDraggedOperationId('');
    setDragOverOperationId('');
    setDragOverPosition('before');
  }, []);

  const moveOperation = useCallback(
    (sourceId, targetId, position = 'before') => {
      if (!sourceId || !targetId || sourceId === targetId) {
        return;
      }
      setOperations((prev) =>
        reorderOperations(prev, sourceId, targetId, position),
      );
      setSelectedOperationId(sourceId);
    },
    [],
  );

  const handleOperationDragStart = useCallback((event, operationId) => {
    setDraggedOperationId(operationId);
    setSelectedOperationId(operationId);
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData('text/plain', operationId);
  }, []);

  const handleOperationDragOver = useCallback(
    (event, operationId) => {
      event.preventDefault();
      if (!draggedOperationId || draggedOperationId === operationId) {
        return;
      }
      const rect = event.currentTarget.getBoundingClientRect();
      const position =
        event.clientY - rect.top > rect.height / 2 ? 'after' : 'before';
      setDragOverOperationId(operationId);
      setDragOverPosition(position);
      event.dataTransfer.dropEffect = 'move';
    },
    [draggedOperationId],
  );

  const handleOperationDrop = useCallback(
    (event, operationId) => {
      event.preventDefault();
      const sourceId =
        draggedOperationId || event.dataTransfer.getData('text/plain');
      const position =
        dragOverOperationId === operationId ? dragOverPosition : 'before';
      moveOperation(sourceId, operationId, position);
      resetOperationDragState();
    },
    [
      dragOverOperationId,
      dragOverPosition,
      draggedOperationId,
      moveOperation,
      resetOperationDragState,
    ],
  );

  const duplicateOperation = (operationId) => {
    let insertedId = '';
    setOperations((prev) => {
      const index = prev.findIndex((item) => item.id === operationId);
      if (index < 0) return prev;
      const source = prev[index];
      const cloned = normalizeOperation({
        description: source.description,
        path: source.path,
        mode: source.mode,
        value: parseLooseValue(source.value_text),
        keep_origin: source.keep_origin,
        from: source.from,
        to: source.to,
        logic: source.logic,
        conditions: (source.conditions || []).map((condition) => ({
          path: condition.path,
          mode: condition.mode,
          value: parseLooseValue(condition.value_text),
          invert: condition.invert,
          pass_missing_key: condition.pass_missing_key,
        })),
      });
      insertedId = cloned.id;
      const next = [...prev];
      next.splice(index + 1, 0, cloned);
      return next;
    });
    if (insertedId) {
      setSelectedOperationId(insertedId);
    }
  };

  const removeOperation = (operationId) => {
    setOperations((prev) => {
      if (prev.length <= 1) return [createDefaultOperation()];
      return prev.filter((item) => item.id !== operationId);
    });
    setExpandedConditionMap((prev) => {
      if (!Object.prototype.hasOwnProperty.call(prev, operationId)) {
        return prev;
      }
      const next = { ...prev };
      delete next[operationId];
      return next;
    });
  };

  const addCondition = (operationId) => {
    const createdCondition = createDefaultCondition();
    setOperations((prev) =>
      prev.map((operation) =>
        operation.id === operationId
          ? {
              ...operation,
              conditions: [...(operation.conditions || []), createdCondition],
            }
          : operation,
      ),
    );
    setExpandedConditionMap((prev) => ({
      ...prev,
      [operationId]: [...(prev[operationId] || []), createdCondition.id],
    }));
  };

  const updateCondition = (operationId, conditionId, patch) => {
    setOperations((prev) =>
      prev.map((operation) => {
        if (operation.id !== operationId) return operation;
        return {
          ...operation,
          conditions: (operation.conditions || []).map((condition) =>
            condition.id === conditionId
              ? { ...condition, ...patch }
              : condition,
          ),
        };
      }),
    );
  };

  const removeCondition = (operationId, conditionId) => {
    setOperations((prev) =>
      prev.map((operation) => {
        if (operation.id !== operationId) return operation;
        return {
          ...operation,
          conditions: (operation.conditions || []).filter(
            (condition) => condition.id !== conditionId,
          ),
        };
      }),
    );
    setExpandedConditionMap((prev) => ({
      ...prev,
      [operationId]: (prev[operationId] || []).filter(
        (id) => id !== conditionId,
      ),
    }));
  };

  const selectedConditionKeys = useMemo(
    () => expandedConditionMap[selectedOperationId] || [],
    [expandedConditionMap, selectedOperationId],
  );

  const handleConditionCollapseChange = useCallback(
    (operationId, activeKeys) => {
      const keys = (
        Array.isArray(activeKeys) ? activeKeys : [activeKeys]
      ).filter(Boolean);
      setExpandedConditionMap((prev) => ({
        ...prev,
        [operationId]: keys,
      }));
    },
    [],
  );

  const expandAllSelectedConditions = useCallback(() => {
    if (!selectedOperationId || !selectedOperation) return;
    setExpandedConditionMap((prev) => ({
      ...prev,
      [selectedOperationId]: (selectedOperation.conditions || []).map(
        (condition) => condition.id,
      ),
    }));
  }, [selectedOperation, selectedOperationId]);

  const collapseAllSelectedConditions = useCallback(() => {
    if (!selectedOperationId) return;
    setExpandedConditionMap((prev) => ({
      ...prev,
      [selectedOperationId]: [],
    }));
  }, [selectedOperationId]);

  const handleJsonChange = (nextValue) => {
    setJsonText(nextValue);
    const trimmed = String(nextValue || '').trim();
    if (!trimmed) {
      setJsonError('');
      return;
    }
    if (!verifyJSON(trimmed)) {
      setJsonError(t('JSON格式错误'));
      return;
    }
    setJsonError('');
  };

  const formatJson = () => {
    const trimmed = jsonText.trim();
    if (!trimmed) return;
    if (!verifyJSON(trimmed)) {
      showError(t('参数覆盖必须是合法的 JSON 格式！'));
      return;
    }
    setJsonText(JSON.stringify(JSON.parse(trimmed), null, 2));
    setJsonError('');
  };

  const visualValidationError = useMemo(() => {
    if (editMode !== 'visual') {
      return '';
    }
    try {
      buildVisualJson();
      return '';
    } catch (error) {
      return error?.message || t('参数配置有误');
    }
  }, [buildVisualJson, editMode, t]);
  const shouldShowOperationEmptyState =
    editMode === 'visual' &&
    visualMode === 'operations' &&
    !operationEditorActive &&
    operationCount === 0;

  const handleSave = () => {
    try {
      let result = '';
      if (editMode === 'json') {
        const trimmed = jsonText.trim();
        if (!trimmed) {
          result = '';
        } else {
          if (!verifyJSON(trimmed)) {
            throw new Error(t('参数覆盖必须是合法的 JSON 格式！'));
          }
          result = JSON.stringify(JSON.parse(trimmed), null, 2);
        }
      } else {
        result = buildVisualJson();
      }
      onSave?.(result);
    } catch (error) {
      showError(error.message);
    }
  };

  return (
    <>
      <Modal
        title={t('高级请求规则')}
        visible={visible}
        width={
          isMobile ? 'calc(100vw - 16px)' : 'min(980px, calc(100vw - 24px))'
        }
        style={isMobile ? { margin: '8px auto' } : undefined}
        bodyStyle={{
          maxHeight: isMobile ? 'calc(100vh - 188px)' : '76vh',
          overflowY: 'auto',
          overflowX: 'hidden',
          padding: isMobile ? '10px 12px 12px' : undefined,
          paddingTop: isMobile ? 10 : 10,
        }}
        onCancel={onCancel}
        onOk={handleSave}
        okText={t('保存')}
        cancelText={t('取消')}
      >
        <div className='flex flex-col gap-3 min-w-0'>
          <div
            className='rounded-lg px-3 py-2 min-w-0'
            style={{
              background: 'var(--semi-color-fill-0)',
              border: '1px solid var(--semi-color-fill-2)',
            }}
          >
            <div className='flex items-start justify-between gap-3 flex-wrap'>
              <div className='min-w-0' style={{ maxWidth: 640 }}>
                <Text strong size='small'>
                  {t('只在特殊场景配置')}
                </Text>
                <Text type='tertiary' size='small' className='block mt-1'>
                  {t(
                    '这里会改写请求体或透传运行期请求头。只改 User-Agent、X-Client-Name 等固定请求头时，请使用客户端模板。',
                  )}
                </Text>
              </div>
              <Space wrap spacing={6}>
                <Tag color={operationCount > 0 ? 'cyan' : 'grey'}>
                  {operationCount > 0
                    ? t('规则 {{count}} 条', { count: operationCount })
                    : t('未配置')}
                </Tag>
                <Button
                  size='small'
                  type={editMode === 'visual' ? 'primary' : 'tertiary'}
                  onClick={switchToVisualMode}
                >
                  {t('可视化')}
                </Button>
                <Button
                  size='small'
                  type={editMode === 'json' ? 'primary' : 'tertiary'}
                  onClick={switchToJsonMode}
                >
                  {t('JSON 文本')}
                </Button>
                <Button size='small' type='tertiary' onClick={resetEditorState}>
                  {t('重置')}
                </Button>
              </Space>
            </div>
            <Space wrap spacing={6} className='mt-2'>
              <Tag size='small'>{t('改写请求体字段')}</Tag>
              <Tag size='small'>{t('透传 CLI 动态头')}</Tag>
              <Tag size='small'>{t('兼容上游参数差异')}</Tag>
            </Space>
            <div className='mt-3 flex items-start gap-2 flex-wrap'>
              <Text strong size='small' className='leading-8'>
                {t('快速选择')}
              </Text>
              <Space wrap spacing={6}>
                {QUICK_TEMPLATE_PRESETS.map((presetKey) => {
                  const preset = TEMPLATE_PRESET_CONFIG[presetKey];
                  if (!preset) return null;
                  const isSelected = presetKey === templatePresetKey;
                  return (
                    <Button
                      key={presetKey}
                      size='small'
                      type={isSelected ? 'primary' : 'tertiary'}
                      theme={isSelected ? 'solid' : 'light'}
                      onClick={() => selectTemplatePreset(presetKey)}
                    >
                      {t(preset.label)}
                    </Button>
                  );
                })}
              </Space>
            </div>
            <Text type='tertiary' size='small' className='block mt-1'>
              {t('先选择方案。只有点击应用按钮后，才会修改当前规则。')}
            </Text>
          </div>
          <Collapse
            keepDOM
            defaultActiveKey={['templates']}
            style={{ width: '100%' }}
          >
            <Collapse.Panel
              itemKey='templates'
              header={
                <Space wrap spacing={8}>
                  <Text className='font-medium' size='small'>
                    {t('预置规则方案库')}
                  </Text>
                  <Tag size='small' color='grey'>
                    {t('先选择，再应用')}
                  </Tag>
                </Space>
              }
            >
              <Space vertical spacing={8} style={{ width: '100%' }}>
                <Space wrap spacing={8} style={{ width: '100%' }}>
                  <Text type='tertiary' size='small' className='leading-8'>
                    {t('分类')}
                  </Text>
                  <Select
                    value={templateGroupKey}
                    optionList={TEMPLATE_GROUP_OPTIONS}
                    onChange={(nextValue) =>
                      setTemplateGroupKey(nextValue || 'recommended')
                    }
                    style={{ width: 130 }}
                  />
                  <Text type='tertiary' size='small' className='leading-8'>
                    {t('所选预设')}
                  </Text>
                  <Select
                    value={templatePresetKey}
                    optionList={templatePresetOptions}
                    onChange={(nextValue) =>
                      setTemplatePresetKey(
                        nextValue || 'codex_cli_headers_passthrough',
                      )
                    }
                    style={{ minWidth: 220, flex: '1 1 260px' }}
                  />
                </Space>
                <Space wrap spacing={8} style={{ width: '100%' }}>
                  <Button
                    size='small'
                    type='primary'
                    onClick={replaceWithSelectedTemplate}
                  >
                    {t('替换当前规则')}
                  </Button>
                  <Button
                    size='small'
                    type='tertiary'
                    icon={<IconPlus />}
                    onClick={addSelectedTemplate}
                  >
                    {t('追加到现有规则')}
                  </Button>
                </Space>
              </Space>
              <Text type='tertiary' size='small' className='block mt-2'>
                {t(
                  '替换当前规则会先删除现有规则；追加到现有规则会把所选方案添加到规则末尾。',
                )}
              </Text>
              {selectedTemplatePreset?.description ? (
                <Text type='tertiary' size='small' className='block mt-1'>
                  {t(selectedTemplatePreset.description)}
                </Text>
              ) : null}
            </Collapse.Panel>
          </Collapse>

          {editMode === 'visual' ? (
            <div className='min-w-0' style={{ width: '100%' }}>
              {visualMode === 'legacy' ? (
                <LegacyOverrideEditor
                  t={t}
                  entries={legacyEntries}
                  addEntry={addLegacyEntry}
                  updateEntry={updateLegacyEntry}
                  removeEntry={removeLegacyEntry}
                />
              ) : shouldShowOperationEmptyState ? (
                <Card
                  className='!rounded-lg !border-0'
                  bodyStyle={{
                    padding: 18,
                    background: 'var(--semi-color-fill-0)',
                  }}
                >
                  <div className='flex items-start justify-between gap-3 flex-wrap'>
                    <div className='min-w-0' style={{ maxWidth: 620 }}>
                      <Text strong>{t('当前未配置高级请求规则')}</Text>
                      <Text type='tertiary' size='small' className='block mt-1'>
                        {t(
                          '如果只是给渠道设置 User-Agent 或客户端名称，请返回选择客户端模板；只有需要改写请求体字段或透传客户端动态请求头时再新增规则。',
                        )}
                      </Text>
                    </div>
                    <Space wrap spacing={8}>
                      <Button
                        size='small'
                        type='primary'
                        icon={<IconPlus />}
                        onClick={startOperationEditor}
                      >
                        {t('新增规则')}
                      </Button>
                      <Button
                        size='small'
                        type='tertiary'
                        onClick={replaceWithSelectedTemplate}
                      >
                        {t('用所选预设创建规则')}
                      </Button>
                      <Button
                        size='small'
                        type='tertiary'
                        onClick={switchToJsonMode}
                      >
                        {t('粘贴 JSON')}
                      </Button>
                    </Space>
                  </div>
                </Card>
              ) : (
                <div>
                  <Collapse keepDOM style={{ marginBottom: 8 }}>
                    <Collapse.Panel
                      itemKey='top_level_field_overrides'
                      header={
                        <Space spacing={8}>
                          <Text strong size='small'>
                            {t('顶层字段覆盖')}
                          </Text>
                          <Tag size='small' color='grey'>
                            {
                              legacyEntries.filter(
                                (entry) =>
                                  String(entry.key || '').trim() ||
                                  String(entry.value_text ?? '').trim(),
                              ).length
                            }
                          </Tag>
                        </Space>
                      }
                    >
                      <LegacyOverrideEditor
                        t={t}
                        compact
                        entries={legacyEntries}
                        addEntry={addLegacyEntry}
                        updateEntry={updateLegacyEntry}
                        removeEntry={removeLegacyEntry}
                      />
                    </Collapse.Panel>
                  </Collapse>

                  <div className='flex items-center justify-between gap-2 mb-2 flex-wrap'>
                    <Space wrap spacing={8}>
                      <Text strong size='small'>
                        {t('规则设置')}
                      </Text>
                      <Tag color='cyan'>{`${t('规则')}: ${operationCount}`}</Tag>
                    </Space>
                    <Button
                      size='small'
                      icon={<IconPlus />}
                      onClick={addOperation}
                    >
                      {t('新增规则')}
                    </Button>
                  </div>

                  <Row gutter={12}>
                    <Col xs={24} md={8} style={{ order: isMobile ? 2 : 0 }}>
                      <Card
                        className='!rounded-lg !border-0 h-full'
                        bodyStyle={{
                          padding: 10,
                          background: 'var(--semi-color-fill-0)',
                          display: 'flex',
                          flexDirection: 'column',
                          gap: 8,
                          minHeight: isMobile ? 0 : 420,
                          minWidth: 0,
                        }}
                      >
                        <div className='flex items-center justify-between'>
                          <Text strong size='small'>
                            {t('规则导航')}
                          </Text>
                          <Tag
                            size='small'
                            color='grey'
                          >{`${operationCount}/${operations.length}`}</Tag>
                        </div>

                        {topOperationModes.length > 0 ? (
                          <Space wrap spacing={6}>
                            {topOperationModes.map(([mode, count]) => (
                              <Tag
                                key={`mode_stat_${mode}`}
                                size='small'
                                color={getOperationModeTagColor(mode)}
                              >
                                {`${OPERATION_MODE_LABEL_MAP[mode] || mode} - ${count}`}
                              </Tag>
                            ))}
                          </Space>
                        ) : null}

                        <Input
                          value={operationSearch}
                          placeholder={t(
                            '搜索规则（描述 / 类型 / 路径 / 来源 / 目标）',
                          )}
                          onChange={(nextValue) =>
                            setOperationSearch(nextValue || '')
                          }
                          showClear
                          name='components-table-channels-modals-paramoverrideeditormodal-input-1'
                        />

                        <div
                          className='overflow-auto'
                          style={{
                            flex: 1,
                            minHeight: isMobile ? 0 : 260,
                            maxHeight: isMobile ? 220 : undefined,
                            paddingRight: 2,
                          }}
                        >
                          {filteredOperations.length === 0 ? (
                            <Text type='tertiary' size='small'>
                              {t('没有匹配的规则')}
                            </Text>
                          ) : (
                            <div
                              style={{
                                display: 'flex',
                                flexDirection: 'column',
                                gap: 8,
                                width: '100%',
                              }}
                            >
                              {filteredOperations.map((operation) => {
                                const index = operations.findIndex(
                                  (item) => item.id === operation.id,
                                );
                                const isActive =
                                  operation.id === selectedOperationId;
                                const isDragging =
                                  operation.id === draggedOperationId;
                                const isDropTarget =
                                  operation.id === dragOverOperationId &&
                                  draggedOperationId &&
                                  draggedOperationId !== operation.id;
                                return (
                                  <div
                                    key={operation.id}
                                    role='button'
                                    tabIndex={0}
                                    draggable={operations.length > 1}
                                    onClick={() =>
                                      setSelectedOperationId(operation.id)
                                    }
                                    onDragStart={(event) =>
                                      handleOperationDragStart(
                                        event,
                                        operation.id,
                                      )
                                    }
                                    onDragOver={(event) =>
                                      handleOperationDragOver(
                                        event,
                                        operation.id,
                                      )
                                    }
                                    onDrop={(event) =>
                                      handleOperationDrop(event, operation.id)
                                    }
                                    onDragEnd={resetOperationDragState}
                                    onKeyDown={(event) => {
                                      if (
                                        event.key === 'Enter' ||
                                        event.key === ' '
                                      ) {
                                        event.preventDefault();
                                        setSelectedOperationId(operation.id);
                                      }
                                    }}
                                    className='w-full rounded-md px-2.5 py-2 cursor-pointer transition-colors'
                                    style={{
                                      background: isActive
                                        ? 'var(--semi-color-primary-light-default)'
                                        : 'var(--semi-color-bg-2)',
                                      border: isActive
                                        ? '1px solid var(--semi-color-primary)'
                                        : '1px solid var(--semi-color-border)',
                                      opacity: isDragging ? 0.6 : 1,
                                      boxShadow: isDropTarget
                                        ? dragOverPosition === 'after'
                                          ? 'inset 0 -3px 0 var(--semi-color-primary)'
                                          : 'inset 0 3px 0 var(--semi-color-primary)'
                                        : 'none',
                                    }}
                                  >
                                    <div className='flex items-start justify-between gap-2'>
                                      <div className='flex items-start gap-2 min-w-0'>
                                        <div
                                          className='flex-shrink-0'
                                          style={{
                                            color: 'var(--semi-color-text-2)',
                                            cursor:
                                              operations.length > 1
                                                ? 'grab'
                                                : 'default',
                                            marginTop: 1,
                                          }}
                                        >
                                          <IconMenu />
                                        </div>
                                        <div className='min-w-0'>
                                          <Text strong>{`#${index + 1}`}</Text>
                                          <Text
                                            type='tertiary'
                                            size='small'
                                            className='block mt-1'
                                          >
                                            {getOperationSummary(
                                              operation,
                                              index,
                                            )}
                                          </Text>
                                          {String(
                                            operation.description || '',
                                          ).trim() ? (
                                            <Text
                                              type='tertiary'
                                              size='small'
                                              className='block mt-1'
                                              style={{
                                                lineHeight: 1.5,
                                                wordBreak: 'break-word',
                                                overflow: 'hidden',
                                                display: '-webkit-box',
                                                WebkitLineClamp: 2,
                                                WebkitBoxOrient: 'vertical',
                                              }}
                                            >
                                              {operation.description}
                                            </Text>
                                          ) : null}
                                        </div>
                                      </div>
                                      <Tag size='small' color='grey'>
                                        {(operation.conditions || []).length}
                                      </Tag>
                                    </div>
                                    <Space spacing={6} style={{ marginTop: 6 }}>
                                      <Tag
                                        size='small'
                                        color={getOperationModeTagColor(
                                          operation.mode || 'set',
                                        )}
                                      >
                                        {OPERATION_MODE_LABEL_MAP[
                                          operation.mode || 'set'
                                        ] ||
                                          operation.mode ||
                                          'set'}
                                      </Tag>
                                      <Text type='tertiary' size='small'>
                                        {t('条件数')}
                                      </Text>
                                    </Space>
                                  </div>
                                );
                              })}
                            </div>
                          )}
                        </div>
                      </Card>
                    </Col>
                    <Col xs={24} md={16} style={{ order: isMobile ? 1 : 0 }}>
                      {selectedOperation ? (
                        (() => {
                          const mode = selectedOperation.mode || 'set';
                          const meta = MODE_META[mode] || MODE_META.set;
                          const conditions = selectedOperation.conditions || [];
                          const syncFromTarget =
                            mode === 'sync_fields'
                              ? parseSyncTargetSpec(selectedOperation.from)
                              : null;
                          const syncToTarget =
                            mode === 'sync_fields'
                              ? parseSyncTargetSpec(selectedOperation.to)
                              : null;
                          return (
                            <Card
                              className='!rounded-lg !border-0'
                              bodyStyle={{
                                padding: 14,
                                background: 'var(--semi-color-fill-0)',
                                minWidth: 0,
                              }}
                            >
                              <div className='flex items-center justify-between gap-2 mb-3 flex-wrap'>
                                <Space wrap spacing={8} className='min-w-0'>
                                  <Tag color='blue'>{`#${selectedOperationIndex + 1}`}</Tag>
                                  <Text
                                    strong
                                    ellipsis={{ showTooltip: true }}
                                    style={{ maxWidth: 520 }}
                                  >
                                    {getOperationSummary(
                                      selectedOperation,
                                      selectedOperationIndex,
                                    )}
                                  </Text>
                                </Space>
                                <Space spacing={6}>
                                  <Button
                                    size='small'
                                    type='tertiary'
                                    onClick={() =>
                                      duplicateOperation(selectedOperation.id)
                                    }
                                  >
                                    {t('复制')}
                                  </Button>
                                  <Button
                                    size='small'
                                    type='danger'
                                    theme='borderless'
                                    icon={<IconDelete />}
                                    aria-label={t('删除规则')}
                                    onClick={() =>
                                      removeOperation(selectedOperation.id)
                                    }
                                  />
                                </Space>
                              </div>

                              <Row gutter={12}>
                                <Col xs={24} md={8}>
                                  <Text type='tertiary' size='small'>
                                    {t('操作类型')}
                                  </Text>
                                  <Select
                                    value={mode}
                                    optionList={OPERATION_MODE_OPTIONS}
                                    onChange={(nextMode) =>
                                      updateOperation(selectedOperation.id, {
                                        mode: nextMode,
                                      })
                                    }
                                    style={{ width: '100%' }}
                                  />
                                </Col>
                                {meta.path || meta.pathOptional ? (
                                  <Col xs={24} md={16}>
                                    <Text type='tertiary' size='small'>
                                      {meta.pathOptional
                                        ? t('目标路径（可选）')
                                        : t(getModePathLabel(mode))}
                                    </Text>
                                    <Input
                                      value={selectedOperation.path}
                                      placeholder={getModePathPlaceholder(mode)}
                                      onChange={(nextValue) =>
                                        updateOperation(selectedOperation.id, {
                                          path: nextValue,
                                        })
                                      }
                                      name='components-table-channels-modals-paramoverrideeditormodal-input-2'
                                    />
                                  </Col>
                                ) : null}
                              </Row>

                              <Text
                                type='tertiary'
                                size='small'
                                className='mt-1 block'
                              >
                                {MODE_DESCRIPTIONS[mode] || ''}
                              </Text>
                              <div className='mt-2'>
                                <Text type='tertiary' size='small'>
                                  {t('规则描述（可选）')}
                                </Text>
                                <Input
                                  value={selectedOperation.description || ''}
                                  placeholder={t(
                                    '例如：清理工具参数，避免上游校验错误',
                                  )}
                                  onChange={(nextValue) =>
                                    updateOperation(selectedOperation.id, {
                                      description: nextValue || '',
                                    })
                                  }
                                  maxLength={180}
                                  showClear
                                  name='components-table-channels-modals-paramoverrideeditormodal-input-3'
                                />
                                <Text
                                  type='tertiary'
                                  size='small'
                                  className='mt-1 block'
                                >
                                  {`${String(selectedOperation.description || '').length}/180`}
                                </Text>
                              </div>

                              {meta.value ? (
                                mode === 'return_error' && returnErrorDraft ? (
                                  <div
                                    className='mt-2 rounded-xl p-3'
                                    style={{
                                      background: 'var(--semi-color-bg-1)',
                                      border:
                                        '1px solid var(--semi-color-border)',
                                    }}
                                  >
                                    <div className='flex items-center justify-between mb-2'>
                                      <Text strong>{t('自定义错误响应')}</Text>
                                      <Space spacing={6} align='center'>
                                        <Text type='tertiary' size='small'>
                                          {t('模式')}
                                        </Text>
                                        <Button
                                          size='small'
                                          type={
                                            returnErrorDraft.simpleMode
                                              ? 'primary'
                                              : 'tertiary'
                                          }
                                          onClick={() =>
                                            updateReturnErrorDraft(
                                              selectedOperation.id,
                                              { simpleMode: true },
                                            )
                                          }
                                        >
                                          {t('简洁')}
                                        </Button>
                                        <Button
                                          size='small'
                                          type={
                                            returnErrorDraft.simpleMode
                                              ? 'tertiary'
                                              : 'primary'
                                          }
                                          onClick={() =>
                                            updateReturnErrorDraft(
                                              selectedOperation.id,
                                              { simpleMode: false },
                                            )
                                          }
                                        >
                                          {t('高级')}
                                        </Button>
                                      </Space>
                                    </div>

                                    <Text type='tertiary' size='small'>
                                      {t('错误消息（必填）')}
                                    </Text>
                                    <TextArea
                                      value={returnErrorDraft.message}
                                      autosize={{ minRows: 2, maxRows: 4 }}
                                      placeholder={t(
                                        '例如：该请求不满足准入策略',
                                      )}
                                      onChange={(nextValue) =>
                                        updateReturnErrorDraft(
                                          selectedOperation.id,
                                          { message: nextValue },
                                        )
                                      }
                                      name='components-table-channels-modals-paramoverrideeditormodal-textarea-2'
                                    />

                                    {returnErrorDraft.simpleMode ? (
                                      <Text
                                        type='tertiary'
                                        size='small'
                                        className='mt-2 block'
                                      >
                                        {t(
                                          '简洁模式仅返回 message；状态码和错误类型将使用系统默认值。',
                                        )}
                                      </Text>
                                    ) : (
                                      <>
                                        <Row
                                          gutter={12}
                                          style={{ marginTop: 10 }}
                                        >
                                          <Col xs={24} md={8}>
                                            <Text type='tertiary' size='small'>
                                              {t('状态码')}
                                            </Text>
                                            <Input
                                              value={String(
                                                returnErrorDraft.statusCode ??
                                                  '',
                                              )}
                                              placeholder='400'
                                              onChange={(nextValue) =>
                                                updateReturnErrorDraft(
                                                  selectedOperation.id,
                                                  {
                                                    statusCode:
                                                      parseInt(nextValue, 10) ||
                                                      400,
                                                  },
                                                )
                                              }
                                              name='components-table-channels-modals-paramoverrideeditormodal-input-4'
                                            />
                                          </Col>
                                          <Col xs={24} md={8}>
                                            <Text type='tertiary' size='small'>
                                              {t('错误代码（可选）')}
                                            </Text>
                                            <Input
                                              value={returnErrorDraft.code}
                                              placeholder='forced_bad_request'
                                              onChange={(nextValue) =>
                                                updateReturnErrorDraft(
                                                  selectedOperation.id,
                                                  { code: nextValue },
                                                )
                                              }
                                              name='components-table-channels-modals-paramoverrideeditormodal-input-5'
                                            />
                                          </Col>
                                          <Col xs={24} md={8}>
                                            <Text type='tertiary' size='small'>
                                              {t('错误类型（可选）')}
                                            </Text>
                                            <Input
                                              value={returnErrorDraft.type}
                                              placeholder='invalid_request_error'
                                              onChange={(nextValue) =>
                                                updateReturnErrorDraft(
                                                  selectedOperation.id,
                                                  { type: nextValue },
                                                )
                                              }
                                              name='components-table-channels-modals-paramoverrideeditormodal-input-6'
                                            />
                                          </Col>
                                        </Row>
                                        <div className='mt-2 flex items-center gap-2'>
                                          <Text type='tertiary' size='small'>
                                            {t('重试建议')}
                                          </Text>
                                          <Button
                                            size='small'
                                            type={
                                              returnErrorDraft.skipRetry
                                                ? 'primary'
                                                : 'tertiary'
                                            }
                                            onClick={() =>
                                              updateReturnErrorDraft(
                                                selectedOperation.id,
                                                { skipRetry: true },
                                              )
                                            }
                                          >
                                            {t('停止重试')}
                                          </Button>
                                          <Button
                                            size='small'
                                            type={
                                              returnErrorDraft.skipRetry
                                                ? 'tertiary'
                                                : 'primary'
                                            }
                                            onClick={() =>
                                              updateReturnErrorDraft(
                                                selectedOperation.id,
                                                { skipRetry: false },
                                              )
                                            }
                                          >
                                            {t('允许重试')}
                                          </Button>
                                        </div>
                                        <Space wrap style={{ marginTop: 8 }}>
                                          <Tag
                                            size='small'
                                            color='grey'
                                            className='cursor-pointer'
                                            onClick={() =>
                                              updateReturnErrorDraft(
                                                selectedOperation.id,
                                                {
                                                  statusCode: 400,
                                                  code: 'invalid_request',
                                                  type: 'invalid_request_error',
                                                },
                                              )
                                            }
                                          >
                                            {t('参数错误')}
                                          </Tag>
                                          <Tag
                                            size='small'
                                            color='grey'
                                            className='cursor-pointer'
                                            onClick={() =>
                                              updateReturnErrorDraft(
                                                selectedOperation.id,
                                                {
                                                  statusCode: 401,
                                                  code: 'unauthorized',
                                                  type: 'authentication_error',
                                                },
                                              )
                                            }
                                          >
                                            {t('未授权')}
                                          </Tag>
                                          <Tag
                                            size='small'
                                            color='grey'
                                            className='cursor-pointer'
                                            onClick={() =>
                                              updateReturnErrorDraft(
                                                selectedOperation.id,
                                                {
                                                  statusCode: 429,
                                                  code: 'rate_limited',
                                                  type: 'rate_limit_error',
                                                },
                                              )
                                            }
                                          >
                                            {t('限流')}
                                          </Tag>
                                        </Space>
                                      </>
                                    )}
                                  </div>
                                ) : mode === 'prune_objects' &&
                                  pruneObjectsDraft ? (
                                  <div
                                    className='mt-2 rounded-xl p-3'
                                    style={{
                                      background: 'var(--semi-color-bg-1)',
                                      border:
                                        '1px solid var(--semi-color-border)',
                                    }}
                                  >
                                    <div className='flex items-center justify-between mb-2'>
                                      <Text strong>{t('对象清理规则')}</Text>
                                      <Space spacing={6} align='center'>
                                        <Text type='tertiary' size='small'>
                                          {t('模式')}
                                        </Text>
                                        <Button
                                          size='small'
                                          type={
                                            pruneObjectsDraft.simpleMode
                                              ? 'primary'
                                              : 'tertiary'
                                          }
                                          onClick={() =>
                                            updatePruneObjectsDraft(
                                              selectedOperation.id,
                                              { simpleMode: true },
                                            )
                                          }
                                        >
                                          {t('简洁')}
                                        </Button>
                                        <Button
                                          size='small'
                                          type={
                                            pruneObjectsDraft.simpleMode
                                              ? 'tertiary'
                                              : 'primary'
                                          }
                                          onClick={() =>
                                            updatePruneObjectsDraft(
                                              selectedOperation.id,
                                              { simpleMode: false },
                                            )
                                          }
                                        >
                                          {t('高级')}
                                        </Button>
                                      </Space>
                                    </div>

                                    <Text type='tertiary' size='small'>
                                      {t('类型（常用）')}
                                    </Text>
                                    <Input
                                      value={pruneObjectsDraft.typeText}
                                      placeholder='redacted_thinking'
                                      onChange={(nextValue) =>
                                        updatePruneObjectsDraft(
                                          selectedOperation.id,
                                          {
                                            simpleMode:
                                              pruneObjectsDraft.simpleMode,
                                            typeText: nextValue,
                                          },
                                        )
                                      }
                                      name='components-table-channels-modals-paramoverrideeditormodal-input-7'
                                    />

                                    {pruneObjectsDraft.simpleMode ? (
                                      <Text
                                        type='tertiary'
                                        size='small'
                                        className='mt-2 block'
                                      >
                                        {t(
                                          '简洁模式：按 type 全量清理对象，例如 redacted_thinking。',
                                        )}
                                      </Text>
                                    ) : (
                                      <Text
                                        type='tertiary'
                                        size='small'
                                        className='mt-2 block'
                                      >
                                        {t(
                                          '高级模式已启用，可配置递归策略、匹配逻辑和附加对象字段条件。',
                                        )}
                                      </Text>
                                    )}

                                    {!pruneObjectsDraft.simpleMode ? (
                                      <>
                                        <Row
                                          gutter={12}
                                          style={{ marginTop: 10 }}
                                        >
                                          <Col xs={24} md={12}>
                                            <Text type='tertiary' size='small'>
                                              {t('逻辑')}
                                            </Text>
                                            <Select
                                              value={pruneObjectsDraft.logic}
                                              optionList={[
                                                {
                                                  label: t('全部满足（AND）'),
                                                  value: 'AND',
                                                },
                                                {
                                                  label: t('任一满足（OR）'),
                                                  value: 'OR',
                                                },
                                              ]}
                                              style={{ width: '100%' }}
                                              onChange={(nextValue) =>
                                                updatePruneObjectsDraft(
                                                  selectedOperation.id,
                                                  {
                                                    simpleMode: false,
                                                    logic: nextValue || 'AND',
                                                  },
                                                )
                                              }
                                            />
                                          </Col>
                                          <Col xs={24} md={12}>
                                            <Text type='tertiary' size='small'>
                                              {t('递归策略')}
                                            </Text>
                                            <Space
                                              spacing={6}
                                              style={{ marginTop: 2 }}
                                            >
                                              <Button
                                                size='small'
                                                type={
                                                  pruneObjectsDraft.recursive
                                                    ? 'primary'
                                                    : 'tertiary'
                                                }
                                                onClick={() =>
                                                  updatePruneObjectsDraft(
                                                    selectedOperation.id,
                                                    {
                                                      simpleMode: false,
                                                      recursive: true,
                                                    },
                                                  )
                                                }
                                              >
                                                {t('递归')}
                                              </Button>
                                              <Button
                                                size='small'
                                                type={
                                                  pruneObjectsDraft.recursive
                                                    ? 'tertiary'
                                                    : 'primary'
                                                }
                                                onClick={() =>
                                                  updatePruneObjectsDraft(
                                                    selectedOperation.id,
                                                    {
                                                      simpleMode: false,
                                                      recursive: false,
                                                    },
                                                  )
                                                }
                                              >
                                                {t('仅当前层')}
                                              </Button>
                                            </Space>
                                          </Col>
                                        </Row>

                                        <div
                                          className='mt-2 rounded-lg p-2'
                                          style={{
                                            background:
                                              'var(--semi-color-fill-0)',
                                          }}
                                        >
                                          <div className='flex items-center justify-between mb-2'>
                                            <Text strong>{t('附加条件')}</Text>
                                            <Button
                                              size='small'
                                              icon={<IconPlus />}
                                              onClick={() =>
                                                addPruneRule(
                                                  selectedOperation.id,
                                                )
                                              }
                                            >
                                              {t('新增条件')}
                                            </Button>
                                          </div>
                                          {(pruneObjectsDraft.rules || [])
                                            .length === 0 ? (
                                            <Text type='tertiary' size='small'>
                                              {t(
                                                '未添加附加条件时，仅使用上方 type 进行清理。',
                                              )}
                                            </Text>
                                          ) : (
                                            <div className='flex flex-col gap-2'>
                                              {(
                                                pruneObjectsDraft.rules || []
                                              ).map((rule, ruleIndex) => (
                                                <div
                                                  key={rule.id}
                                                  className='rounded-lg p-2'
                                                  style={{
                                                    border:
                                                      '1px solid var(--semi-color-border)',
                                                    background:
                                                      'var(--semi-color-bg-0)',
                                                  }}
                                                >
                                                  <div className='flex items-center justify-between mb-2'>
                                                    <Tag size='small'>
                                                      {`R${ruleIndex + 1}`}
                                                    </Tag>
                                                    <Button
                                                      size='small'
                                                      type='danger'
                                                      theme='borderless'
                                                      icon={<IconDelete />}
                                                      onClick={() =>
                                                        removePruneRule(
                                                          selectedOperation.id,
                                                          rule.id,
                                                        )
                                                      }
                                                    >
                                                      {t('删除条件')}
                                                    </Button>
                                                  </div>
                                                  <Row gutter={8}>
                                                    <Col xs={24} md={9}>
                                                      <Text
                                                        type='tertiary'
                                                        size='small'
                                                      >
                                                        {t('字段路径')}
                                                      </Text>
                                                      <Input
                                                        value={rule.path}
                                                        placeholder='type'
                                                        onChange={(nextValue) =>
                                                          updatePruneRule(
                                                            selectedOperation.id,
                                                            rule.id,
                                                            { path: nextValue },
                                                          )
                                                        }
                                                        name='components-table-channels-modals-paramoverrideeditormodal-input-8'
                                                      />
                                                    </Col>
                                                    <Col xs={24} md={7}>
                                                      <Text
                                                        type='tertiary'
                                                        size='small'
                                                      >
                                                        {t('匹配方式')}
                                                      </Text>
                                                      <Select
                                                        value={rule.mode}
                                                        optionList={
                                                          CONDITION_MODE_OPTIONS
                                                        }
                                                        style={{
                                                          width: '100%',
                                                        }}
                                                        onChange={(nextValue) =>
                                                          updatePruneRule(
                                                            selectedOperation.id,
                                                            rule.id,
                                                            { mode: nextValue },
                                                          )
                                                        }
                                                      />
                                                    </Col>
                                                    <Col xs={24} md={8}>
                                                      <Text
                                                        type='tertiary'
                                                        size='small'
                                                      >
                                                        {t('匹配值（可选）')}
                                                      </Text>
                                                      <StructuredValueNodeEditor
                                                        t={t}
                                                        node={parseStructuredValueNodeForDisplay(
                                                          rule.value_text,
                                                        )}
                                                        sourceKey={
                                                          rule.value_text
                                                        }
                                                        placeholder='redacted_thinking'
                                                        onChange={(node) =>
                                                          updatePruneRule(
                                                            selectedOperation.id,
                                                            rule.id,
                                                            {
                                                              value_text:
                                                                buildStructuredValueText(
                                                                  node,
                                                                ),
                                                            },
                                                          )
                                                        }
                                                      />
                                                    </Col>
                                                  </Row>
                                                  <Space
                                                    wrap
                                                    spacing={8}
                                                    style={{ marginTop: 8 }}
                                                  >
                                                    <Button
                                                      size='small'
                                                      type={
                                                        rule.invert
                                                          ? 'primary'
                                                          : 'tertiary'
                                                      }
                                                      onClick={() =>
                                                        updatePruneRule(
                                                          selectedOperation.id,
                                                          rule.id,
                                                          {
                                                            invert:
                                                              !rule.invert,
                                                          },
                                                        )
                                                      }
                                                    >
                                                      {t('条件取反')}
                                                    </Button>
                                                    <Button
                                                      size='small'
                                                      type={
                                                        rule.pass_missing_key
                                                          ? 'primary'
                                                          : 'tertiary'
                                                      }
                                                      onClick={() =>
                                                        updatePruneRule(
                                                          selectedOperation.id,
                                                          rule.id,
                                                          {
                                                            pass_missing_key:
                                                              !rule.pass_missing_key,
                                                          },
                                                        )
                                                      }
                                                    >
                                                      {t('字段缺失视为命中')}
                                                    </Button>
                                                  </Space>
                                                </div>
                                              ))}
                                            </div>
                                          )}
                                        </div>
                                      </>
                                    ) : null}
                                  </div>
                                ) : mode === 'pass_headers' ? (
                                  <PassHeadersEditor
                                    t={t}
                                    operationId={selectedOperation.id}
                                    valueText={selectedOperation.value_text}
                                    updateOperation={updateOperation}
                                  />
                                ) : mode === 'set_header' ? (
                                  <HeaderValueEditor
                                    t={t}
                                    operationId={selectedOperation.id}
                                    valueText={selectedOperation.value_text}
                                    updateOperation={updateOperation}
                                    onShowExample={() =>
                                      setHeaderValueExampleVisible(true)
                                    }
                                  />
                                ) : (
                                  <div className='mt-2'>
                                    <StructuredValueEditor
                                      t={t}
                                      operationId={selectedOperation.id}
                                      label={getModeValueLabel(mode)}
                                      valueText={selectedOperation.value_text}
                                      placeholder={getModeValuePlaceholder(
                                        mode,
                                      )}
                                      updateOperation={updateOperation}
                                    />
                                  </div>
                                )
                              ) : null}

                              {meta.keepOrigin ? (
                                <div className='mt-2 flex items-center gap-2'>
                                  <Switch
                                    checked={Boolean(
                                      selectedOperation.keep_origin,
                                    )}
                                    checkedText={t('开')}
                                    uncheckedText={t('关')}
                                    onChange={(nextValue) =>
                                      updateOperation(selectedOperation.id, {
                                        keep_origin: nextValue,
                                      })
                                    }
                                    id='components-table-channels-modals-paramoverrideeditormodal-switch-1'
                                  />
                                  <Text
                                    type='tertiary'
                                    size='small'
                                    className='leading-6'
                                  >
                                    {t('保留原值（目标已有值时不覆盖）')}
                                  </Text>
                                </div>
                              ) : null}

                              {mode === 'sync_fields' ? (
                                <div className='mt-2'>
                                  <Text type='tertiary' size='small'>
                                    {t('同步端点')}
                                  </Text>
                                  <Row gutter={12} style={{ marginTop: 6 }}>
                                    <Col xs={24} md={12}>
                                      <Text type='tertiary' size='small'>
                                        {t('来源端点')}
                                      </Text>
                                      <div className='flex gap-2'>
                                        <Select
                                          value={syncFromTarget?.type || 'json'}
                                          optionList={SYNC_TARGET_TYPE_OPTIONS}
                                          style={{ width: 120 }}
                                          onChange={(nextType) =>
                                            updateOperation(
                                              selectedOperation.id,
                                              {
                                                from: buildSyncTargetSpec(
                                                  nextType,
                                                  syncFromTarget?.key || '',
                                                ),
                                              },
                                            )
                                          }
                                        />
                                        <Input
                                          value={syncFromTarget?.key || ''}
                                          placeholder='session_id'
                                          onChange={(nextKey) =>
                                            updateOperation(
                                              selectedOperation.id,
                                              {
                                                from: buildSyncTargetSpec(
                                                  syncFromTarget?.type ||
                                                    'json',
                                                  nextKey,
                                                ),
                                              },
                                            )
                                          }
                                          name='components-table-channels-modals-paramoverrideeditormodal-input-10'
                                        />
                                      </div>
                                    </Col>
                                    <Col xs={24} md={12}>
                                      <Text type='tertiary' size='small'>
                                        {t('目标端点')}
                                      </Text>
                                      <div className='flex gap-2'>
                                        <Select
                                          value={syncToTarget?.type || 'json'}
                                          optionList={SYNC_TARGET_TYPE_OPTIONS}
                                          style={{ width: 120 }}
                                          onChange={(nextType) =>
                                            updateOperation(
                                              selectedOperation.id,
                                              {
                                                to: buildSyncTargetSpec(
                                                  nextType,
                                                  syncToTarget?.key || '',
                                                ),
                                              },
                                            )
                                          }
                                        />
                                        <Input
                                          value={syncToTarget?.key || ''}
                                          placeholder='prompt_cache_key'
                                          onChange={(nextKey) =>
                                            updateOperation(
                                              selectedOperation.id,
                                              {
                                                to: buildSyncTargetSpec(
                                                  syncToTarget?.type || 'json',
                                                  nextKey,
                                                ),
                                              },
                                            )
                                          }
                                          name='components-table-channels-modals-paramoverrideeditormodal-input-11'
                                        />
                                      </div>
                                    </Col>
                                  </Row>
                                  <Space wrap style={{ marginTop: 8 }}>
                                    <Tag
                                      size='small'
                                      color='cyan'
                                      className='cursor-pointer'
                                      onClick={() =>
                                        updateOperation(selectedOperation.id, {
                                          from: 'header:session_id',
                                          to: 'json:prompt_cache_key',
                                        })
                                      }
                                    >
                                      {
                                        'header:session_id -> json:prompt_cache_key'
                                      }
                                    </Tag>
                                    <Tag
                                      size='small'
                                      color='cyan'
                                      className='cursor-pointer'
                                      onClick={() =>
                                        updateOperation(selectedOperation.id, {
                                          from: 'json:prompt_cache_key',
                                          to: 'header:session_id',
                                        })
                                      }
                                    >
                                      {
                                        'json:prompt_cache_key -> header:session_id'
                                      }
                                    </Tag>
                                  </Space>
                                </div>
                              ) : meta.from || meta.to === false || meta.to ? (
                                <Row gutter={12} style={{ marginTop: 8 }}>
                                  {meta.from || meta.to === false ? (
                                    <Col xs={24} md={12}>
                                      <Text type='tertiary' size='small'>
                                        {t(getModeFromLabel(mode))}
                                      </Text>
                                      <Input
                                        value={selectedOperation.from}
                                        placeholder={getModeFromPlaceholder(
                                          mode,
                                        )}
                                        onChange={(nextValue) =>
                                          updateOperation(
                                            selectedOperation.id,
                                            {
                                              from: nextValue,
                                            },
                                          )
                                        }
                                        name='components-table-channels-modals-paramoverrideeditormodal-input-12'
                                      />
                                    </Col>
                                  ) : null}
                                  {meta.to || meta.to === false ? (
                                    <Col xs={24} md={12}>
                                      <Text type='tertiary' size='small'>
                                        {t(getModeToLabel(mode))}
                                      </Text>
                                      <Input
                                        value={selectedOperation.to}
                                        placeholder={getModeToPlaceholder(mode)}
                                        onChange={(nextValue) =>
                                          updateOperation(
                                            selectedOperation.id,
                                            {
                                              to: nextValue,
                                            },
                                          )
                                        }
                                        name='components-table-channels-modals-paramoverrideeditormodal-input-13'
                                      />
                                    </Col>
                                  ) : null}
                                </Row>
                              ) : null}

                              <div
                                className='mt-3 rounded-lg p-3'
                                style={{
                                  background: 'rgba(127, 127, 127, 0.08)',
                                }}
                              >
                                <div className='flex items-center justify-between mb-2'>
                                  <Space align='center'>
                                    <Text>{t('条件规则')}</Text>
                                    <Select
                                      value={selectedOperation.logic || 'OR'}
                                      optionList={[
                                        {
                                          label: t('满足任一条件（OR）'),
                                          value: 'OR',
                                        },
                                        {
                                          label: t('必须全部满足（AND）'),
                                          value: 'AND',
                                        },
                                      ]}
                                      size='small'
                                      style={{ width: 180 }}
                                      onChange={(nextValue) =>
                                        updateOperation(selectedOperation.id, {
                                          logic: nextValue,
                                        })
                                      }
                                    />
                                  </Space>
                                  <Space spacing={6}>
                                    <Button
                                      size='small'
                                      type='tertiary'
                                      onClick={expandAllSelectedConditions}
                                    >
                                      {t('全部展开')}
                                    </Button>
                                    <Button
                                      size='small'
                                      type='tertiary'
                                      onClick={collapseAllSelectedConditions}
                                    >
                                      {t('全部收起')}
                                    </Button>
                                    <Button
                                      icon={<IconPlus />}
                                      size='small'
                                      onClick={() =>
                                        addCondition(selectedOperation.id)
                                      }
                                    >
                                      {t('新增条件')}
                                    </Button>
                                  </Space>
                                </div>

                                {conditions.length === 0 ? (
                                  <Text type='tertiary' size='small'>
                                    {t('没有条件时，默认总是执行该操作。')}
                                  </Text>
                                ) : (
                                  <Collapse
                                    keepDOM
                                    activeKey={selectedConditionKeys}
                                    onChange={(activeKeys) =>
                                      handleConditionCollapseChange(
                                        selectedOperation.id,
                                        activeKeys,
                                      )
                                    }
                                  >
                                    {conditions.map(
                                      (condition, conditionIndex) => (
                                        <Collapse.Panel
                                          key={condition.id}
                                          itemKey={condition.id}
                                          header={
                                            <Space spacing={8}>
                                              <Tag size='small'>
                                                {`C${conditionIndex + 1}`}
                                              </Tag>
                                              <Text
                                                type='tertiary'
                                                size='small'
                                              >
                                                {condition.path ||
                                                  t('未设置路径')}
                                              </Text>
                                            </Space>
                                          }
                                        >
                                          <div>
                                            <div className='flex items-center justify-between mb-2'>
                                              <Text
                                                type='tertiary'
                                                size='small'
                                              >
                                                {t('条件项设置')}
                                              </Text>
                                              <Button
                                                theme='borderless'
                                                type='danger'
                                                icon={<IconDelete />}
                                                size='small'
                                                onClick={() =>
                                                  removeCondition(
                                                    selectedOperation.id,
                                                    condition.id,
                                                  )
                                                }
                                              >
                                                {t('删除条件')}
                                              </Button>
                                            </div>
                                            <Row gutter={12}>
                                              <Col xs={24} md={10}>
                                                <Text
                                                  type='tertiary'
                                                  size='small'
                                                >
                                                  {t('字段路径')}
                                                </Text>
                                                <Input
                                                  value={condition.path}
                                                  placeholder='model'
                                                  onChange={(nextValue) =>
                                                    updateCondition(
                                                      selectedOperation.id,
                                                      condition.id,
                                                      { path: nextValue },
                                                    )
                                                  }
                                                  name='components-table-channels-modals-paramoverrideeditormodal-input-14'
                                                />
                                              </Col>
                                              <Col xs={24} md={8}>
                                                <Text
                                                  type='tertiary'
                                                  size='small'
                                                >
                                                  {t('匹配方式')}
                                                </Text>
                                                <Select
                                                  value={condition.mode}
                                                  optionList={
                                                    CONDITION_MODE_OPTIONS
                                                  }
                                                  onChange={(nextValue) =>
                                                    updateCondition(
                                                      selectedOperation.id,
                                                      condition.id,
                                                      { mode: nextValue },
                                                    )
                                                  }
                                                  style={{ width: '100%' }}
                                                />
                                              </Col>
                                              <Col xs={24} md={6}>
                                                <Text
                                                  type='tertiary'
                                                  size='small'
                                                >
                                                  {t('匹配值')}
                                                </Text>
                                                <StructuredValueNodeEditor
                                                  t={t}
                                                  node={parseStructuredValueNodeForDisplay(
                                                    condition.value_text,
                                                  )}
                                                  sourceKey={
                                                    condition.value_text
                                                  }
                                                  placeholder='gpt'
                                                  onChange={(node) =>
                                                    updateCondition(
                                                      selectedOperation.id,
                                                      condition.id,
                                                      {
                                                        value_text:
                                                          buildStructuredValueText(
                                                            node,
                                                          ),
                                                      },
                                                    )
                                                  }
                                                />
                                              </Col>
                                            </Row>
                                            <div className='mt-2 flex flex-wrap gap-3'>
                                              <div className='flex items-center gap-2'>
                                                <Text
                                                  type='tertiary'
                                                  size='small'
                                                >
                                                  {t('条件取反')}
                                                </Text>
                                                <Switch
                                                  checked={Boolean(
                                                    condition.invert,
                                                  )}
                                                  checkedText={t('开')}
                                                  uncheckedText={t('关')}
                                                  onChange={(nextValue) =>
                                                    updateCondition(
                                                      selectedOperation.id,
                                                      condition.id,
                                                      { invert: nextValue },
                                                    )
                                                  }
                                                  id={`components-table-channels-modals-paramoverrideeditormodal-switch-2-${conditionIndex}`}
                                                />
                                              </div>
                                              <div className='flex items-center gap-2'>
                                                <Text
                                                  type='tertiary'
                                                  size='small'
                                                >
                                                  {t('字段缺失视为命中')}
                                                </Text>
                                                <Switch
                                                  checked={Boolean(
                                                    condition.pass_missing_key,
                                                  )}
                                                  checkedText={t('开')}
                                                  uncheckedText={t('关')}
                                                  onChange={(nextValue) =>
                                                    updateCondition(
                                                      selectedOperation.id,
                                                      condition.id,
                                                      {
                                                        pass_missing_key:
                                                          nextValue,
                                                      },
                                                    )
                                                  }
                                                  id={`components-table-channels-modals-paramoverrideeditormodal-switch-3-${conditionIndex}`}
                                                />
                                              </div>
                                            </div>
                                          </div>
                                        </Collapse.Panel>
                                      ),
                                    )}
                                  </Collapse>
                                )}
                              </div>
                            </Card>
                          );
                        })()
                      ) : (
                        <Card
                          className='!rounded-lg !border-0'
                          bodyStyle={{
                            padding: 14,
                            background: 'var(--semi-color-fill-0)',
                          }}
                        >
                          <Text type='tertiary'>
                            {t('请选择一条规则进行编辑。')}
                          </Text>
                        </Card>
                      )}

                      {visualValidationError ? (
                        <Card
                          className='!rounded-lg !border-0 mt-3'
                          bodyStyle={{
                            padding: 12,
                            background: 'var(--semi-color-fill-0)',
                          }}
                        >
                          <Space>
                            <Tag color='red'>{t('暂存错误')}</Tag>
                            <Text type='danger'>{visualValidationError}</Text>
                          </Space>
                        </Card>
                      ) : null}
                    </Col>
                  </Row>
                </div>
              )}
            </div>
          ) : (
            <div className='min-w-0' style={{ width: '100%' }}>
              <Space style={{ marginBottom: 8 }} wrap>
                <Button onClick={formatJson}>{t('格式化')}</Button>
                <Tag color='grey'>{t('高级文本编辑')}</Tag>
              </Space>
              <TextArea
                value={jsonText}
                autosize={{ minRows: 18, maxRows: 28 }}
                onChange={(nextValue) => handleJsonChange(nextValue ?? '')}
                placeholder={JSON.stringify(OPERATION_TEMPLATE, null, 2)}
                showClear
                name='components-table-channels-modals-paramoverrideeditormodal-textarea-4'
              />
              <Text type='tertiary' size='small' className='mt-2 block'>
                {t('直接编辑 JSON 文本，保存时会校验格式。')}
              </Text>
              {jsonError ? (
                <Text className='text-red-500 text-xs mt-2'>{jsonError}</Text>
              ) : null}
            </div>
          )}
        </div>
      </Modal>

      <Modal
        title={t('anthropic-beta JSON 示例')}
        visible={headerValueExampleVisible}
        width='min(760px, calc(100vw - 24px))'
        footer={null}
        onCancel={() => setHeaderValueExampleVisible(false)}
        bodyStyle={{ padding: 16, paddingBottom: 24 }}
      >
        <Space vertical align='start' spacing={12} style={{ width: '100%' }}>
          <Text type='tertiary' size='small'>
            {t('下面是带注释的示例，仅用于参考；实际保存时请删除注释。')}
          </Text>
          <TextArea
            value={HEADER_VALUE_JSONC_EXAMPLE}
            readOnly
            autosize={{ minRows: 16, maxRows: 20 }}
            style={{ marginBottom: 8 }}
            name='components-table-channels-modals-paramoverrideeditormodal-textarea-5'
          />
        </Space>
      </Modal>

      <Modal
        title={null}
        visible={fieldGuideVisible}
        width='min(860px, calc(100vw - 24px))'
        footer={null}
        onCancel={() => setFieldGuideVisible(false)}
        bodyStyle={{
          maxHeight: '72vh',
          overflowY: 'auto',
          padding: 16,
          background: 'var(--semi-color-bg-0)',
        }}
      >
        <Space vertical spacing={12} style={{ width: '100%' }}>
          <div className='flex items-start justify-between gap-3'>
            <div>
              <Text strong style={{ fontSize: 22, lineHeight: '30px' }}>
                {t('字段速查')}
              </Text>
              <Text
                type='tertiary'
                size='small'
                className='block mt-1'
                style={{ maxWidth: 560 }}
              >
                {t(
                  '先搜索，再一键复制字段名或填入当前规则。字段名为系统内部路径，可直接用于路径 / 来源 / 目标。',
                )}
              </Text>
            </div>
            <Tag color='blue'>{`${fieldGuideFieldCount} ${t('个字段')}`}</Tag>
          </div>

          <Card
            className='!rounded-xl !border-0'
            bodyStyle={{
              padding: 12,
              background: 'var(--semi-color-fill-0)',
            }}
          >
            <div className='flex items-center gap-2'>
              <Input
                value={fieldGuideKeyword}
                onChange={(nextValue) => setFieldGuideKeyword(nextValue || '')}
                placeholder={t('搜索字段名 / 中文说明')}
                showClear
                style={{ flex: 1 }}
                name='components-table-channels-modals-paramoverrideeditormodal-input-16'
              />
              <Select
                value={fieldGuideTarget}
                optionList={FIELD_GUIDE_TARGET_OPTIONS}
                onChange={(nextValue) =>
                  setFieldGuideTarget(nextValue || 'path')
                }
                style={{ width: 170 }}
              />
            </div>
          </Card>

          {filteredFieldGuideSections.length === 0 ? (
            <Card
              className='!rounded-xl !border-0'
              bodyStyle={{
                padding: 20,
                background: 'var(--semi-color-fill-0)',
              }}
            >
              <Text type='tertiary'>{t('没有匹配的字段')}</Text>
            </Card>
          ) : (
            <div className='flex flex-col gap-2'>
              {filteredFieldGuideSections.map((section) => (
                <Card
                  key={section.title}
                  className='!rounded-xl !border-0'
                  bodyStyle={{
                    padding: 14,
                    background: 'var(--semi-color-fill-0)',
                  }}
                >
                  <div className='flex items-center justify-between mb-1'>
                    <Text strong style={{ fontSize: 18 }}>
                      {section.title}
                    </Text>
                    <Tag color='grey'>{`${section.fields.length} ${t('项')}`}</Tag>
                  </div>
                  <div
                    style={{
                      display: 'flex',
                      flexDirection: 'column',
                      marginTop: 6,
                    }}
                  >
                    {section.fields.map((field, index) => (
                      <div
                        key={field.key}
                        className='flex items-start justify-between gap-3'
                        style={{
                          paddingTop: 10,
                          paddingBottom: 10,
                          borderTop:
                            index === 0
                              ? 'none'
                              : '1px solid var(--semi-color-border)',
                        }}
                      >
                        <div style={{ flex: 1, minWidth: 0 }}>
                          <Text strong>{field.label}</Text>
                          <Text
                            type='secondary'
                            size='small'
                            className='block mt-1 font-mono'
                            style={{
                              background: 'var(--semi-color-bg-1)',
                              border: '1px solid var(--semi-color-border)',
                              borderRadius: 8,
                              padding: '4px 8px',
                              width: 'fit-content',
                            }}
                          >
                            {field.key}
                          </Text>
                          <Text
                            type='tertiary'
                            size='small'
                            className='block mt-1'
                            style={{ lineHeight: '18px' }}
                          >
                            {field.tip}
                          </Text>
                        </div>
                        <Space spacing={6} align='center'>
                          <Button
                            size='small'
                            type='tertiary'
                            onClick={() => copyBuiltinField(field.key)}
                          >
                            {t('复制')}
                          </Button>
                          <Button
                            size='small'
                            onClick={() =>
                              applyBuiltinField(field.key, fieldGuideTarget)
                            }
                          >
                            {fieldGuideActionLabel}
                          </Button>
                        </Space>
                      </div>
                    ))}
                  </div>
                </Card>
              ))}
            </div>
          )}
        </Space>
      </Modal>
    </>
  );
};

const LegacyOverrideEditor = ({
  t,
  entries,
  addEntry,
  updateEntry,
  removeEntry,
  compact = false,
}) => (
  <Card
    className='!rounded-lg !border-0'
    bodyStyle={{
      padding: compact ? 10 : 14,
      background: 'var(--semi-color-fill-0)',
    }}
  >
    <div className='mb-3 flex items-center justify-between gap-2 flex-wrap'>
      <div>
        <Text strong>{compact ? t('顶层字段覆盖') : t('旧格式字段覆盖')}</Text>
        <Text type='tertiary' size='small' className='block mt-1'>
          {t(
            compact
              ? '这些字段会和 operations 保存在同一个 param_override 对象中。'
              : '逐项设置顶层请求字段和值，保存后仍生成旧格式 JSON 对象。',
          )}
        </Text>
      </div>
      <Button size='small' icon={<IconPlus />} onClick={addEntry}>
        {t('新增字段')}
      </Button>
    </div>
    <div className='flex flex-col gap-3'>
      {entries.map((entry, index) => (
        <div
          key={entry.id}
          className='rounded-lg p-3'
          style={{
            background: 'var(--semi-color-bg-1)',
            border: '1px solid var(--semi-color-border)',
          }}
        >
          <div className='mb-2 flex items-center justify-between gap-2'>
            <Tag size='small'>{`#${index + 1}`}</Tag>
            <Button
              size='small'
              type='danger'
              theme='borderless'
              icon={<IconDelete />}
              onClick={() => removeEntry(entry.id)}
            >
              {t('删除字段')}
            </Button>
          </div>
          <Row gutter={12}>
            <Col xs={24} md={compact ? 24 : 8}>
              <Text type='tertiary' size='small'>
                {t('字段名')}
              </Text>
              <Input
                value={entry.key}
                placeholder={FIELD_NAME_PLACEHOLDER}
                onChange={(nextValue) =>
                  updateEntry(entry.id, { key: nextValue || '' })
                }
              />
            </Col>
            <Col xs={24} md={compact ? 24 : 16}>
              <Text type='tertiary' size='small'>
                {t('字段值')}
              </Text>
              <StructuredValueNodeEditor
                t={t}
                node={parseStructuredValueNodeForDisplay(entry.value_text)}
                sourceKey={entry.value_text}
                placeholder='0.7'
                onChange={(node) =>
                  updateEntry(entry.id, {
                    value_text: buildStructuredValueText(node),
                  })
                }
              />
            </Col>
          </Row>
        </div>
      ))}
    </div>
  </Card>
);

const PassHeadersEditor = ({ t, operationId, valueText, updateOperation }) => {
  const draft = useMemo(() => parsePassHeadersDraft(valueText), [valueText]);
  const headers = draft.headers;
  const [headerRows, setHeaderRows] = useState(() =>
    headers.map((header) => ({ id: nextLocalId(), value: header })),
  );

  useEffect(() => {
    setHeaderRows((currentRows) => {
      const nextRows = [];
      const usedRowIds = new Set();
      headers.forEach((header) => {
        const existingRow = currentRows.find(
          (row) => !usedRowIds.has(row.id) && row.value === header,
        );
        if (existingRow) {
          usedRowIds.add(existingRow.id);
          nextRows.push(existingRow);
        } else {
          nextRows.push({ id: nextLocalId(), value: header });
        }
      });
      return nextRows;
    });
  }, [headers]);

  const sourceShapeOptions = useMemo(
    () => [
      { label: t('数组'), value: 'headers' },
      { label: t('对象名称'), value: 'names' },
      { label: t('单个请求头'), value: 'header' },
    ],
    [t],
  );

  const commitDraft = useCallback(
    (nextDraft) => {
      updateOperation(operationId, {
        value_text: buildPassHeadersValueText(nextDraft),
      });
    },
    [operationId, updateOperation],
  );

  const commitHeaders = useCallback(
    (nextHeaders) =>
      commitDraft({
        ...draft,
        headers:
          draft.sourceKey === 'header' ? nextHeaders.slice(0, 1) : nextHeaders,
      }),
    [commitDraft, draft],
  );

  return (
    <div
      className='mt-2 rounded-xl p-3'
      style={{
        background: 'var(--semi-color-bg-1)',
        border: '1px solid var(--semi-color-border)',
      }}
    >
      <div className='flex items-start justify-between gap-2 mb-2'>
        <div>
          <Text strong>{t('透传请求头')}</Text>
          <Text type='tertiary' size='small' className='block mt-1'>
            {t(
              '只会透传客户端原始请求中实际存在的同名请求头，不会生成固定请求头。',
            )}
          </Text>
        </div>
        <Button
          size='small'
          icon={<IconPlus />}
          disabled={draft.sourceKey === 'header' && headers.length > 0}
          onClick={() =>
            commitHeaders(
              draft.sourceKey === 'header' && headers.length > 0
                ? headers
                : Array.from(new Set([...headers, 'X-Header-Name'])),
            )
          }
        >
          {t('新增请求头')}
        </Button>
      </div>
      <Row gutter={8} style={{ marginBottom: 8 }}>
        <Col xs={24} md={8}>
          <Text type='tertiary' size='small'>
            {t('保存形态')}
          </Text>
          <Select
            value={draft.sourceKey}
            optionList={sourceShapeOptions}
            style={{ width: '100%' }}
            onChange={(nextValue) =>
              commitDraft({
                ...draft,
                sourceKey: nextValue || 'headers',
                headers:
                  nextValue === 'header'
                    ? draft.headers.slice(0, 1)
                    : draft.headers,
              })
            }
          />
        </Col>
        <Col xs={24} md={16}>
          <Text type='tertiary' size='small' className='block mt-6'>
            {t('用于兼容已存在的 pass_headers value 格式。')}
          </Text>
        </Col>
      </Row>
      {headerRows.length === 0 ? (
        <Text type='tertiary' size='small'>
          {t('未配置透传请求头。')}
        </Text>
      ) : (
        <div className='flex flex-col gap-2'>
          {headerRows.map((headerRow, index) => (
            <div
              key={headerRow.id}
              className='grid gap-2'
              style={{ gridTemplateColumns: '1fr auto' }}
            >
              <Input
                value={headerRow.value}
                placeholder={HEADER_NAME_PLACEHOLDER}
                onChange={(nextValue) => {
                  const nextHeaders = [...headers];
                  nextHeaders[index] = nextValue || '';
                  commitHeaders(nextHeaders);
                }}
              />
              <Button
                type='danger'
                theme='borderless'
                icon={<IconDelete />}
                aria-label={t('删除请求头')}
                onClick={() =>
                  commitHeaders(
                    headers.filter((_, itemIndex) => itemIndex !== index),
                  )
                }
              />
            </div>
          ))}
        </div>
      )}
      <Space wrap spacing={6} style={{ marginTop: 8 }}>
        {[
          'User-Agent',
          'Session_id',
          'X-Client-Request-Id',
          'X-Codex-Turn-Metadata',
          'Anthropic-Beta',
          'X-Stainless-Runtime',
        ].map((header) => (
          <Tag
            key={header}
            size='small'
            color='cyan'
            className='cursor-pointer'
            onClick={() =>
              commitHeaders(
                draft.sourceKey === 'header'
                  ? [header]
                  : Array.from(new Set([...headers, header])),
              )
            }
          >
            {header}
          </Tag>
        ))}
      </Space>
    </div>
  );
};

const HeaderValueEditor = ({
  t,
  operationId,
  valueText,
  updateOperation,
  onShowExample,
}) => {
  const draft = useMemo(() => parseHeaderValueDraft(valueText), [valueText]);

  const updateDraft = useCallback(
    (patch) => {
      const nextDraft = { ...draft, ...patch };
      updateOperation(operationId, {
        value_text: buildHeaderValueText(nextDraft),
      });
    },
    [draft, operationId, updateOperation],
  );

  const updateRow = useCallback(
    (rowId, patch) => {
      updateDraft({
        rows: draft.rows.map((row) =>
          row.id === rowId ? { ...row, ...patch } : row,
        ),
      });
    },
    [draft.rows, updateDraft],
  );

  return (
    <div
      className='mt-2 rounded-xl p-3'
      style={{
        background: 'var(--semi-color-bg-1)',
        border: '1px solid var(--semi-color-border)',
      }}
    >
      <div className='flex items-start justify-between gap-2 mb-2 flex-wrap'>
        <div>
          <Text strong>{t('请求头值')}</Text>
          <Text type='tertiary' size='small' className='block mt-1'>
            {t(
              '整值模式会覆盖整条请求头；Token 映射模式用于处理逗号分隔的 beta / feature token。',
            )}
          </Text>
        </div>
        <Space spacing={6}>
          <Select
            value={draft.mode}
            optionList={HEADER_VALUE_MODE_OPTIONS}
            onChange={(nextValue) =>
              updateDraft({ mode: nextValue || 'direct' })
            }
            style={{ width: 160 }}
          />
          <Button size='small' type='tertiary' onClick={onShowExample}>
            {t('查看示例')}
          </Button>
        </Space>
      </div>

      {draft.mode === 'direct' ? (
        <Input
          value={draft.directText}
          placeholder={HEADER_DIRECT_VALUE_PLACEHOLDER}
          onChange={(nextValue) => updateDraft({ directText: nextValue || '' })}
        />
      ) : (
        <div className='flex flex-col gap-3'>
          <div className='flex items-center gap-2'>
            <Switch
              checked={Boolean(draft.keepOnlyDeclared)}
              checkedText={t('开')}
              uncheckedText={t('关')}
              onChange={(nextValue) =>
                updateDraft({ keepOnlyDeclared: nextValue })
              }
              id='components-table-channels-modals-paramoverrideeditormodal-header-keep-only'
            />
            <Text type='tertiary' size='small'>
              {t('只保留已声明的 token')}
            </Text>
          </div>
          <div>
            <Text type='tertiary' size='small'>
              {t('追加 token')}
            </Text>
            <Input
              value={draft.appendText}
              placeholder={HEADER_APPEND_TOKENS_PLACEHOLDER}
              onChange={(nextValue) => updateDraft({ appendText: nextValue })}
            />
          </div>
          <Row gutter={8}>
            <Col xs={24} md={9}>
              <Text type='tertiary' size='small'>
                {t('通配规则')}
              </Text>
              <Select
                value={draft.wildcardAction}
                optionList={[
                  { label: t('不处理未声明 token'), value: 'none' },
                  { label: t('替换未声明 token'), value: 'replace' },
                  { label: t('删除未声明 token'), value: 'delete' },
                ]}
                onChange={(nextValue) =>
                  updateDraft({ wildcardAction: nextValue || 'none' })
                }
                style={{ width: '100%' }}
              />
            </Col>
            <Col xs={24} md={15}>
              <Text type='tertiary' size='small'>
                {t('通配替换为')}
              </Text>
              <Input
                value={draft.wildcardReplacement}
                disabled={draft.wildcardAction !== 'replace'}
                placeholder={HEADER_REPLACEMENT_PLACEHOLDER}
                onChange={(nextValue) =>
                  updateDraft({ wildcardReplacement: nextValue || '' })
                }
              />
            </Col>
          </Row>
          <div>
            <div className='flex items-center justify-between mb-2'>
              <Text strong>{t('Token 规则')}</Text>
              <Button
                size='small'
                icon={<IconPlus />}
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
                {t('新增规则')}
              </Button>
            </div>
            {draft.rows.length === 0 ? (
              <Text type='tertiary' size='small'>
                {t('未配置 token 规则。')}
              </Text>
            ) : (
              <div className='flex flex-col gap-2'>
                {draft.rows.map((row) => (
                  <div
                    key={row.id}
                    className='rounded-lg p-2'
                    style={{
                      border: '1px solid var(--semi-color-border)',
                      background: 'var(--semi-color-bg-0)',
                    }}
                  >
                    <Row gutter={8}>
                      <Col xs={24} md={9}>
                        <Text type='tertiary' size='small'>
                          {t('原 token')}
                        </Text>
                        <Input
                          value={row.token}
                          placeholder={HEADER_TOKEN_PLACEHOLDER}
                          onChange={(nextValue) =>
                            updateRow(row.id, { token: nextValue || '' })
                          }
                        />
                      </Col>
                      <Col xs={24} md={5}>
                        <Text type='tertiary' size='small'>
                          {t('动作')}
                        </Text>
                        <Select
                          value={row.action}
                          optionList={HEADER_TOKEN_ACTION_OPTIONS}
                          onChange={(nextValue) =>
                            updateRow(row.id, {
                              action: nextValue || 'replace',
                            })
                          }
                          style={{ width: '100%' }}
                        />
                      </Col>
                      <Col xs={24} md={8}>
                        <Text type='tertiary' size='small'>
                          {t('替换为')}
                        </Text>
                        <Input
                          value={row.replacement}
                          disabled={row.action !== 'replace'}
                          placeholder={HEADER_REPLACEMENT_PLACEHOLDER}
                          onChange={(nextValue) =>
                            updateRow(row.id, {
                              replacement: nextValue || '',
                            })
                          }
                        />
                      </Col>
                      <Col xs={24} md={2}>
                        <Button
                          type='danger'
                          theme='borderless'
                          icon={<IconDelete />}
                          aria-label={t('删除规则')}
                          style={{ marginTop: 20 }}
                          onClick={() =>
                            updateDraft({
                              rows: draft.rows.filter(
                                (item) => item.id !== row.id,
                              ),
                            })
                          }
                        />
                      </Col>
                    </Row>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

const StructuredValueEditor = ({
  t,
  operationId,
  label,
  valueText,
  placeholder,
  updateOperation,
}) => {
  const node = useMemo(
    () => parseStructuredValueNodeForDisplay(valueText),
    [valueText],
  );

  const commitNode = useCallback(
    (nextNode) => {
      updateOperation(operationId, {
        value_text: buildStructuredValueText(nextNode),
      });
    },
    [operationId, updateOperation],
  );

  return (
    <div
      className='rounded-xl p-3'
      style={{
        background: 'var(--semi-color-bg-1)',
        border: '1px solid var(--semi-color-border)',
      }}
    >
      <div className='flex items-center justify-between mb-2'>
        <Text strong>{t(label)}</Text>
        <Text type='tertiary' size='small'>
          {t('值类型')}
        </Text>
      </div>
      <StructuredValueNodeEditor
        t={t}
        node={node}
        sourceKey={valueText}
        placeholder={placeholder}
        onChange={commitNode}
      />
    </div>
  );
};

const StructuredValueNodeEditor = ({
  t,
  node,
  sourceKey,
  placeholder,
  depth = 0,
  onChange,
}) => {
  const source = sourceKey ?? node;
  const [draftState, setDraftState] = useState(() => ({
    source,
    draft: node,
  }));
  const currentNode = Object.is(draftState.source, source)
    ? draftState.draft
    : node;
  const emitNode = useCallback(
    (nextNode) => {
      setDraftState({
        source,
        draft: nextNode,
      });
      if (!canSerializeStructuredValueNode(nextNode)) {
        return;
      }
      onChange(nextNode);
    },
    [onChange, source],
  );
  const updateNode = useCallback(
    (patch) => emitNode({ ...currentNode, ...patch }),
    [currentNode, emitNode],
  );
  const compact = depth > 1;
  const canAddChild = depth < MAX_STRUCTURED_VALUE_DEPTH;
  const numberInvalid =
    currentNode.kind === 'number' &&
    !isCompleteStructuredNumberText(currentNode.text);

  return (
    <div className='flex flex-col gap-2'>
      <Select
        value={currentNode.kind}
        optionList={STRUCTURED_VALUE_TYPE_OPTIONS}
        onChange={(nextKind) => {
          const nextNode = createStructuredValueNode(nextKind || 'string');
          emitNode({ ...nextNode, id: currentNode.id });
        }}
        style={{ width: 150 }}
      />

      {currentNode.kind === 'string' || currentNode.kind === 'number' ? (
        <div className='flex flex-col gap-1'>
          <Input
            value={currentNode.text}
            placeholder={
              currentNode.kind === 'number' ? '0.7' : placeholder || 'value'
            }
            validateStatus={numberInvalid ? 'error' : 'default'}
            onChange={(nextValue) => {
              const nextText = nextValue ?? '';
              updateNode({ text: nextText });
            }}
          />
          {numberInvalid ? (
            <Text type='danger' size='small'>
              {t('数字值无效')}
            </Text>
          ) : null}
        </div>
      ) : null}

      {currentNode.kind === 'boolean' ? (
        <Space spacing={6}>
          <Button
            size='small'
            type={currentNode.boolValue ? 'primary' : 'tertiary'}
            onClick={() => updateNode({ boolValue: true })}
          >
            {BOOLEAN_TRUE_LABEL}
          </Button>
          <Button
            size='small'
            type={currentNode.boolValue ? 'tertiary' : 'primary'}
            onClick={() => updateNode({ boolValue: false })}
          >
            {BOOLEAN_FALSE_LABEL}
          </Button>
        </Space>
      ) : null}

      {currentNode.kind === 'null' ? (
        <Text type='tertiary' size='small'>
          {t('该值会保存为 null。')}
        </Text>
      ) : null}

      {currentNode.kind === 'object' ? (
        <div
          className='rounded-lg p-2'
          style={{ border: '1px solid var(--semi-color-border)' }}
        >
          <div className='flex items-center justify-between mb-2'>
            <Text strong size='small'>
              {t('对象字段')}
            </Text>
            <Button
              size='small'
              icon={<IconPlus />}
              disabled={!canAddChild}
              onClick={() =>
                updateNode({
                  objectEntries: [
                    ...(currentNode.objectEntries || []),
                    {
                      id: nextLocalId(),
                      key: '',
                      value: createStructuredValueNode('string'),
                    },
                  ],
                })
              }
            >
              {t('新增字段')}
            </Button>
          </div>
          {!canAddChild ? (
            <Text type='tertiary' size='small'>
              {t('已达到最大嵌套深度')}
            </Text>
          ) : null}
          {(currentNode.objectEntries || []).length === 0 ? (
            <Text type='tertiary' size='small'>
              {t('未配置对象字段。')}
            </Text>
          ) : (
            <div className='flex flex-col gap-2'>
              {(currentNode.objectEntries || []).map((entry) => (
                <div
                  key={entry.id}
                  className='grid gap-2'
                  style={{ gridTemplateColumns: '150px 1fr auto' }}
                >
                  <Input
                    value={entry.key}
                    placeholder={OBJECT_KEY_PLACEHOLDER}
                    onChange={(nextValue) =>
                      updateNode({
                        objectEntries: currentNode.objectEntries.map((item) =>
                          item.id === entry.id
                            ? { ...item, key: nextValue || '' }
                            : item,
                        ),
                      })
                    }
                  />
                  <StructuredValueNodeEditor
                    t={t}
                    node={entry.value}
                    depth={depth + 1}
                    onChange={(value) =>
                      updateNode({
                        objectEntries: currentNode.objectEntries.map((item) =>
                          item.id === entry.id ? { ...item, value } : item,
                        ),
                      })
                    }
                  />
                  <Button
                    type='danger'
                    theme='borderless'
                    icon={<IconDelete />}
                    aria-label={t('删除字段')}
                    onClick={() =>
                      updateNode({
                        objectEntries: currentNode.objectEntries.filter(
                          (item) => item.id !== entry.id,
                        ),
                      })
                    }
                  />
                </div>
              ))}
            </div>
          )}
        </div>
      ) : null}

      {currentNode.kind === 'array' ? (
        <div
          className='rounded-lg p-2'
          style={{ border: '1px solid var(--semi-color-border)' }}
        >
          <div className='flex items-center justify-between mb-2'>
            <Text strong size='small'>
              {t('数组项')}
            </Text>
            <Button
              size='small'
              icon={<IconPlus />}
              disabled={!canAddChild}
              onClick={() =>
                updateNode({
                  arrayItems: [
                    ...(currentNode.arrayItems || []),
                    {
                      id: nextLocalId(),
                      value: createStructuredValueNode('string'),
                    },
                  ],
                })
              }
            >
              {t('新增项')}
            </Button>
          </div>
          {!canAddChild ? (
            <Text type='tertiary' size='small'>
              {t('已达到最大嵌套深度')}
            </Text>
          ) : null}
          {(currentNode.arrayItems || []).length === 0 ? (
            <Text type='tertiary' size='small'>
              {t('未配置数组项。')}
            </Text>
          ) : (
            <div className='flex flex-col gap-2'>
              {(currentNode.arrayItems || []).map((item, index) => (
                <div
                  key={item.id}
                  className='grid gap-2'
                  style={{ gridTemplateColumns: '42px 1fr auto' }}
                >
                  <Text type='tertiary' size='small' className='leading-8'>
                    {`#${index + 1}`}
                  </Text>
                  <StructuredValueNodeEditor
                    t={t}
                    node={item.value}
                    depth={depth + 1}
                    onChange={(value) =>
                      updateNode({
                        arrayItems: currentNode.arrayItems.map((entry) =>
                          entry.id === item.id ? { ...entry, value } : entry,
                        ),
                      })
                    }
                  />
                  <Button
                    type='danger'
                    theme='borderless'
                    icon={<IconDelete />}
                    aria-label={t('删除项')}
                    onClick={() =>
                      updateNode({
                        arrayItems: currentNode.arrayItems.filter(
                          (entry) => entry.id !== item.id,
                        ),
                      })
                    }
                  />
                </div>
              ))}
            </div>
          )}
        </div>
      ) : null}

      {compact ? null : (
        <Text type='tertiary' size='small'>
          {`${t('预览')}: ${
            canSerializeStructuredValueNode(currentNode)
              ? buildStructuredValueText(currentNode) || '-'
              : '-'
          }`}
        </Text>
      )}
    </div>
  );
};

export default ParamOverrideEditorModal;
