import test from 'node:test';
import assert from 'node:assert/strict';

import {
  buildSelectedProfileItems,
  createLegacyHeaderProfileDraft,
  normalizeHeaderProfileStrategy,
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
  assert.equal(items[0].group, HEADER_PROFILE_PRESETS['codex-cli'].group);
  assert.match(items[0].previewText, /OpenAI Codex CLI/i);
  assert.deepEqual(items[0].headers, HEADER_PROFILE_PRESETS['codex-cli'].headers);
  assert.equal(items[0].name, 'Codex CLI');
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
