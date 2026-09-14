import { describe, expect, test } from 'bun:test'
import { readFileSync } from 'node:fs'
import {
  getChannelHealthRuntimeDominantEntry,
  getChannelHealthRuntimeChannelPresentation,
  getChannelHealthRuntimeChannelSearch,
  getChannelHealthRuntimeEventDisplay,
  getChannelHealthRuntimeEvents,
  getChannelHealthRuntimeRefreshDelay,
  getChannelHealthRuntimeStateBehavior,
  getChannelHealthRuntimeEntriesForChannel,
  getChannelHealthRuntimePresentation,
  getChannelHealthRuntimeStateDisplay,
  summarizeChannelHealthRuntime,
} from '../src/features/system-settings/general/channel-health-runtime'

const channelHealthDynamicTranslationKeys = [
  'Awaiting half-open probe',
  'Cooling down',
  'Configure temporary channel cooling and recovery probes',
  'Half-open probe active',
  'Unknown runtime state',
  'Routing impact',
  'Next transition',
  'Matching group and model requests bypass this channel while cooldown remains.',
  'When cooldown expires, the next matching request may acquire a recovery probe.',
  'One matching request is probing this channel while competing requests bypass it.',
  'A successful probe clears the temporary exclusion; a qualifying failure restarts cooldown.',
  'Cooldown has ended and this channel is waiting for a recovery probe.',
  'The next matching request may acquire the single recovery probe.',
  'The current runtime state does not define a routing exclusion.',
  'The next transition is not available for this runtime state.',
]

