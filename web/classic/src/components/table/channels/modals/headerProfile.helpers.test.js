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

import test from 'node:test';
import assert from 'node:assert/strict';

import {
  applyHeaderProfileStrategyToChannelInputs,
  buildHeaderProfilePreviewText,
  buildHeaderProfileStrategySettings,
  buildProfileItems,
  buildSelectedProfileItems,
  createLegacyHeaderProfileDraft,
  disableEmptyHeaderProfileStrategy,
  getHeaderProfileStrategyFromSettings,
  mergeChannelSubmitFormValues,
  normalizeHeaderProfileStrategy,
  removeEquivalentVersionedProfileIds,
  reorderSelectedProfileIds,
  toggleSelectedProfile,
  validateHeaderProfileDraft,
} from './headerProfile.helpers.js';
import {
  AI_CODING_CLI_DEFAULT_PLATFORM,
  buildAiCodingCliVersionMeta,
  buildAiCodingCliUserAgent,
  buildNpmCliVersionOptions,
  buildVersionedAiCodingCliProfile,
  fetchNpmCliVersionOptions,
  HEADER_PROFILE_PRESETS,
  normalizeNpmCliVersionOptions,
} from './headerProfile.constants.js';
import {
  appendParamOverrideTemplatePayload,
  PARAM_OVERRIDE_TEMPLATES,
  stringifyParamOverrideTemplatePayload,
} from '../../../../constants/channel-affinity-template.constants.js';

test('builtin AI CLI profiles distinguish fixed headers from required passthrough', () => {
  assert.equal(HEADER_PROFILE_PRESETS['codex-cli'].passthroughRequired, false);
  assert.equal(
    HEADER_PROFILE_PRESETS['codex-desktop'].passthroughRequired,
    false,
  );
  assert.equal(
    HEADER_PROFILE_PRESETS['claude-code'].passthroughRequired,
    false,
  );
  assert.equal(HEADER_PROFILE_PRESETS['gemini-cli'].passthroughRequired, false);
  assert.equal(HEADER_PROFILE_PRESETS['opencode'], undefined);
  assert.match(
    HEADER_PROFILE_PRESETS['codex-cli'].description,
    /显式选择 Codex CLI 请求头透传模板/,
  );
  assert.equal(
    HEADER_PROFILE_PRESETS['codex-desktop'].headers['User-Agent'],
    'Codex Desktop/0.133.0-alpha.1 (Mac OS 15.7.3; x86_64) unknown (Codex Desktop; 26.519.41501)',
  );
  assert.equal(
    HEADER_PROFILE_PRESETS['codex-desktop'].headers.Originator,
    'Codex Desktop',
  );
  assert.match(
    HEADER_PROFILE_PRESETS['codex-desktop'].description,
    /显式选择 Codex Desktop 请求头透传模板/,
  );
  assert.match(
    HEADER_PROFILE_PRESETS['claude-code'].description,
    /显式选择 Claude CLI 请求头透传模板/,
  );
  assert.equal(
    HEADER_PROFILE_PRESETS['gemini-cli'].headers['User-Agent'],
    'GeminiCLI/0.44.0/gemini-3.1-pro-preview (darwin; x64; terminal)',
  );
  assert.match(
    HEADER_PROFILE_PRESETS['gemini-cli'].description,
    /x-goog-api-client/,
  );
  assert.equal(HEADER_PROFILE_PRESETS['qwen-code'].passthroughRequired, false);
  assert.equal(
    HEADER_PROFILE_PRESETS['qwen-code'].headers['User-Agent'],
    'QwenCode/0.16.2 (darwin; x64)',
  );
  assert.match(
    HEADER_PROFILE_PRESETS['qwen-code'].description,
    /显式选择 Qwen Code 请求头透传模板/,
  );
  assert.equal(HEADER_PROFILE_PRESETS['droid'].passthroughRequired, false);
  assert.equal(
    HEADER_PROFILE_PRESETS['droid'].headers['User-Agent'],
    'factory-cli/0.135.0',
  );
  assert.match(
    HEADER_PROFILE_PRESETS['droid'].description,
    /显式选择 Droid CLI 请求头透传模板/,
  );
});

test('builtin AI CLI profiles expose default latest version metadata', () => {
  assert.deepEqual(
    [
      ['codex-cli', '@openai/codex'],
      ['claude-code', '@anthropic-ai/claude-code'],
      ['gemini-cli', '@google/gemini-cli'],
      ['qwen-code', '@qwen-code/qwen-code'],
      ['droid', 'droid'],
    ].map(([profileId, packageName]) => [
      profileId,
      buildAiCodingCliVersionMeta(HEADER_PROFILE_PRESETS[profileId], 'latest'),
      packageName,
    ]),
    [
      [
        'codex-cli',
        {
          baseProfileId: 'codex-cli',
          packageName: '@openai/codex',
          source: 'npm',
          version: 'latest',
          platform: AI_CODING_CLI_DEFAULT_PLATFORM,
        },
        '@openai/codex',
      ],
      [
        'claude-code',
        {
          baseProfileId: 'claude-code',
          packageName: '@anthropic-ai/claude-code',
          source: 'npm',
          version: 'latest',
          platform: AI_CODING_CLI_DEFAULT_PLATFORM,
        },
        '@anthropic-ai/claude-code',
      ],
      [
        'gemini-cli',
        {
          baseProfileId: 'gemini-cli',
          packageName: '@google/gemini-cli',
          source: 'npm',
          version: 'latest',
          platform: AI_CODING_CLI_DEFAULT_PLATFORM,
        },
        '@google/gemini-cli',
      ],
      [
        'qwen-code',
        {
          baseProfileId: 'qwen-code',
          packageName: '@qwen-code/qwen-code',
          source: 'npm',
          version: 'latest',
          platform: AI_CODING_CLI_DEFAULT_PLATFORM,
        },
        '@qwen-code/qwen-code',
      ],
      [
        'droid',
        {
          baseProfileId: 'droid',
          packageName: 'droid',
          source: 'npm',
          version: 'latest',
          platform: AI_CODING_CLI_DEFAULT_PLATFORM,
        },
        'droid',
      ],
    ],
  );
  assert.equal(
    buildAiCodingCliVersionMeta(
      HEADER_PROFILE_PRESETS['codex-desktop'],
      'latest',
    ),
    null,
  );
});

