import assert from "node:assert/strict";
import test from "node:test";

import { installReadonlyXHRGuard } from "../classic/src/helpers/readonlyGuard.js";

// 只读联调环境要求前端自己阻断写入（AGENTS.md 第 7 节）。classic 这一层此前
// 只 patch 了 fetch 与 axios：任何用 new XMLHttpRequest() 发起的请求都会绕过
// 前端守卫，只剩 nginx 代理一层。守卫必须是"一层"而不是"选择性接入"。

function makeStubXHR() {
  return class {
    opened = null;
    open(method, url) {
      this.opened = { method, url };
    }
  };
}

function withStubXHR(run) {
  const Ctor = makeStubXHR();
  const original = globalThis.XMLHttpRequest;
  globalThis.XMLHttpRequest = Ctor;
  try {
    return run(Ctor);
  } finally {
    globalThis.XMLHttpRequest = original;
  }
}

const blockWrites = (method) => String(method || "get").toLowerCase() !== "get";

test("blocks unsafe XHR methods before the socket is opened", () => {
  withStubXHR((Ctor) => {
    installReadonlyXHRGuard({
      isBlocked: blockWrites,
      message: "只读模式已阻止写入",
    });
    const xhr = new Ctor();
    assert.throws(
      () => xhr.open("POST", "/v1/chat/completions"),
      /只读模式已阻止写入/,
      "POST 必须在 open 阶段就被拒绝",
    );
    assert.equal(xhr.opened, null, "被拒绝的请求不得进入原始 open");
  });
});

test("leaves allowed requests untouched", () => {
  withStubXHR((Ctor) => {
    installReadonlyXHRGuard({
      isBlocked: blockWrites,
      message: "只读模式已阻止写入",
    });
    const xhr = new Ctor();
    xhr.open("GET", "/api/status");
    assert.deepEqual(xhr.opened, { method: "GET", url: "/api/status" });
  });
});

test("is inert when read-only mode is off", () => {
  withStubXHR((Ctor) => {
    installReadonlyXHRGuard({
      isBlocked: blockWrites,
      message: "只读模式已阻止写入",
      enabled: false,
    });
    const xhr = new Ctor();
    xhr.open("POST", "/api/user/self");
    assert.deepEqual(xhr.opened, { method: "POST", url: "/api/user/self" });
  });
});
