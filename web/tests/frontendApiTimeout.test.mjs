import assert from "node:assert/strict";
import fs from "node:fs/promises";
import test from "node:test";

// 两套前端的 axios 实例以前都没有 timeout：上游或网关挂住时，登录、
// 保存、测试渠道等请求会无限期悬着，页面既没有错误也没有重试机会。
// 请求层必须有统一默认超时；个别确实更慢的端点要显式覆盖，不能靠
// “实例不设超时”来放行。这里用静态契约守住实例配置与覆盖点。

const defaultFile = new URL("../default/src/lib/api.ts", import.meta.url);
const classicFile = new URL("../classic/src/helpers/apiCore.js", import.meta.url);

test("default axios instance has a finite default timeout", async () => {
  const source = await fs.readFile(defaultFile, "utf8");
  const createBlock = source.slice(
    source.indexOf("export const api = axios.create("),
    source.indexOf("applyReadonlyRequestGuard(api)"),
  );
  assert.match(
    createBlock,
    /timeout:\s*\S+\s*,/,
    "api instance must declare a finite timeout",
  );
  assert.match(
    source,
    /export const DEFAULT_API_TIMEOUT_MS = [1-9][\d_]*\s*;?/,
    "default timeout constant must be a finite positive number",
  );
});

test("classic axios instances both have a finite default timeout", async () => {
  const source = await fs.readFile(classicFile, "utf8");
  const matches = source.match(/axios\.create\(\{[\s\S]*?\n\s*\}\)/g) ?? [];
  assert.equal(
    matches.length,
    2,
    `expected both initial and updateAPI axios instances, found ${matches.length}`,
  );
  for (const block of matches) {
    assert.match(
      block,
      /timeout:\s*\S+\s*,/,
      "each classic axios instance must declare a finite timeout",
    );
  }
  assert.match(
    source,
    /export const DEFAULT_API_TIMEOUT_MS = [1-9][\d_]*\s*;/,
    "default timeout constant must be a finite positive number",
  );
});

test("long-running endpoints explicitly opt into a larger timeout", async () => {
  const defaultChannels = await fs.readFile(
    new URL("../default/src/features/channels/api.ts", import.meta.url),
    "utf8",
  );
  for (const endpoint of [
    "/api/channel/test/",
    "/api/channel/update_balance/",
    "/api/channel/fetch_models/",
  ]) {
    const index = defaultChannels.indexOf(`\`${endpoint}`);
    assert.notEqual(index, -1, `${endpoint} call not found`);
    const call = defaultChannels.slice(index, index + 400);
    assert.match(call, /channelActionConfig\([^)]*timeout/, `${endpoint} must set a timeout`);
  }

  const classicChannels = await fs.readFile(
    new URL("../classic/src/hooks/channels/useChannelsData.jsx", import.meta.url),
    "utf8",
  );
  const testAll = classicChannels.indexOf("API.get(`/api/channel/test`");
  assert.notEqual(testAll, -1, "classic test-all-channels call not found");
  assert.match(
    classicChannels.slice(testAll, testAll + 250),
    /timeout:\s*[1-9]/,
    "classic test-all-channels must set a timeout",
  );
});