test('Codex CLI builtin profile does not reuse codex exec request identity', () => {
  const headers = HEADER_PROFILE_PRESETS['codex-cli'].headers;
  const serializedHeaders = JSON.stringify(headers).toLowerCase();

  assert.equal(headers.Originator, 'codex-tui');
  assert.match(headers['User-Agent'], /^codex-tui\//);
  assert.doesNotMatch(serializedHeaders, /codex_exec/);
  assert.doesNotMatch(serializedHeaders, /source=exec/);
});

test('Codex Desktop builtin profile uses desktop app identity', () => {
  const headers = HEADER_PROFILE_PRESETS['codex-desktop'].headers;

  assert.match(headers['User-Agent'], /^Codex Desktop\//);
  assert.equal(headers.Originator, 'Codex Desktop');
  assert.doesNotMatch(headers['User-Agent'], /codex-cli@/);
  assert.doesNotMatch(headers['User-Agent'], /codex-tui/);
});

test('npm cli version options use latest first and keep five stable choices', () => {
  const options = buildNpmCliVersionOptions({
    'dist-tags': { latest: '0.134.0' },
    versions: {
      '1.0.0': {},
      '1.1.0': {},
      '1.2.0-beta.1': {},
      '1.2.0': {},
      '1.3.0': {},
      '1.4.0': {},
      '1.5.0-alpha.1': {},
    },
  });

  assert.deepEqual(
    options.map((option) => option.value),
    ['latest', '0.134.0', '1.4.0', '1.3.0', '1.2.0', '1.1.0'],
  );
  assert.equal(options[0].isLatest, true);
  assert.equal(options[0].resolvedVersion, '0.134.0');
});

test('npm cli version options use highest stable when latest tag is missing', () => {
  const options = buildNpmCliVersionOptions({
    'dist-tags': {},
    versions: {
      '1.0.0': {},
      '1.2.0': {},
      '1.3.0-beta.1': {},
    },
  });

  assert.deepEqual(
    options.map((option) => option.value),
    ['latest', '1.2.0', '1.0.0'],
  );
  assert.equal(options[0].resolvedVersion, '1.2.0');
});

test('npm cli version options ignore invalid latest tags', () => {
  const options = buildNpmCliVersionOptions({
    'dist-tags': {
      latest: '1.2.0\nInjected',
    },
    versions: {
      '1.0.0': {},
      '1.2.0': {},
    },
  });

  assert.deepEqual(
    options.map((option) => option.value),
    ['latest', '1.2.0', '1.0.0'],
  );
  assert.equal(options[0].resolvedVersion, '1.2.0');
});

test('normalizeNpmCliVersionOptions keeps backend option contract strict', () => {
  assert.deepEqual(
    normalizeNpmCliVersionOptions([
      {
        value: 'latest',
        label: 'latest (1.0.0)',
        is_latest: true,
        resolved_version: '1.0.0',
      },
      { value: '0.9.0' },
      { value: '2.0.0\nInjected' },
      { value: '' },
      null,
    ]),
    [
      {
        value: 'latest',
        label: 'latest (1.0.0)',
        isLatest: true,
        resolvedVersion: '1.0.0',
      },
      {
        value: '0.9.0',
        label: '0.9.0',
        isLatest: false,
        resolvedVersion: '0.9.0',
      },
    ],
  );
});

test('normalizeNpmCliVersionOptions upgrades legacy latest options', () => {
  assert.deepEqual(
    normalizeNpmCliVersionOptions([
      { value: '1.0.0', label: '1.0.0 (latest)', is_latest: true },
      { value: '1.0.0' },
      { value: '0.9.0' },
    ]),
    [
      {
        value: 'latest',
        label: '1.0.0 (latest)',
        isLatest: true,
        resolvedVersion: '1.0.0',
      },
      {
        value: '1.0.0',
        label: '1.0.0',
        isLatest: false,
        resolvedVersion: '1.0.0',
      },
      {
        value: '0.9.0',
        label: '0.9.0',
        isLatest: false,
        resolvedVersion: '0.9.0',
      },
    ],
  );
});

test('normalizeNpmCliVersionOptions keeps latest alias first even if backend order changes', () => {
  assert.deepEqual(
    normalizeNpmCliVersionOptions([
      { value: '0.9.0' },
      {
        value: 'latest',
        label: 'latest (1.0.0)',
        is_latest: true,
        resolved_version: '1.0.0',
      },
      { value: '1.0.0' },
    ]),
    [
      {
        value: 'latest',
        label: 'latest (1.0.0)',
        isLatest: true,
        resolvedVersion: '1.0.0',
      },
      {
        value: '0.9.0',
        label: '0.9.0',
        isLatest: false,
        resolvedVersion: '0.9.0',
      },
      {
        value: '1.0.0',
        label: '1.0.0',
        isLatest: false,
        resolvedVersion: '1.0.0',
      },
    ],
  );
});

test('fetchNpmCliVersionOptions requests new-api backend instead of npm registry', async () => {
  let requestedUrl = '';
  let requestedOptions = null;
  const options = await fetchNpmCliVersionOptions(
    '@openai/codex',
    async (url, requestOptions) => {
      requestedUrl = url;
      requestedOptions = requestOptions;
      return {
        data: {
          success: true,
          data: [
            {
              value: 'latest',
              label: 'latest (1.0.0)',
              isLatest: true,
              resolvedVersion: '1.0.0',
            },
          ],
        },
      };
    },
  );

  assert.equal(requestedUrl, '/api/channel/npm_version_options');
  assert.deepEqual(requestedOptions.params, { package: '@openai/codex' });
  assert.equal(requestedOptions.skipErrorHandler, true);
  assert.equal(requestedOptions.disableDuplicate, true);
  assert.equal(requestedOptions.timeout, 5000);
  assert.deepEqual(options, [
    {
      value: 'latest',
      label: 'latest (1.0.0)',
      isLatest: true,
      resolvedVersion: '1.0.0',
    },
  ]);
});

test('fetchNpmCliVersionOptions rejects failed backend responses', async () => {
  await assert.rejects(
    fetchNpmCliVersionOptions('@openai/codex', async () => ({
      data: {
        success: false,
        message: 'package is not allowed',
      },
    })),
    /package is not allowed/,
  );
});

test('versioned AI CLI profiles generate pinned User-Agent snapshots', () => {
  const codexProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['codex-cli'],
    '0.134.0',
  );
  const claudeProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['claude-code'],
    '2.1.153',
  );

  assert.equal(codexProfile.id, 'codex-cli@0.134.0');
  assert.equal(codexProfile.versionMeta.packageName, '@openai/codex');
  assert.equal(
    codexProfile.headers['User-Agent'],
    buildAiCodingCliUserAgent('codex-cli', '0.134.0'),
  );
  assert.equal(codexProfile.headers.Originator, 'codex-tui');
  assert.equal(
    claudeProfile.headers['User-Agent'],
    'claude-cli/2.1.153 (external, sdk-cli)',
  );
});

test('Codex Desktop profile is not converted to codex-cli version ids', () => {
  const codexDesktopProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['codex-desktop'],
    '0.133.0-alpha.1',
  );

  assert.equal(codexDesktopProfile.id, undefined);
  assert.equal(codexDesktopProfile.key, 'codex-desktop');
  assert.equal(codexDesktopProfile.versionMeta, undefined);
  assert.match(codexDesktopProfile.headers['User-Agent'], /^Codex Desktop\//);
  assert.equal(codexDesktopProfile.headers.Originator, 'Codex Desktop');
});

test('param override template payloads can replace rule template JSON', () => {
  const text = stringifyParamOverrideTemplatePayload(
    PARAM_OVERRIDE_TEMPLATES.codexHeaders.payload,
  );

  assert.deepEqual(JSON.parse(text), {
    operations: [
      {
        mode: 'pass_headers',
        value: [
          'User-Agent',
          'Originator',
          'Session_id',
          'Session-Id',
          'Thread-Id',
          'X-Codex-Beta-Features',
          'X-Codex-Turn-Metadata',
          'X-Codex-Window-Id',
          'X-Client-Request-Id',
        ],
        keep_origin: true,
      },
      {
        mode: 'copy_header',
        from: 'X-Client-Request-Id',
        to: 'Session_id',
        keep_origin: true,
      },
    ],
  });
});

test('legacy combined param override preset is absent', () => {
  assert.equal(
    Object.hasOwn(PARAM_OVERRIDE_TEMPLATES, 'codexHeadersWithoutImageTool'),
    false,
  );
});

