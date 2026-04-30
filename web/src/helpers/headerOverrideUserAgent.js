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

export const HEADER_OVERRIDE_USER_AGENT_PRESET_GROUPS = [
  {
    key: 'browser',
    label: '浏览器',
    items: [
      {
        id: 'chrome-windows',
        label: 'Chrome Windows',
        ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36',
      },
      {
        id: 'chrome-macos',
        label: 'Chrome macOS',
        ua: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36',
      },
      {
        id: 'safari-macos',
        label: 'Safari macOS',
        ua: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15',
      },
      {
        id: 'edge-windows',
        label: 'Edge Windows',
        ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36 Edg/139.0.0.0',
      },
      {
        id: 'firefox-windows',
        label: 'Firefox Windows',
        ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:141.0) Gecko/20100101 Firefox/141.0',
      },
      {
        id: 'mobile-safari-iphone',
        label: 'Mobile Safari iPhone',
        ua: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1',
      },
      {
        id: 'chrome-android',
        label: 'Chrome Android',
        ua: 'Mozilla/5.0 (Linux; Android 15; Pixel 9 Pro) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Mobile Safari/537.36',
      },
    ],
  },
  {
    key: 'ai-coding-cli',
    label: 'AI Coding CLI（固定 UA）',
    items: [
      {
        id: 'codex-cli',
        label: 'Codex CLI 固定 UA',
        ua: 'codex_exec/0.125.0 (Mac OS 15.7.3; x86_64) ghostty/1.3.1 (codex_exec; 0.125.0)',
      },
      {
        id: 'claude-code',
        label: 'Claude Code 固定 UA',
        ua: 'claude-code/1.0.0',
      },
      {
        id: 'gemini-cli',
        label: 'Gemini CLI 固定 UA',
        ua: 'gemini-cli/1.0.0',
      },
      { id: 'qwen-code', label: 'Qwen Code 固定 UA', ua: 'qwen-code/1.0.0' },
      { id: 'opencode', label: 'OpenCode 固定 UA', ua: 'opencode/1.0.0' },
      { id: 'droid', label: 'Droid 固定 UA', ua: 'droid/1.0.0' },
      { id: 'amp', label: 'AMP 固定 UA', ua: 'amp/1.0.0' },
    ],
  },
  {
    key: 'api-sdk-tools',
    label: 'API SDK / 调试工具',
    items: [
      {
        id: 'openai-python',
        label: 'OpenAI Python',
        ua: 'OpenAI/Python 1.99.0',
      },
      {
        id: 'openai-node',
        label: 'OpenAI Node',
        ua: 'OpenAI/JavaScript 4.99.0',
      },
      {
        id: 'anthropic-python',
        label: 'Anthropic Python',
        ua: 'Anthropic/Python 0.57.1',
      },
      {
        id: 'anthropic-ts',
        label: 'Anthropic TypeScript',
        ua: 'Anthropic/TypeScript 0.57.1',
      },
      {
        id: 'postman-runtime',
        label: 'PostmanRuntime',
        ua: 'PostmanRuntime/7.43.0',
      },
      { id: 'curl', label: 'curl', ua: 'curl/8.9.1' },
    ],
  },
];

export function buildHeaderOverrideUserAgentPresetMenu(t, onSelect) {
  const menu = [];
  HEADER_OVERRIDE_USER_AGENT_PRESET_GROUPS.forEach((group, groupIndex) => {
    if (groupIndex > 0) {
      menu.push({
        node: 'divider',
        key: `divider-${group.key}`,
      });
    }
    group.items.forEach((item) => {
      menu.push({
        node: 'item',
        key: `${group.key}-${item.id}`,
        name: `${t(group.label)} / ${item.label}`,
        onClick: () => onSelect(item),
      });
    });
  });
  return menu;
}

const USER_AGENT_STRATEGY_MODES = new Set(['round_robin', 'random']);

export function normalizeUserAgentValues(values) {
  if (!Array.isArray(values)) {
    return [];
  }

  const normalized = [];
  const seen = new Set();
  values.forEach((item) => {
    String(item ?? '')
      .split(',')
      .map((value) => value.trim())
      .filter(Boolean)
      .forEach((value) => {
        if (seen.has(value)) {
          return;
        }
        seen.add(value);
        normalized.push(value);
      });
  });
  return normalized;
}

