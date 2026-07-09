import path from 'node:path';
import { AUTH_FILE, CONFIG_FILE, ENV_KEY, RECEIPT_FILE } from './constants.js';
import { readExperimentalBearerToken } from './config.js';
import { readTextIfExists } from './files.js';
import { parseReceipt } from './receipt.js';
import { createSetupPlan, writeSetupPlan } from './setup.js';

export async function refreshFromReceipt({ codexHome, env = process.env, fetchImpl }) {
  const receipt = parseReceipt(await readTextIfExists(path.join(codexHome, RECEIPT_FILE)));
  const apiKey = await resolveRefreshKey({ codexHome, env, receipt });

  const plan = await createSetupPlan({
    codexHome,
    baseUrl: receipt.providerBaseUrl,
    apiKey,
    authMode: receipt.authMode || 'auth-json',
    envKey: receipt.envKey,
    defaultModel: receipt.defaultModel,
    selectedModels: receipt.selectionMode === 'all' ? undefined : receipt.selectedModels,
    fetchImpl,
  });
  const backupId = await writeSetupPlan(plan);
  return { plan, backupId };
}

async function resolveRefreshKey({ codexHome, env, receipt }) {
  if (receipt.authMode === 'provider-token') {
    const configText = await readTextIfExists(path.join(codexHome, CONFIG_FILE));
    if (!configText.trim()) {
      throw new Error('缺少 config.toml');
    }
    const token = readExperimentalBearerToken(configText);
    if (!token) {
      throw new Error('config.toml 不包含 experimental_bearer_token');
    }
    return token;
  }
  if (receipt.authMode === 'env') {
    const keyName = receipt.envKey || ENV_KEY;
    if (!env[keyName]) {
      throw new Error(`环境变量 ${keyName} 为空`);
    }
    return env[keyName];
  }

  const authText = await readTextIfExists(path.join(codexHome, AUTH_FILE));
  if (!authText.trim()) {
    throw new Error('缺少 auth.json');
  }
  let auth;
  try {
    auth = JSON.parse(authText);
  } catch {
    throw new Error('auth.json 不是有效 JSON');
  }
  if (!auth[ENV_KEY]) {
    throw new Error(`auth.json 不包含 ${ENV_KEY}`);
  }
  return auth[ENV_KEY];
}
