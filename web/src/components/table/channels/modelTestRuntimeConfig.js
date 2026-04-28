export const DEFAULT_MODEL_TEST_ENDPOINT_TYPE = 'openai-response';

export const MODEL_TEST_RESPONSE_PROTOCOLS = {
  NATIVE: 'native',
  CHAT_COMPLETIONS: 'chat_completions',
};

export const DEFAULT_MODEL_TEST_RUNTIME_CONFIG = {
  enabled: true,
  headerConfig: true,
  paramOverride: true,
  proxy: true,
  modelMapping: true,
  responseProtocol: MODEL_TEST_RESPONSE_PROTOCOLS.NATIVE,
  testPrompt: 'hi',
  maxTokens: 16,
};

export const normalizeModelTestRuntimeConfig = (config) => {
  const normalized = {
    ...DEFAULT_MODEL_TEST_RUNTIME_CONFIG,
    ...(config || {}),
  };
  if (
    !Object.values(MODEL_TEST_RESPONSE_PROTOCOLS).includes(
      normalized.responseProtocol,
    )
  ) {
    normalized.responseProtocol = MODEL_TEST_RESPONSE_PROTOCOLS.NATIVE;
  }
  normalized.testPrompt =
    typeof normalized.testPrompt === 'string' && normalized.testPrompt.trim()
      ? normalized.testPrompt.trim()
      : DEFAULT_MODEL_TEST_RUNTIME_CONFIG.testPrompt;
  const parsedMaxTokens = Number(normalized.maxTokens);
  normalized.maxTokens =
    Number.isFinite(parsedMaxTokens) && parsedMaxTokens > 0
      ? Math.floor(parsedMaxTokens)
      : DEFAULT_MODEL_TEST_RUNTIME_CONFIG.maxTokens;
  return normalized;
};

export const appendModelTestRuntimeParams = (params, config) => {
  const runtimeConfig = normalizeModelTestRuntimeConfig(config);
  params.set('runtime_config', runtimeConfig.enabled ? 'true' : 'false');
  params.set('header_config', runtimeConfig.headerConfig ? 'true' : 'false');
  params.set('param_override', runtimeConfig.paramOverride ? 'true' : 'false');
  params.set('proxy', runtimeConfig.proxy ? 'true' : 'false');
  params.set('model_mapping', runtimeConfig.modelMapping ? 'true' : 'false');
  params.set('response_protocol', runtimeConfig.responseProtocol);
  params.set('test_prompt', runtimeConfig.testPrompt);
  params.set('max_tokens', String(runtimeConfig.maxTokens));
};

const safeParseJsonObject = (value) => {
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    return value;
  }
  if (!value || typeof value !== 'string') {
    return {};
  }
  try {
    const parsed = JSON.parse(value);
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed)
      ? parsed
      : {};
  } catch {
    return {};
  }
};

const hasConfigText = (value) =>
  typeof value === 'string' && value.trim() !== '' && value.trim() !== '{}';

const compactList = (items, limit = 3) => {
  const values = items.map((item) => String(item).trim()).filter(Boolean);
  if (values.length <= limit) {
    return values.join(', ');
  }
  return `${values.slice(0, limit).join(', ')} +${values.length - limit}`;
};

const maskProxyValue = (proxy) => {
  if (typeof proxy !== 'string' || proxy.trim() === '') {
    return '';
  }
  try {
    const parsed = new URL(proxy);
    return `${parsed.protocol}//${parsed.host}`;
  } catch {
    return 'configured';
  }
};

const normalizeHeaderNames = (value) => {
  if (typeof value === 'string') {
    return value
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean);
  }
  if (Array.isArray(value)) {
    return value.map((item) => String(item).trim()).filter(Boolean);
  }
  if (value && typeof value === 'object') {
    return Object.keys(value)
      .map((item) => item.trim())
      .filter(Boolean);
  }
  return [];
};

const summarizeParamOverride = (value, t) => {
  if (!hasConfigText(value)) {
    return '';
  }
  const config = safeParseJsonObject(value);
  const operations = Array.isArray(config.operations) ? config.operations : [];
  if (operations.length > 0) {
    const modes = compactList(
      operations.map((operation) => operation?.mode || '').filter(Boolean),
      4,
    );
    const passHeaders = operations
      .filter((operation) => operation?.mode === 'pass_headers')
      .flatMap((operation) => normalizeHeaderNames(operation?.value));
    if (passHeaders.length > 0) {
      return `${operations.length} ${t('条规则')} | pass_headers: ${compactList(passHeaders, 4)}`;
    }
    return modes
      ? `${operations.length} ${t('条规则')} | ${modes}`
      : `${operations.length} ${t('条规则')}`;
  }
  const keys = Object.keys(config).filter(Boolean);
  return keys.length > 0 ? `${t('字段')}: ${compactList(keys, 4)}` : '';
};

