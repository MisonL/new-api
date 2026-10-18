#!/usr/bin/env node
/**
 * 校验前端产物里实际打包的依赖版本与 node_modules 解析结果一致。
 *
 * 背景（CR-MULTI-ROUND-AUDIT-2026-10-16 §11.1）：`bun audit` 读的是 lockfile，
 * 而构建产物可能打包了另一份副本——overrides 只影响依赖解析，不会删除已存在
 * 的不满足新范围的嵌套 node_modules。实测 classic 的 katex 就曾带着
 * node_modules/mermaid/node_modules/katex@0.16.47 打进 dist，而 audit 显示
 * 0.18.11 已修。只看 audit 会给出错误的"已修复"结论。
 *
 * 判据：每个被检查的包配一个只存在于该包源码里的哨兵字符串。
 *   - 产物里找不到哨兵 => 该包没有被打包，跳过；
 *   - 找到哨兵 => 每一个含哨兵的文件里都必须出现 node_modules 解析出的版本号。
 * 打包了旧副本时，该文件不带期望版本号，检查失败。逐文件而不是"任一文件
 * 匹配"，是因为产物里可能同时存在多份独立副本：classic 除了 Rollup 打包的
 * markdown-core，还有一份手工 copy 的 vendor/mermaid 预构建 chunk，后者自带
 * 内联 katex，绕过打包器与 override。
 *
 * 用法：node scripts/frontend-bundle-versions.mjs [--root DIR]
 * 退出码：0 全部一致；1 有产物版本与解析版本不符；2 用法或环境错误。
 */

import { readFileSync, readdirSync, existsSync } from "node:fs";
import { join, relative, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const DEFAULT_ROOT = resolve(HERE, "..");

// 每个条目：dist 目录、node_modules 位置、包名、只属于该包源码的哨兵。
const CHECKS = [
  {
    label: "web/classic",
    distDir: "web/classic/dist",
    modulesDir: "web/classic/node_modules",
    pkg: "katex",
    // katex 渲染时给 MathML 容器的类名，只出现在 katex 自身源码里。
    sentinel: "katex-mathml",
  },
  {
    label: "web/default",
    distDir: "web/default/dist",
    modulesDir: "web/default/node_modules",
    pkg: "katex",
    sentinel: "katex-mathml",
  },
];

const TEXT_EXTENSIONS = new Set([".js", ".mjs", ".cjs"]);

function fail(message) {
  console.error(`frontend bundle version check failed: ${message}`);
  process.exit(1);
}

function walk(dir, out = []) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      walk(full, out);
    } else if (
      TEXT_EXTENSIONS.has(entry.name.slice(entry.name.lastIndexOf(".")))
    ) {
      out.push(full);
    }
  }
  return out;
}

function installedVersion(modulesDir, pkg) {
  const manifest = join(modulesDir, pkg, "package.json");
  if (!existsSync(manifest)) {
    return null;
  }
  return JSON.parse(readFileSync(manifest, "utf8")).version ?? null;
}

function main() {
  const args = process.argv.slice(2);
  let root = DEFAULT_ROOT;
  let only = null;
  let distDirOverride = null;
  let modulesDirOverride = null;
  for (let i = 0; i < args.length; i += 1) {
    if (args[i] === "--root") {
      const value = args[i + 1];
      if (!value) {
        console.error("--root 需要目录参数");
        return 2;
      }
      root = resolve(value);
      i += 1;
    } else if (args[i] === "--only") {
      // Dockerfile 里两个前端在各自独立的构建阶段，看不到彼此的 dist，
      // 只能按阶段分别校验。
      const value = args[i + 1];
      if (!value) {
        console.error("--only 需要前端名（web/classic 或 web/default）");
        return 2;
      }
      only = value;
      i += 1;
    } else if (args[i] === "--dist-dir") {
      // Docker 构建阶段的工作目录布局与仓库不同（dist 在 /build/dist），
      // 允许显式覆盖，避免为容器另写一套检查。
      const value = args[i + 1];
      if (!value) {
        console.error("--dist-dir 需要目录参数");
        return 2;
      }
      distDirOverride = value;
      i += 1;
    } else if (args[i] === "--modules-dir") {
      const value = args[i + 1];
      if (!value) {
        console.error("--modules-dir 需要目录参数");
        return 2;
      }
      modulesDirOverride = value;
      i += 1;
    } else if (args[i] === "--help" || args[i] === "-h") {
      console.log(
        "usage: node scripts/frontend-bundle-versions.mjs [--root DIR] [--only FRONTEND] [--dist-dir DIR] [--modules-dir DIR]",
      );
      return 0;
    } else {
      console.error(`unknown argument: ${args[i]}`);
      return 2;
    }
  }

  const selected = only ? CHECKS.filter((c) => c.label === only) : CHECKS;
  if (selected.length === 0) {
    console.error(`--only 的值不在已知前端里: ${only}`);
    return 2;
  }
  if ((distDirOverride || modulesDirOverride) && !only) {
    console.error("--dist-dir / --modules-dir 必须与 --only 一起使用");
    return 2;
  }

  for (const check of selected) {
    if (distDirOverride) check.distDir = distDirOverride;
    if (modulesDirOverride) check.modulesDir = modulesDirOverride;
    const distDir = join(root, check.distDir);
    if (!existsSync(distDir)) {
      fail(`${check.label}: dist 不存在（先构建）: ${check.distDir}`);
    }

    const files = walk(distDir);
    // 构建成功的产物一定含 JS。"有 dist 目录但没有任何 JS"意味着构建静默失败，
    // 此时下面的哨兵扫描会得出"未打包"并放行，是个 fail-open 缺口。
    if (files.length === 0) {
      fail(
        `${check.label}: ${check.distDir} 里没有任何 js/mjs/cjs 文件（构建可能失败）`,
      );
    }
    const withSentinel = files.filter((file) =>
      readFileSync(file, "utf8").includes(check.sentinel),
    );
    if (withSentinel.length === 0) {
      console.log(`${check.label}: ${check.pkg} 未打包进产物，跳过`);
      continue;
    }

    // 只有确认该包被打包时才需要 node_modules：未打包的产物不关心其安装版本。
    const version = installedVersion(join(root, check.modulesDir), check.pkg);
    if (!version) {
      fail(
        `${check.label}: 产物打包了 ${check.pkg}，但找不到其安装版本（先安装依赖）`,
      );
    }

    const versionLiteral = `"${version}"`;
    // 每个含哨兵的文件都要带解析版本，不是"至少一个"。产物里可能存在多份
    // 独立副本：Rollup 打包的走 node_modules 解析，而手工 copy 的 vendor 目录
    // （如 classic 的 mermaid 预构建 chunk）绕过打包器、自带内联副本。
    // "任一文件匹配即通过"会放过后者——实测 classic 的 vendor katex 0.16.45
    // 就是这样漏掉的。
    const stale = withSentinel.filter(
      (file) => !readFileSync(file, "utf8").includes(versionLiteral),
    );
    if (stale.length > 0) {
      fail(
        `${check.label}: 产物里有 ${stale.length} 个文件打包的 ${check.pkg} 不是 ${versionLiteral}：` +
          `${stale.map((f) => relative(root, f)).join(", ")}；` +
          `可能是嵌套 node_modules 里的旧副本（执行 bun install --force 后重建），` +
          `也可能是绕过打包器的手工 vendor 产物（需升级提供方依赖）`,
      );
    }
    console.log(
      `${check.label}: ${check.pkg} ${version} 已确认打包（${withSentinel.length} 个文件）`,
    );
  }
  return 0;
}

process.exit(main());
