import { describe, expect, test } from 'bun:test'
import {
  BUILTIN_HEADER_PROFILES,
  NPM_VERSION_LATEST_ALIAS,
  buildHeaderProfileStrategySettings,
  buildSelectedProfileItems,
  buildVersionedAiCodingCliProfile,
  disableEmptyHeaderProfileStrategy,
  getHeaderProfileStrategyFromSettings,
  normalizeHeaderProfileMode,
  normalizeNpmCliVersionOptions,
} from '../src/features/channels/lib/header-profile-utils'

describe('channel header profile strategy settings', () => {
  test('reads and normalizes saved multi-template strategy', () => {
    const strategy = getHeaderProfileStrategyFromSettings(
      JSON.stringify({
        azure_responses_version: 'preview',
        header_profile_strategy: {
          enabled: true,
          mode: 'round_robin',
          selected_profile_ids: [
            'codex-cli@latest',
            'claude-code@latest',
            'codex-cli@latest',
          ],
          profiles: [],
        },
      })
    )

    expect(strategy?.enabled).toBe(true)
    expect(strategy?.mode).toBe('round_robin')
    expect(strategy?.selectedProfileIds).toEqual([
      'codex-cli@latest',
      'claude-code@latest',
    ])
  })

  test('writes latest profile snapshots without removing unrelated settings', () => {
    const codex = BUILTIN_HEADER_PROFILES.find(
      (profile) => profile.id === 'codex-cli'
    )
    const claude = BUILTIN_HEADER_PROFILES.find(
      (profile) => profile.id === 'claude-code'
    )
    expect(codex).toBeDefined()
    expect(claude).toBeDefined()

    const selectedProfiles = [
      buildVersionedAiCodingCliProfile(
        codex!,
        NPM_VERSION_LATEST_ALIAS,
        '0.200.0',
        'linux-arm64'
      ),
      buildVersionedAiCodingCliProfile(
        claude!,
        NPM_VERSION_LATEST_ALIAS,
        '2.2.0',
        'windows-x64'
      ),
    ]
    const nextSettings = buildHeaderProfileStrategySettings(
      JSON.stringify({ azure_responses_version: 'preview' }),
      {
        enabled: true,
        mode: 'random',
        selectedProfileIds: selectedProfiles.map((profile) => profile.id),
        profiles: selectedProfiles,
      }
    )
    const parsed = JSON.parse(nextSettings)

    expect(parsed.azure_responses_version).toBe('preview')
    expect(parsed.header_profile_strategy.mode).toBe('random')
    expect(parsed.header_profile_strategy.selected_profile_ids).toEqual([
      'codex-cli@latest',
      'claude-code@latest',
    ])
    expect(parsed.header_profile_strategy.profiles).toHaveLength(2)
    expect(parsed.header_profile_strategy.profiles[0].version_meta).toEqual({
      base_profile_id: 'codex-cli',
      package_name: '@openai/codex',
      source: 'npm',
      version: 'latest',
      platform: 'linux-arm64',
    })
    expect(
      parsed.header_profile_strategy.profiles[0].headers['User-Agent']
    ).toContain('0.200.0')
  })

  test('fixed mode keeps one selected profile and empty enabled strategy is disabled', () => {
    expect(normalizeHeaderProfileMode('bad')).toBe('fixed')
    const empty = disableEmptyHeaderProfileStrategy({
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: [],
      profiles: [],
    })
    expect(empty?.enabled).toBe(false)

    const selected = buildSelectedProfileItems(
      ['codex-cli@latest', 'missing-profile'],
      BUILTIN_HEADER_PROFILES
    )
    expect(selected[0].id).toBe('codex-cli@latest')
    expect(selected[0].versionMeta?.version).toBe('latest')
    expect(selected[1].missing).toBe(true)
  })

  test('custom profiles can be selected with built-in profiles', () => {
    const customProfile = {
      id: 'hp_custom_cli',
      name: 'Custom CLI',
      category: 'custom' as const,
      scope: 'user' as const,
      headers: {
        'User-Agent': 'CustomCLI/1.0',
        'X-Custom-Client': 'enabled',
      },
      previewText: '',
    }
    const selected = buildSelectedProfileItems(
      ['codex-cli@latest', 'hp_custom_cli'],
      [...BUILTIN_HEADER_PROFILES, customProfile]
    )
    expect(selected.map((profile) => profile.id)).toEqual([
      'codex-cli@latest',
      'hp_custom_cli',
    ])

    const settings = buildHeaderProfileStrategySettings('{}', {
      enabled: true,
      mode: 'round_robin',
      selectedProfileIds: selected.map((profile) => profile.id),
      profiles: selected,
    })
    const parsed = JSON.parse(settings)

    expect(parsed.header_profile_strategy.selected_profile_ids).toEqual([
      'codex-cli@latest',
      'hp_custom_cli',
    ])
    expect(parsed.header_profile_strategy.profiles[1]).toMatchObject({
      id: 'hp_custom_cli',
      scope: 'user',
      headers: {
        'User-Agent': 'CustomCLI/1.0',
        'X-Custom-Client': 'enabled',
      },
    })
  })

  test('custom profile ids containing @ are not treated as built-in versions', () => {
    const scopedCustomProfile = {
      id: '@team/custom-cli',
      name: 'Scoped Custom CLI',
      category: 'custom' as const,
      scope: 'user' as const,
      headers: {
        'User-Agent': 'ScopedCustomCLI/1.0',
      },
      previewText: '',
    }
    const selected = buildSelectedProfileItems(
      ['@team/custom-cli'],
      [...BUILTIN_HEADER_PROFILES, scopedCustomProfile]
    )

    expect(selected[0]).toMatchObject({
      id: '@team/custom-cli',
      name: 'Scoped Custom CLI',
    })
    expect('missing' in selected[0]).toBe(false)

    const settings = buildHeaderProfileStrategySettings('{}', {
      enabled: true,
      mode: 'random',
      selectedProfileIds: selected.map((profile) => profile.id),
      profiles: selected,
    })
    const parsed = JSON.parse(settings)

    expect(parsed.header_profile_strategy.selected_profile_ids).toEqual([
      '@team/custom-cli',
    ])
    expect(parsed.header_profile_strategy.profiles[0].headers).toEqual({
      'User-Agent': 'ScopedCustomCLI/1.0',
    })
  })

  test('editing can keep an enabled empty strategy until submit cleanup', () => {
    const draftSettings = buildHeaderProfileStrategySettings('{}', {
      enabled: true,
      mode: 'round_robin',
      selectedProfileIds: [],
      profiles: [],
    })
    const draft = getHeaderProfileStrategyFromSettings(draftSettings)

    expect(draft?.enabled).toBe(true)
    expect(draft?.selectedProfileIds).toEqual([])

    const cleanedSettings = buildHeaderProfileStrategySettings(
      draftSettings,
      disableEmptyHeaderProfileStrategy(draft)
    )
    const cleaned = getHeaderProfileStrategyFromSettings(cleanedSettings)

    expect(cleaned?.enabled).toBe(false)
    expect(cleaned?.selectedProfileIds).toEqual([])
  })

  test('rejects invalid non-empty settings instead of overwriting them', () => {
    expect(() =>
      buildHeaderProfileStrategySettings('{bad json', {
        enabled: true,
        mode: 'fixed',
        selectedProfileIds: ['codex-cli@latest'],
        profiles: [
          buildVersionedAiCodingCliProfile(
            BUILTIN_HEADER_PROFILES.find(
              (profile) => profile.id === 'codex-cli'
            )!,
            NPM_VERSION_LATEST_ALIAS,
            '0.200.0',
            'linux-x64'
          ),
        ],
      })
    ).toThrow()
  })

  test('reads invalid settings as empty without crashing the editor', () => {
    expect(getHeaderProfileStrategyFromSettings('{bad json')).toBeNull()
  })

  test('limits npm version options to latest plus four pinned versions', () => {
    const options = normalizeNpmCliVersionOptions([
      {
        value: '0.205.0',
        label: '0.205.0',
        resolvedVersion: '0.205.0',
      },
      {
        value: 'latest',
        label: 'latest (0.206.0)',
        resolvedVersion: '0.206.0',
      },
      {
        value: '0.204.0',
        label: '0.204.0',
        resolvedVersion: '0.204.0',
      },
      {
        value: '0.203.0',
        label: '0.203.0',
        resolvedVersion: '0.203.0',
      },
      {
        value: '0.202.0',
        label: '0.202.0',
        resolvedVersion: '0.202.0',
      },
      {
        value: '0.201.0',
        label: '0.201.0',
        resolvedVersion: '0.201.0',
      },
    ])

    expect(options.map((option) => option.value)).toEqual([
      'latest',
      '0.205.0',
      '0.204.0',
      '0.203.0',
      '0.202.0',
    ])
  })
})
