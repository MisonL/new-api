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
        '"Google Chrome";v="150", "Chromium";v="150", "Not.A/Brand";v="24"',
      'Sec-CH-UA-Mobile': '?0',
      'Sec-CH-UA-Platform': '"macOS"',
      'User-Agent':
        'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.7871.47 Safari/537.36',
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
    fallbackVersion: '0.142.4',
  },
  'claude-code': {
    packageName: '@anthropic-ai/claude-code',
    fallbackVersion: '2.1.197',
  },
  'gemini-cli': {
    packageName: '@google/gemini-cli',
    fallbackVersion: '0.49.0',
  },
  'qwen-code': {
    packageName: '@qwen-code/qwen-code',
    fallbackVersion: '0.19.3',
  },
  droid: {
    packageName: 'droid',
    fallbackVersion: '0.161.0',
  },
};

export const NPM_VERSION_OPTION_LIMIT = 5;
export const NPM_VERSION_LATEST_ALIAS = 'latest';
export const NPM_VERSION_REFRESH_TIMEOUT_MS = 10000;
export const AI_CODING_CLI_DEFAULT_PLATFORM = 'macos-x64';
export const NPM_VERSION_LOAD_ERROR_CODE = 'npm_version_load_failed';
export const NPM_VERSION_AUTH_ERROR_CODE = 'npm_version_auth_required';
export const NPM_VERSION_FORBIDDEN_CODE = 'npm_version_forbidden';
export const NPM_VERSION_RATE_LIMITED_CODE = 'npm_version_rate_limited';
export const NPM_VERSION_EMPTY_ERROR_CODE = 'npm_version_empty';
export const NPM_VERSION_NOT_RECORDED_CODE = 'npm_version_not_recorded';

export class NpmCliVersionLoadError extends Error {
  constructor(code, message) {
    super(message);
    this.name = 'NpmCliVersionLoadError';
    this.code = code;
  }
}

function normalizeNpmCliVersionLoadError(error) {
  if (error instanceof NpmCliVersionLoadError) {
    return error;
  }
  const status = Number(error?.response?.status ?? error?.status);
  const message = error?.response?.data?.message || error?.message;
  return new NpmCliVersionLoadError(
    normalizeNpmCliVersionPayloadErrorCode({ message }, status),
    message || 'failed to load npm versions',
  );
}

function isForbiddenNpmCliVersionMessage(message) {
  const normalized = String(message || '')
    .trim()
    .toLowerCase();
  return (
    normalized.includes('insufficient privilege') ||
    normalized.includes('insufficient privileges') ||
    normalized.includes('权限不足') ||
    normalized.includes('權限不足')
  );
}

export function normalizeNpmCliVersionPayloadErrorCode(payload = {}, status) {
  if (status === 401) {
    return NPM_VERSION_AUTH_ERROR_CODE;
  }
  if (status === 403) {
    return NPM_VERSION_FORBIDDEN_CODE;
  }
  if (status === 429) {
    return NPM_VERSION_RATE_LIMITED_CODE;
  }
  const code = String(payload.code || '').trim();
  if (code) {
    return code;
  }
  if (isForbiddenNpmCliVersionMessage(payload.message)) {
    return NPM_VERSION_FORBIDDEN_CODE;
  }
  return NPM_VERSION_LOAD_ERROR_CODE;
}

export const AI_CODING_CLI_PLATFORM_OPTIONS = [
  {
    value: 'macos-x64',
    label: 'macOS x64',
    codexOS: 'Mac OS 15.7.3',
    codexArch: 'x86_64',
    geminiOS: 'darwin',
    geminiArch: 'x64',
    qwenOS: 'darwin',
    qwenArch: 'x64',
  },
  {
    value: 'macos-arm64',
    label: 'macOS arm64',
    codexOS: 'Mac OS 15.7.3',
    codexArch: 'aarch64',
    geminiOS: 'darwin',
    geminiArch: 'arm64',
    qwenOS: 'darwin',
    qwenArch: 'arm64',
  },
  {
    value: 'linux-x64',
    label: 'Linux x64',
    codexOS: 'Linux',
    codexArch: 'x86_64',
    geminiOS: 'linux',
    geminiArch: 'x64',
    qwenOS: 'linux',
    qwenArch: 'x64',
  },
  {
    value: 'linux-arm64',
    label: 'Linux arm64',
    codexOS: 'Linux',
    codexArch: 'aarch64',
    geminiOS: 'linux',
    geminiArch: 'arm64',
    qwenOS: 'linux',
    qwenArch: 'arm64',
  },
  {
    value: 'windows-x64',
    label: 'Windows x64',
    codexOS: 'Windows NT 10.0',
    codexArch: 'x86_64',
    geminiOS: 'win32',
    geminiArch: 'x64',
    qwenOS: 'win32',
    qwenArch: 'x64',
  },
  {
    value: 'windows-arm64',
    label: 'Windows arm64',
    codexOS: 'Windows NT 10.0',
    codexArch: 'aarch64',
    geminiOS: 'win32',
    geminiArch: 'arm64',
    qwenOS: 'win32',
    qwenArch: 'arm64',
  },
];