test('param override preset templates contain expected operations', () => {
  for (const [key, template] of Object.entries(PARAM_OVERRIDE_TEMPLATES)) {
    const operations = template.payload.operations;
    assert.ok(Array.isArray(operations), key);
    assert.equal(
      operations.length,
      ['codexCliHeaders', 'codexHeaders'].includes(key) ? 2 : 1,
      key,
    );
  }
});

test('param override template append preserves existing operations order', () => {
  const text = appendParamOverrideTemplatePayload(
    JSON.stringify({
      operations: [
        {
          mode: 'trim_prefix',
          path: 'model',
          value: 'openai/',
        },
      ],
    }),
    PARAM_OVERRIDE_TEMPLATES.geminiHeaders.payload,
  );

  assert.deepEqual(JSON.parse(text).operations, [
    {
      mode: 'trim_prefix',
      path: 'model',
      value: 'openai/',
    },
    {
      mode: 'pass_headers',
      value: ['X-Goog-Api-Client'],
      keep_origin: true,
    },
  ]);
});

test('param override template append rejects non-object JSON', () => {
  assert.throws(
    () =>
      appendParamOverrideTemplatePayload(
        '[]',
        PARAM_OVERRIDE_TEMPLATES.codexHeaders.payload,
      ),
    /JSON object/,
  );
});

test('normalizeHeaderProfileStrategy falls back to fixed', () => {
  assert.equal(normalizeHeaderProfileStrategy(undefined), 'fixed');
  assert.equal(normalizeHeaderProfileStrategy('unknown'), 'fixed');
  assert.equal(normalizeHeaderProfileStrategy('round_robin'), 'round_robin');
});

test('toggleSelectedProfile replaces selection in fixed mode', () => {
  const result = toggleSelectedProfile({
    strategy: 'fixed',
    selectedProfileKeys: ['codex-cli'],
    profileKey: 'claude-code',
  });

  assert.deepEqual(result, ['claude-code']);
});

test('toggleSelectedProfile deselects current profile in fixed mode', () => {
  const result = toggleSelectedProfile({
    strategy: 'fixed',
    selectedProfileIds: ['codex-cli@0.134.0'],
    profileId: 'codex-cli@0.134.0',
  });

  assert.deepEqual(result, []);
});

test('toggleSelectedProfile toggles multiple items in round_robin mode', () => {
  const added = toggleSelectedProfile({
    strategy: 'round_robin',
    selectedProfileKeys: ['codex-cli'],
    profileKey: 'claude-code',
  });
  const removed = toggleSelectedProfile({
    strategy: 'round_robin',
    selectedProfileKeys: added,
    profileKey: 'codex-cli',
  });

  assert.deepEqual(added, ['codex-cli', 'claude-code']);
  assert.deepEqual(removed, ['claude-code']);
});

test('round robin versioned profile selection appends different base templates', () => {
  const codexProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['codex-cli'],
    'latest',
    'npm',
    '0.134.0',
  );
  const claudeProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['claude-code'],
    'latest',
    'npm',
    '2.1.153',
  );
  const currentSelectedProfileIds = removeEquivalentVersionedProfileIds(
    [codexProfile.id],
    [codexProfile],
    claudeProfile.id,
    claudeProfile,
  );
  const nextSelectedProfileIds = toggleSelectedProfile({
    strategy: 'round_robin',
    selectedProfileIds: currentSelectedProfileIds,
    profileId: claudeProfile.id,
  });

  assert.deepEqual(nextSelectedProfileIds, [
    'codex-cli@latest',
    'claude-code@latest',
  ]);
});

test('fixed versioned profile selection replaces existing base templates', () => {
  const codexProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['codex-cli'],
    'latest',
    'npm',
    '0.134.0',
  );
  const claudeProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['claude-code'],
    'latest',
    'npm',
    '2.1.153',
  );
  const currentSelectedProfileIds = removeEquivalentVersionedProfileIds(
    [codexProfile.id],
    [codexProfile],
    claudeProfile.id,
    claudeProfile,
  );
  const nextSelectedProfileIds = toggleSelectedProfile({
    strategy: 'fixed',
    selectedProfileIds: currentSelectedProfileIds,
    profileId: claudeProfile.id,
  });

  assert.deepEqual(nextSelectedProfileIds, ['claude-code@latest']);
});

test('random versioned profile selection keeps existing template when adding another base template', () => {
  const codexProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['codex-cli'],
    'latest',
    'npm',
    '0.134.0',
  );
  const qwenProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['qwen-code'],
    'latest',
    'npm',
    '0.16.2',
  );
  const currentSelectedProfileIds = removeEquivalentVersionedProfileIds(
    [codexProfile.id],
    [codexProfile],
    qwenProfile.id,
    qwenProfile,
  );
  const nextSelectedProfileIds = toggleSelectedProfile({
    strategy: 'random',
    selectedProfileIds: currentSelectedProfileIds,
    profileId: qwenProfile.id,
  });

  assert.deepEqual(nextSelectedProfileIds, [
    'codex-cli@latest',
    'qwen-code@latest',
  ]);
});

test('buildSelectedProfileItems keeps structured headers while main fields stay name/group/preview', () => {
  const items = buildSelectedProfileItems(['codex-cli']);

  assert.equal(items.length, 1);
  assert.equal(items[0].name, HEADER_PROFILE_PRESETS['codex-cli'].name);
  assert.equal(items[0].category, HEADER_PROFILE_PRESETS['codex-cli'].group);
  assert.doesNotMatch(items[0].previewText, /codex_exec/i);
  assert.deepEqual(
    items[0].headers,
    HEADER_PROFILE_PRESETS['codex-cli'].headers,
  );
  assert.equal(items[0].name, 'Codex CLI');
  assert.notEqual(items[0].passthroughRequired, true);
  assert.match(items[0].description, /显式选择 Codex CLI 请求头透传模板/);
});

test('buildSelectedProfileItems resolves builtin latest ids without snapshots', () => {
  const items = buildSelectedProfileItems(['codex-cli@latest']);

  assert.equal(items.length, 1);
  assert.equal(items[0].id, 'codex-cli@latest');
  assert.equal(items[0].missing, undefined);
  assert.equal(items[0].versionMeta.baseProfileId, 'codex-cli');
  assert.equal(items[0].versionMeta.packageName, '@openai/codex');
  assert.equal(items[0].versionMeta.version, 'latest');
  assert.equal(items[0].versionMeta.platform, AI_CODING_CLI_DEFAULT_PLATFORM);
  assert.equal(
    items[0].headers['User-Agent'],
    buildAiCodingCliUserAgent('codex-cli', '0.134.0'),
  );
});

test('buildSelectedProfileItems resolves builtin pinned version ids without snapshots', () => {
  const items = buildSelectedProfileItems(['claude-code@2.2.0']);

  assert.equal(items.length, 1);
  assert.equal(items[0].id, 'claude-code@2.2.0');
  assert.equal(items[0].missing, undefined);
  assert.equal(items[0].versionMeta.baseProfileId, 'claude-code');
  assert.equal(items[0].versionMeta.packageName, '@anthropic-ai/claude-code');
  assert.equal(items[0].versionMeta.version, '2.2.0');
  assert.equal(items[0].versionMeta.platform, AI_CODING_CLI_DEFAULT_PLATFORM);
  assert.equal(
    items[0].headers['User-Agent'],
    buildAiCodingCliUserAgent('claude-code', '2.2.0'),
  );
});

