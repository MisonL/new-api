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
  getHeaderProfileStrategyFromSettings,
  mergeChannelSubmitFormValues,
  normalizeHeaderProfileStrategy,
  reorderSelectedProfileIds,
  toggleSelectedProfile,
  validateHeaderProfileDraft,
} from './headerProfile.helpers.js';
import { HEADER_PROFILE_PRESETS } from './headerProfile.constants.js';
import {
  CLAUDE_CLI_HEADER_PASSTHROUGH_HEADERS,
  CODEX_CLI_HEADER_PASSTHROUGH_HEADERS,
  DROID_CLI_HEADER_PASSTHROUGH_HEADERS,
  QWEN_CODE_CLI_HEADER_PASSTHROUGH_HEADERS,
} from '../../../../constants/channel-affinity-template.constants.js';

test('builtin AI CLI profiles distinguish fixed headers from required passthrough', () => {
  assert.equal(HEADER_PROFILE_PRESETS['codex-cli'].passthroughRequired, true);
  assert.equal(HEADER_PROFILE_PRESETS['claude-code'].passthroughRequired, true);
  assert.equal(HEADER_PROFILE_PRESETS['gemini-cli'].passthroughRequired, true);
  assert.equal(HEADER_PROFILE_PRESETS['opencode'], undefined);
  assert.match(
    HEADER_PROFILE_PRESETS['codex-cli'].description,
    /自动写入 Codex CLI 请求头透传规则/,
  );
  assert.match(
    HEADER_PROFILE_PRESETS['claude-code'].description,
    /自动写入 Claude CLI 请求头透传规则/,
  );
  assert.equal(
    HEADER_PROFILE_PRESETS['gemini-cli'].headers['User-Agent'],
    'GeminiCLI/0.40.1/gemini-3.1-pro-preview (darwin; x64; terminal)',
  );
  assert.match(
    HEADER_PROFILE_PRESETS['gemini-cli'].description,
    /x-goog-api-client/,
  );
  assert.equal(HEADER_PROFILE_PRESETS['qwen-code'].passthroughRequired, true);
  assert.equal(
    HEADER_PROFILE_PRESETS['qwen-code'].headers['User-Agent'],
    'QwenCode/0.15.6 (darwin; x64)',
  );
  assert.match(
    HEADER_PROFILE_PRESETS['qwen-code'].description,
    /自动写入 Qwen Code 请求头透传规则/,
  );
  assert.equal(HEADER_PROFILE_PRESETS['droid'].passthroughRequired, true);
  assert.equal(
    HEADER_PROFILE_PRESETS['droid'].headers['User-Agent'],
    'factory-cli/0.115.0',
  );
  assert.match(
    HEADER_PROFILE_PRESETS['droid'].description,
    /自动写入 Droid CLI 请求头透传规则/,
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

test('buildSelectedProfileItems keeps structured headers while main fields stay name/group/preview', () => {
  const items = buildSelectedProfileItems(['codex-cli']);

  assert.equal(items.length, 1);
  assert.equal(items[0].name, HEADER_PROFILE_PRESETS['codex-cli'].name);
  assert.equal(items[0].category, HEADER_PROFILE_PRESETS['codex-cli'].group);
  assert.match(items[0].previewText, /codex_exec\/0\.128\.0/i);
  assert.deepEqual(
    items[0].headers,
    HEADER_PROFILE_PRESETS['codex-cli'].headers,
  );
  assert.equal(items[0].name, 'Codex CLI');
  assert.equal(items[0].passthroughRequired, true);
  assert.match(items[0].description, /自动写入 Codex CLI 请求头透传规则/);
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
  assert.match(builtin.previewText, /codex_exec\/0\.128\.0/i);

  assert.ok(custom);
  assert.equal(custom.scope, 'user');
  assert.equal(custom.readonly, false);
  assert.equal(custom.previewText, 'User-Agent: MyAgent/1.0\nX-Test: yes');
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

test('buildHeaderProfileStrategySettings stores passthrough metadata with api field names', () => {
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
      passthrough_required: true,
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

test('applyHeaderProfileStrategyToChannelInputs adds Codex pass_headers when applying Codex template', () => {
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
  assert.deepEqual(paramOverride.operations[0], {
    mode: 'pass_headers',
    value: CODEX_CLI_HEADER_PASSTHROUGH_HEADERS,
    keep_origin: true,
  });
  assert.deepEqual(paramOverride.operations[1], {
    mode: 'trim_prefix',
    path: 'model',
    value: 'openai/',
  });
});

test('applyHeaderProfileStrategyToChannelInputs merges all required CLI passthrough templates without duplicates', () => {
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override:
        '{"operations":[{"mode":"pass_headers","value":["User-Agent","Originator"],"keep_origin":true}]}',
    },
    strategy: {
      enabled: true,
      mode: 'round_robin',
      selectedProfileIds: ['codex-cli', 'claude-code'],
    },
    headerProfiles: [],
    snapshotProfiles: [],
  });

  const operations = JSON.parse(result.param_override).operations;
  const passHeaderOperations = operations.filter(
    (operation) => operation.mode === 'pass_headers',
  );
  const passedHeaders = new Set(
    passHeaderOperations.flatMap((operation) => operation.value),
  );

  assert.equal(passHeaderOperations.length, 1);
  assert.equal(operations.length, 1);
  CODEX_CLI_HEADER_PASSTHROUGH_HEADERS.forEach((header) => {
    assert.equal(passedHeaders.has(header), true);
  });
  CLAUDE_CLI_HEADER_PASSTHROUGH_HEADERS.forEach((header) => {
    assert.equal(passedHeaders.has(header), true);
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

test('applyHeaderProfileStrategyToChannelInputs preserves stringified JSON pass_headers values when adding required headers', () => {
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override:
        '{"operations":[{"mode":"pass_headers","value":"[\\"X-Trace-Id\\"]","keep_origin":true}]}',
    },
    strategy: {
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: ['codex-cli'],
    },
    headerProfiles: [],
    snapshotProfiles: [],
  });

  const operations = JSON.parse(result.param_override).operations;
  assert.equal(operations.length, 1);
  assert.deepEqual(operations[0], {
    mode: 'pass_headers',
    value: ['X-Trace-Id', ...CODEX_CLI_HEADER_PASSTHROUGH_HEADERS],
    keep_origin: true,
  });
});

test('applyHeaderProfileStrategyToChannelInputs preserves object names pass_headers values when adding required headers', () => {
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override:
        '{"operations":[{"mode":"pass_headers","value":{"names":"X-Trace-Id"},"keep_origin":true}]}',
    },
    strategy: {
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: ['codex-cli'],
    },
    headerProfiles: [],
    snapshotProfiles: [],
  });

  const operations = JSON.parse(result.param_override).operations;
  assert.equal(operations.length, 1);
  assert.deepEqual(operations[0], {
    mode: 'pass_headers',
    value: ['X-Trace-Id', ...CODEX_CLI_HEADER_PASSTHROUGH_HEADERS],
    keep_origin: true,
  });
});

test('applyHeaderProfileStrategyToChannelInputs keeps conditional pass_headers separate from required CLI passthrough', () => {
  const result = applyHeaderProfileStrategyToChannelInputs({
    inputs: {
      settings: '{}',
      param_override:
        '{"operations":[{"mode":"pass_headers","value":["X-Trace-Id"],"conditions":[{"path":"model","mode":"prefix","value":"gpt-4"}],"keep_origin":true}]}',
    },
    strategy: {
      enabled: true,
      mode: 'fixed',
      selectedProfileIds: ['codex-cli'],
    },
    headerProfiles: [],
    snapshotProfiles: [],
  });

  const operations = JSON.parse(result.param_override).operations;
  assert.equal(operations.length, 2);
  assert.deepEqual(operations[0], {
    mode: 'pass_headers',
    value: CODEX_CLI_HEADER_PASSTHROUGH_HEADERS,
    keep_origin: true,
  });
  assert.deepEqual(operations[1], {
    mode: 'pass_headers',
    value: ['X-Trace-Id'],
    conditions: [{ path: 'model', mode: 'prefix', value: 'gpt-4' }],
    keep_origin: true,
  });
});

test('applyHeaderProfileStrategyToChannelInputs backfills Codex pass_headers for existing strategy on submit', () => {
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

  assert.deepEqual(JSON.parse(result.param_override), {
    operations: [
      {
        mode: 'pass_headers',
        value: CODEX_CLI_HEADER_PASSTHROUGH_HEADERS,
        keep_origin: true,
      },
    ],
  });
});

test('applyHeaderProfileStrategyToChannelInputs backfills passthrough for legacy builtin snapshot without passthrough flag', () => {
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

  assert.deepEqual(JSON.parse(result.param_override), {
    operations: [
      {
        mode: 'pass_headers',
        value: CODEX_CLI_HEADER_PASSTHROUGH_HEADERS,
        keep_origin: true,
      },
    ],
  });
});

test('applyHeaderProfileStrategyToChannelInputs adds Gemini pass_headers when applying Gemini template', () => {
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

  assert.deepEqual(JSON.parse(result.param_override), {
    operations: [
      {
        mode: 'pass_headers',
        value: ['User-Agent', 'X-Goog-Api-Client'],
        keep_origin: true,
      },
    ],
  });
});

test('applyHeaderProfileStrategyToChannelInputs adds Qwen pass_headers when applying Qwen template', () => {
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

  assert.deepEqual(JSON.parse(result.param_override), {
    operations: [
      {
        mode: 'pass_headers',
        value: QWEN_CODE_CLI_HEADER_PASSTHROUGH_HEADERS,
        keep_origin: true,
      },
    ],
  });
});

test('applyHeaderProfileStrategyToChannelInputs adds Droid pass_headers when applying Droid template', () => {
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

  assert.deepEqual(JSON.parse(result.param_override), {
    operations: [
      {
        mode: 'pass_headers',
        value: DROID_CLI_HEADER_PASSTHROUGH_HEADERS,
        keep_origin: true,
      },
    ],
  });
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