export function normalizeAiCodingCliPlatform(platform) {
  const normalizedPlatform = String(platform || '').trim();
  return AI_CODING_CLI_PLATFORM_OPTIONS.some(
    (option) => option.value === normalizedPlatform,
  )
    ? normalizedPlatform
    : AI_CODING_CLI_DEFAULT_PLATFORM;
}

function getAiCodingCliPlatformTokens(platform) {
  const normalizedPlatform = normalizeAiCodingCliPlatform(platform);
  return (
    AI_CODING_CLI_PLATFORM_OPTIONS.find(
      (option) => option.value === normalizedPlatform,
    ) || AI_CODING_CLI_PLATFORM_OPTIONS[0]
  );
}

export function buildAiCodingCliUserAgent(
  profileId,
  version,
  platform = AI_CODING_CLI_DEFAULT_PLATFORM,
) {
  const platformTokens = getAiCodingCliPlatformTokens(platform);
  switch (profileId) {
    case 'codex-cli':
      return `codex-tui/${version} (${platformTokens.codexOS}; ${platformTokens.codexArch}) ghostty/1.3.1 (codex-tui; ${version})`;
    case 'claude-code':
      return `claude-cli/${version} (external, sdk-cli)`;
    case 'gemini-cli':
      return `GeminiCLI/${version}/gemini-3.1-pro-preview (${platformTokens.geminiOS}; ${platformTokens.geminiArch}; terminal)`;
    case 'qwen-code':
      return `QwenCode/${version} (${platformTokens.qwenOS}; ${platformTokens.qwenArch})`;
    case 'droid':
      return `factory-cli/${version}`;
    default:
      return '';
  }
}