test('buildSelectedProfileItems completes partial latest metadata from profile id', () => {
  const items = buildSelectedProfileItems(
    ['qwen-code@latest'],
    [],
    [
      {
        id: 'qwen-code@latest',
        name: 'Qwen Code latest',
        category: 'ai_coding_cli',
        scope: 'builtin',
        readonly: true,
        headers: {
          'User-Agent': buildAiCodingCliUserAgent('qwen-code', '0.16.2'),
        },
        version_meta: {
          source: 'npm',
          version: 'latest',
        },
      },
    ],
  );

  assert.equal(items.length, 1);
  assert.deepEqual(items[0].versionMeta, {
    baseProfileId: 'qwen-code',
    packageName: '@qwen-code/qwen-code',
    source: 'npm',
    version: 'latest',
    platform: AI_CODING_CLI_DEFAULT_PLATFORM,
  });
});

test('buildSelectedProfileItems prefers saved platform snapshot over builtin latest alias', () => {
  const snapshot = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['gemini-cli'],
    'latest',
    'npm',
    '0.50.0',
    'windows-arm64',
  );
  const items = buildSelectedProfileItems(
    ['gemini-cli@latest'],
    [],
    [snapshot],
  );

  assert.equal(items.length, 1);
  assert.equal(items[0].id, 'gemini-cli@latest');
  assert.equal(items[0].versionMeta.platform, 'windows-arm64');
  assert.equal(
    items[0].headers['User-Agent'],
    buildAiCodingCliUserAgent('gemini-cli', '0.50.0', 'windows-arm64'),
  );
});

test('buildProfileItems merges builtin and user profiles into a normalized list', () => {
  const items = buildProfileItems([
    {
      id: 'hp_custom',
      name: 'My Custom Profile',
      category: 'custom',
      scope: 'user',
      headers: {
        'User-Agent': 'MyAgent/1.0',
        'X-Test': 'yes',
      },
    },
  ]);

  const builtin = items.find((item) => item.id === 'codex-cli');
  const custom = items.find((item) => item.id === 'hp_custom');

  assert.ok(builtin);
  assert.equal(builtin.scope, 'builtin');
  assert.equal(builtin.readonly, true);
  assert.doesNotMatch(builtin.previewText, /codex_exec/i);

  assert.ok(custom);
  assert.equal(custom.scope, 'user');
  assert.equal(custom.readonly, false);
  assert.equal(custom.previewText, 'User-Agent: MyAgent/1.0\nX-Test: yes');
});

test('buildProfileItems does not expose implicit latest aliases in the library', () => {
  const ids = buildProfileItems([]).map((item) => item.id);

  assert.equal(ids.includes('codex-cli'), true);
  assert.equal(ids.includes('claude-code'), true);
  assert.equal(ids.includes('codex-cli@latest'), false);
  assert.equal(ids.includes('claude-code@latest'), false);
});

test('buildSelectedProfileItems keeps unknown selected ids removable', () => {
  const items = buildSelectedProfileItems(['missing-profile'], []);

  assert.deepEqual(items, [
    {
      id: 'missing-profile',
      key: 'missing-profile',
      name: 'missing-profile',
      category: 'custom',
      scope: 'missing',
      readonly: true,
      headers: {},
      previewText: '',
      missing: true,
    },
  ]);
});

test('buildHeaderProfilePreviewText returns empty string for empty headers', () => {
  assert.equal(buildHeaderProfilePreviewText({}), '');
});

test('getHeaderProfileStrategyFromSettings reads strategy from settings json', () => {
  assert.deepEqual(
    getHeaderProfileStrategyFromSettings(
      JSON.stringify({
        azure_responses_version: 'preview',
        header_profile_strategy: {
          enabled: true,
          mode: 'random',
          selected_profile_ids: [' a ', 'b', 'a', ''],
          profiles: [
            {
              id: 'a',
              name: 'Profile A',
              headers: { 'User-Agent': 'A/1.0' },
            },
          ],
        },
      }),
    ),
    {
      enabled: true,
      mode: 'random',
      selectedProfileIds: ['a', 'b'],
      profiles: [
        {
          id: 'a',
          key: 'a',
          name: 'Profile A',
          category: 'custom',
          scope: 'user',
          readonly: false,
          description: '',
          headers: { 'User-Agent': 'A/1.0' },
          previewText: 'User-Agent: A/1.0',
        },
      ],
    },
  );
});

test('buildHeaderProfileStrategySettings writes and removes header_profile_strategy without touching other settings', () => {
  const written = buildHeaderProfileStrategySettings(
    JSON.stringify({
      azure_responses_version: 'preview',
    }),
    {
      enabled: true,
      mode: 'round_robin',
      selectedProfileIds: ['profile-a', ' profile-b ', 'profile-a'],
      profiles: [
        {
          id: 'profile-a',
          name: 'Profile A',
          category: 'custom',
          headers: { 'User-Agent': 'A/1.0' },
        },
      ],
    },
  );

  assert.deepEqual(JSON.parse(written), {
    azure_responses_version: 'preview',
    header_profile_strategy: {
      enabled: true,
      mode: 'round_robin',
      selected_profile_ids: ['profile-a', 'profile-b'],
      profiles: [
        {
          id: 'profile-a',
          name: 'Profile A',
          category: 'custom',
          scope: 'user',
          readonly: false,
          description: '',
          headers: { 'User-Agent': 'A/1.0' },
        },
      ],
    },
  });

  const removed = buildHeaderProfileStrategySettings(written, null);
  assert.deepEqual(JSON.parse(removed), {
    azure_responses_version: 'preview',
  });
});

test('disableEmptyHeaderProfileStrategy turns enabled empty selections off', () => {
  assert.deepEqual(
    disableEmptyHeaderProfileStrategy({
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: [],
    }),
    {
      enabled: false,
      mode: 'fixed',
      selectedProfileIds: [],
      profiles: [],
    },
  );

  assert.deepEqual(
    disableEmptyHeaderProfileStrategy({
      enabled: true,
      mode: 'round_robin',
      selected_profile_ids: [''],
      profiles: [{ id: 'stale-profile' }],
    }),
    {
      enabled: false,
      mode: 'round_robin',
      selected_profile_ids: [''],
      selectedProfileIds: [],
      profiles: [],
    },
  );
});

test('disableEmptyHeaderProfileStrategy keeps enabled selections intact', () => {
  assert.deepEqual(
    disableEmptyHeaderProfileStrategy({
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: ['codex-cli'],
      profiles: [{ id: 'codex-cli' }],
    }),
    {
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: ['codex-cli'],
      profiles: [{ id: 'codex-cli' }],
    },
  );
});

test('buildHeaderProfileStrategySettings writes latest metadata for Codex profile', () => {
  const written = buildHeaderProfileStrategySettings('{}', {
    enabled: true,
    mode: 'fixed',
    selectedProfileIds: ['codex-cli'],
    profiles: [HEADER_PROFILE_PRESETS['codex-cli']],
  });

  const parsed = JSON.parse(written);
  assert.deepEqual(parsed.header_profile_strategy.profiles, [
    {
      id: 'codex-cli',
      name: 'Codex CLI',
      category: 'ai_coding_cli',
      scope: 'builtin',
      readonly: true,
      description: HEADER_PROFILE_PRESETS['codex-cli'].description,
      headers: HEADER_PROFILE_PRESETS['codex-cli'].headers,
      version_meta: {
        base_profile_id: 'codex-cli',
        package_name: '@openai/codex',
        source: 'npm',
        version: 'latest',
        platform: AI_CODING_CLI_DEFAULT_PLATFORM,
      },
    },
  ]);
  assert.equal(
    Object.hasOwn(
      parsed.header_profile_strategy.profiles[0],
      'passthroughRequired',
    ),
    false,
  );
});

