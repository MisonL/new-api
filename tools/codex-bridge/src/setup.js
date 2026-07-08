import fs from 'node:fs/promises';
import path from 'node:path';
import { AUTH_FILE, CATALOG_FILE, CONFIG_FILE, RECEIPT_FILE } from './constants.js';
import { buildModelCatalog } from './catalog.js';
import { buildAuthJson, buildConfigToml } from './config.js';
import { createBackup, atomicWrite, readTextIfExists, restoreBackup } from './files.js';
import { discoverModels } from './models.js';
import { buildReceipt } from './receipt.js';

export async function createSetupPlan({
  codexHome,
  baseUrl,
  apiKey,
  authMode = 'provider-token',
  envKey,
  defaultModel,
  selectedModels,
  fetchImpl,
  discovery,
}) {
  const resolvedDiscovery = discovery ?? await discoverModels({ baseUrl, apiKey, fetchImpl });
  const rawSelectedModels = Array.isArray(selectedModels) ? selectedModels : [];
  const explicitModels = rawSelectedModels.length
    ? [...new Set(rawSelectedModels.map((model) => String(model).trim()).filter(Boolean))]
    : [];
  if (rawSelectedModels.length && !explicitModels.length) {
    throw new Error('所选模型不能为空');
  }
  const selectionMode = explicitModels.length ? 'explicit' : 'all';
  if (selectionMode === 'explicit') {
    const missing = explicitModels.filter((model) => !resolvedDiscovery.modelIds.includes(model));
    if (missing.length) {
      throw new Error(`所选模型不在 new-api 返回列表中：${missing.join(', ')}`);
    }
  }
  const modelIds = selectionMode === 'explicit' ? explicitModels : resolvedDiscovery.modelIds;
  if (!Array.isArray(modelIds) || modelIds.length === 0) {
    throw new Error('未发现可用模型，无法生成 Codex 配置');
  }
  const finalDefaultModel = defaultModel || modelIds[0];
  if (!modelIds.includes(finalDefaultModel)) {
    throw new Error(`默认模型 ${finalDefaultModel} 不在所选模型列表中`);
  }

  const configPath = path.join(codexHome, CONFIG_FILE);
  const existingConfig = await readTextIfExists(configPath);
  const catalogText = `${JSON.stringify(buildModelCatalog(modelIds), null, 2)}\n`;
  const configText = buildConfigToml({
    existingText: existingConfig,
    defaultModel: finalDefaultModel,
    providerBaseUrl: resolvedDiscovery.providerBaseUrl,
    authMode,
    apiKey,
    envKey,
  });
  const files = {
    [CONFIG_FILE]: configText,
    [CATALOG_FILE]: catalogText,
  };
  if (authMode === 'auth-json') {
    const authPath = path.join(codexHome, AUTH_FILE);
    const existingAuth = await readTextIfExists(authPath);
    files[AUTH_FILE] = buildAuthJson({ existingText: existingAuth, apiKey });
  }

  return {
    codexHome,
    providerBaseUrl: resolvedDiscovery.providerBaseUrl,
    discoveredModels: resolvedDiscovery.modelIds,
    selectedModels: modelIds,
    selectionMode,
    authMode,
    envKey,
    defaultModel: finalDefaultModel,
    files,
    sensitiveConfig: authMode === 'provider-token',
  };
}

export async function writeSetupPlan(plan) {
  const { backupId } = await createBackup(plan.codexHome);
  try {
    await atomicWrite(path.join(plan.codexHome, CONFIG_FILE), plan.files[CONFIG_FILE], {
      mode: plan.sensitiveConfig && process.platform !== 'win32' ? 0o600 : undefined,
    });
    await atomicWrite(path.join(plan.codexHome, CATALOG_FILE), plan.files[CATALOG_FILE]);
    if (plan.files[AUTH_FILE]) {
      await atomicWrite(path.join(plan.codexHome, AUTH_FILE), plan.files[AUTH_FILE], {
        mode: process.platform === 'win32' ? undefined : 0o600,
      });
    }
    await atomicWrite(
      path.join(plan.codexHome, RECEIPT_FILE),
      buildReceipt({
        providerBaseUrl: plan.providerBaseUrl,
        defaultModel: plan.defaultModel,
        selectedModels: plan.selectedModels,
        selectionMode: plan.selectionMode,
        authMode: plan.authMode,
        envKey: plan.envKey,
        backupId,
      }),
    );
    return backupId;
  } catch (error) {
    try {
      await restoreBackup(plan.codexHome, backupId);
    } catch (restoreError) {
      const writeMessage = error instanceof Error ? error.message : String(error);
      const restoreMessage =
        restoreError instanceof Error ? restoreError.message : String(restoreError);
      throw new Error(`写入失败（${writeMessage}），且回滚失败（${restoreMessage}）`);
    }
    throw error;
  }
}
