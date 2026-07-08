import path from 'node:path';
import { AUTH_FILE, CATALOG_FILE, CONFIG_FILE, ENV_KEY, RECEIPT_FILE } from './constants.js';
import { readExperimentalBearerToken, readManagedProviderBlock } from './config.js';
import { readTextIfExists } from './files.js';
import { parseReceipt } from './receipt.js';

export async function runDoctor(codexHome) {
  const problems = [];
  const configText = await readTextIfExists(path.join(codexHome, CONFIG_FILE));
  const authText = await readTextIfExists(path.join(codexHome, AUTH_FILE));
  const catalogText = await readTextIfExists(path.join(codexHome, CATALOG_FILE));
  const receiptText = await readTextIfExists(path.join(codexHome, RECEIPT_FILE));

  if (!configText) {
    problems.push(`缺少 ${CONFIG_FILE}`);
  }
  if (!catalogText) {
    problems.push(`缺少 ${CATALOG_FILE}`);
  }
  if (!receiptText) {
    problems.push(`缺少 ${RECEIPT_FILE}`);
  }

  let receipt = null;
  if (receiptText) {
    try {
      receipt = parseReceipt(receiptText);
    } catch (error) {
      problems.push(error.message);
    }
  }
  const providerBlock = configText ? readManagedProviderBlock(configText) : '';
  let providerToken = '';
  let providerTokenError = null;
  if (configText) {
    try {
      providerToken = readExperimentalBearerToken(configText);
    } catch (error) {
      providerTokenError = error;
    }
  }
  const configAuthMode = configText ? inferConfigAuthMode(configText, providerToken) : null;

  if (configText && !hasTomlStringAssignment(configText, 'model_provider', 'new-api')) {
    problems.push('config.toml 未设置 model_provider = "new-api"');
  }
  if (configText && !hasTomlStringAssignment(configText, 'model_catalog_json', CATALOG_FILE)) {
    problems.push('config.toml 未使用相对路径 new-api 模型目录');
  }
  if (configText && !providerBlock.includes('wire_api = "responses"')) {
    problems.push('config.toml 未设置 wire_api = "responses"');
  }
  if (receipt && configText && !providerBlock.includes(`base_url = "${receipt.providerBaseUrl}"`)) {
    problems.push('config.toml 的提供方 base_url 与回执不一致');
  }
  if (receipt?.authMode && configAuthMode && receipt.authMode !== configAuthMode) {
    problems.push('config.toml 的认证模式与回执不一致');
  }

  const authMode = configAuthMode || receipt?.authMode || null;
  if (authMode === 'provider-token') {
    if (providerTokenError) {
      problems.push(providerTokenError.message);
    } else if (configText && !providerToken) {
      problems.push('config.toml 不包含 experimental_bearer_token');
    }
  } else if (authMode === 'auth-json') {
    if (configText && configAuthMode !== 'auth-json') {
      problems.push('config.toml 不包含 requires_openai_auth = true');
    }
    if (!authText) {
      problems.push('缺少 auth.json');
    } else {
      try {
        const auth = JSON.parse(authText);
        if (!auth[ENV_KEY]) {
          problems.push(`auth.json 不包含 ${ENV_KEY}`);
        }
      } catch {
        problems.push('auth.json 不是有效 JSON');
      }
    }
  } else if (authMode === 'env') {
    if (configText && receipt?.envKey && !providerBlock.includes(`env_key = "${receipt.envKey}"`)) {
      problems.push('config.toml 的 env_key 与回执不一致');
    }
    if (configText && !/^\s*env_key\s*=/m.test(providerBlock)) {
      problems.push('config.toml 不包含 env_key');
    }
  } else if (!authMode && configText) {
    problems.push('config.toml 不包含可识别的认证配置');
  }

  if (catalogText) {
    try {
      const catalog = JSON.parse(catalogText);
      const catalogModels = Array.isArray(catalog.models) ? catalog.models : [];
      const validCatalogModels = catalogModels.filter(isCatalogModelEntry);
      if (catalogModels.length === 0) {
        problems.push('模型目录不包含模型');
      }
      if (catalogModels.some((model) => !isCatalogModelEntry(model))) {
        problems.push('模型目录存在缺少 Codex 必要字段的条目');
      }
      if (
        receipt?.defaultModel &&
        validCatalogModels.every((model) => model.slug !== receipt.defaultModel)
      ) {
        problems.push(`回执默认模型 ${receipt.defaultModel} 不在模型目录中`);
      }
      if (Array.isArray(receipt?.selectedModels)) {
        const catalogSlugs = new Set(validCatalogModels.map((model) => model.slug));
        const missingModels = receipt.selectedModels.filter(
          (model) => typeof model === 'string' && model.trim() && !catalogSlugs.has(model),
        );
        if (missingModels.length > 0) {
          problems.push(`模型目录缺少回执中的模型：${missingModels.join(', ')}`);
        }
      }
    } catch {
      problems.push('模型目录不是有效 JSON');
    }
  }

  return {
    ok: problems.length === 0,
    problems,
  };
}