test('buildHeaderProfileStrategySettings writes latest metadata for all AI CLI presets', () => {
  const profileIds = [
    'codex-cli',
    'claude-code',
    'gemini-cli',
    'qwen-code',
    'droid',
  ];
  const written = buildHeaderProfileStrategySettings('{}', {
    enabled: true,
    mode: 'round_robin',
    selectedProfileIds: profileIds,
    profiles: profileIds.map((profileId) => HEADER_PROFILE_PRESETS[profileId]),
  });

  const parsed = JSON.parse(written);
  assert.deepEqual(
    parsed.header_profile_strategy.profiles.map(
      (profile) => profile.version_meta,
    ),
    [
      {
        base_profile_id: 'codex-cli',
        package_name: '@openai/codex',
        source: 'npm',
        version: 'latest',
        platform: AI_CODING_CLI_DEFAULT_PLATFORM,
      },
      {
        base_profile_id: 'claude-code',
        package_name: '@anthropic-ai/claude-code',
        source: 'npm',
        version: 'latest',
        platform: AI_CODING_CLI_DEFAULT_PLATFORM,
      },
      {
        base_profile_id: 'gemini-cli',
        package_name: '@google/gemini-cli',
        source: 'npm',
        version: 'latest',
        platform: AI_CODING_CLI_DEFAULT_PLATFORM,
      },
      {
        base_profile_id: 'qwen-code',
        package_name: '@qwen-code/qwen-code',
        source: 'npm',
        version: 'latest',
        platform: AI_CODING_CLI_DEFAULT_PLATFORM,
      },
      {
        base_profile_id: 'droid',
        package_name: 'droid',
        source: 'npm',
        version: 'latest',
        platform: AI_CODING_CLI_DEFAULT_PLATFORM,
      },
    ],
  );
});

test('applyHeaderProfileStrategyToChannelInputs does not add Codex pass_headers when applying Codex template', () => {
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override:
        '{"operations":[{"mode":"trim_prefix","path":"model","value":"openai/"}]}',
    },
    strategy: {
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: ['codex-cli'],
    },
    headerProfiles: [],
    snapshotProfiles: [],
  });

  const settings = JSON.parse(result.settings);
  const paramOverride = JSON.parse(result.param_override);

  assert.deepEqual(settings.header_profile_strategy.selected_profile_ids, [
    'codex-cli',
  ]);
  assert.deepEqual(paramOverride.operations, [
    {
      mode: 'trim_prefix',
      path: 'model',
      value: 'openai/',
    },
  ]);
});

test('applyHeaderProfileStrategyToChannelInputs persists selected CLI version snapshot', () => {
  const versionedProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['codex-cli'],
    'latest',
    'npm',
    '0.134.0',
  );
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override: '{}',
    },
    strategy: {
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: [versionedProfile.id],
      profiles: [versionedProfile],
    },
    headerProfiles: [],
    snapshotProfiles: [],
  });

  const settings = JSON.parse(result.settings);

  assert.deepEqual(settings.header_profile_strategy.selected_profile_ids, [
    'codex-cli@latest',
  ]);
  assert.deepEqual(settings.header_profile_strategy.profiles[0].version_meta, {
    base_profile_id: 'codex-cli',
    package_name: '@openai/codex',
    source: 'npm',
    version: 'latest',
    platform: AI_CODING_CLI_DEFAULT_PLATFORM,
  });
  assert.equal(
    settings.header_profile_strategy.profiles[0].headers['User-Agent'],
    buildAiCodingCliUserAgent('codex-cli', '0.134.0'),
  );
});

test('buildVersionedAiCodingCliProfile keeps latest source dynamic with resolved snapshot', () => {
  const versionedProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['claude-code'],
    'latest',
    'npm',
    '2.2.0',
  );

  assert.equal(versionedProfile.id, 'claude-code@latest');
  assert.equal(versionedProfile.versionMeta.source, 'npm');
  assert.equal(versionedProfile.versionMeta.version, 'latest');
  assert.equal(
    versionedProfile.headers['User-Agent'],
    buildAiCodingCliUserAgent('claude-code', '2.2.0'),
  );
});

test('buildVersionedAiCodingCliProfile writes platform metadata and platform UA', () => {
  const versionedProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['codex-cli'],
    'latest',
    'npm',
    '0.200.0',
    'linux-x64',
  );

  assert.equal(versionedProfile.id, 'codex-cli@latest');
  assert.equal(versionedProfile.versionMeta.platform, 'linux-x64');
  assert.match(versionedProfile.name, /Linux x64$/);
  assert.equal(
    versionedProfile.headers['User-Agent'],
    buildAiCodingCliUserAgent('codex-cli', '0.200.0', 'linux-x64'),
  );
});

test('buildVersionedAiCodingCliProfile supports arm64 platform snapshots', () => {
  const versionedProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['qwen-code'],
    'latest',
    'npm',
    '0.20.0',
    'linux-arm64',
  );

  assert.equal(versionedProfile.versionMeta.platform, 'linux-arm64');
  assert.match(versionedProfile.name, /Linux arm64$/);
  assert.equal(
    versionedProfile.headers['User-Agent'],
    buildAiCodingCliUserAgent('qwen-code', '0.20.0', 'linux-arm64'),
  );
});

test('buildVersionedAiCodingCliProfile rejects invalid version snapshots', () => {
  const versionedProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['claude-code'],
    '2.2.0\nInjected',
  );

  assert.equal(versionedProfile, HEADER_PROFILE_PRESETS['claude-code']);
});

test('buildVersionedAiCodingCliProfile uses fallback snapshot when latest is unresolved', () => {
  const versionedProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['qwen-code'],
    'latest',
  );

  assert.equal(versionedProfile.id, 'qwen-code@latest');
  assert.equal(versionedProfile.versionMeta.version, 'latest');
  assert.equal(
    versionedProfile.headers['User-Agent'],
    buildAiCodingCliUserAgent('qwen-code', '0.16.2'),
  );
});

test('applyHeaderProfileStrategyToChannelInputs persists selected CLI platform snapshot', () => {
  const versionedProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['gemini-cli'],
    'latest',
    'npm',
    '0.50.0',
    'windows-x64',
  );
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override: '{}',
    },
    strategy: {
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: [versionedProfile.id],
      profiles: [versionedProfile],
    },
    headerProfiles: [],
    snapshotProfiles: [],
  });

  const settings = JSON.parse(result.settings);

  assert.equal(
    settings.header_profile_strategy.profiles[0].version_meta.platform,
    'windows-x64',
  );
  assert.equal(
    settings.header_profile_strategy.profiles[0].headers['User-Agent'],
    buildAiCodingCliUserAgent('gemini-cli', '0.50.0', 'windows-x64'),
  );
});

test('applyHeaderProfileStrategyToChannelInputs preserves saved platform when strategy profiles are omitted', () => {
  const versionedProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['codex-cli'],
    'latest',
    'npm',
    '0.200.0',
    'linux-arm64',
  );
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override: '{}',
    },
    strategy: {
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: [versionedProfile.id],
    },
    headerProfiles: [],
    snapshotProfiles: [versionedProfile],
  });

  const settings = JSON.parse(result.settings);

  assert.equal(
    settings.header_profile_strategy.profiles[0].version_meta.platform,
    'linux-arm64',
  );
  assert.equal(
    settings.header_profile_strategy.profiles[0].headers['User-Agent'],
    buildAiCodingCliUserAgent('codex-cli', '0.200.0', 'linux-arm64'),
  );
});

