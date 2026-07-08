import { describe, expect, test } from 'bun:test'
import { readFileSync } from 'node:fs'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  mergeChannelSubmitFormValues,
  transformFormDataToUpdatePayload,
} from '../src/features/channels/lib/channel-form'
import {
  BUILTIN_HEADER_PROFILES,
  NPM_VERSION_FORBIDDEN_CODE,
  NPM_VERSION_LATEST_ALIAS,
  NPM_VERSION_LOAD_ERROR_CODE,
  NPM_VERSION_RATE_LIMITED_CODE,
  buildHeaderProfileStrategySettings,
  buildNpmCliFallbackVersionOptions,
  buildNpmVersionRequestConfig,
  buildSelectedProfileItems,
  buildVersionedAiCodingCliProfile,
  clearParamOverridePreservingUserAgentPassHeaders,
  disableEmptyHeaderProfileStrategy,
  getHeaderProfileStrategyFromSettings,
  normalizeHeaderProfileMode,
  normalizeNpmCliVersionOptions,
  normalizeNpmCliVersionOptionsResult,
  normalizeNpmVersionLoadError,
  normalizeNpmVersionPayloadErrorCode,
  refreshSelectedVersionedProfileSnapshots,
} from '../src/features/channels/lib/header-profile-utils'
import { PARAM_OVERRIDE_TEMPLATES } from '../src/features/system-settings/general/channel-affinity/constants'

