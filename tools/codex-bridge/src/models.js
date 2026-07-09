import { modelsEndpoint, normalizeProviderBaseUrl } from './url.js';
import { MODEL_DISCOVERY_TIMEOUT_MS } from './constants.js';

export function parseModelIds(payload) {
  if (!payload || !Array.isArray(payload.data)) {
    throw new Error('/v1/models 响应必须包含 data 数组');
  }

  const ids = [];
  const seen = new Set();
  for (const item of payload.data) {
    const id = typeof item === 'string' ? item : item && item.id;
    if (typeof id !== 'string' || !id.trim()) {
      continue;
    }
    const normalized = id.trim();
    if (!seen.has(normalized)) {
      seen.add(normalized);
      ids.push(normalized);
    }
  }

  if (ids.length === 0) {
    throw new Error('new-api 返回了空模型列表');
  }
  return ids;
}

export async function discoverModels({
  baseUrl,
  apiKey,
  fetchImpl = globalThis.fetch,
  timeoutMs = MODEL_DISCOVERY_TIMEOUT_MS,
}) {
  if (!fetchImpl) {
    throw new Error('当前 Node 运行时不支持 fetch');
  }
  if (typeof apiKey !== 'string' || !apiKey.trim()) {
    throw new Error('需要 API Key');
  }
  const trimmedApiKey = apiKey.trim();
  const providerBaseUrl = normalizeProviderBaseUrl(baseUrl);
  const requestInit = {
    headers: {
      Authorization: `Bearer ${trimmedApiKey}`,
      Accept: 'application/json',
    },
  };
  const timeoutSignal = createTimeoutSignal(timeoutMs);
  if (timeoutSignal) {
    requestInit.signal = timeoutSignal.signal;
  }
  let response;
  try {
    response = await fetchImpl(modelsEndpoint(providerBaseUrl), requestInit);
    const bodyText = await response.text();
    if (!response.ok) {
      throw new Error(
        `new-api /v1/models 请求失败，HTTP ${response.status}: ${bodyText.slice(0, 200)}`
      );
    }

    let payload;
    try {
      payload = JSON.parse(bodyText);
    } catch {
      throw new Error('new-api /v1/models 没有返回有效 JSON');
    }

    return {
      providerBaseUrl,
      modelIds: parseModelIds(payload),
    };
  } catch (error) {
    if (isAbortError(error)) {
      throw new Error(`new-api /v1/models 请求超时，超过 ${timeoutMs}ms`);
    }
    throw error;
  } finally {
    if (timeoutSignal && timeoutSignal.timer) {
      clearTimeout(timeoutSignal.timer);
    }
  }
}

function createTimeoutSignal(timeoutMs) {
  if (!Number.isFinite(timeoutMs) || timeoutMs <= 0) {
    return null;
  }
  if (typeof AbortSignal !== 'undefined' && typeof AbortSignal.timeout === 'function') {
    return { signal: AbortSignal.timeout(timeoutMs), timer: null };
  }
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  if (typeof timer?.unref === 'function') {
    timer.unref();
  }
  return { signal: controller.signal, timer };
}

function isAbortError(error) {
  return error && (error.name === 'AbortError' || error.code === 'ABORT_ERR');
}
