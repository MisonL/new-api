import { CATALOG_FILE, PROVIDER_ID, TOOL_ID } from './constants.js';
import { isValidBackupId } from './files.js';

export function buildReceipt({
  providerBaseUrl,
  defaultModel,
  selectedModels,
  selectionMode,
  authMode,
  envKey,
  backupId,
}) {
  const receipt = {
    schema: 1,
    tool: TOOL_ID,
    providerId: PROVIDER_ID,
    providerBaseUrl,
    catalogFile: CATALOG_FILE,
    defaultModel,
    selectedModels,
    selectionMode,
    authMode,
    envKey,
    backupId,
    updatedAt: new Date().toISOString(),
  };
  validateReceipt(receipt);
  return `${JSON.stringify(receipt, null, 2)}\n`;
}

export function parseReceipt(text) {
  if (!text.trim()) {
    throw new Error('缺少 receipt');
  }
  let receipt;
  try {
    receipt = JSON.parse(text);
  } catch {
    throw new Error('receipt 不是有效 JSON');
  }
  if (!receipt || receipt.tool !== TOOL_ID) {
    throw new Error(`receipt 不是由 ${TOOL_ID} 创建的`);
  }
  validateReceipt(receipt);
  return receipt;
}

function validateReceipt(receipt) {
  if (receipt.schema !== 1) {
    throw new Error('receipt schema 不受支持');
  }
  if (receipt.providerId !== PROVIDER_ID) {
    throw new Error('receipt providerId 与 new-api 不一致');
  }
  requireString(receipt.catalogFile, 'catalogFile');
  if (receipt.catalogFile !== CATALOG_FILE) {
    throw new Error('receipt catalogFile 与 new-api 模型目录不一致');
  }
  requireString(receipt.providerBaseUrl, 'providerBaseUrl');
  requireString(receipt.defaultModel, 'defaultModel');
  if (receipt.selectionMode && !['all', 'explicit'].includes(receipt.selectionMode)) {
    throw new Error('receipt selectionMode 不受支持');
  }
  if (receipt.authMode && !['provider-token', 'auth-json', 'env'].includes(receipt.authMode)) {
    throw new Error('receipt authMode 不受支持');
  }
  if (receipt.authMode === 'env') {
    requireString(receipt.envKey, 'envKey');
  }
  if (receipt.envKey !== undefined && typeof receipt.envKey !== 'string') {
    throw new Error('receipt envKey 必须是字符串');
  }
  if (receipt.backupId !== undefined && !isValidBackupId(receipt.backupId)) {
    throw new Error('receipt backupId 无效');
  }
  if (receipt.selectionMode === 'explicit' || receipt.selectedModels !== undefined) {
    if (receipt.selectedModels === undefined) {
      throw new Error('receipt selectionMode 为 explicit 时必须包含 selectedModels');
    }
    validateModelList(receipt.selectedModels);
  }
}

function requireString(value, field) {
  if (typeof value !== 'string' || !value.trim()) {
    throw new Error(`receipt ${field} 必须是非空字符串`);
  }
}

function validateModelList(models) {
  if (!Array.isArray(models) || models.length === 0) {
    throw new Error('receipt selectedModels 必须是非空数组');
  }
  if (models.some((model) => typeof model !== 'string' || !model.trim())) {
    throw new Error('receipt selectedModels 必须只包含非空字符串');
  }
}
