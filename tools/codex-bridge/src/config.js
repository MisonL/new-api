import {
  CATALOG_FILE,
  ENV_KEY,
  PROVIDER_ID,
  PROVIDER_MARKER_END,
  PROVIDER_MARKER_START,
  PROVIDER_NAME,
  SETTINGS_MARKER_END,
  SETTINGS_MARKER_START,
} from './constants.js';
import {
  extractManagedBlock,
  parseTomlBasicStringContent,
  removeTableBlock,
  removeTopLevelAssignments,
  stripManagedBlock,
  tomlString,
} from './toml.js';

export function buildConfigToml({
  existingText = '',
  defaultModel,
  providerBaseUrl,
  authMode = 'provider-token',
  apiKey,
  envKey = ENV_KEY,
}) {
  let body = stripManagedBlock(existingText, SETTINGS_MARKER_START, SETTINGS_MARKER_END);
  body = stripManagedBlock(body, PROVIDER_MARKER_START, PROVIDER_MARKER_END);
  body = removeTopLevelAssignments(body, ['model', 'model_provider', 'model_catalog_json']);
  body = removeTableBlock(body, `model_providers.${PROVIDER_ID}`).trim();

  const settingsBlock = [
    SETTINGS_MARKER_START,
    `model = ${tomlString(defaultModel)}`,
    `model_provider = ${tomlString(PROVIDER_ID)}`,
    `model_catalog_json = ${tomlString(CATALOG_FILE)}`,
    SETTINGS_MARKER_END,
  ].join('\n');

  const providerBlock = [
    PROVIDER_MARKER_START,
    `[model_providers.${PROVIDER_ID}]`,
    `name = ${tomlString(PROVIDER_NAME)}`,
    `base_url = ${tomlString(providerBaseUrl)}`,
    'wire_api = "responses"',
    ...providerAuthLines({ authMode, apiKey, envKey }),
    PROVIDER_MARKER_END,
  ].join('\n');

  return [settingsBlock, body, providerBlock].filter(Boolean).join('\n\n').trimEnd() + '\n';
}

export function providerAuthLines({ authMode, apiKey, envKey }) {
  const normalizedApiKey = typeof apiKey === 'string' ? apiKey.trim() : '';
  const normalizedEnvKey = typeof envKey === 'string' ? envKey.trim() : '';
  if (authMode === 'provider-token') {
    if (!normalizedApiKey) {
      throw new Error('provider-token 认证模式需要 API Key');
    }
    return [`experimental_bearer_token = ${tomlString(normalizedApiKey)}`];
  }
  if (authMode === 'auth-json') {
    return ['requires_openai_auth = true'];
  }
  if (authMode === 'env') {
    if (envKey != null && !normalizedEnvKey) {
      throw new Error('env 认证模式需要非空环境变量名');
    }
    return [`env_key = ${tomlString(normalizedEnvKey || ENV_KEY)}`];
  }
  throw new Error(`不支持的认证模式：${authMode}`);
}

export function readExperimentalBearerToken(configText, providerId = PROVIDER_ID) {
  const providerBlock = readManagedProviderBlock(configText, providerId);
  if (!providerBlock) {
    return '';
  }
  const match = providerBlock.match(/^\s*experimental_bearer_token\s*=\s*"((?:\\.|[^"\\])*)"/m);
  if (!match) {
    return '';
  }
  try {
    return parseTomlBasicStringContent(match[1]);
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    throw new Error(`experimental_bearer_token 不是有效 TOML 字符串：${message}`);
  }
}

export function readManagedProviderBlock(configText, providerId = PROVIDER_ID) {
  const managedBlock = extractManagedBlock(
    configText,
    PROVIDER_MARKER_START,
    PROVIDER_MARKER_END
  );
  if (!managedBlock) {
    return '';
  }
  let inProvider = false;
  const lines = [];
  for (const line of managedBlock.split(/\r?\n/)) {
    const trimmed = line.trim();
    const header = safeParseHeaderName(trimmed);
    if (header) {
      if (inProvider) {
        break;
      }
      inProvider = header === `model_providers.${providerId}`;
      continue;
    }
    if (!inProvider) {
      continue;
    }
    lines.push(line);
  }
  return lines.join('\n').trim();
}

function safeParseHeaderName(trimmed) {
  try {
    return parseHeaderName(trimmed);
  } catch {
    return '';
  }
}

export function buildAuthJson({ existingText = '', apiKey }) {
  const normalizedApiKey = typeof apiKey === 'string' ? apiKey.trim() : '';
  if (!normalizedApiKey) {
    throw new Error('auth.json 需要 API Key');
  }
  let auth = {};
  if (existingText.trim()) {
    try {
      auth = JSON.parse(existingText);
    } catch {
      throw new Error('已有 auth.json 不是有效 JSON');
    }
    if (!auth || Array.isArray(auth) || typeof auth !== 'object') {
      throw new Error('已有 auth.json 必须是 JSON object');
    }
  }
  return `${JSON.stringify({ ...auth, [ENV_KEY]: normalizedApiKey }, null, 2)}\n`;
}

function parseHeaderName(trimmed) {
  const arrayMatch = trimmed.match(/^\[\[([^\]]+)\]\](?:\s*#.*)?$/);
  if (arrayMatch) {
    const name = arrayMatch[1].trim();
    if (!name) {
      throw new Error('TOML 表头名称不能为空');
    }
    return name;
  }
  const tableMatch = trimmed.match(/^\[([^\]]+)\](?:\s*#.*)?$/);
  if (tableMatch) {
    const name = tableMatch[1].trim();
    if (!name) {
      throw new Error('TOML 表头名称不能为空');
    }
    return name;
  }
  return '';
}
