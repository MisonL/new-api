import assert from "node:assert/strict";
import test from "node:test";

import { getUserIdFromLocalStorage } from "../classic/src/helpers/userStorage.js";

// apiCore.js 在模块顶层就用它构造默认请求头（axios.create 的 headers）。
// 此前实现直接 JSON.parse(localStorage.getItem('user'))：一条损坏的 localStorage
// 会在 import 阶段抛异常，整个应用白屏。解析失败必须降级成"未登录"（-1），
// 而不是让模块加载失败。

function fakeStorage(values) {
  return {
    getItem(key) {
      return Object.prototype.hasOwnProperty.call(values, key) ? values[key] : null;
    },
  };
}

test("returns the id from a well-formed user record", () => {
  const storage = fakeStorage({ user: JSON.stringify({ id: 42, username: "u" }) });
  assert.equal(getUserIdFromLocalStorage(storage), 42);
});

test("returns -1 when the user record is absent", () => {
  assert.equal(getUserIdFromLocalStorage(fakeStorage({})), -1);
});

test("returns -1 instead of throwing on corrupt JSON", () => {
  const storage = fakeStorage({ user: "{not json" });
  assert.equal(
    getUserIdFromLocalStorage(storage),
    -1,
    "损坏的 localStorage 不得抛异常",
  );
});

test("returns -1 when the record has no id", () => {
  const storage = fakeStorage({ user: JSON.stringify({ username: "u" }) });
  assert.equal(getUserIdFromLocalStorage(storage), -1);
});

test("returns -1 when storage access itself throws", () => {
  const storage = {
    getItem() {
      throw new Error("storage disabled");
    },
  };
  assert.equal(getUserIdFromLocalStorage(storage), -1);
});
