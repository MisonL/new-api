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
  versionSource,
  passthroughRequired = false,
) => ({
  key,
  name,
  group: 'ai_coding_cli',
  scope: 'builtin',
  readonly: true,
  passthroughRequired,
  description,
  versionSource,
  headers,
});

const AI_CODING_CLI_VERSION_SOURCES = {
  'codex-cli': {
    packageName: '@openai/codex',
    fallbackVersion: '0.130.0',
  },
  'claude-code': {
    packageName: '@anthropic-ai/claude-code',
    fallbackVersion: '2.1.139',
  },
  'gemini-cli': {
    packageName: '@google/gemini-cli',
    fallbackVersion: '0.41.2',
  },
  'qwen-code': {
    packageName: '@qwen-code/qwen-code',
    fallbackVersion: '0.15.10',
  },
  droid: {
    packageName: 'droid',
    fallbackVersion: '0.123.0',
  },
};

export const NPM_VERSION_OPTION_LIMIT = 5;

export function buildAiCodingCliUserAgent(profileId, version) {
  switch (profileId) {
    case 'codex-cli':
      return `codex-tui/${version} (Mac OS 15.7.3; x86_64) ghostty/1.3.1 (codex-tui; ${version})`;
    case 'claude-code':
      return `claude-cli/${version} (external, sdk-cli)`;
    case 'gemini-cli':
      return `GeminiCLI/${version}/gemini-3.1-pro-preview (darwin; x64; terminal)`;
    case 'qwen-code':
      return `QwenCode/${version} (darwin; x64)`;
    case 'droid':
      return `factory-cli/${version}`;
    default:
      return '';
  }
}

function buildAiCodingCliHeaders(profileId, version, extraHeaders = {}) {
  return {
    ...extraHeaders,
    'User-Agent': buildAiCodingCliUserAgent(profileId, version),
  };
}

