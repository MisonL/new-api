import { useEffect, useMemo, useState } from 'react'
import {
  Check,
  GripVertical,
  Info,
  Loader2,
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
  NPM_VERSION_LATEST_ALIAS,
  type HeaderProfile,
  type HeaderProfileMode,
  type HeaderProfileStrategy,
  type NpmCliVersionOption,
  buildSelectedProfileItems,
  buildVersionedAiCodingCliProfile,
  getProfileBaseId,
  latestFallbackOption,
  normalizeHeaderProfileMode,
  normalizeNpmCliVersionOptions,
} from '../lib/header-profile-utils'

type HeaderProfileStrategyEditorProps = {
  value: HeaderProfileStrategy | null
  customProfiles?: HeaderProfile[]
  onChange: (strategy: HeaderProfileStrategy | null) => void
  disabled?: boolean
}

type VersionState = {
  loading: boolean
  options: NpmCliVersionOption[]
  selectedVersion: string
  selectedPlatform: string
}

const VERSION_CACHE = new Map<string, Promise<NpmCliVersionOption[]>>()

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

function fetchNpmVersionOptions(packageName: string) {
  const cacheKey = packageName.trim()
  const cached = VERSION_CACHE.get(cacheKey)
  if (cached) return cached
  const request = api
    .get('/api/channel/npm_version_options', {
      params: { package: cacheKey },
      skipErrorHandler: true,
      disableDuplicate: true,
    } as Record<string, unknown>)
    .then((response) => {
      const payload = response.data || {}
      if (payload.success !== true) {
        throw new Error(payload.message || 'failed to load npm versions')
      }
      const options = normalizeNpmCliVersionOptions(payload.data)
      if (options.length === 0) {
        throw new Error('empty npm version options')
      }
      return options
    })
    .catch((error) => {
      VERSION_CACHE.delete(cacheKey)
      throw error
    })
  VERSION_CACHE.set(cacheKey, request)
  return request
}

function buildInitialVersionStates(
  profiles: HeaderProfile[],
  savedProfiles: HeaderProfile[] = []
): Record<string, VersionState> {
  const states = Object.fromEntries(
    profiles
      .filter((profile) => profile.versionSource?.packageName)
      .map((profile) => {
        const fallback = latestFallbackOption(
          profile.versionSource?.fallbackVersion || ''
        )
        return [
          profile.id,
          {
            loading: true,
            options: [fallback],
            selectedVersion: NPM_VERSION_LATEST_ALIAS,
            selectedPlatform: AI_CODING_CLI_DEFAULT_PLATFORM,
          },
        ]
      })
  )
  for (const savedProfile of savedProfiles) {
    const meta = savedProfile.versionMeta
    if (!meta?.baseProfileId || !states[meta.baseProfileId]) continue
    states[meta.baseProfileId] = {
      ...states[meta.baseProfileId],
      selectedVersion: meta.version || NPM_VERSION_LATEST_ALIAS,
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

  useEffect(() => {
    let active = true
    const savedVersionStates = buildSavedVersionStates(savedProfiles)
    for (const profile of profiles) {
      const versionSource = profile.versionSource
      if (!versionSource?.packageName) continue
      const fallback = latestFallbackOption(versionSource.fallbackVersion)
      fetchNpmVersionOptions(versionSource.packageName)
        .then((options) => {
          if (!active) return
          setVersionStates((current) => ({
            ...current,
            [profile.id]: {
              loading: false,
              options,
              selectedVersion:
                savedVersionStates[profile.id]?.selectedVersion ||
                current[profile.id]?.selectedVersion ||
                options[0]?.value ||
                NPM_VERSION_LATEST_ALIAS,
              selectedPlatform:
                savedVersionStates[profile.id]?.selectedPlatform ||
                current[profile.id]?.selectedPlatform ||
                AI_CODING_CLI_DEFAULT_PLATFORM,
            },
          }))
        })
        .catch(() => {
          if (!active) return
          setVersionStates((current) => ({
            ...current,
            [profile.id]: {
              loading: false,
              options: [fallback],
              selectedVersion:
                savedVersionStates[profile.id]?.selectedVersion ||
                current[profile.id]?.selectedVersion ||
                NPM_VERSION_LATEST_ALIAS,
              selectedPlatform:
                savedVersionStates[profile.id]?.selectedPlatform ||
                current[profile.id]?.selectedPlatform ||
                AI_CODING_CLI_DEFAULT_PLATFORM,
            },
          }))
        })
    }
    return () => {
      active = false
    }
  }, [profiles, savedProfiles])

  return [versionStates, setVersionStates] as const
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
  const strategy = value || defaultStrategy()
  const selectableProfiles = useMemo(
    () => [...BUILTIN_HEADER_PROFILES, ...customProfiles],
    [customProfiles]
  )
  const [versionStates, setVersionStates] = useVersionStates(
    BUILTIN_HEADER_PROFILES,
    strategy.profiles
  )

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

  function emit(next: HeaderProfileStrategy | null) {
    onChange(next)
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
    const options = state?.options || [
      latestFallbackOption(profile.versionSource.fallbackVersion),
    ]
    const selectedOption = options.find(
      (option) => option.value === selectedVersion
    )
    return buildVersionedAiCodingCliProfile(
      profile,
      selectedVersion,
      selectedOption?.resolvedVersion || profile.versionSource.fallbackVersion,
      overrides.selectedPlatform ||
        state?.selectedPlatform ||
        AI_CODING_CLI_DEFAULT_PLATFORM
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
        options:
          current[profile.id]?.options ||
          (profile.versionSource
            ? [latestFallbackOption(profile.versionSource.fallbackVersion)]
            : []),
        selectedVersion:
          current[profile.id]?.selectedVersion || NPM_VERSION_LATEST_ALIAS,
        selectedPlatform:
          current[profile.id]?.selectedPlatform ||
          AI_CODING_CLI_DEFAULT_PLATFORM,
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
                (id) => id === profile.id || getProfileBaseId(id) === profile.id
              )
              const versionSource = profile.versionSource
              const versionState = versionStates[profile.id]
              const effectiveVersionState = versionState
              const versionOptions =
                effectiveVersionState?.options ||
                (versionSource
                  ? [latestFallbackOption(versionSource.fallbackVersion)]
                  : [])

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
                        <Badge variant='outline'>{profile.category}</Badge>
                        {selected && <Check className='text-primary h-4 w-4' />}
                      </div>
                      <p className='text-muted-foreground mt-1 line-clamp-2 text-xs'>
                        {profile.description ||
                          t('Complete static request header snapshot.')}
                      </p>
                    </button>

                    {versionSource && (
                      <div className='grid shrink-0 gap-2 sm:w-56'>
                        <Select
                          disabled={disabled}
                          value={
                            effectiveVersionState?.selectedVersion ||
                            NPM_VERSION_LATEST_ALIAS
                          }
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
                          <SelectTrigger className='h-8'>
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
                            {AI_CODING_CLI_PLATFORM_OPTIONS.map((option) => (
                              <SelectItem
                                key={option.value}
                                value={option.value}
                              >
                                {option.label}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                        {effectiveVersionState?.loading && (
                          <div className='text-muted-foreground flex items-center gap-1 text-xs'>
                            <Loader2 className='h-3 w-3 animate-spin' />
                            {t('Loading versions')}
                          </div>
                        )}
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
