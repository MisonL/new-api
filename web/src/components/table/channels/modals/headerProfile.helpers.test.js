import test from 'node:test';
import assert from 'node:assert/strict';

import {
  buildHeaderProfilePreviewText,
  buildHeaderProfileStrategySettings,
  buildProfileItems,
  buildSelectedProfileItems,
  createLegacyHeaderProfileDraft,
  getHeaderProfileStrategyFromSettings,
  normalizeHeaderProfileStrategy,
  reorderSelectedProfileIds,
  toggleSelectedProfile,
  validateHeaderProfileDraft,
} from './headerProfile.helpers.js';
import { HEADER_PROFILE_PRESETS } from './headerProfile.constants.js';

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
  assert.match(items[0].previewText, /OpenAI Codex CLI/i);
  assert.deepEqual(items[0].headers, HEADER_PROFILE_PRESETS['codex-cli'].headers);
  assert.equal(items[0].name, 'Codex CLI');
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
  assert.match(builtin.previewText, /OpenAI Codex CLI/i);

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
        },
      }),
    ),
    {
      enabled: true,
      mode: 'random',
      selectedProfileIds: ['a', 'b'],
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
    },
  );

  assert.deepEqual(JSON.parse(written), {
    azure_responses_version: 'preview',
    header_profile_strategy: {
      enabled: true,
      mode: 'round_robin',
      selected_profile_ids: ['profile-a', 'profile-b'],
    },
  });

  const removed = buildHeaderProfileStrategySettings(written, null);
  assert.deepEqual(JSON.parse(removed), {
    azure_responses_version: 'preview',
  });
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
      parsedHeaders: {},
    },
  );
});

test('validateHeaderProfileDraft rejects non-string header values', () => {
  assert.deepEqual(
    validateHeaderProfileDraft({
      name: 'Invalid Headers',
      headersText: '{"User-Agent":{"nested":true},"X-Test":["a"],"X-Null":null}',
    }),
    {
      isValid: false,
      errors: {
        headersText: 'Headers JSON 的值必须全部是字符串',
      },
      parsedHeaders: {
        'User-Agent': { nested: true },
        'X-Test': ['a'],
        'X-Null': null,
      },
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