export function normalizeUserAgentStrategyMode(mode) {
  const normalized = String(mode ?? '').trim();
  if (!USER_AGENT_STRATEGY_MODES.has(normalized)) {
    return null;
  }
  return normalized;
}

export function normalizeUserAgentStrategy(input) {
  if (!input?.enabled) {
    return null;
  }

  const mode = normalizeUserAgentStrategyMode(input.mode);
  if (!mode) {
    return null;
  }

  const userAgents = normalizeUserAgentValues(
    input.userAgents || input.user_agents,
  );
  if (userAgents.length === 0) {
    return null;
  }

  return {
    enabled: true,
    mode,
    userAgents,
  };
}

export function buildUserAgentStrategyPayload(input) {
  const configured = input?.configured === true;
  const enabled = input?.enabled === true;
  const mode = normalizeUserAgentStrategyMode(input?.mode);
  const userAgents = normalizeUserAgentValues(input?.userAgents);

  if (enabled) {
    if (!mode) {
      return {
        ok: false,
        message: '请选择合法的 UA 策略模式！',
      };
    }
    if (userAgents.length === 0) {
      return {
        ok: false,
        message: '请至少选择一个 User-Agent！',
      };
    }
    return {
      ok: true,
      value: {
        enabled: true,
        mode,
        user_agents: userAgents,
      },
    };
  }

  if (!configured && userAgents.length === 0) {
    return {
      ok: true,
      value: null,
    };
  }

  const payload = {
    enabled: false,
  };
  if (mode) {
    payload.mode = mode;
  }
  if (userAgents.length > 0) {
    payload.user_agents = userAgents;
  }

  return {
    ok: true,
    value: payload,
  };
}

export function normalizeHeaderTemplateContent(rawValue, options = {}) {
  const allowEmpty = options.allowEmpty !== false;
  const trimmed = typeof rawValue === 'string' ? rawValue.trim() : '';

  if (!trimmed) {
    if (allowEmpty) {
      return {
        ok: true,
        value: '',
      };
    }
    return {
      ok: false,
      message: '模板内容不能为空！',
    };
  }

  let parsed;
  try {
    parsed = JSON.parse(trimmed);
  } catch {
    return {
      ok: false,
      message: '请求头覆盖必须是合法的 JSON 格式！',
    };
  }

  if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
    return {
      ok: false,
      message: '请求头覆盖必须是 JSON 对象！',
    };
  }

  for (const [key, value] of Object.entries(parsed)) {
    if (typeof key !== 'string' || key.trim() === '') {
      return {
        ok: false,
        message: '请求头名称不能为空！',
      };
    }
    if (
      typeof value !== 'string' &&
      typeof value !== 'number' &&
      typeof value !== 'boolean'
    ) {
      return {
        ok: false,
        message: `请求头值类型不受支持: ${key}`,
      };
    }
  }

  return {
    ok: true,
    value: JSON.stringify(parsed, null, 2),
  };
}

export function findHeaderOverrideUserAgentPreset(id) {
  for (const group of HEADER_OVERRIDE_USER_AGENT_PRESET_GROUPS) {
    for (const item of group.items) {
      if (item.id === id) {
        return {
          ...item,
          groupKey: group.key,
          groupLabel: group.label,
        };
      }
    }
  }
  return null;
}

export function applyUserAgentPresetToHeaderOverride(rawValue, userAgent) {
  const normalized = normalizeHeaderTemplateContent(rawValue, {
    allowEmpty: true,
  });
  if (!normalized.ok) {
    return normalized;
  }
  if (!normalized.value) {
    return {
      ok: true,
      value: JSON.stringify(
        {
          'User-Agent': userAgent,
        },
        null,
        2,
      ),
    };
  }

  const parsed = JSON.parse(normalized.value);

  parsed['User-Agent'] = userAgent;

  return {
    ok: true,
    value: JSON.stringify(parsed, null, 2),
  };
}
