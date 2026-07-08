export type HeaderProfileMode = 'fixed' | 'round_robin' | 'random'
export type HeaderProfileScope = 'builtin' | 'user' | 'missing'
export type HeaderProfileCategory =
  'browser' | 'ai_coding_cli' | 'api_sdk' | 'custom'

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

export type HeaderProfileVersionSelection = {
  baseProfileId: string
  packageName?: string
  selectedVersion?: string
  selectedPlatform?: string
  options: NpmCliVersionOption[]
}

export type NpmCliVersionOption = {
  value: string
  label: string
  isLatest: boolean
  resolvedVersion: string
  source?: string
}

export type NpmCliVersionOptionsResult = {
  packageName?: string
  source: string
  refreshedAt?: string
  latestVersion?: string
  options: NpmCliVersionOption[]
}

const NPM_VERSION_OPTION_LIMIT = 5
export const NPM_VERSION_LOAD_ERROR_CODE = 'npm_version_load_failed'
export const NPM_VERSION_AUTH_ERROR_CODE = 'npm_version_auth_required'
export const NPM_VERSION_FORBIDDEN_CODE = 'npm_version_forbidden'
export const NPM_VERSION_RATE_LIMITED_CODE = 'npm_version_rate_limited'
export const NPM_VERSION_EMPTY_ERROR_CODE = 'npm_version_empty'
export const NPM_VERSION_NOT_RECORDED_CODE = 'npm_version_not_recorded'
export const NPM_VERSION_LATEST_ALIAS = 'latest'
export const AI_CODING_CLI_DEFAULT_PLATFORM = 'macos-x64'

export class NpmVersionLoadError extends Error {
  code: string

  constructor(code: string, message: string) {
    super(message)
    this.name = 'NpmVersionLoadError'
    this.code = code
  }
}

function statusFromNpmVersionError(error: unknown): number {
  const record = error as {
    response?: { status?: number | string }
    status?: number | string
  }
  return Number(record?.response?.status ?? record?.status)
}

function isForbiddenNpmVersionMessage(message: unknown): boolean {
  const normalized = String(message || '')
    .trim()
    .toLowerCase()
  return (
    normalized.includes('insufficient privilege') ||
    normalized.includes('insufficient privileges') ||
    normalized.includes('权限不足') ||
    normalized.includes('權限不足')
  )
}

export function normalizeNpmVersionPayloadErrorCode(
  payload: { code?: string; message?: string } = {},
  status?: number
): string {
  if (status === 401) return NPM_VERSION_AUTH_ERROR_CODE
  if (status === 403) return NPM_VERSION_FORBIDDEN_CODE
  if (status === 429) return NPM_VERSION_RATE_LIMITED_CODE
  const code = String(payload.code || '').trim()
  if (code) return code
  if (isForbiddenNpmVersionMessage(payload.message)) {
    return NPM_VERSION_FORBIDDEN_CODE
  }
  return NPM_VERSION_LOAD_ERROR_CODE
}

export function normalizeNpmVersionLoadError(error: unknown) {
  if (error instanceof NpmVersionLoadError) return error
  const record = error as {
    message?: string
    response?: { data?: { message?: string } }
  }
  const status = statusFromNpmVersionError(error)
  return new NpmVersionLoadError(
    normalizeNpmVersionPayloadErrorCode(
      { message: record?.response?.data?.message || record?.message },
      status
    ),
    record?.response?.data?.message ||
      record?.message ||
      'failed to load npm versions'
  )
}

export function getNpmVersionLoadErrorCode(error: unknown) {
  return error instanceof NpmVersionLoadError
    ? error.code
    : normalizeNpmVersionLoadError(error).code
}

export function getNpmVersionLoadErrorText(
  t: (key: string) => string,
  code?: string
) {
  if (code === NPM_VERSION_AUTH_ERROR_CODE) {
    return t('Session expired, sign in again to load npm versions')
  }
  if (code === NPM_VERSION_FORBIDDEN_CODE) {
    return t('Admin access required to load npm versions')
  }
  if (code === NPM_VERSION_RATE_LIMITED_CODE) {
    return t('Too many npm version requests, try again later')
  }
  if (code === NPM_VERSION_EMPTY_ERROR_CODE) {
    return t('No npm versions recorded yet, using built-in versions')
  }
  if (code === NPM_VERSION_NOT_RECORDED_CODE) {
    return t('No npm versions recorded yet, using built-in versions')
  }
  return t('npm version load failed, using built-in versions')
}