function isCatalogModelEntry(model) {
  return Boolean(
    model &&
      typeof model === 'object' &&
      typeof model.slug === 'string' &&
      model.slug &&
      typeof model.base_instructions === 'string' &&
      model.base_instructions,
  );
}

function inferConfigAuthMode(configText, providerToken) {
  const providerBlock = readManagedProviderBlock(configText);
  if (providerToken || /^\s*experimental_bearer_token\s*=/m.test(providerBlock)) {
    return 'provider-token';
  }
  if (/^\s*env_key\s*=/m.test(providerBlock)) {
    return 'env';
  }
  if (/^\s*requires_openai_auth\s*=\s*true\b/m.test(providerBlock)) {
    return 'auth-json';
  }
  return null;
}

function hasTomlStringAssignment(text, key, value) {
  const escapedKey = escapeRegExp(key);
  const escapedValue = escapeRegExp(JSON.stringify(String(value)));
  const assignmentPattern = new RegExp(`^\\s*${escapedKey}\\s*=\\s*${escapedValue}\\s*(?:#.*)?$`);
  let inTable = false;
  for (const line of String(text || '').split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith('#')) {
      continue;
    }
    if (isTomlTableHeader(trimmed)) {
      inTable = true;
      continue;
    }
    if (!inTable && assignmentPattern.test(line)) {
      return true;
    }
  }
  return false;
}

function isTomlTableHeader(trimmed) {
  const header = trimmed.replace(/\s+#.*$/, '');
  const isArrayTable = header.startsWith('[[') && header.endsWith(']]');
  const isTable = header.startsWith('[') && header.endsWith(']');
  if (!isTable) {
    return false;
  }
  const body = isArrayTable ? header.slice(2, -2) : header.slice(1, -1);
  const parts = splitTomlDottedKey(body);
  return parts.length > 0 && parts.every(isTomlTableKeyPart);
}

function splitTomlDottedKey(value) {
  const parts = [];
  let current = '';
  let quote = '';
  let escaped = false;

  for (const char of String(value)) {
    if (escaped) {
      current += char;
      escaped = false;
      continue;
    }
    if (quote === '"' && char === '\\') {
      current += char;
      escaped = true;
      continue;
    }
    if (quote) {
      current += char;
      if (char === quote) {
        quote = '';
      }
      continue;
    }
    if (char === '"' || char === "'") {
      quote = char;
      current += char;
      continue;
    }
    if (char === '.') {
      parts.push(current.trim());
      current = '';
      continue;
    }
    current += char;
  }

  if (quote) {
    return [];
  }
  parts.push(current.trim());
  return parts;
}

function isTomlTableKeyPart(part) {
  if (!part) {
    return false;
  }
  if (/^"([^"\\]|\\.)+"$/.test(part) || /^'[^']+'$/.test(part)) {
    return true;
  }
  return /^[A-Za-z0-9_-]+$/.test(part);
}

function escapeRegExp(value) {
  return String(value).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}
