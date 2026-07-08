import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Check,
  GripVertical,
  Info,
  Loader2,
  RefreshCw,
  SquareStack,
  Trash2,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  AI_CODING_CLI_DEFAULT_PLATFORM,
  AI_CODING_CLI_PLATFORM_OPTIONS,
  BUILTIN_HEADER_PROFILES,
  NPM_VERSION_EMPTY_ERROR_CODE,
  NPM_VERSION_LATEST_ALIAS,
  NpmVersionLoadError,
  type HeaderProfile,
  type HeaderProfileMode,
  type HeaderProfileStrategy,
  type NpmCliVersionOption,
  buildNpmCliFallbackVersionOptions,
  buildNpmVersionRequestConfig,
  buildSelectedProfileItems,
  buildVersionedAiCodingCliProfile,
  getProfileBaseId,
  getNpmCliVersionOptionSource,
  getNpmVersionLoadErrorCode,
  getNpmVersionLoadErrorText,
  normalizeHeaderProfileMode,
  normalizeNpmCliVersionOptionsResult,
  normalizeNpmVersionLoadError,
  normalizeNpmVersionPayloadErrorCode,
  refreshSelectedVersionedProfileSnapshots,
  type NpmCliVersionOptionsResult,
} from '../lib/header-profile-utils'

type HeaderProfileStrategyEditorProps = {
  value: HeaderProfileStrategy | null
  customProfiles?: HeaderProfile[]
  onChange: (strategy: HeaderProfileStrategy | null) => void
  disabled?: boolean
}

type VersionState = {
  loading: boolean
  error?: string
  options: NpmCliVersionOption[]
  selectedVersion: string
  selectedPlatform: string
  packageName?: string
  source?: string
  refreshedAt?: string
  latestVersion?: string
}

type NpmVersionDiagnostic = {
  package?: string
  source?: string
  refreshed_at?: string
  cache_age_ms?: number
  latest_version?: string
  option_count?: number
  recorded?: boolean
  last_error_scope?: string
  last_error?: {
    code?: string
    message?: string
    source?: string
    updated_at?: string
  } | null
}

const CLI_VERSION_OPTIONS_CACHE_TTL_MS = 10 * 60 * 1000
const CLI_VERSION_REFRESH_TIMEOUT_MS = 10_000
const VERSION_CACHE = new Map<
  string,
  { expiresAt: number; request: Promise<NpmCliVersionOptionsResult> }
>()

function isMultiTemplateMode(mode: HeaderProfileMode): boolean {
  return mode === 'round_robin' || mode === 'random'
}

function defaultStrategy(): HeaderProfileStrategy {
  return {
    enabled: false,
    mode: 'fixed',
    selectedProfileIds: [],
    profiles: [],
  }
}

function fetchNpmVersionOptions(
  packageName: string,
  options: { force?: boolean } = {}
) {
  const cacheKey = packageName.trim()
  const now = Date.now()
  const cached = VERSION_CACHE.get(cacheKey)
  if (!options.force && cached && cached.expiresAt > now) return cached.request
  if (cached) VERSION_CACHE.delete(cacheKey)
  const request = api
    .get(
      '/api/channel/npm_version_options',
      buildNpmVersionRequestConfig(cacheKey, {
        timeout: CLI_VERSION_REFRESH_TIMEOUT_MS,
      })
    )
    .then((response) => {
      const payload = response.data || {}
      const status = Number(response.status)
      const errorCode = normalizeNpmVersionPayloadErrorCode(payload, status)
      if (payload.success !== true) {
        throw new NpmVersionLoadError(
          errorCode,
          payload.message || 'failed to load npm versions'
        )
      }
      const result = normalizeNpmCliVersionOptionsResult(payload.data)
      if (result.options.length === 0) {
        throw new NpmVersionLoadError(
          NPM_VERSION_EMPTY_ERROR_CODE,
          'empty npm version options'
        )
      }
      return result
    })
    .catch((error) => {
      if (VERSION_CACHE.get(cacheKey)?.request === request) {
        VERSION_CACHE.delete(cacheKey)
      }
      throw normalizeNpmVersionLoadError(error)
    })
  VERSION_CACHE.set(cacheKey, {
    expiresAt: now + CLI_VERSION_OPTIONS_CACHE_TTL_MS,
    request,
  })
  return request
}

function refreshNpmVersionOptions(packageName: string) {
  const cacheKey = packageName.trim()
  const request = api
    .post(
      '/api/channel/npm_version_options/refresh',
      undefined,
      buildNpmVersionRequestConfig(cacheKey, {
        timeout: CLI_VERSION_REFRESH_TIMEOUT_MS,
      })
    )
    .then((response) => {
      const payload = response.data || {}
      const status = Number(response.status)
      const errorCode = normalizeNpmVersionPayloadErrorCode(payload, status)
      if (payload.success !== true) {
        throw new NpmVersionLoadError(
          errorCode,
          payload.message || 'failed to refresh npm versions'
        )
      }
      const result = normalizeNpmCliVersionOptionsResult(payload.data)
      if (result.options.length === 0) {
        throw new NpmVersionLoadError(
          NPM_VERSION_EMPTY_ERROR_CODE,
          'empty npm version options'
        )
      }
      return result
    })
    .catch((error) => {
      if (VERSION_CACHE.get(cacheKey)?.request === request) {
        VERSION_CACHE.delete(cacheKey)
      }
      throw normalizeNpmVersionLoadError(error)
    })
  VERSION_CACHE.set(cacheKey, {
    expiresAt: Date.now() + CLI_VERSION_OPTIONS_CACHE_TTL_MS,
    request,
  })
  return request
}

