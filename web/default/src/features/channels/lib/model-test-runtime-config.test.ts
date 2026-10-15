import assert from 'node:assert/strict'
import test from 'node:test'
import type { Channel } from '../types'
import {
  buildModelTestRuntimeParams,
  DEFAULT_MODEL_TEST_RUNTIME_CONFIG,
  getModelTestRuntimeSnapshot,
  normalizeModelTestRuntimeConfig,
} from './model-test-runtime-config'

test('uses the same defaults as the classic frontend', () => {
  assert.deepEqual(
    normalizeModelTestRuntimeConfig(),
    DEFAULT_MODEL_TEST_RUNTIME_CONFIG
  )
})

test('keeps explicit false values and normalizes user input', () => {
  assert.deepEqual(
    normalizeModelTestRuntimeConfig({
      enabled: false,
      headerConfig: false,
      paramOverride: false,
      proxy: false,
      modelMapping: false,
      responseProtocol: 'invalid' as never,
      testPrompt: '  explain this  ',
      maxTokens: 10000,
    }),
    {
      enabled: false,
      headerConfig: false,
      paramOverride: false,
      proxy: false,
      modelMapping: false,
      responseProtocol: 'native',
      testPrompt: 'explain this',
      maxTokens: 8192,
    }
  )
})

test('serializes all runtime query parameters, including false', () => {
  assert.deepEqual(
    buildModelTestRuntimeParams({
      enabled: false,
      headerConfig: false,
      paramOverride: false,
      proxy: false,
      modelMapping: false,
      responseProtocol: 'chat_completions',
      testPrompt: 'hello',
      maxTokens: 64,
    }),
    {
      runtime_config: 'false',
      header_config: 'false',
      param_override: 'false',
      proxy: 'false',
      model_mapping: 'false',
      response_protocol: 'chat_completions',
      test_prompt: 'hello',
      max_tokens: '64',
    }
  )
})

test('detects configured channel capabilities for the runtime panel', () => {
  const channel: Channel = {
    id: 1,
    type: 1,
    key: '',
    status: 1,
    name: 'test',
    created_time: 0,
    test_time: 0,
    response_time: 0,
    models: 'model',
    group: 'default',
    other: '',
    balance: 0,
    balance_updated_time: 0,
    used_quota: 0,
    test_model: null,
    base_url: null,
    openai_organization: null,
    weight: null,
    model_mapping: '{"model":"upstream-model"}',
    param_override: '{"operations":[]}',
    header_override: null,
    setting: '{"proxy":"http://127.0.0.1:7890"}',
    settings:
      '{"header_profile_strategy":{"enabled":true,"selected_profile_ids":["codex"]}}',
    remark: '',
    max_input_tokens: 0,
    max_concurrency: 0,
    other_info: '',
    channel_info: {
      is_multi_key: false,
      multi_key_size: 0,
      multi_key_polling_index: 0,
      multi_key_mode: 'random',
    },
  }

  assert.deepEqual(getModelTestRuntimeSnapshot(channel), {
    headerConfigured: true,
    headerValue: 'Header Profile: codex',
    paramConfigured: true,
    paramValue: 'operations',
    proxyConfigured: true,
    proxyValue: 'http://127.0.0.1:7890',
    modelMappingConfigured: true,
    modelMappingValue: 'model -> upstream-model',
  })
})
