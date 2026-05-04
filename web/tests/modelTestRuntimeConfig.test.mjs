import test from "node:test";
import assert from "node:assert/strict";

import {
  DEFAULT_MODEL_TEST_ENDPOINT_TYPE,
  DEFAULT_MODEL_TEST_RUNTIME_CONFIG,
  appendModelTestRuntimeParams,
  getModelTestRuntimeSnapshot,
  normalizeModelTestRuntimeConfig,
} from "../classic/src/components/table/channels/modelTestRuntimeConfig.js";

test("模型测试默认使用 Responses 端点", () => {
  assert.equal(DEFAULT_MODEL_TEST_ENDPOINT_TYPE, "openai-response");
});

test("模型测试运行参数默认使用原生 Responses 协议和最小请求参数", () => {
  assert.deepEqual(
    normalizeModelTestRuntimeConfig({
      headerConfig: false,
    }),
    {
      ...DEFAULT_MODEL_TEST_RUNTIME_CONFIG,
      headerConfig: false,
    },
  );
  assert.equal(DEFAULT_MODEL_TEST_RUNTIME_CONFIG.responseProtocol, "native");
  assert.equal(DEFAULT_MODEL_TEST_RUNTIME_CONFIG.testPrompt, "hi");
  assert.equal(DEFAULT_MODEL_TEST_RUNTIME_CONFIG.maxTokens, 16);
});

test("appendModelTestRuntimeParams appends protocol and custom request params", () => {
  const params = new URLSearchParams();

  appendModelTestRuntimeParams(params, {
    enabled: true,
    headerConfig: true,
    paramOverride: true,
    proxy: false,
    modelMapping: true,
    responseProtocol: "chat_completions",
    testPrompt: "summarize context",
    maxTokens: 64,
  });

  assert.equal(params.get("runtime_config"), "true");
  assert.equal(params.get("proxy"), "false");
  assert.equal(params.get("response_protocol"), "chat_completions");
  assert.equal(params.get("test_prompt"), "summarize context");
  assert.equal(params.get("max_tokens"), "64");
});

test("runtime snapshot labels header policy mode without legacy UA wording", () => {
  const snapshot = getModelTestRuntimeSnapshot(
    {
      settings: '{"header_policy_mode":"system_default"}',
    },
    (text) => text,
  );

  assert.equal(snapshot.headerConfigured, true);
  assert.equal(snapshot.headerValue, "请求头策略: 系统默认");
});