describe('channel header profile strategy settings', () => {
  test('builtin Antigravity CLI profile is fixed snapshot only', () => {
    const agy = BUILTIN_HEADER_PROFILES.find(
      (profile) => profile.id === 'agy'
    )

    expect(agy).toBeDefined()
    expect(agy?.scope).toBe('builtin')
    expect(agy?.readonly).toBe(true)
    expect(agy?.versionSource).toBeNull()
    expect(agy?.headers['User-Agent']).toBe(
      'antigravity/cli/1.0.14 (aidev_client; os_type=darwin; arch=amd64)'
    )
    expect(agy?.description).toContain('手动配置 pass_headers')
    expect(agy?.description).not.toContain('Antigravity CLI 请求头透传模板')
  })

  test('channel edit keeps untouched User-Agent related submit state', () => {
    const savedSettings = JSON.stringify({
      responses_compact_mode: 'auto',
      header_profile_strategy: {
        enabled: true,
        mode: 'fixed',
        selected_profile_ids: ['claude-code@latest'],
        profiles: [
          {
            id: 'claude-code@latest',
            name: 'Claude Code',
            category: 'coding_cli',
            scope: 'builtin',
            headers: {
              'User-Agent': 'claude-cli/1.0',
              'X-Client-Name': 'claude-code',
            },
          },
        ],
      },
    })
    const savedParamOverride = JSON.stringify({
      operations: [
        {
          mode: 'pass_headers',
          value: ['User-Agent'],
          keep_origin: true,
        },
      ],
    })
    const savedHeaderOverride = JSON.stringify({
      'User-Agent': 'claude-cli/1.0',
    })
    const formValues = {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'prod-channel',
      type: 14,
      models: 'claude-sonnet-4',
      group: ['default'],
      settings: '{}',
      param_override: '',
      header_override: '',
      status_code_mapping: '',
      remark: 'changed by admin',
    }

    const merged = mergeChannelSubmitFormValues(formValues, {
      settings: savedSettings,
      param_override: savedParamOverride,
      header_override: savedHeaderOverride,
      status_code_mapping: '{"403":1}',
    })
    const payload = transformFormDataToUpdatePayload(merged, 209)

    expect(merged.settings).toBe(savedSettings)
    expect(merged.param_override).toBe(savedParamOverride)
    expect(merged.header_override).toBe(savedHeaderOverride)
    expect(merged.status_code_mapping).toBe('{"403":1}')
    expect(payload.param_override).toBe(savedParamOverride)
    expect(payload.header_override).toBe(savedHeaderOverride)
    expect(payload.status_code_mapping).toBe('{"403":1}')
    expect(JSON.parse(String(payload.settings)).header_profile_strategy).toEqual(
      JSON.parse(savedSettings).header_profile_strategy
    )
  })

  test('channel edit keeps explicit clearing of advanced submit state', () => {
    const savedParamOverride = JSON.stringify({
      operations: [
        {
          mode: 'pass_headers',
          value: ['User-Agent'],
          keep_origin: true,
        },
      ],
    })
    const formValues = {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'prod-channel',
      type: 14,
      models: 'claude-sonnet-4',
      group: ['default'],
      settings: '{}',
      param_override: '',
      header_override: '',
      status_code_mapping: '',
    }

    const merged = mergeChannelSubmitFormValues(
      formValues,
      {
        settings: JSON.stringify({
          header_profile_strategy: {
            enabled: true,
            mode: 'fixed',
            selected_profile_ids: ['claude-code@latest'],
            profiles: [],
          },
        }),
        param_override: savedParamOverride,
        header_override: '{"User-Agent":"claude-cli/1.0"}',
        status_code_mapping: '{"403":1}',
      },
      {
        settings: true,
        param_override: true,
        header_override: true,
        status_code_mapping: true,
      }
    )

    expect(merged.settings).toBe('{}')
    expect(merged.param_override).toBe('')
    expect(merged.header_override).toBe('')
    expect(merged.status_code_mapping).toBe('')
  })

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

  test('writes fallback source for pinned built-in npm snapshot', () => {
    const codex = BUILTIN_HEADER_PROFILES.find(
      (profile) => profile.id === 'codex-cli'
    )
    expect(codex).toBeDefined()

    const selectedProfile = buildVersionedAiCodingCliProfile(
      codex!,
      '0.142.4',
      '0.142.4',
      'macos-x64',
      'fallback'
    )
    const nextSettings = buildHeaderProfileStrategySettings('{}', {
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: [selectedProfile.id],
      profiles: [selectedProfile],
    })
    const parsed = JSON.parse(nextSettings)

    expect(parsed.header_profile_strategy.profiles[0].version_meta).toEqual({
      base_profile_id: 'codex-cli',
      package_name: '@openai/codex',
      source: 'fallback',
      version: '0.142.4',
      platform: 'macos-x64',
    })
  })

  test('refreshes selected fallback snapshot after npm versions load', () => {
    const codex = BUILTIN_HEADER_PROFILES.find(
      (profile) => profile.id === 'codex-cli'
    )
    expect(codex).toBeDefined()

    const fallbackProfile = buildVersionedAiCodingCliProfile(
      codex!,
      NPM_VERSION_LATEST_ALIAS,
      '0.142.4',
      'macos-x64',
      'fallback'
    )
    const refreshed = refreshSelectedVersionedProfileSnapshots(
      {
        enabled: true,
        mode: 'fixed',
        selectedProfileIds: [fallbackProfile.id],
        profiles: [fallbackProfile],
      },
      BUILTIN_HEADER_PROFILES,
      [
        {
          baseProfileId: 'codex-cli',
          selectedVersion: NPM_VERSION_LATEST_ALIAS,
          selectedPlatform: 'macos-x64',
          options: [
            {
              value: NPM_VERSION_LATEST_ALIAS,
              label: 'latest (0.200.0)',
              isLatest: true,
              resolvedVersion: '0.200.0',
            },
          ],
        },
      ]
    )

    expect(refreshed.profiles[0].versionMeta).toEqual({
      baseProfileId: 'codex-cli',
      packageName: '@openai/codex',
      source: 'npm',
      version: NPM_VERSION_LATEST_ALIAS,
      platform: 'macos-x64',
    })
    expect(refreshed.profiles[0].headers['User-Agent']).toContain('0.200.0')
  })

  test('refresh keeps current selected version over stale loaded state', () => {
    const codex = BUILTIN_HEADER_PROFILES.find(
      (profile) => profile.id === 'codex-cli'
    )
    expect(codex).toBeDefined()

    const pinnedProfile = buildVersionedAiCodingCliProfile(
      codex!,
      '0.142.4',
      '0.142.4',
      'linux-arm64',
      'fallback'
    )
    const refreshed = refreshSelectedVersionedProfileSnapshots(
      {
        enabled: true,
        mode: 'fixed',
        selectedProfileIds: [pinnedProfile.id],
        profiles: [pinnedProfile],
      },
      BUILTIN_HEADER_PROFILES,
      [
        {
          baseProfileId: 'codex-cli',
          selectedVersion: NPM_VERSION_LATEST_ALIAS,
          selectedPlatform: 'macos-x64',
          options: [
            {
              value: NPM_VERSION_LATEST_ALIAS,
              label: 'latest (0.200.0)',
              isLatest: true,
              resolvedVersion: '0.200.0',
            },
            {
              value: '0.142.4',
              label: '0.142.4',
              isLatest: false,
              resolvedVersion: '0.142.4',
            },
          ],
        },
      ]
    )

    expect(refreshed.selectedProfileIds).toEqual(['codex-cli@0.142.4'])
    expect(refreshed.profiles[0].versionMeta).toMatchObject({
      source: 'npm',
      version: '0.142.4',
      platform: 'linux-arm64',
    })
    expect(refreshed.profiles[0].headers['User-Agent']).toContain('0.142.4')
  })

  test('refresh preserves retained source for selected versions outside backend list', () => {
    const codex = BUILTIN_HEADER_PROFILES.find(
      (profile) => profile.id === 'codex-cli'
    )
    expect(codex).toBeDefined()

    const retainedProfile = buildVersionedAiCodingCliProfile(
      codex!,
      '0.120.0',
      '0.120.0',
      'macos-x64',
      'retained'
    )
    const refreshed = refreshSelectedVersionedProfileSnapshots(
      {
        enabled: true,
        mode: 'fixed',
        selectedProfileIds: [retainedProfile.id],
        profiles: [retainedProfile],
      },
      BUILTIN_HEADER_PROFILES,
      [
        {
          baseProfileId: 'codex-cli',
          packageName: '@openai/codex',
          selectedVersion: '0.120.0',
          selectedPlatform: 'macos-x64',
          options: [
            {
              value: NPM_VERSION_LATEST_ALIAS,
              label: 'latest (0.200.0)',
              isLatest: true,
              resolvedVersion: '0.200.0',
              source: 'npm',
            },
            {
              value: '0.120.0',
              label: '0.120.0',
              isLatest: false,
              resolvedVersion: '0.120.0',
              source: 'retained',
            },
          ],
        },
      ]
    )

    expect(refreshed.profiles[0].versionMeta).toMatchObject({
      source: 'retained',
      version: '0.120.0',
    })
    expect(refreshed.profiles[0].headers['User-Agent']).toContain('0.120.0')
  })

  test('refresh ignores version options from another npm package', () => {
    const codex = BUILTIN_HEADER_PROFILES.find(
      (profile) => profile.id === 'codex-cli'
    )
    expect(codex).toBeDefined()

    const fallbackProfile = buildVersionedAiCodingCliProfile(
      codex!,
      NPM_VERSION_LATEST_ALIAS,
      '0.142.4',
      'macos-x64',
      'fallback'
    )
    const strategy = {
      enabled: true,
      mode: 'fixed' as const,
      selectedProfileIds: [fallbackProfile.id],
      profiles: [fallbackProfile],
    }
    const refreshed = refreshSelectedVersionedProfileSnapshots(
      strategy,
      BUILTIN_HEADER_PROFILES,
      [
        {
          baseProfileId: 'codex-cli',
          packageName: '@anthropic-ai/claude-code',
          selectedVersion: NPM_VERSION_LATEST_ALIAS,
          selectedPlatform: 'macos-x64',
          options: [
            {
              value: NPM_VERSION_LATEST_ALIAS,
              label: 'latest (9.9.9)',
              isLatest: true,
              resolvedVersion: '9.9.9',
            },
          ],
        },
      ]
    )

    expect(refreshed).toBe(strategy)
    expect(refreshed.profiles[0].versionMeta?.source).toBe('fallback')
    expect(refreshed.profiles[0].headers['User-Agent']).toContain('0.142.4')
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

	  test('limits npm version options to latest plus five pinned versions', () => {
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
      '0.201.0',
	    ])
	  })

	  test('limits legacy npm option arrays without latest to five pinned versions', () => {
	    const options = normalizeNpmCliVersionOptions([
	      { value: '0.206.0', label: '0.206.0' },
	      { value: '0.205.0', label: '0.205.0' },
	      { value: '0.204.0', label: '0.204.0' },
	      { value: '0.203.0', label: '0.203.0' },
	      { value: '0.202.0', label: '0.202.0' },
	      { value: '0.201.0', label: '0.201.0' },
	    ])

	    expect(options.map((option) => option.value)).toEqual([
	      '0.206.0',
	      '0.205.0',
	      '0.204.0',
	      '0.203.0',
	      '0.202.0',
	    ])
	  })

	  test('normalizes legacy latest npm option flags', () => {
	    const options = normalizeNpmCliVersionOptions([
      {
        value: '0.206.0',
        label: '0.206.0',
        is_latest: true,
      },
      {
        value: '0.205.0',
        label: '0.205.0',
      },
    ])

    expect(options).toEqual([
      {
        value: 'latest',
        label: '0.206.0',
        isLatest: true,
        resolvedVersion: '0.206.0',
        source: 'npm',
      },
      {
        value: '0.205.0',
        label: '0.205.0',
        isLatest: false,
        resolvedVersion: '0.205.0',
        source: 'npm',
      },
    ])
  })

  test('fallback npm version options keep pinned builtin version selectable', () => {
    expect(buildNpmCliFallbackVersionOptions('0.142.4')).toEqual([
      {
        value: 'latest',
        label: 'latest (0.142.4)',
        isLatest: true,
        resolvedVersion: '0.142.4',
        source: 'fallback',
      },
      {
        value: '0.142.4',
        label: '0.142.4',
        isLatest: false,
        resolvedVersion: '0.142.4',
        source: 'fallback',
      },
    ])
  })

  test('normalizes structured backend npm version response', () => {
    const payload = {
      package: '@openai/codex',
      source: 'recorded',
      refreshed_at: '2026-07-06T10:00:00Z',
      latest_version: '0.200.0',
      options: [
        {
          value: 'latest',
          label: 'latest (0.200.0)',
          isLatest: true,
          resolvedVersion: '0.200.0',
        },
        {
          value: '0.199.0',
          label: '0.199.0',
          isLatest: false,
          resolvedVersion: '0.199.0',
          source: 'npm',
        },
      ],
    }
    const options = normalizeNpmCliVersionOptions(payload)

    expect(options).toEqual([
      {
        value: 'latest',
        label: 'latest (0.200.0)',
        isLatest: true,
        resolvedVersion: '0.200.0',
        source: 'recorded',
      },
      {
        value: '0.199.0',
        label: '0.199.0',
        isLatest: false,
        resolvedVersion: '0.199.0',
        source: 'npm',
      },
    ])

    expect(normalizeNpmCliVersionOptionsResult(payload)).toEqual({
      packageName: '@openai/codex',
      source: 'recorded',
      refreshedAt: '2026-07-06T10:00:00Z',
      latestVersion: '0.200.0',
      options,
    })
  })

  test('normalizes npm version rate limit and permission errors', () => {
    expect(
      normalizeNpmVersionLoadError({
        response: { status: 429, data: { message: 'too many requests' } },
      }).code
    ).toBe(NPM_VERSION_RATE_LIMITED_CODE)
    expect(
      normalizeNpmVersionPayloadErrorCode({
        message: '无权进行此操作，权限不足',
      })
    ).toBe(NPM_VERSION_FORBIDDEN_CODE)
    expect(
      normalizeNpmVersionPayloadErrorCode({
        message: 'Unauthorized, insufficient privileges',
      })
    ).toBe(NPM_VERSION_FORBIDDEN_CODE)
    expect(
      normalizeNpmVersionPayloadErrorCode({
        message: 'package is not allowed',
      })
    ).toBe(NPM_VERSION_LOAD_ERROR_CODE)
  })

  test('npm version request config suppresses duplicate business toasts', () => {
    expect(buildNpmVersionRequestConfig('  @openai/codex  ')).toEqual({
      params: { package: '@openai/codex' },
      skipBusinessError: true,
      skipErrorHandler: true,
      disableDuplicate: true,
    })

    expect(
      buildNpmVersionRequestConfig('@openai/codex', { timeout: 5000 })
    ).toEqual({
      params: { package: '@openai/codex' },
      skipBusinessError: true,
      skipErrorHandler: true,
      disableDuplicate: true,
      timeout: 5000,
    })

    expect(
      buildNpmVersionRequestConfig('@openai/codex', { timeout: 10000 })
    ).toMatchObject({
      timeout: 10000,
      skipBusinessError: true,
      skipErrorHandler: true,
    })
  })

  test('npm version diagnostics request uses bounded timeout', () => {
    const source = readFileSync(
      new URL(
        '../src/features/channels/components/header-profile-strategy-editor.tsx',
        import.meta.url
      ),
      'utf8'
    )

    expect(source).toMatch(
      /\.get\('\/api\/channel\/npm_version_options\/diagnostics',\s*\{\s*timeout: CLI_VERSION_REFRESH_TIMEOUT_MS,\s*skipBusinessError: true,\s*skipErrorHandler: true,\s*disableDuplicate: true,/s
    )
  })

  test('performance settings expose read-only npm version diagnostics', () => {
    const performanceSource = readFileSync(
      new URL(
        '../src/features/system-settings/maintenance/performance-section.tsx',
        import.meta.url
      ),
      'utf8'
    )
    const diagnosticsSource = readFileSync(
      new URL(
        '../src/features/system-settings/maintenance/npm-cli-version-diagnostics.tsx',
        import.meta.url
      ),
      'utf8'
    )

    expect(performanceSource).toContain(
      "const NPM_CLI_VERSION_DIAGNOSTICS_TIMEOUT_MS = 10000"
    )
    expect(performanceSource).toMatch(
      /const diagnosticsRequestConfig: DiagnosticsRequestConfig = \{[\s\S]*?timeout: NPM_CLI_VERSION_DIAGNOSTICS_TIMEOUT_MS,[\s\S]*?skipBusinessError: true,[\s\S]*?skipErrorHandler: true,[\s\S]*?disableDuplicate: true,/s
    )
    expect(performanceSource).toMatch(
      /\.get\(\s*'\/api\/channel\/npm_version_options\/diagnostics',\s*diagnosticsRequestConfig\s*\)/s
    )
    expect(performanceSource).toContain('<NpmCLIVersionDiagnostics')
    expect(performanceSource).not.toContain(
      "'/api/channel/npm_version_options/refresh'"
    )
    expect(diagnosticsSource).toContain('scheduled_runs')
    expect(diagnosticsSource).toContain('last_manual_code')
    expect(diagnosticsSource).toContain('recommended_action')
    expect(diagnosticsSource).toContain('recent_errors')
  })

  test('npm version manual reload is guarded after unmount', () => {
    const source = readFileSync(
      new URL(
        '../src/features/channels/components/header-profile-strategy-editor.tsx',
        import.meta.url
      ),
      'utf8'
    )

    expect(source).toMatch(
      /function reloadVersionOptions\(profile: HeaderProfile\)[\s\S]*?if \(!mountedRef\.current\) return[\s\S]*?setVersionStates\(/s
    )
  })

  test('Codex dynamic header passthrough templates do not include User-Agent', () => {
    for (const key of ['codexCliHeaders', 'codexHeaders'] as const) {
      const payload = PARAM_OVERRIDE_TEMPLATES[key].payload as {
        operations: Array<Record<string, unknown>>
      }
      const operations = payload.operations
      expect(Array.isArray(operations)).toBe(true)
      const passHeaders = operations.find(
        (operation) => operation.mode === 'pass_headers'
      )
      expect(passHeaders).toBeDefined()
      const headers = passHeaders?.value as string[]
      expect(headers).toContain('X-Codex-Turn-Metadata')
      expect(headers).not.toContain('User-Agent')
    }
  })

  test('Responses image input removal template prunes only message content images', () => {
    const payload = PARAM_OVERRIDE_TEMPLATES.codexWithoutResponsesImageInput
      .payload as {
      operations: Array<Record<string, unknown>>
    }

    expect(payload.operations).toEqual([
      {
        path: 'input.*.content',
        mode: 'prune_objects',
        value: {
          type: 'input_image',
          recursive: true,
        },
      },
    ])
  })

  test('clearParamOverridePreservingUserAgentPassHeaders keeps only User-Agent passthrough', () => {
    const cleared = clearParamOverridePreservingUserAgentPassHeaders(
      JSON.stringify({
        operations: [
          {
            mode: 'pass_headers',
            value: ['User-Agent', 'Originator', 'X-Codex-Turn-Metadata'],
            keep_origin: true,
          },
          {
            mode: 'copy_header',
            from: 'X-Client-Request-Id',
            to: 'Session_id',
            keep_origin: true,
          },
          {
            mode: 'delete',
            path: 'tools.*.custom.input_examples',
          },
        ],
      })
    )

    expect(JSON.parse(cleared)).toEqual({
      operations: [
        {
          mode: 'pass_headers',
          value: ['User-Agent'],
          keep_origin: true,
        },
      ],
    })
    expect(
      clearParamOverridePreservingUserAgentPassHeaders(
        '{"operations":[{"mode":"pass_headers","value":["Originator"]}]}'
      )
    ).toBe('')
    expect(clearParamOverridePreservingUserAgentPassHeaders('{')).toBe('')
  })
})
