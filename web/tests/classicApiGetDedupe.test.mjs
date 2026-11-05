import assert from "node:assert/strict";
import test from "node:test";

import { installGetDeduplication } from "../classic/src/helpers/apiGetDedupe.js";

// GET 去重把同 URL 的并发请求合并成一个 Promise。带 AbortSignal 的请求不能参与
// 合并：共享 Promise 无法承载两个独立的取消生命周期——加入方的 signal 永远传不到
// 请求上，创建方取消又会连带取消所有加入方。default 的 api.ts 已按此豁免，
// classic 此前没有，只是现有调用点恰好都同时传了 disableDuplicate。

function createFakeInstance() {
  const calls = [];
  const instance = {
    get: (url, config) => {
      calls.push({ url, config });
      return Promise.resolve({ url, config });
    },
  };
  return { instance, calls };
}

test("deduplicates concurrent identical GETs without a signal", async () => {
  const { instance, calls } = createFakeInstance();
  installGetDeduplication(instance);

  const [a, b] = await Promise.all([
    instance.get("/api/status"),
    instance.get("/api/status"),
  ]);
  assert.equal(calls.length, 1, "并发同 URL 请求必须合并");
  assert.equal(a, b, "合并后的调用方应拿到同一个 Promise 结果");
});

test("does not merge GETs that carry an AbortSignal", async () => {
  const { instance, calls } = createFakeInstance();
  installGetDeduplication(instance);

  const controllerA = new AbortController();
  const controllerB = new AbortController();
  await Promise.all([
    instance.get("/api/user/self", { signal: controllerA.signal }),
    instance.get("/api/user/self", { signal: controllerB.signal }),
  ]);
  assert.equal(calls.length, 2, "带 signal 的请求必须各自发起，不能被合并");
  assert.equal(calls[0].config.signal, controllerA.signal);
  assert.equal(calls[1].config.signal, controllerB.signal);
});

test("does not merge when disableDuplicate is set", async () => {
  const { instance, calls } = createFakeInstance();
  installGetDeduplication(instance);

  await Promise.all([
    instance.get("/api/log/self", { disableDuplicate: true }),
    instance.get("/api/log/self", { disableDuplicate: true }),
  ]);
  assert.equal(calls.length, 2, "disableDuplicate 的请求必须各自发起");
});

test("different params are not merged", async () => {
  const { instance, calls } = createFakeInstance();
  installGetDeduplication(instance);

  await Promise.all([
    instance.get("/api/log/self", { params: { page: 1 } }),
    instance.get("/api/log/self", { params: { page: 2 } }),
  ]);
  assert.equal(calls.length, 2, "参数不同的 GET 不得合并");
});