test('applyHeaderProfileStrategyToChannelInputs persists multiple selected CLI version snapshots', () => {
  const codexProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['codex-cli'],
    'latest',
    'npm',
    '0.134.0',
  );
  const claudeProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['claude-code'],
    '2.1.153',
  );
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override: '{}',
    },
    strategy: {
      enabled: true,
      mode: 'round_robin',
      selectedProfileIds: [codexProfile.id, claudeProfile.id],
      profiles: [codexProfile, claudeProfile],
    },
    headerProfiles: [],
    snapshotProfiles: [],
  });

  const settings = JSON.parse(result.settings);

  assert.deepEqual(settings.header_profile_strategy.selected_profile_ids, [
    'codex-cli@latest',
    'claude-code@2.1.153',
  ]);
  assert.deepEqual(
    settings.header_profile_strategy.profiles.map(
      (profile) => profile.version_meta.version,
    ),
    ['latest', '2.1.153'],
  );
});

test('applyHeaderProfileStrategyToChannelInputs preserves existing snapshots when only mode changes', () => {
  const codexProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['codex-cli'],
    'latest',
    'npm',
    '0.200.0',
    'linux-arm64',
  );
  const qwenProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['qwen-code'],
    '0.20.0',
    'npm',
    '0.20.0',
    'windows-x64',
  );
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override: '{}',
    },
    strategy: {
      enabled: true,
      mode: 'random',
      selectedProfileIds: [codexProfile.id, qwenProfile.id],
    },
    headerProfiles: [],
    snapshotProfiles: [codexProfile, qwenProfile],
  });

  const settings = JSON.parse(result.settings);

  assert.deepEqual(settings.header_profile_strategy.selected_profile_ids, [
    'codex-cli@latest',
    'qwen-code@0.20.0',
  ]);
  assert.deepEqual(
    settings.header_profile_strategy.profiles.map(
      (profile) => profile.version_meta.platform,
    ),
    ['linux-arm64', 'windows-x64'],
  );
  assert.deepEqual(
    settings.header_profile_strategy.profiles.map(
      (profile) => profile.headers['User-Agent'],
    ),
    [
      buildAiCodingCliUserAgent('codex-cli', '0.200.0', 'linux-arm64'),
      buildAiCodingCliUserAgent('qwen-code', '0.20.0', 'windows-x64'),
    ],
  );
});

test('applyHeaderProfileStrategyToChannelInputs persists latest snapshots for all AI CLI presets', () => {
  const profileInputs = [
    ['codex-cli', '0.134.0'],
    ['claude-code', '2.1.153'],
    ['gemini-cli', '0.44.0'],
    ['qwen-code', '0.16.2'],
    ['droid', '0.135.0'],
  ];
  const versionedProfiles = profileInputs.map(([profileId, resolvedVersion]) =>
    buildVersionedAiCodingCliProfile(
      HEADER_PROFILE_PRESETS[profileId],
      'latest',
      'npm',
      resolvedVersion,
    ),
  );
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override: '{}',
    },
    strategy: {
      enabled: true,
      mode: 'round_robin',
      selectedProfileIds: versionedProfiles.map((profile) => profile.id),
      profiles: versionedProfiles,
    },
    headerProfiles: [],
    snapshotProfiles: [],
  });

  const settings = JSON.parse(result.settings);

  assert.deepEqual(
    settings.header_profile_strategy.selected_profile_ids,
    profileInputs.map(([profileId]) => `${profileId}@latest`),
  );
  assert.deepEqual(
    settings.header_profile_strategy.profiles.map(
      (profile) => profile.version_meta.version,
    ),
    ['latest', 'latest', 'latest', 'latest', 'latest'],
  );
  assert.deepEqual(
    settings.header_profile_strategy.profiles.map(
      (profile) => profile.version_meta.source,
    ),
    ['npm', 'npm', 'npm', 'npm', 'npm'],
  );
  assert.deepEqual(
    settings.header_profile_strategy.profiles.map(
      (profile) => profile.headers['User-Agent'],
    ),
    profileInputs.map(([profileId, resolvedVersion]) =>
      buildAiCodingCliUserAgent(profileId, resolvedVersion),
    ),
  );
});

test('applyHeaderProfileStrategyToChannelInputs does not add built-in CLI passthrough templates', () => {
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override:
        '{"operations":[{"mode":"pass_headers","value":["User-Agent","Originator"],"keep_origin":true}]}',
    },
    strategy: {
      enabled: true,
      mode: 'round_robin',
      selectedProfileIds: [
        'codex-cli',
        'codex-desktop',
        'claude-code',
        'gemini-cli',
        'qwen-code',
        'droid',
      ],
    },
    headerProfiles: [],
    snapshotProfiles: [],
  });

  assert.deepEqual(JSON.parse(result.param_override), {
    operations: [
      {
        mode: 'pass_headers',
        value: ['User-Agent', 'Originator'],
        keep_origin: true,
      },
    ],
  });
});

test('applyHeaderProfileStrategyToChannelInputs keeps param_override unchanged for non-passthrough custom profile', () => {
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override: '{}',
    },
    strategy: {
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: ['custom-fixed'],
    },
    headerProfiles: [
      {
        id: 'custom-fixed',
        name: 'Custom Fixed',
        headers: {
          'User-Agent': 'CustomAgent/1.0',
        },
      },
    ],
    snapshotProfiles: [],
  });

  assert.equal(result.param_override, '{}');
});

test('applyHeaderProfileStrategyToChannelInputs preserves stringified JSON pass_headers values when adding custom required headers', () => {
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override:
        '{"operations":[{"mode":"pass_headers","value":"[\\"X-Trace-Id\\"]","keep_origin":true}]}',
    },
    strategy: {
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: ['custom-cli'],
    },
    headerProfiles: [
      {
        id: 'custom-cli',
        name: 'Custom CLI',
        headers: {
          'X-Client-Z': 'z',
          'User-Agent': 'Custom/1.0',
        },
        passthrough_required: true,
      },
    ],
    snapshotProfiles: [],
  });

  const operations = JSON.parse(result.param_override).operations;
  assert.equal(operations.length, 1);
  assert.deepEqual(operations[0], {
    mode: 'pass_headers',
    value: ['X-Trace-Id', 'User-Agent', 'X-Client-Z'],
    keep_origin: true,
  });
});

test('applyHeaderProfileStrategyToChannelInputs preserves object names pass_headers values when adding custom required headers', () => {
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override:
        '{"operations":[{"mode":"pass_headers","value":{"names":"X-Trace-Id"},"keep_origin":true}]}',
    },
    strategy: {
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: ['custom-cli'],
    },
    headerProfiles: [
      {
        id: 'custom-cli',
        name: 'Custom CLI',
        headers: {
          'X-Client-Z': 'z',
          'User-Agent': 'Custom/1.0',
        },
        passthrough_required: true,
      },
    ],
    snapshotProfiles: [],
  });

  const operations = JSON.parse(result.param_override).operations;
  assert.equal(operations.length, 1);
  assert.deepEqual(operations[0], {
    mode: 'pass_headers',
    value: ['X-Trace-Id', 'User-Agent', 'X-Client-Z'],
    keep_origin: true,
  });
});

