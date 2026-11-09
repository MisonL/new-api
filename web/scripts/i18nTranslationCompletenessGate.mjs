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

// Latin-script locales (fr, vi) legitimately use Latin letters, so a value is
// judged by sentence shape below instead of the script alone.

function stripPlaceholders(value) {
  return value.replace(PLACEHOLDER, " ");
}

function looksUntranslated(value) {
  if (typeof value !== "string") return false;
  const raw = value.trim();
  if (raw.length === 0) return false;
  if (NON_LATIN_SCRIPT.test(raw)) return false;
  // URLs, emails and example domains are configuration, not prose.
  if (raw.includes("://") || /\bhttps?\./i.test(raw)) return false;
  if (/[\w.+-]+@[\w-]+\.[\w.]+/.test(raw)) return false;
  if (/example\.(com|org)|&#10;/.test(raw)) return false;

  // Strip placeholders before classifying the remainder. A sentence that
  // merely starts with "{{count}}" is prose, not a JSON snippet; checking the
  // raw prefix used to let "{{count}} logs deleted" through as JSON-like.
  const text = stripPlaceholders(raw).trim();
  if (text.length === 0) return false;
  // A single dotted/dashed identifier (gpt-3.5-turbo, checkout.session.completed,
  // AIGC2D) or a "a / b" pair of such identifiers is a value, not prose.
  const segments = text.split(/[\s/]+/).filter(Boolean);
  if (segments.length > 0 && segments.every((s) => /^[A-Za-z0-9][A-Za-z0-9._+-]*$/.test(s))) {
    if (segments.length === 1) return false;
    // "a / b"-style pairs: no word of ordinary sentence length in prose order.
    if (segments.every((s) => !/^[A-Z][a-z]{2,}$/.test(s))) return false;
  }

  // Strip inline identifiers and code-ish fragments (snake_case names,
  // paths, key=value hints) and judge what is left: "Remove
  // image_generation tool objects rejected by an upstream." is prose with
  // an identifier in it, not an identifier.
  const prose = text
    .replace(/[A-Za-z0-9_.-]*_[A-Za-z0-9_.-]+/g, " ")
    .replace(/https?:\/\/\S+/g, " ")
    .replace(/\/[A-Za-z0-9._-]+(?:\/[A-Za-z0-9._-]+)*/g, " ")
    .replace(/\b[\w.+-]+@[\w-]+\.[\w.]+\b/g, " ")
    .trim();

  // Only a bare JSON/array payload or a quoted fragment is configuration.
  if (/^[\[{]/.test(prose) && !/^[\[{][^\]}]*[A-Za-z][A-Za-z'-]*\s[A-Za-z]{2,}/.test(prose)) {
    return false;
  }
  if (/^"/.test(prose) && !/\s[A-Za-z]{3,}/.test(prose)) return false;
  const words = prose.match(/[A-Za-z][A-Za-z'-]*/g) ?? [];
  // A lone word is a unit, status or brand ("s", "min", "bps", "GET");
  // only multi-word remains count as an untranslated label or sentence.
  return words.length > 1;
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

// Values that are rewritten but whose body is still an English sentence:
// "Only configured combinations are overridden. Unconfigured combinations
// still use the token group's base ratio." These pass the equality check
// because the text differs from the base, yet a fr/ru/vi user still reads
// English. The signal is English function words together with a high overlap
// Measured 2026-11-09 on the whole catalog: catches every known case while
// ignoring real French/Russian/Vietnamese prose and path-only examples.
const ENGLISH_FUNCTION_WORDS =
  /\b(the|and|is|are|will|when|with|this|that|for|from|not|you|your|have|has|can|should|if|of|to|in|on|by|it|all|any|be|was|were|but|or|as|at|which|their|they|more|only|used|use|using|configure|configured|selected|available|default|new|set|see|same|without|check|append|add|delete|confirm|enabled|enabling|creating|created)\b/gi;
const ENGLISH_SENTENCE_MIN_WORDS = 6;
const ENGLISH_FUNCTION_WORD_MIN_HITS = 2;
const ENGLISH_FUNCTION_WORD_RATIO = 0.25;
const ENGLISH_BASE_WORD_OVERLAP = 0.55;
// Any letter outside basic Latin means the value really is in another
// language (Cyrillic, kana, Han, accented French/Vietnamese). Technical
// identifiers embedded in Cyrillic prose are not evidence of English.
const NON_LATIN_LETTER = /(?![a-zA-Z])\p{L}/u;

function looksLikeEnglishSentenceBody(value, baseValue) {
  if (NON_LATIN_LETTER.test(value)) return false;
  const plain = stripPlaceholders(value);
  const words = plain.match(/[A-Za-z][A-Za-z'-]*/g) ?? [];
  if (words.length < ENGLISH_SENTENCE_MIN_WORDS) return false;
  const hits = (plain.match(ENGLISH_FUNCTION_WORDS) ?? []).length;
  if (
    hits < ENGLISH_FUNCTION_WORD_MIN_HITS ||
    hits / words.length < ENGLISH_FUNCTION_WORD_RATIO
  ) {
    return false;
  }
  const baseWords = new Set(
    (stripPlaceholders(baseValue).match(/[A-Za-z][A-Za-z'-]*/g) ?? []).map((w) =>
      w.toLowerCase(),
    ),
  );
  const overlap = words.filter((w) => baseWords.has(w.toLowerCase())).length;
  return overlap / words.length >= ENGLISH_BASE_WORD_OVERLAP;
}

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

      if (value === baseValue) {
        if (!looksUntranslated(baseValue)) continue;
        if (allowed.has(baseValue)) continue;
        untranslated.push(`${JSON.stringify(key)} -> ${JSON.stringify(baseValue)}`);
        continue;
      }
      // Rewritten but still English prose: the equality check cannot see it.
      if (!allowed.has(baseValue) && looksLikeEnglishSentenceBody(value, baseValue)) {
        untranslated.push(`${JSON.stringify(key)} -> ${JSON.stringify(value)}`);
      }
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
