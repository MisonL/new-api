import test from 'node:test';
import assert from 'node:assert/strict';

import {
  applyUserAgentPresetToHeaderOverride,
  buildUserAgentStrategyPayload,
  findHeaderOverrideUserAgentPreset,
  normalizeHeaderTemplateContent,
  normalizeUserAgentStrategy,
} from '../src/helpers/headerOverrideUserAgent.js';

const codexCliUserAgent =
  'codex-tui/0.128.0 (Mac OS 15.7.3; x86_64) ghostty/1.3.1 (codex-tui; 0.128.0)';
const droidCliUserAgent = 'factory-cli/0.115.0';

test('空白 header_override 可写入最小 User-Agent JSON', () => {
  assert.deepEqual(
    applyUserAgentPresetToHeaderOverride('', codexCliUserAgent),
    {
      ok: true,
      value: JSON.stringify(
        {
          'User-Agent': codexCliUserAgent,
        },
        null,
        2,
      ),
    },
  );
});

test('合法对象 JSON 只更新 User-Agent 并保留其它字段', () => {
  const current = JSON.stringify(
    {
      Authorization: 'Bearer {api_key}',
      'X-Trace': 'demo',
      'User-Agent': 'old-client/1.0.0',
    },
    null,
    2,
  );

  assert.deepEqual(
    applyUserAgentPresetToHeaderOverride(
      current,
      'Mozilla/5.0 (Windows NT 10.0; Win64; x64)',
    ),
    {
      ok: true,
      value: JSON.stringify(
        {
          Authorization: 'Bearer {api_key}',
          'X-Trace': 'demo',
          'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)',
        },
        null,
        2,
      ),
    },
  );
});

test('非法 JSON 返回错误且不覆盖原内容', () => {
  assert.deepEqual(
    applyUserAgentPresetToHeaderOverride('{invalid', 'claude-code/1.0.0'),
    {
      ok: false,
      message: '请求头覆盖必须是合法的 JSON 格式！',
    },
  );
});

test('非对象 JSON 返回对象类型错误', () => {
  assert.deepEqual(
    applyUserAgentPresetToHeaderOverride('[]', 'gemini-cli/1.0.0'),
    {
      ok: false,
      message: '请求头覆盖必须是 JSON 对象！',
    },
  );
});

test('normalize user agent strategy removes blanks and duplicates', () => {
  assert.deepEqual(
    normalizeUserAgentStrategy({
      enabled: true,
      mode: 'round_robin',
      userAgents: [
        codexCliUserAgent,
        ' ',
        codexCliUserAgent,
        droidCliUserAgent,
      ],
    }),
    {
      enabled: true,
      mode: 'round_robin',
      userAgents: [codexCliUserAgent, droidCliUserAgent],
    },
  );
});

test('build user agent strategy payload keeps explicit disabled state', () => {
  assert.deepEqual(
    buildUserAgentStrategyPayload({
      configured: true,
      enabled: false,
      mode: 'random',
      userAgents: [` ${codexCliUserAgent} `, ''],
    }),
    {
      ok: true,
      value: {
        enabled: false,
        mode: 'random',
        user_agents: [codexCliUserAgent],
      },
    },
  );
});

test('build user agent strategy payload returns null for untouched empty state', () => {
  assert.deepEqual(
    buildUserAgentStrategyPayload({
      configured: false,
      enabled: false,
      mode: 'round_robin',
      userAgents: [],
    }),
    {
      ok: true,
      value: null,
    },
  );
});

test('normalize header template content rejects non-object json', () => {
  assert.deepEqual(
    normalizeHeaderTemplateContent('[]', { allowEmpty: false }),
    {
      ok: false,
      message: '请求头覆盖必须是 JSON 对象！',
    },
  );
});

test('normalize header template content formats object json', () => {
  assert.deepEqual(
    normalizeHeaderTemplateContent('{"Authorization":"Bearer {api_key}"}', {
      allowEmpty: false,
    }),
    {
      ok: true,
      value: JSON.stringify(
        {
          Authorization: 'Bearer {api_key}',
        },
        null,
        2,
      ),
    },
  );
});

test('可通过 id 找到主流 AI Coding CLI 预置', () => {
  const preset = findHeaderOverrideUserAgentPreset('codex-cli');

  assert.equal(preset.id, 'codex-cli');
  assert.equal(preset.groupKey, 'ai-coding-cli');
  assert.match(preset.ua, /^codex-tui\//);
  assert.doesNotMatch(preset.ua.toLowerCase(), /codex_exec/);
  assert.doesNotMatch(preset.ua.toLowerCase(), /source=exec/);
});

test('可通过 id 找到 Droid CLI 预置', () => {
  const preset = findHeaderOverrideUserAgentPreset('droid');

  assert.equal(preset.id, 'droid');
  assert.equal(preset.groupKey, 'ai-coding-cli');
  assert.equal(preset.ua, droidCliUserAgent);
});
