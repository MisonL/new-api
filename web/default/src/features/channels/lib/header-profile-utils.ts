export type HeaderProfileMode = 'fixed' | 'round_robin' | 'random'
export type HeaderProfileScope = 'builtin' | 'user' | 'missing'
export type HeaderProfileCategory =
  | 'browser'
  | 'ai_coding_cli'
  | 'api_sdk'
  | 'custom'

export type HeaderProfileVersionMeta = {
  baseProfileId: string
  packageName: string
  source: string
  version: string
  platform: string
}

export type HeaderProfile = {
  id: string
  name: string
  category: HeaderProfileCategory
  scope: HeaderProfileScope
  readonly?: boolean
  description?: string
  headers: Record<string, string>
  previewText: string
  passthroughRequired?: boolean
  versionSource?: {
    packageName: string
    fallbackVersion: string
  } | null
  versionMeta?: HeaderProfileVersionMeta
  missing?: boolean
}

export type HeaderProfileStrategy = {
  enabled: boolean
  mode: HeaderProfileMode
  selectedProfileIds: string[]
  profiles: HeaderProfile[]
}

export type NpmCliVersionOption = {
  value: string
  label: string
  isLatest: boolean
  resolvedVersion: string
}

const NPM_VERSION_OPTION_LIMIT = 5
export const NPM_VERSION_LATEST_ALIAS = 'latest'
export const AI_CODING_CLI_DEFAULT_PLATFORM = 'macos-x64'

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
] as const

const AI_CODING_CLI_VERSION_SOURCES = {
  'codex-cli': { packageName: '@openai/codex', fallbackVersion: '0.134.0' },
  'claude-code': {
    packageName: '@anthropic-ai/claude-code',
    fallbackVersion: '2.1.153',
  },
  'gemini-cli': {
    packageName: '@google/gemini-cli',
    fallbackVersion: '0.44.0',
  },
  'qwen-code': {
    packageName: '@qwen-code/qwen-code',
    fallbackVersion: '0.16.2',
  },
  droid: { packageName: 'droid', fallbackVersion: '0.135.0' },
} as const

const BASE_HEADER_PROFILES: HeaderProfile[] = [
  {
    id: 'chrome-macos',
    name: 'Chrome macOS',
    category: 'browser',
    scope: 'builtin',
    readonly: true,
    headers: {
      Accept: 'text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8',
      'Accept-Language': 'en-US,en;q=0.9',
      'Sec-CH-UA':
        '"Google Chrome";v="148", "Chromium";v="148", "Not.A/Brand";v="24"',
      'Sec-CH-UA-Mobile': '?0',
      'Sec-CH-UA-Platform': '"macOS"',
      'User-Agent':
        'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36',
    },
    previewText: '',
  },
  {
    id: 'codex-cli',
    name: 'Codex CLI',
    category: 'ai_coding_cli',
    scope: 'builtin',
    readonly: true,
    versionSource: AI_CODING_CLI_VERSION_SOURCES['codex-cli'],
    headers: buildAiCodingCliHeaders('codex-cli', '0.134.0', 'macos-x64'),
    previewText: '',
  },
  {
    id: 'codex-desktop',
    name: 'Codex Desktop',
    category: 'ai_coding_cli',
    scope: 'builtin',
    readonly: true,
    headers: {
      'User-Agent':
        'Codex Desktop/0.133.0-alpha.1 (Mac OS 15.7.3; x86_64) unknown (Codex Desktop; 26.519.41501)',
      Originator: 'Codex Desktop',
    },
    previewText: '',
  },
  {
    id: 'claude-code',
    name: 'Claude Code',
    category: 'ai_coding_cli',
    scope: 'builtin',
    readonly: true,
    versionSource: AI_CODING_CLI_VERSION_SOURCES['claude-code'],
    headers: buildAiCodingCliHeaders('claude-code', '2.1.153', 'macos-x64'),
    previewText: '',
  },
  {
    id: 'gemini-cli',
    name: 'Gemini CLI',
    category: 'ai_coding_cli',
    scope: 'builtin',
    readonly: true,
    versionSource: AI_CODING_CLI_VERSION_SOURCES['gemini-cli'],
    headers: buildAiCodingCliHeaders('gemini-cli', '0.44.0', 'macos-x64'),
    previewText: '',
  },
  {
    id: 'qwen-code',
    name: 'Qwen Code',
    category: 'ai_coding_cli',
    scope: 'builtin',
    readonly: true,
    versionSource: AI_CODING_CLI_VERSION_SOURCES['qwen-code'],
    headers: buildAiCodingCliHeaders('qwen-code', '0.16.2', 'macos-x64'),
    previewText: '',
  },
  {
    id: 'droid',
    name: 'Droid CLI',
    category: 'ai_coding_cli',
    scope: 'builtin',
    readonly: true,
    versionSource: AI_CODING_CLI_VERSION_SOURCES.droid,
    headers: buildAiCodingCliHeaders('droid', '0.135.0', 'macos-x64'),
    previewText: '',
  },
  {
    id: 'postman-runtime',
    name: 'Postman Runtime',
    category: 'api_sdk',
    scope: 'builtin',
    readonly: true,
    headers: {
      Accept: '*/*',
      'Cache-Control': 'no-cache',
      'Postman-Token': '00000000-0000-0000-0000-000000000000',
      'User-Agent': 'PostmanRuntime/7.54.0',
    },
    previewText: '',
  },
]

