import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

// 只读的"翻译完成度"门禁：查出仍然原样等于基准语言的条目。
//
// 既有的门禁只保证"键存在"（i18nCoverageGate / i18nDefaultCoverageGate），
// 不保证"值被翻译过"。classic 的 i18nLintGate 查的是源码里的硬编码字符串。
// 结果是 fr/ru/ja/vi 长期停留在一两百条英文文案上而无人发现
// （2026-11-08 实测补齐前 classic fr 180 / ru 179 / ja 216 / vi 211，
// web/default fr 91 / vi 91；第二轮又发现短标签仍有 120 条级别缺口）。
//
// 判定口径：值等于基准值、去掉 {{占位符}} 后仍含拉丁字母，且不是
// URL、邮箱、JSON/数组片段、snake_case 或单 token 标识符，即算未翻译。
// 上一版只查"3 个词以上且含小写开头的句子"，漏掉了两词以内的标签
// （All groups、Gift Codes）和全 Title Case 的标签
// （Header Profile Applied、Change Deployment Name）。
//
// 品牌名、协议字段名、时区标识等确实应保留英文的文案登记在
// web/tests/i18nUntranslatedAllowlist.json：
//   - "shared"：两个前端所有语言共用；
//   - "前端:语言"：只对特定语言豁免（例如法语中与英文同形的词）。
// 清单按基准值匹配，因为译文缺口的判据是"值"而不是键。
//
// 边界：单 token 值不计入。Create、Start 这类单词标签与 gpt-4o、bps、
// GET、true 等品牌/型号/枚举无法静态区分，要靠一份极大的豁免清单才
// 能安全覆盖，本轮未做。
//
// 本脚本不写入任何文件。

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const webRoot = path.resolve(scriptDir, "..");
const allowlistPath = path.join(
  webRoot,
  "tests",
  "i18nUntranslatedAllowlist.json",
);

const FRONTS = [
  {
    name: "classic",
    localesDir: path.join(webRoot, "classic", "src", "i18n", "locales"),
    baseLocale: "en",
  },
  {
    name: "default",
    localesDir: path.join(webRoot, "default", "src", "i18n", "locales"),
    baseLocale: "en",
  },
];

const PLACEHOLDER = /\{\{[^}]*\}\}/g;
// Han ideographs and kana mark a value as already localized for CJK locales;
// such a value is never an "untranslated English sentence" even if it embeds
// English words like "API Key".
const NON_LATIN_SCRIPT = /[぀-ヿ㐀-䶿一-鿿豈-﫿]/;

function stripPlaceholders(value) {
  return value.replace(PLACEHOLDER, " ");
}

