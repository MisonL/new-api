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

import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';

import {
  getChannelHealthRuntimeDominantEntry,
  getChannelHealthRuntimeEntrySearchCriteria,
  getChannelHealthRuntimeChannelStateDisplay,
  getChannelHealthRuntimeEventDisplay,
  getChannelHealthRuntimeEvents,
  getChannelHealthRuntimeRefreshDelay,
  getChannelHealthRuntimeStateBehavior,
  getChannelHealthRuntimeEntriesForChannel,
  getChannelHealthRuntimeStateDisplay,
  summarizeChannelHealthRuntime,
} from '../src/helpers/channelHealthRuntime.js';

function getChannelHealthSettingsTranslationKeys() {
  const files = [
    new URL(
      '../src/pages/Setting/Operation/SettingsChannelHealth.jsx',
      import.meta.url,
    ),
    new URL('../src/helpers/channelHealthRuntime.js', import.meta.url),
    new URL(
      '../src/hooks/channels/useChannelHealthRuntime.jsx',
      import.meta.url,
    ),
    new URL(
      '../src/components/table/channels/ChannelHealthRuntimeOverview.jsx',
      import.meta.url,
    ),
    new URL(
      '../src/components/table/channels/ChannelHealthRuntimeChannelState.jsx',
      import.meta.url,
    ),
    new URL(
      '../src/components/table/channels/ChannelHealthRuntimeEvents.jsx',
      import.meta.url,
    ),
    new URL(
      '../src/components/table/usage-logs/ChannelHealthRoutingTrace.jsx',
      import.meta.url,
    ),
    new URL('../src/helpers/usageLogsChannelHealthRouting.js', import.meta.url),
  ];
  const keys = new Set();

  for (const file of files) {
    const source = readFileSync(file, 'utf8');
    for (const match of source.matchAll(/\bt\(\s*['"]([^'"\n]+)['"]/g)) {
      keys.add(match[1]);
    }
  }

  return [...keys];
}

function getMissingChannelHealthSettingsTranslations() {
  const keys = getChannelHealthSettingsTranslationKeys();
  const locales = ['zh-CN', 'zh-TW', 'en', 'fr', 'ru', 'ja', 'vi'];
  const missing = [];

  for (const locale of locales) {
    const file = new URL(`../src/i18n/locales/${locale}.json`, import.meta.url);
    const { translation } = JSON.parse(readFileSync(file, 'utf8'));
    for (const key of keys) {
      if (typeof translation[key] !== 'string' || !translation[key].trim()) {
        missing.push(`${locale}:${key}`);
      }
    }
  }

  return missing;
}

describe('classic channel health runtime presentation', () => {
  test('localizes every channel-health setting and runtime state in each locale', () => {
    expect(getMissingChannelHealthSettingsTranslations()).toEqual([]);
  });

  test('uses separate presentation for cooldown and half-open states', () => {
    const t = (key) => key;
    expect(getChannelHealthRuntimeStateDisplay('cooldown', t)).toEqual({
      label: '渠道冷却中',
      color: 'orange',
    });
    expect(getChannelHealthRuntimeStateDisplay('half_open_probe', t)).toEqual({
      label: '半开探测进行中',
      color: 'blue',
    });
    expect(
      getChannelHealthRuntimeStateDisplay('awaiting_half_open_probe', t),
    ).toEqual({
      label: '等待半开探测',
      color: 'grey',
    });
  });

  test('explains routing impact and the next transition for each active state', () => {
    const t = (key) => key;
    expect(getChannelHealthRuntimeStateBehavior('cooldown', t)).toEqual({
      impact: '冷却期间，相同分组和模型的请求会绕过该渠道。',
      nextStep: '冷却结束后，下一个匹配请求可以获取恢复探测。',
    });
    expect(getChannelHealthRuntimeStateBehavior('half_open_probe', t)).toEqual({
      impact: '一个匹配请求正在探测恢复，其他匹配请求继续绕过该渠道。',
      nextStep: '探测成功会清除临时绕过；合格失败会重新进入冷却。',
    });
  });

  test('reports Redis fail-open separately from an active healthy gate', () => {
    expect(
      summarizeChannelHealthRuntime({
        enabled: true,
        redis_available: false,
        entries: [],
      }),
    ).toEqual({
      state: 'redis_unavailable',
      cooling: 0,
      probing: 0,
      awaitingProbe: 0,
    });
  });

  test('keeps recent lifecycle transitions visible after the active state clears', () => {
    const runtime = {
      enabled: true,
      redis_available: true,
      entries: [],
      recent_events: [
        {
          occurred_at: 1784997217,
          group: 'coding',
          model_name: 'gpt-5.6-terra',
          channel_id: 208,
          action: 'half_open_recovered',
        },
        {
          occurred_at: 1784997201,
          group: 'coding',
          model_name: 'gpt-5.6-terra',
          channel_id: 208,
          action: 'cooldown',
          status_code: 502,
          error_code: 'bad_response_status_code',
        },
      ],
    };

    const events = getChannelHealthRuntimeEvents(runtime);
    expect(events).toHaveLength(2);
    expect(events[0]).toMatchObject({
      action: 'half_open_recovered',
      channel_id: 208,
      group: 'coding',
      model_name: 'gpt-5.6-terra',
    });
    expect(
      getChannelHealthRuntimeEventDisplay(events[1], (key) => key),
    ).toEqual({
      label: '渠道冷却',
      color: 'orange',
    });
  });

  test('samples active cooling and probing quickly without keeping idle polling hot', () => {
    const activeRuntime = {
      enabled: true,
      redis_available: true,
      entries: [{ state: 'half_open_probe' }],
    };
    expect(getChannelHealthRuntimeRefreshDelay(activeRuntime)).toBe(1000);
    expect(
      getChannelHealthRuntimeRefreshDelay({ ...activeRuntime, entries: [] }),
    ).toBe(5000);
    expect(
      getChannelHealthRuntimeRefreshDelay({
        ...activeRuntime,
        redis_available: false,
      }),
    ).toBe(5000);
  });

  test('only attaches temporary state to the affected channel and keeps cooldown dominant', () => {
    const runtime = {
      enabled: true,
      redis_available: true,
      entries: [
        {
          channel_id: 21,
          group: 'default',
          model_name: 'gpt-5.6-terra',
          state: 'half_open_probe',
        },
        {
          channel_id: 21,
          group: 'coding',
          model_name: 'gpt-5.6-terra',
          state: 'cooldown',
        },
        {
          channel_id: 22,
          group: 'default',
          model_name: 'gpt-5.6-terra',
          state: 'awaiting_half_open_probe',
        },
      ],
    };

    const entries = getChannelHealthRuntimeEntriesForChannel(runtime, 21);
    expect(entries).toHaveLength(2);
    expect(getChannelHealthRuntimeDominantEntry(entries)).toMatchObject({
      group: 'coding',
      model_name: 'gpt-5.6-terra',
      state: 'cooldown',
    });
    expect(
      getChannelHealthRuntimeChannelStateDisplay(entries, (key) => key),
    ).toEqual({
      label: '渠道冷却中',
      color: 'orange',
    });
    expect(getChannelHealthRuntimeEntriesForChannel(runtime, 23)).toEqual([]);
    expect(
      getChannelHealthRuntimeEntriesForChannel(
        { ...runtime, redis_available: false },
        21,
      ),
    ).toEqual([]);
  });

  test('builds an exact ID search and clears stale filters when locating runtime state', () => {
    expect(
      getChannelHealthRuntimeEntrySearchCriteria({
        channel_id: 920651,
        group: 'uihealth',
        model_name: 'gpt-5.6-terra',
        state: 'cooldown',
      }),
    ).toEqual({
      searchKeyword: '920651',
      searchGroup: '',
      searchModel: '',
    });
  });

  test('uses a bounded, single-flight refresh loop for runtime feedback', () => {
    const source = readFileSync(
      new URL(
        '../src/hooks/channels/useChannelHealthRuntime.jsx',
        import.meta.url,
      ),
      'utf8',
    );

    expect(source).toContain('RUNTIME_REQUEST_TIMEOUT_MS = 8000');
    expect(source).toContain('skipBusinessError: true');
    expect(source).toContain('skipErrorHandler: true');
    expect(source).toContain('runtimeRequestRef');
    expect(source).toContain('runtimeRequestVisibleRef');
    expect(source).toContain('runtimeRequestVisibleRef.current = true');
    expect(source).toContain(
      'const shouldClearRuntimeLoading = runtimeRequestVisibleRef.current',
    );
    expect(source).toContain('getChannelHealthRuntimeRefreshDelay');
    expect(source).toContain("document.visibilityState === 'hidden'");
    expect(source).toContain("document.addEventListener('visibilitychange'");
    expect(source).toContain('refresh({ silent: true })');
    expect(source).toContain(
      'window.setTimeout(scheduleRefresh, refreshDelay)',
    );
    expect(source).not.toContain('window.setInterval');
  });

  test('puts administrator-visible live feedback in the classic channel-management workflow', () => {
    const source = readFileSync(
      new URL(
        '../src/components/table/channels/ChannelHealthRuntimeOverview.jsx',
        import.meta.url,
      ),
      'utf8',
    );

    expect(source).toContain('canConfigure');
    expect(source).toContain('/console/setting');
    expect(source).toContain('/console/log');
    expect(source).toContain('运行态按渠道、分组和模型隔离');
    expect(source).toContain('当前没有生效中的渠道临时绕过。');
    expect(source).toContain('ChannelHealthRuntimeEvents');
    expect(source).toContain("showError(t('无法获取渠道健康运行状态'));");
    expect(source).toContain('getChannelHealthRuntimeEntrySearchCriteria');
    expect(source).toContain('onLocateChannel');

    const dataSource = readFileSync(
      new URL('../src/hooks/channels/useChannelsData.jsx', import.meta.url),
      'utf8',
    );
    expect(dataSource).toContain('setEnableTagMode(false);');
    expect(dataSource).toMatch(
      /await searchChannels\(\s*false,\s*['"]all['"],\s*['"]all['"],\s*1,/,
    );
  });

  test('adds active runtime feedback beside affected classic channel names', () => {
    const source = readFileSync(
      new URL(
        '../src/components/table/channels/ChannelHealthRuntimeChannelState.jsx',
        import.meta.url,
      ),
      'utf8',
    );
    const columnsSource = readFileSync(
      new URL(
        '../src/components/table/channels/ChannelsColumnDefs.jsx',
        import.meta.url,
      ),
      'utf8',
    );

    expect(source).toContain('getChannelHealthRuntimeEntriesForChannel');
    expect(source).toContain('getChannelHealthRuntimeDominantEntry');
    expect(source).not.toContain('entries[0]?.state');
    expect(source).toContain('<Tag color={display.color}');
    expect(source).not.toContain('var(--semi-color-${display.color})');
    expect(source).toContain("t('运行态按渠道、分组和模型隔离。')");
    expect(source).toContain('getChannelHealthRuntimeStateBehavior');
    expect(source).toContain("t('路由影响')");
    expect(source).toContain("t('下一次转换')");
    expect(columnsSource).toContain('ChannelHealthRuntimeChannelState');
    expect(columnsSource).toContain("<Space spacing={6} align='center' wrap>");
  });
});