export const BUILTIN_HEADER_PROFILES =
  BASE_HEADER_PROFILES.map(normalizeProfile)

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function parseSettings(
  settingsText?: string,
  options: { strict?: boolean } = {}
): Record<string, unknown> {
  if (!settingsText?.trim()) return {}
  try {
    const parsed = JSON.parse(settingsText)
    if (isRecord(parsed)) return parsed
  } catch (error) {
    if (options.strict) {
      throw new Error('Channel settings must be valid JSON', { cause: error })
    }
  }
  if (options.strict) {
    throw new Error('Channel settings must be a JSON object')
  }
  return {}
}

function buildPreviewText(headers: Record<string, string>): string {
  return Object.entries(headers)
    .map(([key, value]) => `${key}: ${value}`)
    .join('\n')
}

function normalizeVersionValue(version: unknown): string {
  const normalized = String(version || '').trim()
  if (
    !normalized ||
    normalized === NPM_VERSION_LATEST_ALIAS ||
    normalized.length > 64 ||
    !/^[0-9][0-9A-Za-z.+-]*$/.test(normalized)
  ) {
    return ''
  }
  return normalized
}

export function normalizeAiCodingCliPlatform(platform: unknown): string {
  const normalized = String(platform || '').trim()
  return AI_CODING_CLI_PLATFORM_OPTIONS.some(
    (option) => option.value === normalized
  )
    ? normalized
    : AI_CODING_CLI_DEFAULT_PLATFORM
}

function platformTokens(platform: string) {
  return (
    AI_CODING_CLI_PLATFORM_OPTIONS.find(
      (option) => option.value === normalizeAiCodingCliPlatform(platform)
    ) || AI_CODING_CLI_PLATFORM_OPTIONS[0]
  )
}

function buildAiCodingCliUserAgent(
  profileId: string,
  version: string,
  platform: string
): string {
  const tokens = platformTokens(platform)
  switch (profileId) {
    case 'codex-cli':
      return `codex-tui/${version} (${tokens.codexOS}; ${tokens.codexArch}) ghostty/1.3.1 (codex-tui; ${version})`
    case 'claude-code':
      return `claude-cli/${version} (external, sdk-cli)`
    case 'gemini-cli':
      return `GeminiCLI/${version}/gemini-3.1-pro-preview (${tokens.geminiOS}; ${tokens.geminiArch}; terminal)`
    case 'qwen-code':
      return `QwenCode/${version} (${tokens.qwenOS}; ${tokens.qwenArch})`
    case 'droid':
      return `factory-cli/${version}`
    default:
      return ''
  }
}

