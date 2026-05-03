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

export const HEADER_PROFILE_GROUPS = [
  {
    key: 'browser',
    name: 'Browser',
  },
  {
    key: 'ai_coding_cli',
    name: 'AI Coding CLI',
  },
  {
    key: 'api_sdk',
    name: 'API SDK / Debug',
  },
];

const browserProfiles = {
  'chrome-macos': {
    key: 'chrome-macos',
    name: 'Chrome macOS',
    group: 'browser',
    scope: 'builtin',
    readonly: true,
    headers: {
      Accept: 'text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8',
      'Accept-Language': 'en-US,en;q=0.9',
      'Sec-CH-UA':
        '"Google Chrome";v="135", "Chromium";v="135", "Not.A/Brand";v="24"',
      'Sec-CH-UA-Mobile': '?0',
      'Sec-CH-UA-Platform': '"macOS"',
      'User-Agent':
        'Mozilla/5.0 (Macintosh; Intel Mac OS X 14_4) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36',
    },
  },
};

const buildAiCodingCliProfile = (
  key,
  name,
  headers,
  description,
  passthroughRequired = false,
) => ({
  key,
  name,
  group: 'ai_coding_cli',
  scope: 'builtin',
  readonly: true,
  passthroughRequired,
  description,
  headers,
});

const CODEX_CLI_USER_AGENT =
  'codex-tui/0.128.0 (Mac OS 15.7.3; x86_64) ghostty/1.3.1 (codex-tui; 0.128.0)';

const aiCodingCliProfiles = {
  'codex-cli': {
    key: 'codex-cli',
    name: 'Codex CLI',
    group: 'ai_coding_cli',
    scope: 'builtin',
    readonly: true,
    passthroughRequired: true,
    description:
      '固定请求头静态快照来自 Codex CLI 0.128.0 交互式 TUI 请求头生成逻辑；选择此模板时会自动写入 Codex CLI 请求头透传规则，保留真实 CLI 的会话、窗口与 turn metadata 动态头。',
    headers: {
      'User-Agent': CODEX_CLI_USER_AGENT,
      Originator: 'codex-tui',
    },
  },
  'claude-code': buildAiCodingCliProfile(
    'claude-code',
    'Claude Code',
    {
      'User-Agent': 'claude-cli/2.1.126 (external, sdk-cli)',
    },
    '固定请求头静态快照来自本机实抓 Claude Code 2.1.126 /v1/messages?beta=true 请求；真实请求还会携带 X-Claude-Code-Session-Id、Anthropic-Version、Anthropic-Beta、X-Stainless-* 等 SDK 头，选择此模板时会自动写入 Claude CLI 请求头透传规则。',
    true,
  ),
  'gemini-cli': buildAiCodingCliProfile(
    'gemini-cli',
    'Gemini CLI',
    {
      'User-Agent':
        'GeminiCLI/0.40.1/gemini-3.1-pro-preview (darwin; x64; terminal)',
    },
    '固定请求头静态快照来自本机实抓 Gemini CLI 0.40.1 的 streamGenerateContent 请求；真实请求还会携带 x-goog-api-client 等运行时头，选择此模板时会自动写入 Gemini CLI 请求头透传规则。',
    true,
  ),
  'qwen-code': buildAiCodingCliProfile(
    'qwen-code',
    'Qwen Code',
    {
      'User-Agent': 'QwenCode/0.15.6 (darwin; x64)',
    },
    '固定请求头静态快照来自本机 Qwen Code 0.15.6 的 OpenAI-compatible /chat/completions 请求；真实请求还会携带已实抓的 x-stainless-* 运行时头，选择此模板时会自动写入 Qwen Code 请求头透传规则。',
    true,
  ),
  droid: buildAiCodingCliProfile(
    'droid',
    'Droid CLI',
    {
      'User-Agent': 'factory-cli/0.115.0',
    },
    '固定请求头静态快照来自本机实抓 Droid 0.115.0 的 OpenAI-compatible /v1/chat/completions 请求；真实请求还会携带 X-Stainless-* 运行时头，选择此模板时会自动写入 Droid CLI 请求头透传规则。',
    true,
  ),
};

const apiSdkProfiles = {
  'postman-runtime': {
    key: 'postman-runtime',
    name: 'Postman Runtime',
    group: 'api_sdk',
    scope: 'builtin',
    readonly: true,
    headers: {
      Accept: '*/*',
      'Cache-Control': 'no-cache',
      'Postman-Token': '00000000-0000-0000-0000-000000000000',
      'User-Agent': 'PostmanRuntime/7.43.0',
    },
  },
};

export const HEADER_PROFILE_PRESETS = {
  ...browserProfiles,
  ...aiCodingCliProfiles,
  ...apiSdkProfiles,
};