function looksUntranslated(value) {
  if (typeof value !== "string") return false;
  const raw = value.trim();
  if (raw.length === 0) return false;
  if (NON_LATIN_SCRIPT.test(raw)) return false;
  // URLs, JSON / array snippets, quoted payloads, emails.
  if (/^[{[":]/.test(raw)) return false;
  if (raw.includes("://") || /\bhttps?\./i.test(raw)) return false;
  if (/[\w.+-]+@[\w-]+\.[\w.]+/.test(raw)) return false;
  // Example/domain placeholders such as "example.com&#10;blocked-site.com".
  if (/example\.(com|org)|&#10;/.test(raw)) return false;
  // Identifiers and single tokens (bps, GET, gpt-4o, /v1, 100%).
  if (/[a-z]+_[a-z]+/.test(raw)) return false;
  if (/^[\w./-]+$/.test(raw)) return false;

  // Anything left that carries Latin letters is user-visible prose or a
  // label: "Download Logs", "Gift Codes", "Header Profile Applied",
  // "Type *", "Key: {{value}}".
  const words =
    stripPlaceholders(raw).match(/[A-Za-z][A-Za-z'-]*/g) ?? [];
  return words.length > 0;
}

function readTranslation(file) {
  const data = JSON.parse(fs.readFileSync(file, "utf8"));
  return data.translation ?? data;
}

const allowlist = fs.existsSync(allowlistPath)
  ? JSON.parse(fs.readFileSync(allowlistPath, "utf8"))
  : {};

const failures = [];

// A translated value must carry exactly the placeholders the base value has.
// Missing or renamed ones break interpolation at runtime; the __ PH_n __ form
// is an artifact left behind by a translation pipeline (found in default/ja).
const PLACEHOLDER_ALL = /\{\{[^}]*\}\}/g;
const PLACEHOLDER_ARTIFACT = /__\s*PH_\d+\s*__/;

for (const front of FRONTS) {
  const base = readTranslation(
    path.join(front.localesDir, `${front.baseLocale}.json`),
  );
  const localeFiles = fs
    .readdirSync(front.localesDir)
    .filter(
      (name) =>
        name.endsWith(".json") && !name.startsWith(`${front.baseLocale}.`),
    )
    .sort();

  for (const file of localeFiles) {
    const locale = file.replace(/\.json$/, "");
    const translations = readTranslation(path.join(front.localesDir, file));
    const allowed = new Set([
      ...(allowlist.shared ?? []),
      ...(allowlist[`${front.name}:${locale}`] ??
        allowlist[`${front.name}:*`] ??
        []),
    ]);
    const untranslated = [];
    const placeholderIssues = [];
    for (const [key, baseValue] of Object.entries(base)) {
      const value = translations[key];
      if (typeof baseValue !== "string" || typeof value !== "string") continue;

      const expected = baseValue.match(PLACEHOLDER_ALL) ?? [];
      const actual = value.match(PLACEHOLDER_ALL) ?? [];
      // Placeholder order is irrelevant for interpolation; compare sorted sets
      // but report the base value's original order for readability.
      const sortKey = (list) => [...list].sort().join("\u0000");
      if (
        sortKey(expected) !== sortKey(actual) ||
        PLACEHOLDER_ARTIFACT.test(value)
      ) {
        placeholderIssues.push(
          `${JSON.stringify(key)}: ${JSON.stringify(value)} (期望 ${expected.join(" ") || "无占位符"})`,
        );
        continue;
      }

      if (value !== baseValue) continue;
      if (!looksUntranslated(baseValue)) continue;
      if (allowed.has(baseValue)) continue;
      untranslated.push(`${JSON.stringify(key)} -> ${JSON.stringify(baseValue)}`);
    }
    if (untranslated.length > 0) {
      failures.push(
        `${front.name}/${locale}: ${untranslated.length} 条与 ${front.baseLocale} 相同`,
      );
      for (const line of untranslated.slice(0, 12)) {
        failures.push(`    ${line}`);
      }
      if (untranslated.length > 12) {
        failures.push(`    ... 另有 ${untranslated.length - 12} 条`);
      }
    }
    if (placeholderIssues.length > 0) {
      failures.push(
        `${front.name}/${locale}: ${placeholderIssues.length} 条占位符与基准不一致`,
      );
      for (const line of placeholderIssues.slice(0, 12)) {
        failures.push(`    ${line}`);
      }
      if (placeholderIssues.length > 12) {
        failures.push(`    ... 另有 ${placeholderIssues.length - 12} 条`);
      }
    }
  }
}

if (failures.length > 0) {
  console.error("翻译完成度检查失败：");
  for (const line of failures) console.error(`- ${line}`);
  console.error(
    `请补齐译文；确实需要保留英文的条目登记到 ${path.relative(webRoot, allowlistPath)}。`,
  );
  process.exit(1);
}

const allowlistCount = Object.values(allowlist).reduce(
  (sum, keys) => sum + keys.length,
  0,
);
console.log(
  `翻译完成度检查通过：${FRONTS.length} 个前端的所有语言均无未翻译的多词文案（允许清单 ${allowlistCount} 条，单 token 值除外）。`,
);
process.exit(0);