function buildAiCodingCliHeaders(
  profileId: string,
  version: string,
  platform: string
): Record<string, string> {
  const userAgent = buildAiCodingCliUserAgent(profileId, version, platform)
  return profileId === 'codex-cli'
    ? { Originator: 'codex-tui', 'User-Agent': userAgent }
    : { 'User-Agent': userAgent }
}

export function getProfileBaseId(profileId: string): string {
  const normalizedProfileId = String(profileId || '').trim()
  const separatorIndex = normalizedProfileId.indexOf('@')
  if (
    separatorIndex <= 0 ||
    separatorIndex !== normalizedProfileId.lastIndexOf('@')
  ) {
    return normalizedProfileId
  }
  return normalizedProfileId.slice(0, separatorIndex).trim()
}

export function normalizeHeaderProfileMode(mode: unknown): HeaderProfileMode {
  return mode === 'round_robin' || mode === 'random' ? mode : 'fixed'
}

export function normalizeSelectedProfileIds(ids: unknown): string[] {
  if (!Array.isArray(ids)) return []
  return Array.from(
    new Set(ids.map((id) => String(id || '').trim()).filter(Boolean))
  )
}

export function normalizeNpmCliVersionOptions(
  options: unknown
): NpmCliVersionOption[] {
  if (!Array.isArray(options)) return []
  const normalized = options
    .map((option) => {
      if (!isRecord(option)) return null
      const value = String(option.value || '').trim()
      const rawResolved = option.resolvedVersion || option.resolved_version
      if (value === NPM_VERSION_LATEST_ALIAS) {
        const resolvedVersion = normalizeVersionValue(rawResolved)
        if (!resolvedVersion) return null
        return {
          value,
          label:
            String(option.label || '').trim() ||
            `${NPM_VERSION_LATEST_ALIAS} (${resolvedVersion})`,
          isLatest: true,
          resolvedVersion,
        }
      }
      const pinnedVersion = normalizeVersionValue(value)
      if (!pinnedVersion) return null
      return {
        value: pinnedVersion,
        label: String(option.label || pinnedVersion).trim(),
        isLatest: false,
        resolvedVersion: normalizeVersionValue(rawResolved) || pinnedVersion,
      }
    })
    .filter((option): option is NpmCliVersionOption => option !== null)
  const seen = new Set<string>()
  return normalized
    .filter((option) => {
      if (seen.has(option.value)) return false
      seen.add(option.value)
      return true
    })
    .sort((left, right) => {
      if (left.value === NPM_VERSION_LATEST_ALIAS) return -1
      if (right.value === NPM_VERSION_LATEST_ALIAS) return 1
      return 0
    })
    .slice(0, NPM_VERSION_OPTION_LIMIT)
}

export function latestFallbackOption(
  fallbackVersion: string
): NpmCliVersionOption {
  return {
    value: NPM_VERSION_LATEST_ALIAS,
    label: fallbackVersion
      ? `${NPM_VERSION_LATEST_ALIAS} (${fallbackVersion})`
      : NPM_VERSION_LATEST_ALIAS,
    isLatest: true,
    resolvedVersion: fallbackVersion,
  }
}