export function buildNpmVersionRequestConfig(
  packageName: string,
  options: { timeout?: number } = {}
) {
  return {
    params: { package: packageName.trim() },
    skipBusinessError: true,
    skipErrorHandler: true,
    disableDuplicate: true,
    ...(typeof options.timeout === 'number'
      ? { timeout: options.timeout }
      : {}),
  } as Record<string, unknown>
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
] as const

const AI_CODING_CLI_VERSION_SOURCES = {
  'codex-cli': { packageName: '@openai/codex', fallbackVersion: '0.142.4' },
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
  droid: { packageName: 'droid', fallbackVersion: '0.161.0' },
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
        '"Google Chrome";v="150", "Chromium";v="150", "Not.A/Brand";v="24"',
      'Sec-CH-UA-Mobile': '?0',
      'Sec-CH-UA-Platform': '"macOS"',
      'User-Agent':
        'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.7871.47 Safari/537.36',
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
    headers: buildAiCodingCliHeaders('codex-cli', '0.142.4', 'macos-x64'),
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
        'Codex Desktop/0.142.4 (Mac OS 15.7.3; x86_64) unknown (Codex Desktop; 26.623.70822)',
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
    headers: buildAiCodingCliHeaders('claude-code', '2.1.197', 'macos-x64'),
    previewText: '',
  },
  {
    id: 'gemini-cli',
    name: 'Gemini CLI',
    category: 'ai_coding_cli',
    scope: 'builtin',
    readonly: true,
    versionSource: AI_CODING_CLI_VERSION_SOURCES['gemini-cli'],
    headers: buildAiCodingCliHeaders('gemini-cli', '0.49.0', 'macos-x64'),
    previewText: '',
  },
  {
    id: 'qwen-code',
    name: 'Qwen Code',
    category: 'ai_coding_cli',
    scope: 'builtin',
    readonly: true,
    versionSource: AI_CODING_CLI_VERSION_SOURCES['qwen-code'],
    headers: buildAiCodingCliHeaders('qwen-code', '0.19.3', 'macos-x64'),
    previewText: '',
  },
  {
    id: 'droid',
    name: 'Droid CLI',
    category: 'ai_coding_cli',
    scope: 'builtin',
    readonly: true,
    versionSource: AI_CODING_CLI_VERSION_SOURCES.droid,
    headers: buildAiCodingCliHeaders('droid', '0.161.0', 'macos-x64'),
    previewText: '',
  },
  {
    id: 'agy',
    name: 'Antigravity CLI',
    category: 'ai_coding_cli',
    scope: 'builtin',
    readonly: true,
    description:
      '固定请求头静态快照来自 Antigravity CLI 原始请求身份；此模板仅固定客户端身份。若后续确认需要动态头，应基于真实客户端抓包在高级参数覆盖中手动配置 pass_headers。',
    headers: {
      'User-Agent':
        'antigravity/cli/1.0.14 (aidev_client; os_type=darwin; arch=amd64)',
    },
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

function normalizePassHeaderValue(value: unknown): string[] {
  if (value == null) return []
  if (Array.isArray(value)) {
    return value.map((item) => String(item || '').trim()).filter(Boolean)
  }
  if (typeof value === 'string') {
    const trimmed = value.trim()
    if (!trimmed) return []
    if (trimmed.startsWith('[') || trimmed.startsWith('{')) {
      try {
        return normalizePassHeaderValue(JSON.parse(trimmed))
      } catch {
        return []
      }
    }
    return trimmed
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean)
  }
  if (isRecord(value)) {
    if (value.headers !== undefined)
      return normalizePassHeaderValue(value.headers)
    if (value.names !== undefined) return normalizePassHeaderValue(value.names)
    if (value.header !== undefined)
      return normalizePassHeaderValue([value.header])
  }
  return []
}

