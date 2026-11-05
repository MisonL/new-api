import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

// axios 实例的默认请求头在创建时就固定了 New-API-User。清除本地登录态后
// 若不重建实例，后续请求仍会带上一个已注销用户的 id（服务端按该 id 鉴权，
// 可能命中同 id 的新用户或直接 401）。所有"仅清状态、不整页跳转"的退出
// 路径都必须在 removeItem('user') 之后调用 updateAPI()。

const classicSrc = fileURLToPath(
  new URL("../classic/src/", import.meta.url),
);

// 整页跳转到登录页会重新加载模块，axios 实例随之重建。
const FULL_NAVIGATION_FILES = new Set(["helpers/authRedirect.js"]);

const REMOVE_USER = /removeItem\(\s*['"]user['"]\s*\)/g;
const AFTER_WINDOW = 400;

async function collectSourceFiles(dir) {
  const entries = await fs.readdir(dir, { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    if (entry.name === "node_modules" || entry.name === "dist") continue;
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await collectSourceFiles(full)));
    } else if (/\.(js|jsx|ts|tsx)$/.test(entry.name)) {
      files.push(full);
    }
  }
  return files;
}

test("every in-app logout rebuilds the axios instance", async () => {
  const files = await collectSourceFiles(classicSrc);
  const offenders = [];
  for (const file of files) {
    const relative = path.relative(classicSrc, file).split(path.sep).join("/");
    if (FULL_NAVIGATION_FILES.has(relative)) continue;
    const source = await fs.readFile(file, "utf8");
    for (const match of source.matchAll(REMOVE_USER)) {
      const after = source.slice(
        match.index,
        match.index + AFTER_WINDOW,
      );
      if (!after.includes("updateAPI(")) {
        const line = source.slice(0, match.index).split("\n").length;
        offenders.push(`${relative}:${line}`);
      }
    }
  }
  assert.deepEqual(offenders, [], `退出后未重建 axios 实例: ${offenders.join(", ")}`);
});
