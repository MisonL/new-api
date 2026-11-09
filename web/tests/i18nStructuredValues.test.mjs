import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

// 语言文件里有三类文本必须逐字保留、不能参与翻译：
//   1. JSON / 数组样例（渠道 region 配置、模型映射的 placeholder 等），
//      被翻译流水线改写过（插入空格、翻转键值）后用户复制到表单里会直接
//      配置失败。实测 default/ja 两条样例被改写成
//      "{\" original - model \":\" replacement - model \"}"。
//   2. 译文必须保留原文中的技术标识符：型号、版本号、区域名（V2、V4、
//      GPT-4、gpt-4o、us-central1）。翻译流水线会给这些 token 插入空格
//      （Baidu V2 -> Baidu V 2、GPT-4 -> GPT -4），值因此不等于基准，
//      完成度门禁不会报；这里按 token 逐个核对。
//   3. 豁免清单里的条目如果已经不在任何 en.json 中，说明它对应的键被
//      删掉或改名了，留着只会掩盖新出现的同类问题。

const webRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);
const FRONTS = [
  ["classic", path.join(webRoot, "classic/src/i18n/locales")],
  ["default", path.join(webRoot, "default/src/i18n/locales")],
];
const allowlistPath = path.join(
  webRoot,
  "tests/i18nUntranslatedAllowlist.json",
);

function loadTranslation(dir, locale) {
  const data = JSON.parse(
    fs.readFileSync(path.join(dir, `${locale}.json`), "utf8"),
  );
  return data.translation ?? data;
}

function localeFiles(dir) {
  return fs
    .readdirSync(dir)
    .filter((name) => name.endsWith(".json") && name !== "en.json")
    .sort();
}

// A value that is a JSON snippet, an array snippet, or a quoted "key": "value"
// pair is configuration the user copies verbatim.
function isStructuredExample(value) {
  const raw = value.trim();
  if (!/^[[{"]/.test(raw)) return false;
  try {
    JSON.parse(raw);
    return true;
  } catch {
    // Quoted key/value fragments such as "\"default\": \"us-central1\"".
    return /^"[^"]*"\s*:/.test(raw);
  }
}

function normalizeJson(value) {
  const raw = value.trim();
  try {
    // A translated display string inside a JSON example (Alipay -> 支付宝)
    // keeps the example valid and importable, so compare structure instead of
    // leaf values: same keys, same array lengths, same value types.
    const shape = (node) => {
      if (Array.isArray(node)) return node.map(shape);
      if (node && typeof node === "object") {
        const out = {};
        for (const key of Object.keys(node).sort()) out[key] = shape(node[key]);
        return out;
      }
      return typeof node;
    };
    return JSON.stringify(shape(JSON.parse(raw)));
  } catch {
    // Quoted key/value fragments such as "\"default\": \"us-central1\"".
    return raw.replace(/\s+/g, "");
  }
}

test("structured i18n values keep the same JSON shape as the base locale", () => {
  const mismatches = [];
  for (const [front, dir] of FRONTS) {
    const base = loadTranslation(dir, "en");
    for (const file of localeFiles(dir)) {
      const locale = file.replace(/\.json$/, "");
      const target = loadTranslation(dir, locale);
      for (const [key, baseValue] of Object.entries(base)) {
        if (typeof baseValue !== "string") continue;
        const value = target[key];
        if (typeof value !== "string") continue;
        if (!isStructuredExample(baseValue)) continue;
        if (value === baseValue) continue;
        if (normalizeJson(value) === normalizeJson(baseValue)) continue;
        mismatches.push(
          `${front}/${locale} ${JSON.stringify(key)} -> ${JSON.stringify(value)}`,
        );
      }
    }
  }
  assert.deepEqual(
    mismatches,
    [],
    `结构化样例被改写：\n${mismatches.join("\n")}`,
  );
});

// Version / model / region identifiers: at least one hyphen-joined segment
// (GPT-4, gpt-4o, us-central1, claude-3-5-sonnet) or a letter-digit unit
// (V2, V4, SDK2). "non-2xx" and similar prose idioms are excluded.
const TECHNICAL_TOKEN =
  /\b[A-Za-z][A-Za-z0-9]*(?:[-_.][A-Za-z0-9]+)+\b|\b[A-Za-z]+[0-9][A-Za-z0-9]*\b/g;

function technicalTokens(value) {
  // URLs and paths contain identifiers the translator may rewrite as part of
  // localization (e.g. dropping "/v1" from a full example URL is intentional),
  // so only bare prose tokens are checked.
  const prose = value.replace(/https?:\/\/\S+/g, " ").replace(/\/\S+/g, " ");
  const found = prose.match(TECHNICAL_TOKEN) ?? [];
  return [...new Set(found)].filter(
    (token) =>
      /[0-9]/.test(token) && !/^non-/i.test(token) && !/[xX]{2}$/.test(token),
  );
}

test("translations keep every technical identifier from the base locale", () => {
  const mismatches = [];
  for (const [front, dir] of FRONTS) {
    const base = loadTranslation(dir, "en");
    for (const file of localeFiles(dir)) {
      const locale = file.replace(/\.json$/, "");
      const target = loadTranslation(dir, locale);
      for (const [key, baseValue] of Object.entries(base)) {
        if (typeof baseValue !== "string") continue;
        const value = target[key];
        if (typeof value !== "string" || value === baseValue) continue;
        for (const token of technicalTokens(baseValue)) {
          if (value.toLowerCase().includes(token.toLowerCase())) continue;
          mismatches.push(
            `${front}/${locale} 缺少标识符 ${JSON.stringify(token)}：${JSON.stringify(value)}`,
          );
        }
      }
    }
  }
  assert.deepEqual(
    mismatches,
    [],
    `技术标识符被改写：\n${mismatches.join("\n")}`,
  );
});

test("allowlist entries still match a base locale value", () => {
  const allowlist = JSON.parse(fs.readFileSync(allowlistPath, "utf8"));
  const baseValues = new Set();
  for (const [, dir] of FRONTS) {
    for (const value of Object.values(loadTranslation(dir, "en"))) {
      if (typeof value === "string") baseValues.add(value);
    }
  }
  const stale = [];
  for (const [group, entries] of Object.entries(allowlist)) {
    for (const entry of entries) {
      if (!baseValues.has(entry))
        stale.push(`${group}: ${JSON.stringify(entry)}`);
    }
  }
  assert.deepEqual(
    stale,
    [],
    `豁免清单中没有对应基准值的条目：\n${stale.join("\n")}`,
  );
});
