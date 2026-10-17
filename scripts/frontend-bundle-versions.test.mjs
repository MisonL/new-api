// 门禁脚本自身的回归测试：夹具覆盖通过、版本不符、依赖缺失、dist 缺失和用法错误。
// 脚本的判据是"产物里打包的版本 == node_modules 解析出的版本"，因此每组夹具都必须
// 同时铺设 dist 与 node_modules，缺一不可——否则测的是夹具而不是判据。

import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const script = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "frontend-bundle-versions.mjs",
);

function makeFixture({
  classicVersion = "0.18.11",
  bundleVersion = "0.18.11",
  omit = [],
} = {}) {
  const root = mkdtempSync(join(tmpdir(), "bundle-versions-"));
  mkdirSync(join(root, "web/classic/dist"), { recursive: true });
  mkdirSync(join(root, "web/default/dist"), { recursive: true });

  if (!omit.includes("classic-modules")) {
    mkdirSync(join(root, "web/classic/node_modules/katex"), {
      recursive: true,
    });
    writeFileSync(
      join(root, "web/classic/node_modules/katex/package.json"),
      JSON.stringify({ name: "katex", version: classicVersion }),
    );
  }
  writeFileSync(
    join(root, "web/classic/dist/chunk-a.js"),
    `var x="katex-mathml";var v="${bundleVersion}";\n`,
  );
  // 每个 dist 都必须有 JS：门禁把"有目录但无 JS"视为构建失败，不再跳过。
  writeFileSync(join(root, "web/default/dist/index.js"), "var ok=1;\n");
  return root;
}

function run(root) {
  try {
    const stdout = execFileSync(process.execPath, [script, "--root", root], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    });
    return { code: 0, stdout, stderr: "" };
  } catch (error) {
    return {
      code: error.status,
      stdout: error.stdout ?? "",
      stderr: error.stderr ?? "",
    };
  }
}

test("passes when the bundled version matches the resolved version", () => {
  const root = makeFixture();
  try {
    const result = run(root);
    assert.equal(result.code, 0, result.stderr);
    assert.match(result.stdout, /web\/classic: katex 0\.18\.11 已确认打包/);
    assert.match(result.stdout, /web\/default: katex 未打包进产物，跳过/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("fails when the bundle contains a stale nested copy", () => {
  const root = makeFixture({ bundleVersion: "0.16.47" });
  try {
    const result = run(root);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /不是 "0\.18\.11"/);
    assert.match(result.stderr, /嵌套 node_modules 里的旧副本/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("fails when one of several copies is stale", () => {
  // 复现 §11.1 的真实形态：Rollup 打包的那份是 0.18.11，而手工 copy 的
  // vendor 目录里还有一份 0.16.45。"任一文件匹配即通过"会放过它。
  const root = makeFixture();
  try {
    mkdirSync(join(root, "web/classic/dist/vendor/mermaid"), {
      recursive: true,
    });
    writeFileSync(
      join(root, "web/classic/dist/vendor/mermaid/katex.mjs"),
      'var x="katex-mathml";var v="0.16.45";\n',
    );
    const result = run(root);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /vendor\/mermaid\/katex\.mjs/);
    assert.match(result.stderr, /手工 vendor 产物/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("fails when the package is bundled but its install version is unknown", () => {
  const root = makeFixture({ omit: ["classic-modules"] });
  try {
    const result = run(root);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /找不到其安装版本/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("fails when a dist directory is missing", () => {
  const root = makeFixture();
  try {
    rmSync(join(root, "web/default/dist"), { recursive: true, force: true });
    const result = run(root);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /dist 不存在（先构建）: web\/default\/dist/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("does not require node_modules when the package is not bundled", () => {
  const root = makeFixture({ omit: ["classic-modules"] });
  try {
    writeFileSync(join(root, "web/classic/dist/chunk-a.js"), 'var v="1";\n');
    const result = run(root);
    assert.equal(result.code, 0, result.stderr);
    assert.match(result.stdout, /web\/classic: katex 未打包进产物，跳过/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("treats a sentinel that only appears in CSS as not bundled", () => {
  // 哨兵扫描限定在 js/mjs/cjs；CSS 里的类名不应把包判定为"已打包"。
  const root = makeFixture({ omit: ["classic-modules"] });
  try {
    writeFileSync(join(root, "web/classic/dist/chunk-a.js"), 'var v="1";\n');
    writeFileSync(
      join(root, "web/classic/dist/style.css"),
      ".katex-mathml{display:none}\n",
    );
    const result = run(root);
    assert.equal(result.code, 0, result.stderr);
    assert.match(result.stdout, /web\/classic: katex 未打包进产物，跳过/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("checks only the requested frontend with --only", () => {
  // Dockerfile 里两个前端各自独立构建，看不到彼此的 dist。
  const root = makeFixture();
  try {
    rmSync(join(root, "web/default/dist"), { recursive: true, force: true });
    const result = execFileSync(
      process.execPath,
      [script, "--root", root, "--only", "web/classic"],
      { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] },
    );
    assert.match(result, /web\/classic: katex 0\.18\.11 已确认打包/);
    assert.doesNotMatch(result, /web\/default/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("accepts Docker-style dist and modules overrides", () => {
  const root = makeFixture();
  try {
    mkdirSync(join(root, "docker-build/node_modules/katex"), {
      recursive: true,
    });
    writeFileSync(
      join(root, "docker-build/node_modules/katex/package.json"),
      JSON.stringify({ name: "katex", version: "0.18.11" }),
    );
    mkdirSync(join(root, "docker-build/dist"), { recursive: true });
    writeFileSync(
      join(root, "docker-build/dist/chunk.js"),
      'var x="katex-mathml";var v="0.18.11";\n',
    );
    const result = execFileSync(
      process.execPath,
      [
        script,
        "--root",
        root,
        "--only",
        "web/classic",
        "--dist-dir",
        "docker-build/dist",
        "--modules-dir",
        "docker-build/node_modules",
      ],
      { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] },
    );
    assert.match(result, /web\/classic: katex 0\.18\.11 已确认打包/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("rejects a dist that has no JavaScript at all", () => {
  // 构建静默失败只留下 index.html 时，哨兵扫描会得出"未打包"并放行；
  // 该缺口必须 fail-closed。
  const root = makeFixture();
  try {
    rmSync(join(root, "web/default/dist/index.js"));
    writeFileSync(
      join(root, "web/default/dist/index.html"),
      "<html>build failed silently</html>\n",
    );
    const result = run(root);
    assert.equal(result.code, 1);
    assert.match(result.stderr, /web\/default: .*没有任何 js\/mjs\/cjs 文件/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("rejects an unknown argument with exit code 2", () => {
  try {
    execFileSync(process.execPath, [script, "--bogus"], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    });
    assert.fail("expected the script to exit non-zero");
  } catch (error) {
    assert.equal(error.status, 2);
    assert.match(error.stderr, /unknown argument: --bogus/);
  }
});