const summarizeModelMapping = (value, t) => {
  if (!hasConfigText(value)) {
    return '';
  }
  const config = safeParseJsonObject(value);
  const entries = Object.entries(config).filter(([from, to]) => from && to);
  if (entries.length === 0) {
    return t('已配置');
  }
  const firstItems = entries
    .slice(0, 2)
    .map(([from, to]) => `${from} -> ${to}`);
  if (entries.length > 2) {
    firstItems.push(`+${entries.length - 2}`);
  }
  return firstItems.join(', ');
};

const formatHeaderPolicyMode = (mode, t) => {
  const normalized = typeof mode === 'string' ? mode.trim() : '';
  if (normalized === 'system_default') {
    return t('系统默认');
  }
  if (normalized === 'channel_priority') {
    return t('渠道优先');
  }
  if (normalized === 'tag_priority') {
    return t('标签优先');
  }
  if (normalized === 'merge') {
    return t('合并');
  }
  return normalized || t('已配置');
};

export const getModelTestRuntimeSnapshot = (channel, t) => {
  const channelSetting = safeParseJsonObject(channel?.setting);
  const channelSettings = safeParseJsonObject(channel?.settings);
  const strategy = channelSettings.header_profile_strategy || {};
  const selectedProfileIds = Array.isArray(strategy.selected_profile_ids)
    ? strategy.selected_profile_ids.filter(Boolean)
    : [];
  const hasHeaderProfile =
    strategy.enabled === true && selectedProfileIds.length > 0;
  const hasHeaderPolicyMode = Boolean(channelSettings.header_policy_mode);
  const hasLegacyUserAgentPolicy =
    channelSettings.override_header_user_agent === true ||
    Boolean(channelSettings.ua_strategy);
  const hasHeaderPolicy = hasHeaderPolicyMode || hasLegacyUserAgentPolicy;
  const hasHeaderOverride = hasConfigText(channel?.header_override);
  const proxy =
    typeof channelSetting.proxy === 'string' ? channelSetting.proxy : '';
  const headerValue = hasHeaderProfile
    ? `Header Profile: ${compactList(selectedProfileIds, 3)}`
    : hasHeaderOverride
      ? 'header_override'
      : hasLegacyUserAgentPolicy
        ? t('旧版 UA 策略')
        : hasHeaderPolicyMode
          ? `${t('请求头策略')}: ${formatHeaderPolicyMode(channelSettings.header_policy_mode, t)}`
          : '';

  return {
    headerConfigured: hasHeaderProfile || hasHeaderOverride || hasHeaderPolicy,
    headerValue,
    paramConfigured: hasConfigText(channel?.param_override),
    paramValue: summarizeParamOverride(channel?.param_override, t),
    proxyConfigured: proxy.trim() !== '',
    proxyValue: maskProxyValue(proxy),
    modelMappingConfigured: hasConfigText(channel?.model_mapping),
    modelMappingValue: summarizeModelMapping(channel?.model_mapping, t),
  };
};

export const formatRuntimeResult = (runtimeConfig, t) => {
  if (!runtimeConfig) {
    return '';
  }
  const parts = [];
  if (!runtimeConfig.runtime_config_enabled) {
    return t('未使用渠道运行配置');
  }
  parts.push(
    runtimeConfig.header_config_enabled
      ? runtimeConfig.header_applied
        ? t('请求头已应用')
        : t('请求头未应用')
      : t('请求头已关闭'),
  );
  parts.push(
    runtimeConfig.param_override_enabled
      ? runtimeConfig.param_override_applied
        ? t('参数覆盖已参与')
        : t('参数覆盖未参与')
      : t('参数覆盖已关闭'),
  );
  parts.push(
    runtimeConfig.proxy_enabled
      ? runtimeConfig.proxy_configured
        ? t('代理已启用')
        : t('代理未配置')
      : t('代理已关闭'),
  );
  if (runtimeConfig.header_profile_id) {
    parts.push(`Profile: ${runtimeConfig.header_profile_id}`);
  }
  if (
    runtimeConfig.model_mapping_applied &&
    runtimeConfig.upstream_model &&
    runtimeConfig.upstream_model !== runtimeConfig.origin_model
  ) {
    parts.push(
      `${runtimeConfig.origin_model} -> ${runtimeConfig.upstream_model}`,
    );
  }
  if (runtimeConfig.final_request_path) {
    const pathText =
      runtimeConfig.request_path &&
      runtimeConfig.request_path !== runtimeConfig.final_request_path
        ? `${runtimeConfig.request_path} -> ${runtimeConfig.final_request_path}`
        : runtimeConfig.final_request_path;
    parts.push(`${t('路径')}: ${pathText}`);
  }
  if (
    Array.isArray(runtimeConfig.request_conversion_chain) &&
    runtimeConfig.request_conversion_chain.length > 1
  ) {
    parts.push(
      `${t('协议')}: ${runtimeConfig.request_conversion_chain.join(' -> ')}`,
    );
  }
  return parts.join(' | ');
};