test('applyHeaderProfileStrategyToChannelInputs keeps conditional pass_headers separate from custom required passthrough', () => {
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override:
        '{"operations":[{"mode":"pass_headers","value":["X-Trace-Id"],"conditions":[{"path":"model","mode":"prefix","value":"custom"}],"keep_origin":true}]}',
    },
    strategy: {
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: ['custom-cli'],
    },
    headerProfiles: [
      {
        id: 'custom-cli',
        name: 'Custom CLI',
        headers: {
          'X-Client-Z': 'z',
          'User-Agent': 'Custom/1.0',
        },
        passthrough_required: true,
      },
    ],
    snapshotProfiles: [],
  });

  const operations = JSON.parse(result.param_override).operations;
  assert.equal(operations.length, 2);
  assert.deepEqual(operations[0], {
    mode: 'pass_headers',
    value: ['User-Agent', 'X-Client-Z'],
    keep_origin: true,
  });
  assert.deepEqual(operations[1], {
    mode: 'pass_headers',
    value: ['X-Trace-Id'],
    conditions: [{ path: 'model', mode: 'prefix', value: 'custom' }],
    keep_origin: true,
  });
});

test('applyHeaderProfileStrategyToChannelInputs does not backfill built-in pass_headers for legacy strategy on submit', () => {
  const strategy = {
    enabled: true,
    mode: 'round_robin',
    selectedProfileIds: ['codex-cli', 'claude-code'],
    profiles: [
      {
        id: 'codex-cli',
        name: 'Codex CLI',
        category: 'ai_coding_cli',
        scope: 'builtin',
        readonly: true,
        description: HEADER_PROFILE_PRESETS['codex-cli'].description,
        headers: HEADER_PROFILE_PRESETS['codex-cli'].headers,
        passthroughRequired: true,
      },
      {
        id: 'claude-code',
        name: 'Claude Code',
        category: 'ai_coding_cli',
        scope: 'builtin',
        readonly: true,
        description: HEADER_PROFILE_PRESETS['claude-code'].description,
        headers: HEADER_PROFILE_PRESETS['claude-code'].headers,
        passthroughRequired: true,
      },
    ],
  };
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: buildHeaderProfileStrategySettings('{}', strategy),
      param_override: '',
      name: 'legacy-channel',
    },
    strategy,
    headerProfiles: [],
    snapshotProfiles: strategy.profiles,
  });

  assert.equal(result.param_override, '');
});

test('applyHeaderProfileStrategyToChannelInputs keeps legacy Codex builtin snapshot without passthrough flag unchanged', () => {
  const strategy = {
    enabled: true,
    mode: 'fixed',
    selectedProfileIds: ['codex-cli'],
    profiles: [
      {
        id: 'codex-cli',
        name: 'Codex CLI',
        category: 'ai_coding_cli',
        scope: 'builtin',
        readonly: true,
        description: HEADER_PROFILE_PRESETS['codex-cli'].description,
        headers: HEADER_PROFILE_PRESETS['codex-cli'].headers,
      },
    ],
  };
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: buildHeaderProfileStrategySettings('{}', strategy),
      param_override: '',
      name: 'legacy-builtin-channel',
    },
    strategy,
    headerProfiles: [],
    snapshotProfiles: strategy.profiles,
  });

  assert.equal(result.param_override, '');
});

test('applyHeaderProfileStrategyToChannelInputs does not add Gemini pass_headers when applying Gemini template', () => {
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override: '{}',
    },
    strategy: {
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: ['gemini-cli'],
    },
    headerProfiles: [],
    snapshotProfiles: [],
  });

  assert.equal(result.param_override, '{}');
});

test('applyHeaderProfileStrategyToChannelInputs does not add Qwen pass_headers when applying Qwen template', () => {
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override: '{}',
    },
    strategy: {
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: ['qwen-code'],
    },
    headerProfiles: [],
    snapshotProfiles: [],
  });

  assert.equal(result.param_override, '{}');
});

test('applyHeaderProfileStrategyToChannelInputs does not add Droid pass_headers when applying Droid template', () => {
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override: '{}',
    },
    strategy: {
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: ['droid'],
    },
    headerProfiles: [],
    snapshotProfiles: [],
  });

  assert.equal(result.param_override, '{}');
});

test('applyHeaderProfileStrategyToChannelInputs preserves param_override when no selected template requires passthrough', () => {
  const paramOverride =
    '{"operations":[{"mode":"set","path":"temperature","value":0.2}]}';
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override: paramOverride,
    },
    strategy: {
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: ['custom-fixed'],
    },
    headerProfiles: [
      {
        id: 'custom-fixed',
        name: 'Custom Fixed',
        headers: {
          'User-Agent': 'CustomAgent/1.0',
        },
      },
    ],
    snapshotProfiles: [],
  });

  assert.equal(result.param_override, paramOverride);
});

test('applyHeaderProfileStrategyToChannelInputs does not inject pass_headers when strategy is disabled', () => {
  const paramOverride =
    '{"operations":[{"mode":"set","path":"temperature","value":0.2}]}';
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override: paramOverride,
    },
    strategy: {
      enabled: false,
      mode: 'fixed',
      selectedProfileIds: ['codex-cli'],
    },
    headerProfiles: [],
    snapshotProfiles: [],
  });

  assert.equal(result.param_override, paramOverride);
});

test('applyHeaderProfileStrategyToChannelInputs clears header profile without changing param_override', () => {
  const paramOverride =
    '{"operations":[{"mode":"pass_headers","value":["User-Agent"],"keep_origin":true}]}';
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{"header_profile_strategy":{"enabled":true}}',
      param_override: paramOverride,
    },
    strategy: null,
    headerProfiles: [],
    snapshotProfiles: [],
  });

  assert.deepEqual(JSON.parse(result.settings), {});
  assert.equal(result.param_override, paramOverride);
});

test('applyHeaderProfileStrategyToChannelInputs falls back to headers on custom passthrough-required profiles', () => {
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override: '{}',
    },
    strategy: {
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: ['custom-cli'],
    },
    headerProfiles: [
      {
        id: 'custom-cli',
        name: 'Custom CLI',
        headers: {
          'X-Client-Z': 'z',
          'User-Agent': 'Custom/1.0',
        },
        passthrough_required: true,
      },
    ],
    snapshotProfiles: [],
  });

  assert.deepEqual(JSON.parse(result.param_override), {
    operations: [
      {
        mode: 'pass_headers',
        value: ['User-Agent', 'X-Client-Z'],
        keep_origin: true,
      },
    ],
  });
});

test('applyHeaderProfileStrategyToChannelInputs preserves invalid param_override instead of overwriting it', () => {
  const invalidParamOverride = '{"operations":[';
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override: invalidParamOverride,
    },
    strategy: {
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: ['codex-cli'],
    },
    headerProfiles: [],
    snapshotProfiles: [],
  });

  assert.equal(result.param_override, invalidParamOverride);
});

test('applyHeaderProfileStrategyToChannelInputs preserves non-object param_override instead of overwriting it', () => {
  const arrayParamOverride = '[]';
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override: arrayParamOverride,
    },
    strategy: {
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: ['codex-cli'],
    },
    headerProfiles: [],
    snapshotProfiles: [],
  });

  assert.equal(result.param_override, arrayParamOverride);
});