function getVersionSourceLabel(t: (key: string) => string, source?: string) {
  switch (source) {
    case 'recorded':
      return t('backend cache')
    case 'npm':
      return t('npm refresh')
    case 'retained':
      return t('retained selection')
    case 'fallback':
      return t('built-in fallback')
    case 'missing':
      return t('not recorded')
    default:
      return t('unknown source')
  }
}

function formatVersionRefreshedAt(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString()
}

function getLastErrorScopeLabel(
  t: (key: string) => string,
  scope?: string
): string {
  if (scope === 'process') return t('current process')
  if (scope === 'recorded') return t('persisted record')
  return ''
}

function formatVersionCacheAge(value?: number) {
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0) {
    return ''
  }
  if (value < 1000) return `${Math.round(value)} ms`
  const seconds = Math.round(value / 1000)
  if (seconds < 60) return `${seconds} s`
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `${minutes} min`
  return `${Math.round(minutes / 60)} h`
}

function formatDiagnosticLastError(
  t: (key: string) => string,
  item: NpmVersionDiagnostic
) {
  const lastError = item.last_error
  if (!lastError) {
    return ''
  }
  return [
    lastError.code,
    lastError.source,
    getLastErrorScopeLabel(t, item.last_error_scope),
    formatVersionRefreshedAt(lastError.updated_at),
    lastError.message,
  ]
    .map((part) => String(part || '').trim())
    .filter(Boolean)
    .join(' / ')
}

function normalizeNpmVersionDiagnostics(payload: unknown) {
  const record = payload as { packages?: unknown }
  if (!Array.isArray(record?.packages)) return []
  return record.packages.filter(
    (item): item is NpmVersionDiagnostic =>
      !!item && typeof item === 'object' && !Array.isArray(item)
  )
}

function invalidateNpmVersionOptions(packageName: string) {
  VERSION_CACHE.delete(packageName.trim())
}

function ensureSelectedVersionOption(
  options: NpmCliVersionOption[],
  selectedVersion: string
): NpmCliVersionOption[] {
  const normalizedVersion = String(selectedVersion || '').trim()
  if (
    !normalizedVersion ||
    normalizedVersion === NPM_VERSION_LATEST_ALIAS ||
    options.some((option) => option.value === normalizedVersion)
  ) {
    return options
  }
  return [
    ...options,
    {
      value: normalizedVersion,
      label: normalizedVersion,
      isLatest: false,
      resolvedVersion: normalizedVersion,
      source: 'retained',
    },
  ]
}

function buildInitialVersionStates(
  profiles: HeaderProfile[],
  savedProfiles: HeaderProfile[] = []
): Record<string, VersionState> {
  const states = Object.fromEntries(
    profiles
      .filter((profile) => profile.versionSource?.packageName)
      .map((profile) => {
        const fallbackOptions = buildNpmCliFallbackVersionOptions(
          profile.versionSource?.fallbackVersion || ''
        )
        return [
          profile.id,
          {
            loading: true,
            options: fallbackOptions,
            selectedVersion: NPM_VERSION_LATEST_ALIAS,
            selectedPlatform: AI_CODING_CLI_DEFAULT_PLATFORM,
            packageName: profile.versionSource?.packageName,
            source: 'fallback',
            latestVersion: profile.versionSource?.fallbackVersion,
          },
        ]
      })
  )
  for (const savedProfile of savedProfiles) {
    const meta = savedProfile.versionMeta
    if (!meta?.baseProfileId || !states[meta.baseProfileId]) continue
    const selectedVersion = meta.version || NPM_VERSION_LATEST_ALIAS
    states[meta.baseProfileId] = {
      ...states[meta.baseProfileId],
      options: ensureSelectedVersionOption(
        states[meta.baseProfileId].options,
        selectedVersion
      ),
      selectedVersion,
      selectedPlatform: meta.platform || AI_CODING_CLI_DEFAULT_PLATFORM,
    }
  }
  return states
}

function buildSavedVersionStates(
  savedProfiles: HeaderProfile[] = []
): Record<string, Pick<VersionState, 'selectedVersion' | 'selectedPlatform'>> {
  const states: Record<
    string,
    Pick<VersionState, 'selectedVersion' | 'selectedPlatform'>
  > = {}
  for (const savedProfile of savedProfiles) {
    const meta = savedProfile.versionMeta
    if (!meta?.baseProfileId) continue
    states[meta.baseProfileId] = {
      selectedVersion: meta.version || NPM_VERSION_LATEST_ALIAS,
      selectedPlatform: meta.platform || AI_CODING_CLI_DEFAULT_PLATFORM,
    }
  }
  return states
}

