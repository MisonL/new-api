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
  userAgent,
  clientName,
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
  headers: {
    'User-Agent': userAgent,
    'X-Client-Name': clientName,
    'X-Client-Platform': 'terminal',
  },
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
      '固定请求头是 codex-tui 0.128.0 交互模式的静态快照；选择此模板时会自动写入 Codex CLI 请求头透传规则，保留真实 CLI 的会话与窗口动态头。',
    headers: {
      // Codex TUI 原生请求使用 Originator，不使用通用的 X-Client-Name。
      'User-Agent': CODEX_CLI_USER_AGENT,
      Originator: 'codex-tui',
    },
  },
  'claude-code': buildAiCodingCliProfile(
    'claude-code',
    'Claude Code',
    'Claude-Code/1.0',
    'claude-code',
    '固定请求头只用于普通渠道标识；选择此模板时会自动写入 Claude CLI 请求头透传规则，保留官方客户端会话与 SDK 元数据。',
    true,
  ),
  'gemini-cli': buildAiCodingCliProfile(
    'gemini-cli',
    'Gemini CLI',
    'GeminiCLI/0.40.1/gemini-3.1-pro-preview (darwin; x64; terminal)',
    'gemini-cli',
    '固定请求头是 Gemini CLI 0.40.1 交互模式的静态快照；选择此模板时会自动写入 Gemini CLI 请求头透传规则，保留真实客户端的 x-goog-api-client 等运行时头。',
    true,
  ),
  'qwen-code': buildAiCodingCliProfile(
    'qwen-code',
    'Qwen Code',
    'QwenCode/0.15.6 (darwin; x64)',
    'qwen-code',
    '固定请求头是 Qwen Code 0.15.6 交互模式的静态快照；真实 CLI 还会附带 x-stainless-* 运行时头，严格复刻上游链路时应改用透传。',
  ),
  opencode: buildAiCodingCliProfile(
    'opencode',
    'OpenCode',
    'ai-sdk/openai/2.0.71 ai-sdk/provider-utils/3.0.17 runtime/bun/1.3.5',
    'opencode',
    '固定请求头是 OpenCode 1.1.14 当前链路下的静态快照；真实客户端会直接走 OpenAI 兼容 Responses 请求，严格复刻上游链路时再按需补透传。',
  ),
  droid: buildAiCodingCliProfile(
    'droid',
    'Droid',
    'Droid/1.0',
    'droid',
    '固定请求头用于普通渠道标识；不能替代真实客户端动态请求头。',
  ),
  amp: buildAiCodingCliProfile(
    'amp',
    'Amp',
    'AmpCLI/1.0',
    'amp',
    '固定请求头用于普通渠道标识；不能替代真实客户端动态请求头。',
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