test('mergeChannelSubmitFormValues preserves request policy state when form values omit hidden fields', () => {
  const settings = buildHeaderProfileStrategySettings('{}', {
    enabled: true,
    mode: 'fixed',
    selectedProfileIds: ['codex-cli'],
    profiles: [HEADER_PROFILE_PRESETS['codex-cli']],
  });

  const merged = mergeChannelSubmitFormValues(
    {
      name: 'channel-a',
      models: ['gpt-5.5'],
      settings: '',
    },
    {
      name: 'stale-name',
      models: ['old-model'],
      settings,
      param_override: '{"operations":[{"mode":"delete","path":"metadata"}]}',
      header_override: '{"X-Test":"1"}',
      status_code_mapping: '{"524":502}',
    },
  );

  assert.equal(merged.name, 'channel-a');
  assert.deepEqual(merged.models, ['gpt-5.5']);
  assert.equal(merged.settings, settings);
  assert.equal(
    merged.param_override,
    '{"operations":[{"mode":"delete","path":"metadata"}]}',
  );
  assert.equal(merged.header_override, '{"X-Test":"1"}');
  assert.equal(merged.status_code_mapping, '{"524":502}');
});

test('mergeChannelSubmitFormValues treats empty hidden json fields as missing form state', () => {
  const settings = buildHeaderProfileStrategySettings('{}', {
    enabled: true,
    mode: 'fixed',
    selectedProfileIds: ['codex-cli'],
    profiles: [HEADER_PROFILE_PRESETS['codex-cli']],
  });

  const merged = mergeChannelSubmitFormValues(
    {
      settings: '{}',
      param_override: '  ',
      header_override: '{}',
      status_code_mapping: '{}',
    },
    {
      settings,
      param_override:
        '{"operations":[{"mode":"pass_headers","value":["User-Agent"]}]}',
      header_override: '{"User-Agent":"Codex CLI"}',
      status_code_mapping: '{"524":502}',
    },
  );

  assert.equal(merged.settings, settings);
  assert.equal(
    merged.param_override,
    '{"operations":[{"mode":"pass_headers","value":["User-Agent"]}]}',
  );
  assert.equal(merged.header_override, '{"User-Agent":"Codex CLI"}');
  assert.equal(merged.status_code_mapping, '{"524":502}');
});

test('mergeChannelSubmitFormValues keeps explicit cleared request policy state from inputs', () => {
  const merged = mergeChannelSubmitFormValues(
    {
      models: ['gpt-5.5'],
    },
    {
      settings: '{}',
      param_override: '',
      header_override: '',
      status_code_mapping: '',
    },
  );

  assert.equal(merged.settings, '{}');
  assert.equal(merged.param_override, '');
  assert.equal(merged.header_override, '');
  assert.equal(merged.status_code_mapping, '');
});

test('reorderSelectedProfileIds follows before and after drop positions', () => {
  assert.deepEqual(
    reorderSelectedProfileIds(
      ['profile-a', 'profile-b', 'profile-c'],
      'profile-c',
      'profile-a',
      'before',
    ),
    ['profile-c', 'profile-a', 'profile-b'],
  );

  assert.deepEqual(
    reorderSelectedProfileIds(
      ['profile-a', 'profile-b', 'profile-c'],
      'profile-a',
      'profile-c',
      'after',
    ),
    ['profile-b', 'profile-c', 'profile-a'],
  );
});

test('removeEquivalentVersionedProfileIds replaces selected version variants', () => {
  const selectedCodexProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['codex-cli'],
    '0.134.0',
  );
  const nextCodexProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['codex-cli'],
    '0.129.0',
  );

  assert.deepEqual(
    removeEquivalentVersionedProfileIds(
      ['codex-cli@0.134.0', 'claude-code@2.1.153'],
      [
        selectedCodexProfile,
        buildVersionedAiCodingCliProfile(
          HEADER_PROFILE_PRESETS['claude-code'],
          '2.1.153',
        ),
      ],
      nextCodexProfile.id,
      nextCodexProfile,
    ),
    ['claude-code@2.1.153'],
  );
});

test('removeEquivalentVersionedProfileIds replaces legacy base selection', () => {
  const nextCodexProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['codex-cli'],
    '0.134.0',
  );

  assert.deepEqual(
    removeEquivalentVersionedProfileIds(
      ['codex-cli', 'claude-code'],
      [
        HEADER_PROFILE_PRESETS['codex-cli'],
        HEADER_PROFILE_PRESETS['claude-code'],
      ],
      nextCodexProfile.id,
      nextCodexProfile,
    ),
    ['claude-code'],
  );
});

test('removeEquivalentVersionedProfileIds replaces missing versioned selection by id base', () => {
  const nextCodexProfile = buildVersionedAiCodingCliProfile(
    HEADER_PROFILE_PRESETS['codex-cli'],
    '0.129.0',
  );

  assert.deepEqual(
    removeEquivalentVersionedProfileIds(
      ['codex-cli@0.134.0', 'claude-code@2.1.153'],
      [
        { id: 'codex-cli@0.134.0', missing: true },
        { id: 'claude-code@2.1.153', missing: true },
      ],
      nextCodexProfile.id,
      nextCodexProfile,
    ),
    ['claude-code@2.1.153'],
  );
});

test('createLegacyHeaderProfileDraft wraps valid header_override json', () => {
  const draft = createLegacyHeaderProfileDraft(
    '{"User-Agent":"legacy-client","X-Trace":"abc"}',
  );

  assert.ok(draft);
  assert.equal(draft.mode, 'custom');
  assert.equal(draft.name, 'Legacy Header Override');
  assert.deepEqual(JSON.parse(draft.headersText), {
    'User-Agent': 'legacy-client',
    'X-Trace': 'abc',
  });
  assert.deepEqual(validateHeaderProfileDraft(draft), {
    isValid: true,
    errors: {},
    parsedHeaders: {
      'User-Agent': 'legacy-client',
      'X-Trace': 'abc',
    },
  });
});

test('createLegacyHeaderProfileDraft returns null for invalid legacy json', () => {
  assert.equal(createLegacyHeaderProfileDraft('not-json'), null);
});

test('validateHeaderProfileDraft rejects empty name, invalid json and empty object', () => {
  assert.deepEqual(
    validateHeaderProfileDraft({
      name: '   ',
      headersText: '',
    }),
    {
      isValid: false,
      errors: {
        name: '名称不能为空',
        headersText: 'Headers JSON 不能为空',
      },
      parsedHeaders: null,
    },
  );

  assert.deepEqual(
    validateHeaderProfileDraft({
      name: 'Broken',
      headersText: '{',
    }),
    {
      isValid: false,
      errors: {
        headersText: 'Headers JSON 必须是合法 JSON',
      },
      parsedHeaders: null,
    },
  );

  assert.deepEqual(
    validateHeaderProfileDraft({
      name: 'Empty',
      headersText: '{}',
    }),
    {
      isValid: false,
      errors: {
        headersText: 'Headers JSON 必须是非空对象',
      },
      parsedHeaders: null,
    },
  );
});

test('validateHeaderProfileDraft rejects non-string header values', () => {
  assert.deepEqual(
    validateHeaderProfileDraft({
      name: 'Invalid Headers',
      headersText:
        '{"User-Agent":{"nested":true},"X-Test":["a"],"X-Null":null}',
    }),
    {
      isValid: false,
      errors: {
        headersText: 'Headers JSON 的值必须全部是字符串',
      },
      parsedHeaders: null,
    },
  );
});

test('validateHeaderProfileDraft rejects duplicate names against existing profiles', () => {
  assert.deepEqual(
    validateHeaderProfileDraft(
      {
        name: 'Codex CLI',
        headersText: '{"User-Agent":"Changed"}',
      },
      {
        profiles: buildProfileItems(),
      },
    ),
    {
      isValid: false,
      errors: {
        name: '名称已存在',
      },
      parsedHeaders: {
        'User-Agent': 'Changed',
      },
    },
  );
});