function useVersionStates(
  profiles: HeaderProfile[],
  savedProfiles: HeaderProfile[] = []
) {
  const [versionStates, setVersionStates] = useState<
    Record<string, VersionState>
  >(() => buildInitialVersionStates(profiles, savedProfiles))
  const requestSeqRef = useRef<Record<string, number>>({})
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  function startVersionRequest(profileId: string) {
    const nextSeq = (requestSeqRef.current[profileId] || 0) + 1
    requestSeqRef.current[profileId] = nextSeq
    return nextSeq
  }

  function isLatestVersionRequest(profileId: string, requestSeq: number) {
    return requestSeqRef.current[profileId] === requestSeq
  }

  useEffect(() => {
    let active = true
    const savedVersionStates = buildSavedVersionStates(savedProfiles)
    for (const profile of profiles) {
      const versionSource = profile.versionSource
      if (!versionSource?.packageName) continue
      const fallbackOptions = buildNpmCliFallbackVersionOptions(
        versionSource.fallbackVersion
      )
      const requestSeq = startVersionRequest(profile.id)
      fetchNpmVersionOptions(versionSource.packageName)
        .then((result) => {
          if (!active || !isLatestVersionRequest(profile.id, requestSeq)) return
          setVersionStates((current) => ({
            ...current,
            [profile.id]: (() => {
              const options = result.options
              const selectedVersion =
                savedVersionStates[profile.id]?.selectedVersion ||
                current[profile.id]?.selectedVersion ||
                options[0]?.value ||
                NPM_VERSION_LATEST_ALIAS
              return {
                loading: false,
                error: '',
                options: ensureSelectedVersionOption(options, selectedVersion),
                selectedVersion,
                selectedPlatform:
                  savedVersionStates[profile.id]?.selectedPlatform ||
                  current[profile.id]?.selectedPlatform ||
                  AI_CODING_CLI_DEFAULT_PLATFORM,
                packageName: versionSource.packageName,
                source: result.source,
                refreshedAt: result.refreshedAt,
                latestVersion: result.latestVersion,
              }
            })(),
          }))
        })
        .catch((error) => {
          if (!active || !isLatestVersionRequest(profile.id, requestSeq)) return
          setVersionStates((current) => ({
            ...current,
            [profile.id]: (() => {
              const selectedVersion =
                savedVersionStates[profile.id]?.selectedVersion ||
                current[profile.id]?.selectedVersion ||
                NPM_VERSION_LATEST_ALIAS
              const options =
                current[profile.id]?.packageName ===
                  versionSource.packageName &&
                current[profile.id]?.options?.length > 1
                  ? current[profile.id].options
                  : fallbackOptions
              const reusingCurrentOptions =
                current[profile.id]?.packageName ===
                  versionSource.packageName &&
                current[profile.id]?.options?.length > 1
              return {
                loading: false,
                error: getNpmVersionLoadErrorCode(error),
                options: ensureSelectedVersionOption(options, selectedVersion),
                selectedVersion,
                selectedPlatform:
                  savedVersionStates[profile.id]?.selectedPlatform ||
                  current[profile.id]?.selectedPlatform ||
                  AI_CODING_CLI_DEFAULT_PLATFORM,
                packageName: versionSource.packageName,
                source: reusingCurrentOptions
                  ? current[profile.id]?.source
                  : 'fallback',
                refreshedAt: reusingCurrentOptions
                  ? current[profile.id]?.refreshedAt
                  : undefined,
                latestVersion: reusingCurrentOptions
                  ? current[profile.id]?.latestVersion
                  : versionSource.fallbackVersion,
              }
            })(),
          }))
        })
    }
    return () => {
      active = false
    }
  }, [profiles, savedProfiles])

  function reloadVersionOptions(profile: HeaderProfile) {
    const versionSource = profile.versionSource
    if (!versionSource?.packageName) return
    if (!mountedRef.current) return
    const fallbackOptions = buildNpmCliFallbackVersionOptions(
      versionSource.fallbackVersion
    )
    invalidateNpmVersionOptions(versionSource.packageName)
    const requestSeq = startVersionRequest(profile.id)
    setVersionStates((current) => {
      const currentState = current[profile.id]
      const selectedVersion =
        currentState?.selectedVersion || NPM_VERSION_LATEST_ALIAS
      const canReuseCurrentOptions =
        currentState?.packageName === versionSource.packageName &&
        currentState?.options?.length > 1
      return {
        ...current,
        [profile.id]: {
          loading: true,
          error: '',
          options: ensureSelectedVersionOption(
            canReuseCurrentOptions ? currentState.options : fallbackOptions,
            selectedVersion
          ),
          selectedVersion,
          selectedPlatform:
            currentState?.selectedPlatform || AI_CODING_CLI_DEFAULT_PLATFORM,
          packageName: versionSource.packageName,
          source: canReuseCurrentOptions ? currentState.source : 'fallback',
          refreshedAt: canReuseCurrentOptions
            ? currentState.refreshedAt
            : undefined,
          latestVersion: canReuseCurrentOptions
            ? currentState.latestVersion
            : versionSource.fallbackVersion,
        },
      }
    })
    refreshNpmVersionOptions(versionSource.packageName)
      .then((result) => {
        if (
          !mountedRef.current ||
          !isLatestVersionRequest(profile.id, requestSeq)
        ) {
          return
        }
        setVersionStates((current) => {
          const currentState = current[profile.id]
          const options = result.options
          const selectedVersion =
            currentState?.selectedVersion ||
            options[0]?.value ||
            NPM_VERSION_LATEST_ALIAS
          return {
            ...current,
            [profile.id]: {
              loading: false,
              error: '',
              options: ensureSelectedVersionOption(options, selectedVersion),
              selectedVersion,
              selectedPlatform:
                currentState?.selectedPlatform ||
                AI_CODING_CLI_DEFAULT_PLATFORM,
              packageName: versionSource.packageName,
              source: result.source,
              refreshedAt: result.refreshedAt,
              latestVersion: result.latestVersion,
            },
          }
        })
      })
      .catch((error) => {
        if (
          !mountedRef.current ||
          !isLatestVersionRequest(profile.id, requestSeq)
        ) {
          return
        }
        setVersionStates((current) => {
          const currentState = current[profile.id]
          const selectedVersion =
            currentState?.selectedVersion || NPM_VERSION_LATEST_ALIAS
          const canReuseCurrentOptions =
            currentState?.packageName === versionSource.packageName &&
            currentState?.options?.length > 1
          return {
            ...current,
            [profile.id]: {
              loading: false,
              error: getNpmVersionLoadErrorCode(error),
              options: ensureSelectedVersionOption(
                canReuseCurrentOptions ? currentState.options : fallbackOptions,
                selectedVersion
              ),
              selectedVersion,
              selectedPlatform:
                currentState?.selectedPlatform ||
                AI_CODING_CLI_DEFAULT_PLATFORM,
              packageName: versionSource.packageName,
              source: canReuseCurrentOptions ? currentState?.source : 'fallback',
              refreshedAt: canReuseCurrentOptions
                ? currentState?.refreshedAt
                : undefined,
              latestVersion: canReuseCurrentOptions
                ? currentState?.latestVersion
                : versionSource.fallbackVersion,
            },
          }
        })
      })
  }

  return [versionStates, setVersionStates, reloadVersionOptions] as const
}