export function normalizeProfile(profile: unknown): HeaderProfile {
  const record = isRecord(profile) ? profile : {}
  const id = String(record.id || record.key || '').trim()
  const headers = isRecord(record.headers)
    ? Object.fromEntries(
        Object.entries(record.headers).map(([key, value]) => [
          String(key).trim(),
          String(value ?? ''),
        ])
      )
    : {}
  const category = String(record.category || record.group || 'custom')
  const versionMeta = normalizeVersionMeta(record, id)
  const normalized: HeaderProfile = {
    id,
    name: String(record.name || id).trim(),
    category: ['browser', 'ai_coding_cli', 'api_sdk'].includes(category)
      ? (category as HeaderProfileCategory)
      : 'custom',
    scope: record.scope === 'builtin' ? 'builtin' : 'user',
    readonly: record.readonly === true || record.readOnly === true,
    description: String(record.description || '').trim(),
    passthroughRequired:
      record.passthroughRequired === true ||
      record.passthrough_required === true,
    versionSource: isRecord(record.versionSource)
      ? {
          packageName: String(record.versionSource.packageName || '').trim(),
          fallbackVersion: String(
            record.versionSource.fallbackVersion || ''
          ).trim(),
        }
      : AI_CODING_CLI_VERSION_SOURCES[
          id as keyof typeof AI_CODING_CLI_VERSION_SOURCES
        ] || null,
    versionMeta,
    headers,
    previewText: buildPreviewText(headers),
  }
  return normalized
}

function normalizeVersionMeta(
  record: Record<string, unknown>,
  profileId: string
): HeaderProfileVersionMeta | undefined {
  const rawMeta = record.versionMeta || record.version_meta
  if (!isRecord(rawMeta)) return undefined
  const baseProfileId = String(
    rawMeta.baseProfileId ||
      rawMeta.base_profile_id ||
      getProfileBaseId(profileId)
  ).trim()
  const source =
    String(rawMeta.source || '').trim() ||
    (String(rawMeta.version || '').trim() ? 'npm' : '')
  const version = String(rawMeta.version || '').trim()
  const packageName = String(
    rawMeta.packageName ||
      rawMeta.package_name ||
      AI_CODING_CLI_VERSION_SOURCES[
        baseProfileId as keyof typeof AI_CODING_CLI_VERSION_SOURCES
      ]?.packageName ||
      ''
  ).trim()
  if (!baseProfileId || !version || !packageName) return undefined
  return {
    baseProfileId,
    packageName,
    source,
    version,
    platform: normalizeAiCodingCliPlatform(rawMeta.platform),
  }
}

export function buildVersionedAiCodingCliProfile(
  profile: HeaderProfile,
  version: string,
  resolvedVersion: string,
  platform: string,
  source = 'npm'
): HeaderProfile {
  const versionSource = profile.versionSource
  const normalizedVersion =
    version === NPM_VERSION_LATEST_ALIAS
      ? NPM_VERSION_LATEST_ALIAS
      : normalizeVersionValue(version)
  const effectiveVersion = normalizeVersionValue(
    resolvedVersion ||
      (normalizedVersion === NPM_VERSION_LATEST_ALIAS
        ? versionSource?.fallbackVersion
        : normalizedVersion)
  )
  if (!versionSource?.packageName || !normalizedVersion || !effectiveVersion) {
    return profile
  }
  const normalizedPlatform = normalizeAiCodingCliPlatform(platform)
  const platformLabel = platformTokens(normalizedPlatform).label
  const displayVersion =
    normalizedVersion === NPM_VERSION_LATEST_ALIAS
      ? `${NPM_VERSION_LATEST_ALIAS} (${effectiveVersion})`
      : normalizedVersion
  return normalizeProfile({
    ...profile,
    id: `${profile.id}@${normalizedVersion}`,
    name: `${profile.name} ${displayVersion} ${platformLabel}`,
    headers: buildAiCodingCliHeaders(
      profile.id,
      effectiveVersion,
      normalizedPlatform
    ),
    version_meta: {
      base_profile_id: profile.id,
      package_name: versionSource.packageName,
      source,
      version: normalizedVersion,
      platform: normalizedPlatform,
    },
  })
}