function isUserAgentHeaderName(header: string): boolean {
  return (
    String(header || '')
      .trim()
      .toLowerCase() === 'user-agent'
  )
}

export function clearParamOverridePreservingUserAgentPassHeaders(
  paramOverrideText: unknown
): string {
  const rawText =
    typeof paramOverrideText === 'string' ? paramOverrideText.trim() : ''
  if (!rawText) return ''
  let parsed: unknown
  try {
    parsed = JSON.parse(rawText)
  } catch {
    return ''
  }
  if (!isRecord(parsed)) return ''
  const operations = Array.isArray(parsed.operations) ? parsed.operations : []
  const userAgentOperations: Array<Record<string, unknown>> = []
  operations.forEach((operation) => {
    if (!isRecord(operation) || operation.mode !== 'pass_headers') return
    const headers = normalizePassHeaderValue(operation.value)
    if (!headers.some(isUserAgentHeaderName)) return
    userAgentOperations.push({
      ...operation,
      value: ['User-Agent'],
      keep_origin: operation.keep_origin !== false,
    })
  })
  if (userAgentOperations.length === 0) return ''
  return JSON.stringify({ operations: userAgentOperations })
}

export function normalizeNpmCliVersionOptions(
  options: unknown
): NpmCliVersionOption[] {
  const responseSource =
    Array.isArray(options) || !isRecord(options)
      ? 'npm'
      : normalizeNpmCliVersionOptionSource(options.source, 'npm')
  const rawOptions =
    Array.isArray(options) || !isRecord(options) ? options : options.options
  if (!Array.isArray(rawOptions)) return []
  const normalized: NpmCliVersionOption[] = []
  for (const option of rawOptions) {
    if (!isRecord(option)) continue
    const value = String(option.value || '').trim()
    const rawResolved = option.resolvedVersion || option.resolved_version
    const source = normalizeNpmCliVersionOptionSource(
      option.source,
      responseSource
    )
    if (value === NPM_VERSION_LATEST_ALIAS) {
      const resolvedVersion = normalizeVersionValue(rawResolved)
      if (!resolvedVersion) continue
      normalized.push({
        value,
        label:
          String(option.label || '').trim() ||
          `${NPM_VERSION_LATEST_ALIAS} (${resolvedVersion})`,
        isLatest: true,
        resolvedVersion,
        source,
      })
      continue
    }
    const pinnedVersion = normalizeVersionValue(value)
    if (!pinnedVersion) continue
    if (option.isLatest === true || option.is_latest === true) {
      normalized.push({
        value: NPM_VERSION_LATEST_ALIAS,
        label:
          String(option.label || '').trim() ||
          `${NPM_VERSION_LATEST_ALIAS} (${pinnedVersion})`,
        isLatest: true,
        resolvedVersion: pinnedVersion,
        source,
      })
      continue
    }
    normalized.push({
      value: pinnedVersion,
      label: String(option.label || pinnedVersion).trim(),
      isLatest: false,
      resolvedVersion: normalizeVersionValue(rawResolved) || pinnedVersion,
      source,
    })
  }
  const seen = new Set<string>()
  const deduped = normalized
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
  const hasLatest = deduped.some(
    (option) => option.value === NPM_VERSION_LATEST_ALIAS
  )
  return deduped.slice(
    0,
    hasLatest ? NPM_VERSION_OPTION_LIMIT + 1 : NPM_VERSION_OPTION_LIMIT
  )
}

export function normalizeNpmCliVersionOptionsResult(
  payload: unknown
): NpmCliVersionOptionsResult {
  const record = isRecord(payload) ? payload : null
  const source = record
    ? normalizeNpmCliVersionOptionSource(record.source, 'npm')
    : 'npm'
  return {
    packageName: record
      ? String(record.package || record.packageName || '').trim() || undefined
      : undefined,
    source,
    refreshedAt: record
      ? String(record.refreshed_at || record.refreshedAt || '').trim() ||
        undefined
      : undefined,
    latestVersion: record
      ? normalizeVersionValue(record.latest_version || record.latestVersion)
      : undefined,
    options: normalizeNpmCliVersionOptions(payload),
  }
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
    source: 'fallback',
  }
}

