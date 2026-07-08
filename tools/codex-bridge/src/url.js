export function normalizeProviderBaseUrl(input) {
  const raw = String(input || '').trim();
  if (!raw) {
    throw new Error('需要 new-api 基础 URL');
  }

  let url;
  try {
    url = new URL(raw);
  } catch {
    throw new Error(`new-api 基础 URL 无效：${raw}`);
  }

  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    throw new Error('new-api 基础 URL 必须以 http:// 或 https:// 开头');
  }
  if (url.username || url.password) {
    throw new Error('new-api 基础 URL 不能包含用户名或密码');
  }

  url.hash = '';
  url.search = '';
  const parts = url.pathname.split('/').filter(Boolean);
  if (isOpenAICompatibleEndpointPath(parts)) {
    throw new Error('new-api 基础 URL 只能填写服务根路径或 /v1，不能填写具体接口路径');
  }
  if (containsKnownEndpointSegment(parts)) {
    throw new Error('new-api 基础 URL 不能直接填写具体接口路径');
  }
  if (parts.at(-1)?.toLowerCase() !== 'v1') {
    parts.push('v1');
  }
  url.pathname = `/${parts.join('/')}`;
  return url.toString().replace(/\/$/, '');
}

export function modelsEndpoint(providerBaseUrl) {
  return `${normalizeProviderBaseUrl(providerBaseUrl)}/models`;
}

function isOpenAICompatibleEndpointPath(parts) {
  const versionIndex = parts.findIndex((part) => part.toLowerCase() === 'v1');
  return versionIndex >= 0 && parts.length > versionIndex + 1;
}

function containsKnownEndpointSegment(parts) {
  const normalized = parts.map((part) => part.toLowerCase());
  const knownEndpointTails = [
    ['chat', 'completions'],
    ['images', 'generations'],
    ['audio', 'speech'],
    ['audio', 'transcriptions'],
    ['audio', 'translations'],
    ['fine_tuning', 'jobs'],
  ];
  if (knownEndpointTails.some((tail) => endsWithSegments(normalized, tail))) {
    return true;
  }

  const knownEndpoints = new Set([
    'models',
    'responses',
    'chat',
    'completions',
    'embeddings',
    'images',
    'audio',
    'batches',
    'files',
    'fine_tuning',
    'moderations',
  ]);
  return normalized.length > 0 && knownEndpoints.has(normalized.at(-1));
}

function endsWithSegments(parts, tail) {
  if (parts.length < tail.length) {
    return false;
  }
  return tail.every((segment, index) => parts[parts.length - tail.length + index] === segment);
}
