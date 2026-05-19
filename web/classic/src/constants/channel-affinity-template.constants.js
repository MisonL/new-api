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

const buildPassHeadersTemplate = (headers) => ({
  operations: [
    {
      mode: 'pass_headers',
      value: [...headers],
      keep_origin: true,
    },
  ],
});

export const CODEX_CLI_HEADER_PASSTHROUGH_HEADERS = [
  'User-Agent',
  'Originator',
  'Session_id',
  'X-Codex-Beta-Features',
  'X-Codex-Turn-Metadata',
  'X-Codex-Window-Id',
  'X-Client-Request-Id',
];

export const CLAUDE_CLI_HEADER_PASSTHROUGH_HEADERS = [
  'X-Claude-Code-Session-Id',
  'X-Stainless-Arch',
  'X-Stainless-Lang',
  'X-Stainless-Os',
  'X-Stainless-Package-Version',
  'X-Stainless-Retry-Count',
  'X-Stainless-Runtime',
  'X-Stainless-Runtime-Version',
  'X-Stainless-Timeout',
  'X-App',
  'Anthropic-Beta',
  'Anthropic-Dangerous-Direct-Browser-Access',
  'Anthropic-Version',
];

export const QWEN_CODE_CLI_HEADER_PASSTHROUGH_HEADERS = [
  'X-Stainless-Arch',
  'X-Stainless-Lang',
  'X-Stainless-Os',
  'X-Stainless-Package-Version',
  'X-Stainless-Retry-Count',
  'X-Stainless-Runtime',
  'X-Stainless-Runtime-Version',
];

export const DROID_CLI_HEADER_PASSTHROUGH_HEADERS = [
  'X-Stainless-Arch',
  'X-Stainless-Lang',
  'X-Stainless-Os',
  'X-Stainless-Package-Version',
  'X-Stainless-Retry-Count',
  'X-Stainless-Runtime',
  'X-Stainless-Runtime-Version',
];

export const GEMINI_CLI_HEADER_PASSTHROUGH_HEADERS = ['X-Goog-Api-Client'];

export const CODEX_CLI_HEADER_PASSTHROUGH_TEMPLATE = buildPassHeadersTemplate(
  CODEX_CLI_HEADER_PASSTHROUGH_HEADERS,
);

export const CLAUDE_CLI_HEADER_PASSTHROUGH_TEMPLATE = buildPassHeadersTemplate(
  CLAUDE_CLI_HEADER_PASSTHROUGH_HEADERS,
);

export const QWEN_CODE_CLI_HEADER_PASSTHROUGH_TEMPLATE =
  buildPassHeadersTemplate(QWEN_CODE_CLI_HEADER_PASSTHROUGH_HEADERS);

export const GEMINI_CLI_HEADER_PASSTHROUGH_TEMPLATE = buildPassHeadersTemplate(
  GEMINI_CLI_HEADER_PASSTHROUGH_HEADERS,
);

export const DROID_CLI_HEADER_PASSTHROUGH_TEMPLATE = buildPassHeadersTemplate(
  DROID_CLI_HEADER_PASSTHROUGH_HEADERS,
);

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
};

export const PARAM_OVERRIDE_TEMPLATES = {
  codexHeaders: {
    label: 'Codex Desktop Header Passthrough',
    payload: CODEX_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  },
  codexWithoutImageTool: {
    label: 'Codex Desktop Compat: Remove Image Generation Tool',
    payload: PRUNE_IMAGE_GENERATION_TOOL_TEMPLATE,
  },
  claudeHeaders: {
    label: 'Claude Code Header Passthrough',
    payload: CLAUDE_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  },
  geminiHeaders: {
    label: 'Gemini CLI Header Passthrough',
    payload: GEMINI_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  },
  qwenCodeHeaders: {
    label: 'Qwen Code Header Passthrough',
    payload: QWEN_CODE_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  },
  droidHeaders: {
    label: 'Droid CLI Header Passthrough',
    payload: DROID_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  },
};

export const CHANNEL_AFFINITY_RULE_TEMPLATES = {
  codexCli: {
    name: 'codex cli trace',
    model_regex: ['^gpt-.*$'],
    path_regex: ['/v1/responses'],
    key_sources: [{ type: 'gjson', path: 'prompt_cache_key' }],
    value_regex: '',
    ttl_seconds: 0,
    skip_retry_on_failure: true,
    include_using_group: true,
    include_rule_name: true,
  },
  claudeCli: {
    name: 'claude cli trace',
    model_regex: ['^claude-.*$'],
    path_regex: ['/v1/messages'],
    key_sources: [{ type: 'gjson', path: 'metadata.user_id' }],
    value_regex: '',
    ttl_seconds: 0,
    skip_retry_on_failure: true,
    include_using_group: true,
    include_rule_name: true,
  },
};

export const cloneChannelAffinityTemplate = (template) =>
  JSON.parse(JSON.stringify(template || {}));

const isPlainRecord = (value) =>
  typeof value === 'object' && value !== null && !Array.isArray(value);

export const stringifyParamOverrideTemplatePayload = (payload) =>
  JSON.stringify(cloneChannelAffinityTemplate(payload), null, 2);

export const appendParamOverrideTemplatePayload = (currentJson, payload) => {
  const nextPayload = cloneChannelAffinityTemplate(payload);
  const raw = String(currentJson || '').trim();
  if (!raw) {
    return stringifyParamOverrideTemplatePayload(nextPayload);
  }

  const current = JSON.parse(raw);
  if (!isPlainRecord(current)) {
    throw new Error('Parameter override template must be a JSON object');
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
      2,
    );
  }

  return JSON.stringify(
    {
      ...current,
      ...nextPayload,
    },
    null,
    2,
  );
};