function selectedProfileIdsAfterToggle(
  strategy: HeaderProfileStrategy,
  profileId: string
): string[] {
  const existing = strategy.selectedProfileIds
  if (existing.includes(profileId)) {
    return existing.filter((id) => id !== profileId)
  }
  if (strategy.mode === 'fixed') return [profileId]
  return [...existing, profileId]
}

function moveSelectedProfile(
  selectedIds: string[],
  sourceId: string,
  direction: 'up' | 'down'
): string[] {
  const sourceIndex = selectedIds.indexOf(sourceId)
  if (sourceIndex < 0) return selectedIds
  const targetIndex = direction === 'up' ? sourceIndex - 1 : sourceIndex + 1
  if (targetIndex < 0 || targetIndex >= selectedIds.length) return selectedIds
  const next = [...selectedIds]
  const [item] = next.splice(sourceIndex, 1)
  next.splice(targetIndex, 0, item)
  return next
}

export function HeaderProfileStrategyEditor({
  value,
  customProfiles = [],
  onChange,
  disabled = false,
}: HeaderProfileStrategyEditorProps) {
  const { t } = useTranslation()
  const [libraryOpen, setLibraryOpen] = useState(false)
  const [diagnosticsOpen, setDiagnosticsOpen] = useState(false)
  const [diagnosticsLoading, setDiagnosticsLoading] = useState(false)
  const [diagnosticsError, setDiagnosticsError] = useState('')
  const [diagnostics, setDiagnostics] = useState<NpmVersionDiagnostic[]>([])
  const diagnosticsRequestSeqRef = useRef(0)
  const diagnosticsMountedRef = useRef(true)
  const strategy = value || defaultStrategy()
  const selectableProfiles = useMemo(
    () => [...BUILTIN_HEADER_PROFILES, ...customProfiles],
    [customProfiles]
  )
  const [versionStates, setVersionStates, reloadVersionOptions] =
    useVersionStates(BUILTIN_HEADER_PROFILES, strategy.profiles)

  const selectedItems = useMemo(
    () =>
      buildSelectedProfileItems(
        strategy.selectedProfileIds,
        selectableProfiles,
        strategy.profiles
      ),
    [selectableProfiles, strategy.profiles, strategy.selectedProfileIds]
  )
  const selectedCount = selectedItems.length
  const mode = normalizeHeaderProfileMode(strategy.mode)
  const modeText =
    mode === 'round_robin'
      ? t('Round robin')
      : mode === 'random'
        ? t('Random')
        : t('Fixed')

  useEffect(() => {
    diagnosticsMountedRef.current = true
    return () => {
      diagnosticsMountedRef.current = false
    }
  }, [])

  function emit(next: HeaderProfileStrategy | null) {
    onChange(next)
  }

  function loadNpmVersionDiagnostics() {
    const requestSeq = diagnosticsRequestSeqRef.current + 1
    diagnosticsRequestSeqRef.current = requestSeq
    setDiagnosticsOpen(true)
    setDiagnosticsLoading(true)
    setDiagnosticsError('')
    api
      .get('/api/channel/npm_version_options/diagnostics', {
        timeout: CLI_VERSION_REFRESH_TIMEOUT_MS,
        skipBusinessError: true,
        skipErrorHandler: true,
        disableDuplicate: true,
      } as Record<string, unknown>)
      .then((response) => {
        const payload = response.data || {}
        const status = Number(response.status)
        const errorCode = normalizeNpmVersionPayloadErrorCode(payload, status)
        if (payload.success !== true) {
          throw new NpmVersionLoadError(
            errorCode,
            payload.message || 'failed to load npm version diagnostics'
          )
        }
        if (
          !diagnosticsMountedRef.current ||
          diagnosticsRequestSeqRef.current !== requestSeq
        ) {
          return
        }
        setDiagnostics(normalizeNpmVersionDiagnostics(payload.data))
      })
      .catch((error) => {
        if (
          !diagnosticsMountedRef.current ||
          diagnosticsRequestSeqRef.current !== requestSeq
        ) {
          return
        }
        setDiagnosticsError(getNpmVersionLoadErrorCode(error))
      })
      .finally(() => {
        if (
          !diagnosticsMountedRef.current ||
          diagnosticsRequestSeqRef.current !== requestSeq
        ) {
          return
        }
        setDiagnosticsLoading(false)
      })
  }

  function updateStrategy(patch: Partial<HeaderProfileStrategy>) {
    const nextMode = normalizeHeaderProfileMode(patch.mode ?? strategy.mode)
    const selectedProfileIds =
      nextMode === 'fixed'
        ? (patch.selectedProfileIds ?? strategy.selectedProfileIds).slice(0, 1)
        : (patch.selectedProfileIds ?? strategy.selectedProfileIds)
    const nextSelectedItems = buildSelectedProfileItems(
      selectedProfileIds,
      selectableProfiles,
      patch.profiles ?? strategy.profiles
    ).filter((profile) => !profile.missing)
    emit({
      enabled: patch.enabled ?? strategy.enabled,
      mode: nextMode,
      selectedProfileIds,
      profiles: nextSelectedItems,
    })
  }

  function versionedProfileForSelection(
    profile: HeaderProfile,
    overrides: Partial<VersionState> = {}
  ): HeaderProfile {
    if (!profile.versionSource) return profile
    const state = versionStates[profile.id]
    const selectedVersion =
      overrides.selectedVersion ||
      state?.selectedVersion ||
      NPM_VERSION_LATEST_ALIAS
    const options =
      state?.options ||
      buildNpmCliFallbackVersionOptions(profile.versionSource.fallbackVersion)
    const selectedOption = options.find(
      (option) => option.value === selectedVersion
    )
    const selectedOptionSource = selectedOption
      ? getNpmCliVersionOptionSource(selectedOption, 'npm')
      : ''
    const source =
      selectedOptionSource === 'retained'
        ? selectedOptionSource
        : state && !state.loading && !state.error && selectedOption
          ? selectedOptionSource
          : 'fallback'
    return buildVersionedAiCodingCliProfile(
      profile,
      selectedVersion,
      selectedOption?.resolvedVersion || profile.versionSource.fallbackVersion,
      overrides.selectedPlatform ||
        state?.selectedPlatform ||
        AI_CODING_CLI_DEFAULT_PLATFORM,
      source
    )
  }

  function toggleProfile(profile: HeaderProfile) {
    const selectableProfile = versionedProfileForSelection(profile)
    const baseId = profile.id
    const nextSelectedIds = strategy.selectedProfileIds.filter(
      (id) => id === selectableProfile.id || getProfileBaseId(id) !== baseId
    )
    const nextStrategy = {
      ...strategy,
      enabled: true,
      selectedProfileIds: selectedProfileIdsAfterToggle(
        { ...strategy, selectedProfileIds: nextSelectedIds },
        selectableProfile.id
      ),
    }
    const snapshots = buildSelectedProfileItems(
      nextStrategy.selectedProfileIds,
      selectableProfiles,
      [...strategy.profiles, selectableProfile]
    ).filter((item) => !item.missing)
    updateStrategy({
      enabled: true,
      selectedProfileIds: nextStrategy.selectedProfileIds,
      profiles: snapshots,
    })
  }

  function updateVersionState(
    profile: HeaderProfile,
    patch: Partial<VersionState>
  ) {
    setVersionStates((current) => ({
      ...current,
      [profile.id]: {
        loading: current[profile.id]?.loading ?? false,
        error: current[profile.id]?.error || '',
        options:
          current[profile.id]?.options ||
          (profile.versionSource
            ? buildNpmCliFallbackVersionOptions(
                profile.versionSource.fallbackVersion
              )
            : []),
        selectedVersion:
          current[profile.id]?.selectedVersion || NPM_VERSION_LATEST_ALIAS,
        selectedPlatform:
          current[profile.id]?.selectedPlatform ||
          AI_CODING_CLI_DEFAULT_PLATFORM,
        packageName:
          current[profile.id]?.packageName ||
          profile.versionSource?.packageName,
        ...patch,
      },
    }))
  }

  function updateSelectedVersionedProfile(
    profile: HeaderProfile,
    overrides: Partial<VersionState> = {}
  ) {
    const currentlySelected = strategy.selectedProfileIds.some(
      (id) => getProfileBaseId(id) === profile.id
    )
    if (!currentlySelected) return
    const nextProfile = versionedProfileForSelection(profile, overrides)
    const nextSelectedIds = strategy.selectedProfileIds.map((id) =>
      getProfileBaseId(id) === profile.id ? nextProfile.id : id
    )
    const nextSnapshots = buildSelectedProfileItems(
      nextSelectedIds,
      selectableProfiles,
      [...strategy.profiles, nextProfile]
    ).filter((item) => !item.missing)
    updateStrategy({
      selectedProfileIds: nextSelectedIds,
      profiles: nextSnapshots,
    })
  }

  useEffect(() => {
    const loadedSelections = Object.entries(versionStates)
      .filter(([baseProfileId, state]) => {
        const profile = selectableProfiles.find(
          (item) => item.id === baseProfileId
        )
        return (
          profile?.versionSource?.packageName &&
          state.packageName === profile.versionSource.packageName &&
          !state.loading &&
          !state.error &&
          state.options.length > 0
        )
      })
      .map(([baseProfileId, state]) => ({
        baseProfileId,
        packageName: state.packageName,
        selectedVersion: state.selectedVersion,
        selectedPlatform: state.selectedPlatform,
        options: state.options,
      }))
    if (loadedSelections.length === 0) return
    const nextStrategy = refreshSelectedVersionedProfileSnapshots(
      strategy,
      selectableProfiles,
      loadedSelections
    )
    if (nextStrategy !== strategy) onChange(nextStrategy)
  }, [onChange, selectableProfiles, strategy, versionStates])

  return (
    <div className='space-y-3 rounded-lg border p-4'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
        <div className='space-y-1'>
          <div className='flex items-center gap-2'>
            <SquareStack className='text-muted-foreground h-4 w-4' />
            <h4 className='text-sm font-medium'>
              {t('Client header templates')}
            </h4>
            <Badge variant={strategy.enabled ? 'default' : 'secondary'}>
              {strategy.enabled ? t('Enabled') : t('Disabled')}
            </Badge>
            {strategy.enabled && <Badge variant='outline'>{modeText}</Badge>}
          </div>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Select one client profile for fixed mode, or multiple profiles for round robin and random mode.'
            )}
          </p>
        </div>
        <div className='flex items-center gap-2'>
          <span className='text-muted-foreground text-xs'>{t('Enable')}</span>
          <Switch
            checked={strategy.enabled}
            disabled={disabled}
            onCheckedChange={(checked) =>
              updateStrategy({ enabled: checked, mode })
            }
          />
        </div>
      </div>

      {strategy.enabled && (
        <div className='grid gap-3 sm:grid-cols-[minmax(0,1fr)_180px]'>
          <div className='rounded-md border p-3'>
            <div className='flex items-center justify-between gap-3'>
              <div className='min-w-0'>
                <div className='text-sm font-medium'>
                  {t('Current selection')}
                </div>
                <p className='text-muted-foreground mt-1 text-xs'>
                  {selectedCount === 0
                    ? t('No template selected')
                    : mode === 'round_robin'
                      ? t('{{count}} templates, used in order', {
                          count: selectedCount,
                        })
                      : mode === 'random'
                        ? t('{{count}} random candidates', {
                            count: selectedCount,
                          })
                        : selectedItems[0]?.name || t('Selected template')}
                </p>
              </div>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={disabled}
                onClick={() => setLibraryOpen(true)}
              >
                {selectedCount > 0 && isMultiTemplateMode(mode)
                  ? t('Manage')
                  : selectedCount > 0
                    ? t('Change')
                    : t('Select')}
              </Button>
            </div>

            {selectedItems.length > 0 && (
              <div className='mt-3 space-y-2'>
                {selectedItems.map((profile, index) => (
                  <div
                    key={profile.id}
                    className='flex items-center justify-between gap-2 rounded-md border px-3 py-2'
                  >
                    <div className='flex min-w-0 items-center gap-2'>
                      {mode === 'round_robin' && (
                        <GripVertical className='text-muted-foreground h-4 w-4' />
                      )}
                      <div className='min-w-0'>
                        <div className='truncate text-sm font-medium'>
                          {profile.name}
                        </div>
                        <div className='text-muted-foreground text-xs'>
                          {mode === 'round_robin'
                            ? t('Order {{index}}', { index: index + 1 })
                            : profile.category}
                        </div>
                      </div>
                    </div>
                    <div className='flex shrink-0 items-center gap-1'>
                      {mode === 'round_robin' && selectedItems.length > 1 && (
                        <>
                          <Button
                            type='button'
                            variant='ghost'
                            size='sm'
                            disabled={index === 0 || disabled}
                            onClick={() =>
                              updateStrategy({
                                selectedProfileIds: moveSelectedProfile(
                                  strategy.selectedProfileIds,
                                  profile.id,
                                  'up'
                                ),
                              })
                            }
                          >
                            {t('Up')}
                          </Button>
                          <Button
                            type='button'
                            variant='ghost'
                            size='sm'
                            disabled={
                              index === selectedItems.length - 1 || disabled
                            }
                            onClick={() =>
                              updateStrategy({
                                selectedProfileIds: moveSelectedProfile(
                                  strategy.selectedProfileIds,
                                  profile.id,
                                  'down'
                                ),
                              })
                            }
                          >
                            {t('Down')}
                          </Button>
                        </>
                      )}
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Button
                            type='button'
                            variant='ghost'
                            size='icon-sm'
                            disabled={disabled}
                            onClick={() =>
                              updateStrategy({
                                selectedProfileIds:
                                  strategy.selectedProfileIds.filter(
                                    (id) => id !== profile.id
                                  ),
                              })
                            }
                          >
                            <Trash2 className='h-4 w-4' />
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>{t('Remove')}</TooltipContent>
                      </Tooltip>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>

          <div className='space-y-2'>
            <label className='text-sm font-medium'>{t('Use mode')}</label>
            <Select
              value={mode}
              disabled={disabled}
              onValueChange={(nextMode) =>
                updateStrategy({
                  mode: normalizeHeaderProfileMode(nextMode),
                })
              }
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='fixed'>{t('Fixed')}</SelectItem>
                <SelectItem value='round_robin'>{t('Round robin')}</SelectItem>
                <SelectItem value='random'>{t('Random')}</SelectItem>
              </SelectContent>
            </Select>
            <p className='text-muted-foreground text-xs'>
              {mode === 'fixed'
                ? t('Fixed mode keeps exactly one template.')
                : mode === 'round_robin'
                  ? t('Round robin follows the selected order.')
                  : t('Random mode picks one selected template per request.')}
            </p>
          </div>
        </div>
      )}

      {strategy.enabled && selectedCount === 0 && (
        <Alert>
          <Info className='h-4 w-4' />
          <AlertDescription>
            {t('Template policy is enabled but no template has been selected.')}
          </AlertDescription>
        </Alert>
      )}

      <Dialog open={libraryOpen} onOpenChange={setLibraryOpen}>
        <DialogContent className='max-h-[86vh] overflow-hidden sm:max-w-3xl'>
          <DialogHeader>
            <DialogTitle>{t('Select client header templates')}</DialogTitle>
            <DialogDescription>
              {isMultiTemplateMode(mode)
                ? t('Multiple templates can be selected for this mode.')
                : t('Fixed mode uses one selected template.')}
            </DialogDescription>
          </DialogHeader>

          <div className='flex items-center justify-between gap-3 rounded-md border px-3 py-2'>
            <div className='min-w-0'>
              <div className='text-sm font-medium'>
                {t('npm version diagnostics')}
              </div>
              <div className='text-muted-foreground truncate text-xs'>
                {t(
                  'Read-only backend cache status; failure reasons include current process and persisted records'
                )}
              </div>
            </div>
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={diagnosticsLoading}
              onClick={loadNpmVersionDiagnostics}
            >
              {diagnosticsLoading ? t('Loading') : t('Diagnostics')}
            </Button>
          </div>

          {diagnosticsOpen && (
            <div className='max-h-32 overflow-auto rounded-md border px-3 py-2 text-xs'>
              {diagnosticsError ? (
                <div className='text-warning'>
                  {getNpmVersionLoadErrorText(t, diagnosticsError)}
                </div>
              ) : diagnostics.length === 0 ? (
                <div className='text-muted-foreground'>
                  {diagnosticsLoading ? t('Loading') : t('No diagnostics')}
                </div>
              ) : (
                <div className='space-y-1'>
                  {diagnostics.map((item) => {
                    const refreshedAt = formatVersionRefreshedAt(
                      item.refreshed_at
                    )
                    const cacheAge = formatVersionCacheAge(item.cache_age_ms)
                    const lastErrorText = formatDiagnosticLastError(t, item)
                    return (
                      <div
                        key={item.package}
                        className='grid gap-1 sm:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)_minmax(0,1fr)]'
                      >
                        <span className='truncate font-medium'>
                          {item.package}
                        </span>
                        <span className='text-muted-foreground truncate'>
                          {getVersionSourceLabel(t, item.source)} /{' '}
                          {item.latest_version || '-'} /{' '}
                          {item.option_count ?? 0}
                          {cacheAge ? ` / ${cacheAge}` : ''}
                        </span>
                        <span
                          title={lastErrorText || refreshedAt || undefined}
                          className={cn(
                            'truncate',
                            item.last_error
                              ? 'text-warning'
                            : 'text-muted-foreground'
                          )}
                        >
                          {item.last_error ? lastErrorText || '-' : refreshedAt || '-'}
                        </span>
                      </div>
                    )
                  })}
                </div>
              )}
            </div>
          )}

          <div className='max-h-[58vh] space-y-3 overflow-y-auto pr-1'>
            {[
              {
                key: 'builtin',
                title: t('Built-in templates'),
                profiles: BUILTIN_HEADER_PROFILES,
              },
              {
                key: 'custom',
                title: t('My templates'),
                profiles: customProfiles,
              },
            ].map((group) => (
              <div key={group.key} className='space-y-2'>
                <div className='text-muted-foreground text-xs font-medium'>
                  {group.title}
                </div>
                {group.profiles.length === 0 ? (
                  <div className='text-muted-foreground rounded-md border border-dashed px-3 py-2 text-xs'>
                    {group.key === 'custom'
                      ? t('No custom templates')
                      : t('No templates')}
                  </div>
                ) : (
                  group.profiles.map((profile) => {
                    const selected = strategy.selectedProfileIds.some(
                      (id) =>
                        id === profile.id || getProfileBaseId(id) === profile.id
                    )
                    const versionSource = profile.versionSource
                    const versionState = versionStates[profile.id]
                    const effectiveVersionState = versionState
                    const versionOptions =
                      effectiveVersionState?.options ||
                      (versionSource
                        ? buildNpmCliFallbackVersionOptions(
                            versionSource.fallbackVersion
                          )
                        : [])
                    const selectedVersion =
                      effectiveVersionState?.selectedVersion ||
                      NPM_VERSION_LATEST_ALIAS
                    const selectedOption = versionOptions.find(
                      (option) => option.value === selectedVersion
                    )
                    const sourceForDisplay =
                      selectedOption?.source ||
                      effectiveVersionState?.source ||
                      'fallback'
                    const refreshedAtText = formatVersionRefreshedAt(
                      effectiveVersionState?.refreshedAt
                    )

                    return (
                      <div
                        key={profile.id}
                        className={cn(
                          'rounded-lg border p-3',
                          selected && 'border-primary bg-primary/5'
                        )}
                      >
                        <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
                          <button
                            type='button'
                            className='min-w-0 flex-1 text-left'
                            disabled={disabled}
                            onClick={() => toggleProfile(profile)}
                          >
                            <div className='flex items-center gap-2'>
                              <span className='truncate text-sm font-medium'>
                                {profile.name}
                              </span>
                              <Badge variant='outline'>
                                {profile.category}
                              </Badge>
                              {selected && (
                                <Check className='text-primary h-4 w-4' />
                              )}
                            </div>
                            <p className='text-muted-foreground mt-1 line-clamp-2 text-xs'>
                              {profile.description ||
                                t('Complete static request header snapshot.')}
                            </p>
                          </button>

                          {versionSource && (
                            <div className='grid shrink-0 gap-2 sm:w-56'>
                              <div className='flex gap-2'>
                                <Select
                                  disabled={disabled}
                                  value={selectedVersion}
                                  onValueChange={(nextVersion) => {
                                    if (disabled) return
                                    updateVersionState(profile, {
                                      selectedVersion: nextVersion,
                                    })
                                    updateSelectedVersionedProfile(profile, {
                                      selectedVersion: nextVersion,
                                    })
                                  }}
                                >
                                  <SelectTrigger className='h-8 flex-1'>
                                    <SelectValue />
                                  </SelectTrigger>
                                  <SelectContent>
                                    {versionOptions.map((option) => (
                                      <SelectItem
                                        key={option.value}
                                        value={option.value}
                                      >
                                        {option.label}
                                      </SelectItem>
                                    ))}
                                  </SelectContent>
                                </Select>
                                <Tooltip>
                                  <TooltipTrigger asChild>
                                    <Button
                                      type='button'
                                      variant='outline'
                                      size='icon'
                                      className='h-8 w-8 shrink-0'
                                      disabled={
                                        disabled ||
                                        effectiveVersionState?.loading
                                      }
                                      onClick={(event) => {
                                        event.stopPropagation()
                                        reloadVersionOptions(profile)
                                      }}
                                      aria-label={t('Reload npm versions')}
                                    >
                                      <RefreshCw
                                        className={cn(
                                          'h-3.5 w-3.5',
                                          effectiveVersionState?.loading &&
                                            'animate-spin'
                                        )}
                                      />
                                    </Button>
                                  </TooltipTrigger>
                                  <TooltipContent>
                                    {t('Reload npm versions')}
                                  </TooltipContent>
                                </Tooltip>
                              </div>
                              <Select
                                disabled={disabled}
                                value={
                                  effectiveVersionState?.selectedPlatform ||
                                  AI_CODING_CLI_DEFAULT_PLATFORM
                                }
                                onValueChange={(nextPlatform) => {
                                  if (disabled) return
                                  updateVersionState(profile, {
                                    selectedPlatform: nextPlatform,
                                  })
                                  updateSelectedVersionedProfile(profile, {
                                    selectedPlatform: nextPlatform,
                                  })
                                }}
                              >
                                <SelectTrigger className='h-8'>
                                  <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                  {AI_CODING_CLI_PLATFORM_OPTIONS.map(
                                    (option) => (
                                      <SelectItem
                                        key={option.value}
                                        value={option.value}
                                      >
                                        {option.label}
                                      </SelectItem>
                                    )
                                  )}
                                </SelectContent>
                              </Select>
                              {effectiveVersionState?.loading && (
                                <div className='text-muted-foreground flex items-center gap-1 text-xs'>
                                  <Loader2 className='h-3 w-3 animate-spin' />
                                  {t('Loading versions')}
                                </div>
                              )}
                              {effectiveVersionState?.error && (
                                <div className='text-warning text-xs'>
                                  {getNpmVersionLoadErrorText(
                                    t,
                                    effectiveVersionState.error
                                  )}
                                </div>
                              )}
                              <div className='text-muted-foreground text-xs'>
                                {refreshedAtText
                                  ? t('{{source}}, refreshed {{time}}', {
                                      source: getVersionSourceLabel(
                                        t,
                                        sourceForDisplay
                                      ),
                                      time: refreshedAtText,
                                    })
                                  : getVersionSourceLabel(t, sourceForDisplay)}
                              </div>
                            </div>
                          )}
                        </div>
                      </div>
                    )
                  })
                )}
              </div>
            ))}
          </div>

          <DialogFooter>
            <Button type='button' onClick={() => setLibraryOpen(false)}>
              {t('Done')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
