import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");

function readRepoFile(path) {
  return readFileSync(resolve(repoRoot, path), "utf8");
}

const nginxTemplate = readRepoFile(
  "deploy/nginx/frontend-readonly.conf.template"
);

const guardSources = [
  "web/default/src/lib/readonly-guard.ts",
  "web/classic/src/helpers/readonlyGuard.js",
];

// 每个被前端 guard 阻断的 GET 端点配一个具体请求路径。前端 guard 的
// READONLY_BLOCKED_PATHS 是正则，代理层是同义的 nginx location 正则；用具体
// 路径比对，才能发现"正则看起来像但其实没覆盖"的漂移。
const SIDE_EFFECT_PATHS = [
  "/api/oauth/github",
  "/api/auth/external/oidc",
  "/api/user/logout",
  "/api/verification",
  "/api/reset_password",
  "/api/channel/test",
  "/api/channel/test/42",
  "/api/channel/update_balance",
  "/api/channel/update_balance/42",
  "/api/channel/fetch_models/42",
  "/api/channel/ollama/version/42",
  "/api/channel/42/codex/usage",
  "/api/user/token",
  "/api/user/aff",
  "/api/user/epay/notify",
  "/api/subscription/epay/notify",
  "/api/subscription/epay/return",
];

// 与上面共享前缀或相邻命名空间的普通读接口。代理层正则写宽一点就会误伤
// 这些路径，把只读联调变成大面积 403。
const HARMLESS_PATHS = [
  "/api/status",
  "/api/user/self",
  "/api/user/groups",
  "/api/user/topup/self",
  "/api/channel/",
  "/api/channel/models",
  "/api/channel/42",
  "/api/channel/42/codex/usage/history",
  "/api/channel/test-models",
  "/api/subscription/plans",
  "/api/verification/resend",
  "/api/user/tokens",
];

function extractNginxLocationRegexes(template) {
  const patterns = [];
  const re = /location\s+~\s+([^\s{]+)\s*\{/g;
  let match;
  while ((match = re.exec(template)) !== null) {
    patterns.push(match[1]);
  }
  return patterns;
}

function extractGuardPatterns(source) {
  const start = source.indexOf("READONLY_BLOCKED_PATHS = [");
  assert.notEqual(start, -1, "guard must export READONLY_BLOCKED_PATHS");
  const end = source.search(/\n\];?/, start);
  assert.notEqual(end, -1, "guard blocked-path array must be terminated");
  const block = source.slice(start, end);

  const patterns = [];
  for (const rawLine of block.split("\n")) {
    const line = rawLine.trim().replace(/,$/, "");
    if (!line.startsWith("/") || line.startsWith("//")) {
      continue;
    }
    const lastSlash = line.lastIndexOf("/");
    if (lastSlash <= 0) {
      continue;
    }
    patterns.push(line.slice(1, lastSlash));
  }
  return patterns;
}

const nginxRegexes = extractNginxLocationRegexes(nginxTemplate);
const guardRegexes = guardSources.map((path) => ({
  path,
  patterns: extractGuardPatterns(readRepoFile(path)),
}));

function matchesAny(patterns, path) {
  return patterns.some((pattern) => new RegExp(pattern).test(path));
}

test("nginx template declares at least one location regex", () => {
  assert.ok(
    nginxRegexes.length > 0,
    "the read-only proxy must block side-effecting GET endpoints with a location regex"
  );
});

test("both frontend guards expose the same blocked-path list", () => {
  const [first, second] = guardRegexes;
  assert.ok(first.patterns.length > 0);
  assert.deepEqual(
    first.patterns,
    second.patterns,
    `${first.path} and ${second.path} must stay in step`
  );
});

test("nginx proxy blocks every GET endpoint the frontend guards block", () => {
  for (const { path, patterns } of guardRegexes) {
    for (const requestPath of SIDE_EFFECT_PATHS) {
      assert.ok(
        matchesAny(patterns, requestPath),
        `${path} must classify ${requestPath} as blocked; add it to the fixture list if the guard list grew`
      );
      assert.ok(
        matchesAny(nginxRegexes, requestPath),
        `nginx read-only proxy must block ${requestPath}, which ${path} blocks`
      );
    }
  }
});

test("nginx proxy also blocks the trailing-slash spelling the backend redirects", () => {
  for (const requestPath of SIDE_EFFECT_PATHS) {
    assert.ok(
      matchesAny(nginxRegexes, `${requestPath}/`),
      `nginx read-only proxy must block ${requestPath}/ because the backend 301s it to the same handler`
    );
  }
});

test("nginx proxy leaves ordinary reads reachable", () => {
  for (const requestPath of HARMLESS_PATHS) {
    assert.ok(
      !matchesAny(nginxRegexes, requestPath),
      `nginx read-only proxy must not block ${requestPath}`
    );
    assert.ok(
      !matchesAny(nginxRegexes, `${requestPath}/`),
      `nginx read-only proxy must not block ${requestPath}/`
    );
  }
});

test("read-only proxy rejects unsafe methods before proxying", () => {
  assert.match(
    nginxTemplate,
    /if \(\$request_method !~ \^\(GET\|HEAD\|OPTIONS\)\$\) \{\s*return 405;\s*\}/,
    "unsafe methods must be rejected with 405"
  );
});