function getChannelHealthSettingsTranslationKeys(): string[] {
  const files = [
    '../src/features/system-settings/general/channel-health-routing-section.tsx',
    '../src/features/system-settings/general/channel-health-runtime.ts',
    '../src/features/channel-health/components/channel-health-runtime-entries.tsx',
    '../src/features/channel-health/components/channel-health-runtime-channel-state.tsx',
    '../src/features/channel-health/components/channel-health-runtime-events.tsx',
    '../src/features/channel-health/use-channel-health-runtime.ts',
    '../src/features/channels/components/channel-health-routing-overview.tsx',
  ]
  const keys = new Set<string>()

  for (const file of files) {
    const source = readFileSync(new URL(file, import.meta.url), 'utf8')
    for (const match of source.matchAll(/\bt\(\s*['"]([^'"\n]+)['"]/g)) {
      keys.add(match[1])
    }
  }

  return [...new Set([...keys, ...channelHealthDynamicTranslationKeys])]
}

function getMissingChannelHealthSettingsTranslations(): string[] {
  const keys = getChannelHealthSettingsTranslationKeys()
  const locales = ['en', 'zh', 'fr', 'ja', 'ru', 'vi']
  const missing: string[] = []

  for (const locale of locales) {
    const file = new URL(`../src/i18n/locales/${locale}.json`, import.meta.url)
    const { translation } = JSON.parse(readFileSync(file, 'utf8')) as {
      translation: Record<string, string | undefined>
    }
    for (const key of keys) {
      if (typeof translation[key] !== 'string' || !translation[key]?.trim()) {
        missing.push(`${locale}:${key}`)
      }
    }
  }

  return missing
}

describe('channel health runtime presentation', () => {
  test(
    'localizes every channel-health setting and runtime state in each locale',
    () => {
      expect(getMissingChannelHealthSettingsTranslations()).toEqual([])
    },
    15000
  )

  test('keeps cooling, active probe, and pending probe visually distinct', () => {
    expect(getChannelHealthRuntimeStateDisplay('cooldown')).toEqual({
      labelKey: 'Cooling down',
      variant: 'warning',
    })
    expect(getChannelHealthRuntimeStateDisplay('half_open_probe')).toEqual({
      labelKey: 'Half-open probe active',
      variant: 'info',
    })
    expect(
      getChannelHealthRuntimeStateDisplay('awaiting_half_open_probe')
    ).toEqual({
      labelKey: 'Awaiting half-open probe',
      variant: 'neutral',
    })
  })

  test('explains the routing impact and next transition for each active state', () => {
    expect(getChannelHealthRuntimeStateBehavior('cooldown')).toEqual({
      impactKey:
        'Matching group and model requests bypass this channel while cooldown remains.',
      nextStepKey:
        'When cooldown expires, the next matching request may acquire a recovery probe.',
    })
    expect(getChannelHealthRuntimeStateBehavior('half_open_probe')).toEqual({
      impactKey:
        'One matching request is probing this channel while competing requests bypass it.',
      nextStepKey:
        'A successful probe clears the temporary exclusion; a qualifying failure restarts cooldown.',
    })
    expect(
      getChannelHealthRuntimeStateBehavior('awaiting_half_open_probe')
    ).toEqual({
      impactKey:
        'Cooldown has ended and this channel is waiting for a recovery probe.',
      nextStepKey:
        'The next matching request may acquire the single recovery probe.',
    })
  })

  test('maps runtime state to the matching channel without inventing a global health result', () => {
    const runtime = {
      enabled: true,
      redis_available: true,
      cooldown_seconds: 30,
      probe_lease_seconds: 15,
      entries: [
        {
          channel_id: 11,
          group: 'default',
          model_name: 'gpt-5.6-sol',
          state: 'half_open_probe' as const,
        },
        {
          channel_id: 11,
          group: 'coding',
          model_name: 'gpt-5.6-sol',
          state: 'cooldown' as const,
        },
        {
          channel_id: 12,
          group: 'default',
          model_name: 'gpt-5.6-sol',
          state: 'awaiting_half_open_probe' as const,
        },
      ],
    }

    const channelEntries = getChannelHealthRuntimeEntriesForChannel(runtime, 11)
    expect(channelEntries).toHaveLength(2)
    expect(channelEntries.map((entry) => entry.group)).toEqual([
      'default',
      'coding',
    ])
    expect(getChannelHealthRuntimeDominantEntry(channelEntries)).toMatchObject({
      group: 'coding',
      model_name: 'gpt-5.6-sol',
      state: 'cooldown',
    })
    expect(getChannelHealthRuntimeChannelPresentation(channelEntries)).toEqual({
      labelKey: 'Cooling down',
      variant: 'warning',
    })
    expect(getChannelHealthRuntimeEntriesForChannel(runtime, 13)).toEqual([])
    expect(
      getChannelHealthRuntimeEntriesForChannel(
        { ...runtime, redis_available: false },
        11
      )
    ).toEqual([])
  })

  test('locates an affected channel by its immutable ID and clears stale filters', () => {
    expect(
      getChannelHealthRuntimeChannelSearch({
        channel_id: 920651,
        group: 'uihealth',
        model_name: 'gpt-5.6-terra',
        state: 'cooldown',
      })
    ).toEqual({
      filter: '920651',
      page: 1,
      status: [],
      type: [],
      group: [],
      model: '',
      runtimeChannelFocus: 920651,
    })
  })

  test('summarizes live entries without treating Redis fail-open as healthy', () => {
    expect(
      summarizeChannelHealthRuntime({
        enabled: true,
        redis_available: true,
        cooldown_seconds: 30,
        probe_lease_seconds: 15,
        entries: [
          { state: 'cooldown' },
          { state: 'half_open_probe' },
          { state: 'awaiting_half_open_probe' },
        ],
      })
    ).toEqual({
      state: 'active',
      cooling: 1,
      probing: 1,
      awaitingProbe: 1,
    })

    expect(
      summarizeChannelHealthRuntime({
        enabled: true,
        redis_available: false,
        cooldown_seconds: 30,
        probe_lease_seconds: 15,
        entries: [],
      })
    ).toEqual({
      state: 'redis_unavailable',
      cooling: 0,
      probing: 0,
      awaitingProbe: 0,
    })
  })

  test('keeps an unavailable runtime distinct from Redis fail-open and healthy routing', () => {
    expect(getChannelHealthRuntimePresentation(null, true)).toEqual({
      state: 'unavailable',
      labelKey: 'Runtime status unavailable',
      variant: 'danger',
    })

    expect(
      getChannelHealthRuntimePresentation(
        {
          enabled: true,
          redis_available: false,
          cooldown_seconds: 30,
          probe_lease_seconds: 15,
          entries: [],
        },
        false
      )
    ).toEqual({
      state: 'redis_unavailable',
      labelKey: 'Redis unavailable: routing fails open',
      variant: 'danger',
    })

    expect(
      getChannelHealthRuntimePresentation(
        {
          enabled: true,
          redis_available: true,
          cooldown_seconds: 30,
          probe_lease_seconds: 15,
          entries: [],
        },
        false
      )
    ).toEqual({
      state: 'active',
      labelKey: 'Health routing active',
      variant: 'success',
    })
  })

  test('keeps recent lifecycle transitions visible after the active state clears', () => {
    const runtime = {
      enabled: true,
      redis_available: true,
      cooldown_seconds: 30,
      probe_lease_seconds: 15,
      entries: [],
      recent_events: [
        {
          occurred_at: 1_784_997_217,
          group: 'coding',
          model_name: 'gpt-5.6-sol',
          channel_id: 206,
          action: 'half_open_recovered',
        },
        {
          occurred_at: 1_784_997_201,
          group: 'coding',
          model_name: 'gpt-5.6-sol',
          channel_id: 206,
          action: 'cooldown',
          status_code: 502,
          error_code: 'bad_response_status_code',
        },
      ],
    }

    const events = getChannelHealthRuntimeEvents(runtime)
    expect(events).toHaveLength(2)
    expect(events[0]).toMatchObject({
      occurred_at: 1_784_997_217,
      action: 'half_open_recovered',
      channel_id: 206,
      group: 'coding',
      model_name: 'gpt-5.6-sol',
    })
    expect(getChannelHealthRuntimeEventDisplay(events[1])).toEqual({
      labelKey: 'Channel Cooling',
      variant: 'warning',
    })
  })

  test('samples active cooling and probing quickly without keeping idle polling hot', () => {
    const activeRuntime = {
      enabled: true,
      redis_available: true,
      cooldown_seconds: 30,
      probe_lease_seconds: 15,
      entries: [{ state: 'cooldown' }],
    }
    const idleRuntime = { ...activeRuntime, entries: [] }

    expect(getChannelHealthRuntimeRefreshDelay(activeRuntime)).toBe(1000)
    expect(getChannelHealthRuntimeRefreshDelay(idleRuntime)).toBe(5000)
    expect(
      getChannelHealthRuntimeRefreshDelay({
        ...activeRuntime,
        redis_available: false,
      })
    ).toBe(5000)
  })

  test('uses vertically scannable runtime entries on narrow screens', () => {
    const source = readFileSync(
      new URL(
        '../src/features/channel-health/components/channel-health-runtime-entries.tsx',
        import.meta.url
      ),
      'utf8'
    )

    expect(source).toContain("space-y-2 md:hidden")
    expect(source).toContain("hidden overflow-x-auto rounded-md border md:block")
    expect(source).toContain("Last qualifying failure")
  })

  test('bounds automatic runtime refreshes and keeps silent failures quiet', () => {
    const source = readFileSync(
      new URL(
        '../src/features/channel-health/use-channel-health-runtime.ts',
        import.meta.url
      ),
      'utf8'
    )

    expect(source).toContain('RUNTIME_REQUEST_TIMEOUT_MS = 8000')
    expect(source).toContain('skipBusinessError: true')
    expect(source).toContain('skipErrorHandler: true')
    expect(source).toContain('runtimeRequestRef')
    expect(source).toContain('runtimeRequestVisibleRef')
    expect(source).toContain('runtimeRequestVisibleRef.current = true')
    expect(source).toContain(
      'const shouldClearRuntimeLoading = runtimeRequestVisibleRef.current'
    )
    expect(source).toContain('getChannelHealthRuntimeRefreshDelay')
    expect(source).toContain("document.visibilityState === 'hidden'")
    expect(source).toContain("document.addEventListener('visibilitychange'")
    expect(source).toContain('refresh({ silent: true })')
    expect(source).toContain('window.setTimeout(scheduleRefresh, refreshDelay)')
    expect(source).not.toContain('window.setInterval')
    expect(source).not.toContain('disableDuplicate: true')
  })

  test('puts administrator-visible live feedback in the channel-management workflow', () => {
    const source = readFileSync(
      new URL(
        '../src/features/channels/components/channel-health-routing-overview.tsx',
        import.meta.url
      ),
      'utf8'
    )

    expect(source).toContain('useChannels')
    expect(source).toContain('canViewChannelHealthRuntime')
    expect(source).toContain('canConfigureChannelHealthRuntime')
    expect(source).toContain("to='/system-settings/general/$section'")
    expect(source).toContain("to='/usage-logs/$section'")
    expect(source).toContain('Runtime scope is channel, group, and model.')
    expect(source).toContain('No active channel exclusions.')
    expect(source).toContain('ChannelHealthRuntimeEvents')
    expect(source).toContain("toast.error(t('Failed to load channel health runtime'))")

    const entriesSource = readFileSync(
      new URL(
        '../src/features/channel-health/components/channel-health-runtime-entries.tsx',
        import.meta.url
      ),
      'utf8'
    )
    expect(entriesSource).toContain('getChannelHealthRuntimeChannelSearch')
    expect(entriesSource).toContain("to='/channels'")

    const providerSource = readFileSync(
      new URL(
        '../src/features/channels/components/channels-provider.tsx',
        import.meta.url
      ),
      'utf8'
    )
    expect(providerSource).toContain('runtimeChannelFocus')
    expect(providerSource).toContain('runtimeChannelFocus === undefined')
    expect(providerSource).toContain('enableTagModePreference')
  })

  test('shows an active routing state next to the affected channel rather than every channel', () => {
    const source = readFileSync(
      new URL(
        '../src/features/channel-health/components/channel-health-runtime-channel-state.tsx',
        import.meta.url
      ),
      'utf8'
    )
    const columnsSource = readFileSync(
      new URL(
        '../src/features/channels/components/channels-columns.tsx',
        import.meta.url
      ),
      'utf8'
    )

    expect(source).toContain('getChannelHealthRuntimeEntriesForChannel')
    expect(source).toContain('getChannelHealthRuntimeDominantEntry')
    expect(source).not.toContain('entries[0]?.state')
    expect(source).toContain('getChannelHealthRuntimeStateBehavior')
    expect(columnsSource).toContain('ChannelHealthRuntimeChannelState')
    const regularChannelNameCell = columnsSource.slice(
      columnsSource.indexOf('// Regular channel row'),
      columnsSource.indexOf('// Type column')
    )
    expect(regularChannelNameCell).toContain(
      "flex flex-wrap items-center gap-1.5"
    )
  })

  test('shows routing impact and the next transition in runtime entry views', () => {
    const entriesSource = readFileSync(
      new URL(
        '../src/features/channel-health/components/channel-health-runtime-entries.tsx',
        import.meta.url
      ),
      'utf8'
    )
    const stateSource = readFileSync(
      new URL(
        '../src/features/channel-health/components/channel-health-runtime-channel-state.tsx',
        import.meta.url
      ),
      'utf8'
    )

    expect(entriesSource).toContain('getChannelHealthRuntimeStateBehavior')
    expect(entriesSource).toContain("t('Routing impact')")
    expect(entriesSource).toContain("t('Next transition')")
    expect(stateSource).toContain('getChannelHealthRuntimeStateBehavior')
    expect(stateSource).toContain("t('Routing impact')")
    expect(stateSource).toContain("t('Next transition')")
  })
})