function buildAiCodingCliHeaders(
  profileId,
  version,
  extraHeaders = {},
  platform = AI_CODING_CLI_DEFAULT_PLATFORM,
) {
  return {
    ...extraHeaders,
    'User-Agent': buildAiCodingCliUserAgent(profileId, version, platform),
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
      '默认使用 Codex CLI npm latest 版本套用交互式 TUI 请求头生成逻辑；清单暂不可用时保留内置快照。此模板仅固定客户端身份。会话、窗口与 turn metadata 动态头需在高级参数覆盖中显式选择 Codex CLI 请求头透传模板。',
    headers: buildAiCodingCliHeaders('codex-cli', '0.142.4', {
      Originator: 'codex-tui',
    }),
  },
  'codex-desktop': buildAiCodingCliProfile(
    'codex-desktop',
    'Codex Desktop',
    {
      'User-Agent':
        'Codex Desktop/0.142.4 (Mac OS 15.7.3; x86_64) unknown (Codex Desktop; 26.623.70822)',
      Originator: 'Codex Desktop',
    },
    '固定请求头静态快照来自本机 Codex Desktop 原始请求抓包；UA 前缀 0.142.4 是内置 Codex core/app-server 客户端版本，括号尾部 26.623.70822 是 Codex Desktop App 版本。此模板仅固定 Codex App 客户端身份，不能与 codex-tui 混用。会话、窗口与 turn metadata 动态头需在高级参数覆盖中显式选择 Codex Desktop 请求头透传模板。',
    null,
    false,
  ),
  'claude-code': buildAiCodingCliProfile(
    'claude-code',
    'Claude Code',
    buildAiCodingCliHeaders('claude-code', '2.1.197'),
    '默认使用 Claude Code npm latest 版本套用既有客户端 UA 格式；清单暂不可用时保留内置快照。此模板仅固定客户端身份。X-Claude-Code-Session-Id、Anthropic-Version、Anthropic-Beta、X-Stainless-* 等动态头需在高级参数覆盖中显式选择 Claude Code 请求头透传模板。',
    AI_CODING_CLI_VERSION_SOURCES['claude-code'],
    false,
  ),
  'gemini-cli': buildAiCodingCliProfile(
    'gemini-cli',
    'Gemini CLI',
    buildAiCodingCliHeaders('gemini-cli', '0.49.0'),
    '默认使用 Gemini CLI npm latest 版本套用既有客户端 UA 格式；清单暂不可用时保留内置快照。此模板仅固定客户端身份。x-goog-api-client 等动态头需在高级参数覆盖中显式选择 Gemini CLI 请求头透传模板。',
    AI_CODING_CLI_VERSION_SOURCES['gemini-cli'],
    false,
  ),
  'qwen-code': buildAiCodingCliProfile(
    'qwen-code',
    'Qwen Code',
    buildAiCodingCliHeaders('qwen-code', '0.19.3'),
    '默认使用 Qwen Code npm latest 版本套用既有客户端 UA 格式；清单暂不可用时保留内置快照。此模板仅固定客户端身份。x-stainless-* 动态头需在高级参数覆盖中显式选择 Qwen Code 请求头透传模板。',
    AI_CODING_CLI_VERSION_SOURCES['qwen-code'],
    false,
  ),
  droid: buildAiCodingCliProfile(
    'droid',
    'Droid CLI',
    buildAiCodingCliHeaders('droid', '0.161.0'),
    '默认使用 Droid CLI npm latest 版本套用既有客户端 UA 格式；清单暂不可用时保留内置快照。此模板仅固定客户端身份。X-Stainless-* 动态头需在高级参数覆盖中显式选择 Droid CLI 请求头透传模板。',
    AI_CODING_CLI_VERSION_SOURCES.droid,
    false,
  ),
  agy: buildAiCodingCliProfile(
    'agy',
    'Antigravity CLI',
    {
      'User-Agent':
        'antigravity/cli/1.0.14 (aidev_client; os_type=darwin; arch=amd64)',
    },
    '固定请求头静态快照来自 Antigravity CLI 原始请求身份；此模板仅固定客户端身份。若后续确认需要动态头，应基于真实客户端抓包在高级参数覆盖中手动配置 pass_headers。',
    null,
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
      'User-Agent': 'PostmanRuntime/7.54.0',
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
  const normalizedVersion = normalizeNpmCliVersionValue(version);
  if (!normalizedVersion || target.includes(normalizedVersion)) {
    return;
  }
  target.push(normalizedVersion);
}

function normalizeNpmCliVersionValue(version) {
  const normalizedVersion = String(version || '').trim();
  if (
    !normalizedVersion ||
    normalizedVersion === NPM_VERSION_LATEST_ALIAS ||
    normalizedVersion.length > 64 ||
    !/^[0-9][0-9A-Za-z.+-]*$/.test(normalizedVersion)
  ) {
    return '';
  }
  return normalizedVersion;
}

export function buildNpmCliFallbackVersionOptions(fallbackVersion) {
  const normalizedVersion = normalizeNpmCliVersionValue(fallbackVersion);
  if (!normalizedVersion) {
    return [
      {
        value: NPM_VERSION_LATEST_ALIAS,
        label: NPM_VERSION_LATEST_ALIAS,
        isLatest: true,
        resolvedVersion: '',
        source: 'fallback',
      },
    ];
  }
  return [
    {
      value: NPM_VERSION_LATEST_ALIAS,
      label: `${NPM_VERSION_LATEST_ALIAS} (${normalizedVersion})`,
      isLatest: true,
      resolvedVersion: normalizedVersion,
      source: 'fallback',
    },
    {
      value: normalizedVersion,
      label: normalizedVersion,
      isLatest: false,
      resolvedVersion: normalizedVersion,
      source: 'fallback',
    },
  ];
}

export function buildNpmCliVersionOptions(
  packageMetadata,
  limit = NPM_VERSION_OPTION_LIMIT,
) {
  let latestVersion = normalizeNpmCliVersionValue(
    packageMetadata?.['dist-tags']?.latest,
  );
  const allVersions = packageMetadata?.versions
    ? Object.keys(packageMetadata.versions)
    : [];
  const stableVersions = allVersions
    .filter((version) => parseStableVersion(version) !== null)
    .sort(compareStableVersionsDesc);
  if (!latestVersion && stableVersions.length > 0) {
    latestVersion = stableVersions[0];
  }
  const selectedVersions = [];
  addUniqueVersion(selectedVersions, latestVersion);
  stableVersions.forEach((version) =>
    addUniqueVersion(selectedVersions, version),
  );
  const pinnedOptions = selectedVersions.slice(0, limit).map((version) => ({
    value: version,
    label: version,
    isLatest: false,
    resolvedVersion: version,
    source: 'npm',
  }));
  if (!latestVersion) {
    return pinnedOptions;
  }
  return [
    {
      value: NPM_VERSION_LATEST_ALIAS,
      label: `${NPM_VERSION_LATEST_ALIAS} (${latestVersion})`,
      isLatest: true,
      resolvedVersion: latestVersion,
      source: 'npm',
    },
    ...pinnedOptions,
  ];
}

function normalizeNpmCliVersionOptionSource(
  source,
  fallbackSource = 'fallback',
) {
  const normalizedSource = String(source || '').trim();
  if (
    normalizedSource === 'npm' ||
    normalizedSource === 'recorded' ||
    normalizedSource === 'retained' ||
    normalizedSource === 'fallback'
  ) {
    return normalizedSource;
  }
  return fallbackSource;
}

export function getNpmCliVersionOptionSource(
  option,
  fallbackSource = 'fallback',
) {
  return normalizeNpmCliVersionOptionSource(option?.source, fallbackSource);
}

function normalizeNpmCliVersionOption(
  option = {},
  latestVersion = '',
  fallbackSource = 'npm',
) {
  if (!option || typeof option !== 'object' || Array.isArray(option)) {
    return null;
  }
  const value = String(option.value || '').trim();
  if (!value) {
    return null;
  }
  const source = normalizeNpmCliVersionOptionSource(
    option.source,
    fallbackSource,
  );
  if (value === NPM_VERSION_LATEST_ALIAS) {
    const resolvedVersion = normalizeNpmCliVersionValue(
      option.resolvedVersion || option.resolved_version || latestVersion,
    );
    if (!resolvedVersion) {
      return null;
    }
    return {
      value: NPM_VERSION_LATEST_ALIAS,
      label:
        String(option.label || '').trim() ||
        `${NPM_VERSION_LATEST_ALIAS} (${resolvedVersion})`,
      isLatest: true,
      resolvedVersion,
      source,
    };
  }
  const normalizedValue = normalizeNpmCliVersionValue(value);
  if (!normalizedValue) {
    return null;
  }
  if (option.isLatest === true || option.is_latest === true) {
    return {
      value: NPM_VERSION_LATEST_ALIAS,
      label:
        String(option.label || '').trim() ||
        `${NPM_VERSION_LATEST_ALIAS} (${normalizedValue})`,
      isLatest: true,
      resolvedVersion: normalizedValue,
      source,
    };
  }
  return {
    value: normalizedValue,
    label: String(option.label || normalizedValue).trim() || normalizedValue,
    isLatest: false,
    resolvedVersion:
      normalizeNpmCliVersionValue(
        option.resolvedVersion || option.resolved_version || '',
      ) || normalizedValue,
    source,
  };
}

export function normalizeNpmCliVersionOptions(options) {
  const responseSource =
    options && typeof options === 'object' && !Array.isArray(options)
      ? normalizeNpmCliVersionOptionSource(options.source, 'npm')
      : 'npm';
  const rawOptions =
    options && typeof options === 'object' && !Array.isArray(options)
      ? options.options
      : options;
  if (!Array.isArray(rawOptions)) {
    return [];
  }
  const normalizedOptions = [];
  const seenValues = new Set();
  let latestVersion = '';
  rawOptions.forEach((option) => {
    const normalizedOption = normalizeNpmCliVersionOption(
      option,
      latestVersion,
      responseSource,
    );
    if (!normalizedOption || seenValues.has(normalizedOption.value)) {
      return;
    }
    if (normalizedOption.value === NPM_VERSION_LATEST_ALIAS) {
      latestVersion = normalizedOption.resolvedVersion;
    }
    seenValues.add(normalizedOption.value);
    normalizedOptions.push(normalizedOption);
  });
  const latestOption = normalizedOptions.find(
    (option) => option.value === NPM_VERSION_LATEST_ALIAS,
  );
  const pinnedOptions = normalizedOptions.filter(
    (option) => option.value !== NPM_VERSION_LATEST_ALIAS,
  );
  const orderedOptions = latestOption
    ? [latestOption, ...pinnedOptions]
    : pinnedOptions;
  return orderedOptions.slice(
    0,
    latestOption ? NPM_VERSION_OPTION_LIMIT + 1 : NPM_VERSION_OPTION_LIMIT,
  );
}

export function normalizeNpmCliVersionOptionsResult(payload) {
  const record =
    payload && typeof payload === 'object' && !Array.isArray(payload)
      ? payload
      : null;
  return {
    packageName: record
      ? String(record.package || record.packageName || '').trim() || undefined
      : undefined,
    source: record
      ? normalizeNpmCliVersionOptionSource(record.source, 'npm')
      : 'npm',
    refreshedAt: record
      ? String(record.refreshed_at || record.refreshedAt || '').trim() ||
        undefined
      : undefined,
    latestVersion: record
      ? normalizeNpmCliVersionValue(
          record.latest_version || record.latestVersion,
        ) || undefined
      : undefined,
    options: normalizeNpmCliVersionOptions(payload),
  };
}

export function getAiCodingCliVersionSource(profile) {
  const profileId = String(profile?.id || profile?.key || '').trim();
  return profile?.versionSource || AI_CODING_CLI_VERSION_SOURCES[profileId];
}

export function buildAiCodingCliVersionMeta(
  profile,
  version,
  platform = AI_CODING_CLI_DEFAULT_PLATFORM,
) {
  const baseProfileId = String(profile?.id || profile?.key || '').trim();
  const rawVersion = String(version || '').trim();
  const normalizedVersion =
    rawVersion === NPM_VERSION_LATEST_ALIAS
      ? NPM_VERSION_LATEST_ALIAS
      : normalizeNpmCliVersionValue(rawVersion);
  const versionSource = getAiCodingCliVersionSource(profile);
  if (!baseProfileId || !normalizedVersion || !versionSource?.packageName) {
    return null;
  }
  return {
    baseProfileId,
    packageName: versionSource.packageName,
    source: 'npm',
    version: normalizedVersion,
    platform: normalizeAiCodingCliPlatform(platform),
  };
}

export function buildVersionedAiCodingCliProfile(
  profile,
  version,
  resolvedVersion = '',
  platform = AI_CODING_CLI_DEFAULT_PLATFORM,
  source = 'npm',
) {
  const normalizedArgs = normalizeBuildVersionedAiCodingCliProfileArgs({
    resolvedVersion,
    platform,
    source,
    argCount: arguments.length,
  });
  const baseProfileId = String(profile?.id || profile?.key || '').trim();
  const rawVersion = String(version || '').trim();
  const normalizedVersion =
    rawVersion === NPM_VERSION_LATEST_ALIAS
      ? NPM_VERSION_LATEST_ALIAS
      : normalizeNpmCliVersionValue(rawVersion);
  const versionSource = getAiCodingCliVersionSource(profile);
  const effectiveVersion = normalizeNpmCliVersionValue(
    normalizedArgs.resolvedVersion ||
      (normalizedVersion === NPM_VERSION_LATEST_ALIAS
        ? versionSource?.fallbackVersion
        : normalizedVersion) ||
      '',
  );
  if (
    !baseProfileId ||
    !normalizedVersion ||
    !effectiveVersion ||
    !versionSource?.packageName
  ) {
    return profile;
  }
  const baseName = String(profile.name || baseProfileId).trim();
  const normalizedPlatform = normalizeAiCodingCliPlatform(
    normalizedArgs.platform,
  );
  const platformLabel = getAiCodingCliPlatformTokens(normalizedPlatform).label;
  const displayVersion =
    normalizedVersion === NPM_VERSION_LATEST_ALIAS
      ? `${NPM_VERSION_LATEST_ALIAS} (${effectiveVersion})`
      : normalizedVersion;
  return {
    ...profile,
    id: `${baseProfileId}@${normalizedVersion}`,
    key: `${baseProfileId}@${normalizedVersion}`,
    name: `${baseName} ${displayVersion} ${platformLabel}`,
    headers: buildAiCodingCliHeaders(
      baseProfileId,
      effectiveVersion,
      baseProfileId === 'codex-cli' ? { Originator: 'codex-tui' } : {},
      normalizedPlatform,
    ),
    versionMeta: {
      ...buildAiCodingCliVersionMeta(
        profile,
        normalizedVersion,
        normalizedPlatform,
      ),
      source: normalizedArgs.source,
    },
  };
}

function normalizeBuildVersionedAiCodingCliProfileArgs({
  resolvedVersion,
  platform,
  source,
  argCount,
}) {
  const normalizedResolvedVersion = String(resolvedVersion || '').trim();
  const normalizedPlatform = String(platform || '').trim();
  const normalizedSource = String(source || '').trim();
  if (
    normalizedResolvedVersion === 'npm' ||
    normalizedResolvedVersion === 'recorded' ||
    normalizedResolvedVersion === 'retained' ||
    normalizedResolvedVersion === 'fallback'
  ) {
    return {
      resolvedVersion: argCount >= 4 ? normalizedPlatform : '',
      platform:
        argCount >= 5
          ? normalizedSource || AI_CODING_CLI_DEFAULT_PLATFORM
          : AI_CODING_CLI_DEFAULT_PLATFORM,
      source: normalizedResolvedVersion,
    };
  }
  return {
    resolvedVersion: normalizedResolvedVersion,
    platform: normalizedPlatform || AI_CODING_CLI_DEFAULT_PLATFORM,
    source: normalizedSource || 'npm',
  };
}

function serializeAiCodingCliSnapshot(profile) {
  if (!profile) {
    return null;
  }
  return {
    id: profile.id,
    key: profile.key,
    name: profile.name,
    headers: profile.headers || {},
    versionMeta: profile.versionMeta || profile.version_meta || null,
  };
}

export function areAiCodingCliProfileSnapshotsEqual(left, right) {
  return (
    JSON.stringify(serializeAiCodingCliSnapshot(left)) ===
    JSON.stringify(serializeAiCodingCliSnapshot(right))
  );
}

export function buildRefreshedAiCodingCliProfileSnapshot({
  profile,
  selectedProfile,
  selectedVersion,
  selectedPlatform,
  options = [],
}) {
  const versionSource = getAiCodingCliVersionSource(profile);
  if (!versionSource?.packageName || !Array.isArray(options)) {
    return null;
  }
  const currentMeta =
    selectedProfile?.versionMeta || selectedProfile?.version_meta || {};
  const normalizedVersion =
    String(currentMeta.version || selectedVersion || '').trim() ||
    NPM_VERSION_LATEST_ALIAS;
  const selectedOption = options.find(
    (option) => option.value === normalizedVersion,
  );
  if (!selectedOption) {
    return null;
  }
  return buildVersionedAiCodingCliProfile(
    profile,
    normalizedVersion,
    selectedOption.resolvedVersion || versionSource.fallbackVersion,
    currentMeta.platform || selectedPlatform || AI_CODING_CLI_DEFAULT_PLATFORM,
    getNpmCliVersionOptionSource(selectedOption, 'npm'),
  );
}

export async function fetchNpmCliVersionOptionsResult(
  packageName,
  requestImpl,
  options = {},
) {
  if (typeof requestImpl !== 'function') {
    throw new Error('request implementation is required');
  }
  const timeoutMs = Number.isFinite(options.timeoutMs)
    ? options.timeoutMs
    : NPM_VERSION_REFRESH_TIMEOUT_MS;
  let response;
  try {
    const requestPath = options.refresh
      ? '/api/channel/npm_version_options/refresh'
      : '/api/channel/npm_version_options';
    response = await requestImpl(requestPath, {
      params: {
        package: String(packageName || '').trim(),
      },
      timeout: timeoutMs,
      skipErrorHandler: true,
      disableDuplicate: true,
    });
  } catch (error) {
    throw normalizeNpmCliVersionLoadError(error);
  }
  const payload = response?.data || {};
  const status = Number(response?.status);
  const errorCode = normalizeNpmCliVersionPayloadErrorCode(payload, status);
  if (payload.success !== true) {
    throw new NpmCliVersionLoadError(
      errorCode,
      payload.message || 'failed to load npm versions',
    );
  }
  const result = normalizeNpmCliVersionOptionsResult(payload.data);
  if (result.options.length === 0) {
    throw new NpmCliVersionLoadError(
      NPM_VERSION_EMPTY_ERROR_CODE,
      'empty npm version options',
    );
  }
  return result;
}

export async function fetchNpmCliVersionOptions(
  packageName,
  requestImpl,
  options = {},
) {
  const result = await fetchNpmCliVersionOptionsResult(
    packageName,
    requestImpl,
    options,
  );
  return result.options;
}