export function buildNpmCliFallbackVersionOptions(
  fallbackVersion: string
): NpmCliVersionOption[] {
  const normalizedVersion = normalizeVersionValue(fallbackVersion)
  if (!normalizedVersion) return [latestFallbackOption('')]
  return [
    latestFallbackOption(normalizedVersion),
    {
      value: normalizedVersion,
      label: normalizedVersion,
      isLatest: false,
      resolvedVersion: normalizedVersion,
      source: 'fallback',
    },
  ]
}

export function normalizeNpmCliVersionOptionSource(
  source: unknown,
  fallbackSource = 'fallback'
): string {
  const normalized = String(source || '').trim()
  if (
    normalized === 'npm' ||
    normalized === 'recorded' ||
    normalized === 'retained' ||
    normalized === 'fallback'
  ) {
    return normalized
  }
  return fallbackSource
}

export function getNpmCliVersionOptionSource(
  option: NpmCliVersionOption | undefined,
  fallbackSource = 'fallback'
): string {
  return normalizeNpmCliVersionOptionSource(option?.source, fallbackSource)
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

function profileSnapshotsEqual(
  left: HeaderProfile | undefined,
  right: HeaderProfile
) {
  return left
    ? JSON.stringify(serializeProfile(left)) ===
        JSON.stringify(serializeProfile(right))
    : false
}

function findVersionedSnapshot(
  strategy: HeaderProfileStrategy,
  selectedId: string,
  baseId: string
) {
  return (
    strategy.profiles.find((profile) => profile.id === selectedId) ||
    strategy.profiles.find(
      (profile) => profile.versionMeta?.baseProfileId === baseId
    )
  )
}

export function refreshSelectedVersionedProfileSnapshots(
  strategy: HeaderProfileStrategy,
  profiles: HeaderProfile[],
  selections: HeaderProfileVersionSelection[]
): HeaderProfileStrategy {
  if (strategy.selectedProfileIds.length === 0 || selections.length === 0) {
    return strategy
  }
  const profileMap = new Map(profiles.map((profile) => [profile.id, profile]))
  const selectionMap = new Map(
    selections.map((selection) => [selection.baseProfileId, selection])
  )
  const refreshedProfiles: HeaderProfile[] = []
  let changed = false
  const selectedProfileIds = strategy.selectedProfileIds.map((selectedId) => {
    const baseId = getProfileBaseId(selectedId)
    const profile = profileMap.get(baseId)
    const selection = selectionMap.get(baseId)
    if (!profile?.versionSource || !selection?.options.length) return selectedId
    if (
      selection.packageName &&
      selection.packageName !== profile.versionSource.packageName
    ) {
      return selectedId
    }
    const currentSnapshot = findVersionedSnapshot(strategy, selectedId, baseId)
    const selectedVersion =
      currentSnapshot?.versionMeta?.version ||
      selection.selectedVersion ||
      NPM_VERSION_LATEST_ALIAS
    const selectedOption = selection.options.find(
      (option) => option.value === selectedVersion
    )
    if (!selectedOption) return selectedId
    const refreshedProfile = buildVersionedAiCodingCliProfile(
      profile,
      selectedVersion,
      selectedOption.resolvedVersion || profile.versionSource.fallbackVersion,
      currentSnapshot?.versionMeta?.platform ||
        selection.selectedPlatform ||
        AI_CODING_CLI_DEFAULT_PLATFORM,
      getNpmCliVersionOptionSource(selectedOption, 'npm')
    )
    if (!refreshedProfile.versionMeta) return selectedId
    refreshedProfiles.push(refreshedProfile)
    if (
      refreshedProfile.id !== selectedId ||
      !profileSnapshotsEqual(currentSnapshot, refreshedProfile)
    ) {
      changed = true
    }
    return refreshedProfile.id
  })
  if (!changed) return strategy
  return {
    ...strategy,
    selectedProfileIds,
    profiles: buildSelectedProfileItems(selectedProfileIds, profiles, [
      ...strategy.profiles,
      ...refreshedProfiles,
    ]).filter((profile) => !profile.missing),
  }
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
