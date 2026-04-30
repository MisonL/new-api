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
  'codex_exec/0.125.0 (Mac OS 15.7.3; x86_64) ghostty/1.3.1 (codex_exec; 0.125.0)';

const aiCodingCliProfiles = {
  'codex-cli': {
    key: 'codex-cli',
    name: 'Codex CLI',
    group: 'ai_coding_cli',
    scope: 'builtin',
    readonly: true,
    passthroughRequired: true,
    description:
      '固定请求头是 Codex CLI 0.125.0 的静态快照；真实 CLI 会携带会话与窗口动态头，需在高级参数覆盖中启用 Codex CLI 请求头透传模板。',
    headers: {
      'User-Agent': CODEX_CLI_USER_AGENT,
      Originator: 'codex_exec',
    },
  },
  'claude-code': buildAiCodingCliProfile(
    'claude-code',
    'Claude Code',
    'Claude-Code/1.0',
    'claude-code',
    '固定请求头只用于普通渠道标识；Claude 官方客户端链路如需保留会话与 SDK 元数据，还需要在高级设置中启用 Claude CLI 真实请求头透传。',
    true,
  ),
  'gemini-cli': buildAiCodingCliProfile(
    'gemini-cli',
    'Gemini CLI',
    'GeminiCLI/1.0',
    'gemini-cli',
    '固定请求头用于普通渠道标识；若上游要求真实客户端会话头，应在高级设置中额外开启请求头透传。',
  ),
  'qwen-code': buildAiCodingCliProfile(
    'qwen-code',
    'Qwen Code',
    'Qwen-Code/1.0',
    'qwen-code',
    '固定请求头用于普通渠道标识；不能替代真实 CLI 请求中携带的动态会话头。',
  ),
  opencode: buildAiCodingCliProfile(
    'opencode',
    'OpenCode',
    'OpenCode/1.0',
    'opencode',
    '固定请求头用于普通渠道标识；不能替代真实客户端动态请求头。',
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