export function getHeaderProfileStrategyFromSettings(
  settingsText?: string
): HeaderProfileStrategy | null {
  const settings = parseSettings(settingsText)
  const rawStrategy = settings.header_profile_strategy
  if (!isRecord(rawStrategy)) return null
  return {
    enabled: rawStrategy.enabled === true,
    mode: normalizeHeaderProfileMode(rawStrategy.mode),
    selectedProfileIds: normalizeSelectedProfileIds(
      rawStrategy.selected_profile_ids || rawStrategy.selectedProfileIds
    ),
    profiles: Array.isArray(rawStrategy.profiles)
      ? rawStrategy.profiles
          .map(normalizeProfile)
          .filter((profile) => profile.id)
      : [],
  }
}

export function disableEmptyHeaderProfileStrategy(
  strategy: HeaderProfileStrategy | null
): HeaderProfileStrategy | null {
  if (!strategy) return null
  if (!strategy.enabled || strategy.selectedProfileIds.length > 0) {
    return strategy
  }
  return { ...strategy, enabled: false, selectedProfileIds: [], profiles: [] }
}

export function buildSelectedProfileItems(
  selectedProfileIds: string[],
  profiles: HeaderProfile[],
  snapshots: HeaderProfile[] = []
): HeaderProfile[] {
  const profileMap = new Map<string, HeaderProfile>()
  for (const profile of [
    ...BUILTIN_HEADER_PROFILES,
    ...profiles,
    ...snapshots,
  ]) {
    if (profile.id) profileMap.set(profile.id, normalizeProfile(profile))
  }
  return selectedProfileIds.map((id) => {
    const direct = profileMap.get(id)
    if (direct) return direct
    const separatorIndex = id.indexOf('@')
    const hasSingleVersionSeparator =
      separatorIndex > 0 && separatorIndex === id.lastIndexOf('@')
    const baseId = hasSingleVersionSeparator
      ? id.slice(0, separatorIndex).trim()
      : ''
    const version = hasSingleVersionSeparator
      ? id.slice(separatorIndex + 1).trim()
      : ''
    const baseProfile = profileMap.get(baseId)
    if (baseProfile?.versionSource && version) {
      return buildVersionedAiCodingCliProfile(
        baseProfile,
        version,
        version === NPM_VERSION_LATEST_ALIAS
          ? baseProfile.versionSource.fallbackVersion
          : version,
        AI_CODING_CLI_DEFAULT_PLATFORM
      )
    }
    return {
      id,
      name: id,
      category: 'custom',
      scope: 'missing',
      headers: {},
      previewText: '',
      missing: true,
    }
  })
}

function serializeProfile(profile: HeaderProfile): Record<string, unknown> {
  const snapshot: Record<string, unknown> = {
    id: profile.id,
    name: profile.name,
    category: profile.category,
    scope: profile.scope,
    readonly: profile.readonly === true,
    description: profile.description || '',
    headers: profile.headers,
  }
  if (profile.passthroughRequired) snapshot.passthrough_required = true
  if (profile.versionMeta) {
    snapshot.version_meta = {
      base_profile_id: profile.versionMeta.baseProfileId,
      package_name: profile.versionMeta.packageName,
      source: profile.versionMeta.source,
      version: profile.versionMeta.version,
      platform: normalizeAiCodingCliPlatform(profile.versionMeta.platform),
    }
  }
  return snapshot
}

export function buildHeaderProfileStrategySettings(
  settingsText: string | undefined,
  strategy: HeaderProfileStrategy | null
): string {
  const settings = parseSettings(settingsText, { strict: true })
  if (!strategy) {
    delete settings.header_profile_strategy
    return JSON.stringify(settings)
  }
  const selectedProfileIds = normalizeSelectedProfileIds(
    strategy.selectedProfileIds
  )
  settings.header_profile_strategy = {
    enabled: strategy.enabled === true,
    mode: normalizeHeaderProfileMode(strategy.mode),
    selected_profile_ids: selectedProfileIds,
    profiles: strategy.profiles
      .filter((profile) => selectedProfileIds.includes(profile.id))
      .map(serializeProfile),
  }
  return JSON.stringify(settings)
}