const aiCodingCliProfiles = {
  'codex-cli': {
    key: 'codex-cli',
    name: 'Codex CLI',
    group: 'ai_coding_cli',
    scope: 'builtin',
    readonly: true,
    passthroughRequired: false,
    versionSource: AI_CODING_CLI_VERSION_SOURCES['codex-cli'],
    description:
      '固定请求头静态快照来自 Codex CLI 0.130.0 交互式 TUI 请求头生成逻辑；此模板仅固定客户端身份。会话、窗口与 turn metadata 动态头需在高级参数覆盖中显式选择 Codex CLI 请求头透传模板。',
    headers: buildAiCodingCliHeaders('codex-cli', '0.130.0', {
      Originator: 'codex-tui',
    }),
  },
  'codex-desktop': buildAiCodingCliProfile(
    'codex-desktop',
    'Codex Desktop',
    {
      'User-Agent':
        'Codex Desktop/0.131.0-alpha.9 (Mac OS 15.7.3; x86_64) unknown (Codex Desktop; 26.513.31313)',
    },
    '固定请求头静态快照来自 Codex Desktop 0.131.0-alpha.9 真实请求；此模板仅固定客户端身份。会话、窗口与 turn metadata 动态头需在高级参数覆盖中显式选择 Codex Desktop 请求头透传模板。',
    null,
    false,
  ),
  'claude-code': buildAiCodingCliProfile(
    'claude-code',
    'Claude Code',
    buildAiCodingCliHeaders('claude-code', '2.1.139'),
    '固定请求头静态快照来自本机实抓 Claude Code 2.1.139 /v1/messages?beta=true 请求；此模板仅固定客户端身份。X-Claude-Code-Session-Id、Anthropic-Version、Anthropic-Beta、X-Stainless-* 等动态头需在高级参数覆盖中显式选择 Claude CLI 请求头透传模板。',
    AI_CODING_CLI_VERSION_SOURCES['claude-code'],
    false,
  ),
  'gemini-cli': buildAiCodingCliProfile(
    'gemini-cli',
    'Gemini CLI',
    buildAiCodingCliHeaders('gemini-cli', '0.41.2'),
    '固定请求头静态快照来自本机实抓 Gemini CLI 0.41.2 的 streamGenerateContent 请求；此模板仅固定客户端身份。x-goog-api-client 等动态头需在高级参数覆盖中显式选择 Gemini CLI 请求头透传模板。',
    AI_CODING_CLI_VERSION_SOURCES['gemini-cli'],
    false,
  ),
  'qwen-code': buildAiCodingCliProfile(
    'qwen-code',
    'Qwen Code',
    buildAiCodingCliHeaders('qwen-code', '0.15.10'),
    '固定请求头静态快照来自本机 Qwen Code 0.15.10 的 OpenAI-compatible /chat/completions 请求；此模板仅固定客户端身份。x-stainless-* 动态头需在高级参数覆盖中显式选择 Qwen Code 请求头透传模板。',
    AI_CODING_CLI_VERSION_SOURCES['qwen-code'],
    false,
  ),
  droid: buildAiCodingCliProfile(
    'droid',
    'Droid CLI',
    buildAiCodingCliHeaders('droid', '0.123.0'),
    '固定请求头静态快照来自本机实抓 Droid 0.123.0 的 OpenAI-compatible /v1/chat/completions 请求；此模板仅固定客户端身份。X-Stainless-* 动态头需在高级参数覆盖中显式选择 Droid CLI 请求头透传模板。',
    AI_CODING_CLI_VERSION_SOURCES.droid,
    false,
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

function parseStableVersion(version) {
  const match = String(version || '').match(/^(\d+)\.(\d+)\.(\d+)$/);
  if (!match) {
    return null;
  }
  return match.slice(1).map((part) => Number.parseInt(part, 10));
}

function compareStableVersionsDesc(left, right) {
  const leftParts = parseStableVersion(left);
  const rightParts = parseStableVersion(right);
  if (!leftParts && !rightParts) {
    return 0;
  }
  if (!leftParts) {
    return 1;
  }
  if (!rightParts) {
    return -1;
  }
  for (let index = 0; index < 3; index += 1) {
    if (leftParts[index] !== rightParts[index]) {
      return rightParts[index] - leftParts[index];
    }
  }
  return 0;
}

function addUniqueVersion(target, version) {
  const normalizedVersion = String(version || '').trim();
  if (!normalizedVersion || target.includes(normalizedVersion)) {
    return;
  }
  target.push(normalizedVersion);
}

export function buildNpmCliVersionOptions(
  packageMetadata,
  limit = NPM_VERSION_OPTION_LIMIT,
) {
  const latestVersion = String(
    packageMetadata?.['dist-tags']?.latest || '',
  ).trim();
  const allVersions = packageMetadata?.versions
    ? Object.keys(packageMetadata.versions)
    : [];
  const stableVersions = allVersions
    .filter((version) => parseStableVersion(version) !== null)
    .sort(compareStableVersionsDesc);
  const selectedVersions = [];
  addUniqueVersion(selectedVersions, latestVersion);
  stableVersions.forEach((version) =>
    addUniqueVersion(selectedVersions, version),
  );
  return selectedVersions.slice(0, limit).map((version) => ({
    value: version,
    label: version === latestVersion ? `${version} (latest)` : version,
    isLatest: version === latestVersion,
  }));
}

function normalizeNpmCliVersionOption(option = {}) {
  if (!option || typeof option !== 'object' || Array.isArray(option)) {
    return null;
  }
  const value = String(option.value || '').trim();
  if (!value) {
    return null;
  }
  return {
    value,
    label: String(option.label || value).trim() || value,
    isLatest: option.isLatest === true || option.is_latest === true,
  };
}

export function normalizeNpmCliVersionOptions(options) {
  if (!Array.isArray(options)) {
    return [];
  }
  return options
    .map((option) => normalizeNpmCliVersionOption(option))
    .filter(Boolean)
    .slice(0, NPM_VERSION_OPTION_LIMIT);
}

export function getAiCodingCliVersionSource(profile) {
  const profileId = String(profile?.id || profile?.key || '').trim();
  return profile?.versionSource || AI_CODING_CLI_VERSION_SOURCES[profileId];
}

export function buildVersionedAiCodingCliProfile(
  profile,
  version,
  source = 'npm',
) {
  const baseProfileId = String(profile?.id || profile?.key || '').trim();
  const normalizedVersion = String(version || '').trim();
  const versionSource = getAiCodingCliVersionSource(profile);
  if (!baseProfileId || !normalizedVersion || !versionSource) {
    return profile;
  }
  const baseName = String(profile.name || baseProfileId).trim();
  return {
    ...profile,
    id: `${baseProfileId}@${normalizedVersion}`,
    key: `${baseProfileId}@${normalizedVersion}`,
    name: `${baseName} ${normalizedVersion}`,
    headers: buildAiCodingCliHeaders(
      baseProfileId,
      normalizedVersion,
      baseProfileId === 'codex-cli' ? { Originator: 'codex-tui' } : {},
    ),
    versionMeta: {
      baseProfileId,
      packageName: versionSource.packageName,
      source,
      version: normalizedVersion,
    },
  };
}

export async function fetchNpmCliVersionOptions(
  packageName,
  requestImpl,
  options = {},
) {
  if (typeof requestImpl !== 'function') {
    throw new Error('request implementation is required');
  }
  const timeoutMs = Number.isFinite(options.timeoutMs)
    ? options.timeoutMs
    : 5000;
  const response = await requestImpl('/api/channel/npm_version_options', {
    params: {
      package: String(packageName || '').trim(),
    },
    timeout: timeoutMs,
    skipErrorHandler: true,
    disableDuplicate: true,
  });
  const payload = response?.data || {};
  if (payload.success !== true) {
    throw new Error(payload.message || 'failed to load npm versions');
  }
  const normalizedOptions = normalizeNpmCliVersionOptions(payload.data);
  if (normalizedOptions.length === 0) {
    throw new Error('empty npm version options');
  }
  return normalizedOptions;
}
