import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

// 只读的 web/default i18n 覆盖度门禁。
//
// web/default 没有提取器（package.json 只有 i18next / react-i18next），
// scripts/sync-i18n.mjs 只做语言文件之间的对齐，从不扫描源码/watch。于是
// "代码里 t('...') 用到的键是否存在于语言文件"从来无人检查，2026-11-07 实测
// 已有 3 个中文键在 6 个语言文件里全部缺失（web/default/src/features/auth/
// api.ts 的微信/Telegram 登录与绑定提示），由 3cb202431 引入。
//
// 本门禁做两件事，均只读：
//   1. 扫 src 下所有 t('...') / i18next.t('...') 字面量键，要求每个语言文件的
//      translation 命名空间里都有它（默认 en 是 fallback，缺 en 也等于缺所有）。
//   2. 校验 src/i18n/static-keys.ts 的 STATIC_I18N_KEYS（运行时动态拼出的键）
//      全部登记在 en.json 里，避免"登记了但没人用得上"的假机制。
//
// 运行期动态拼接的键（模板串、变量）无法静态判断，脚本只认字面量并显式说明。

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const webRoot = path.resolve(scriptDir, "..");
const defaultRoot = path.join(webRoot, "default");
const srcDir = path.join(defaultRoot, "src");
const localesDir = path.join(srcDir, "i18n", "locales");
const staticKeysFile = path.join(srcDir, "i18n", "static-keys.ts");

// 单/双引号字符串，允许内部转义引号（如 'Please use WeChat\'s "Scan QR Code"'）。
const KEY_CALL_PATTERN =
  /(?:i18next\.t|\bt)\(\s*(?:'((?:[^'\\\n]|\\.)*)'|"((?:[^"\\\n]|\\.)*)")/g;

function listSourceFiles(dir) {
  const out = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      out.push(...listSourceFiles(full));
    } else if (
      /\.[jt]sx?$/.test(entry.name) &&
      !/\.(test|spec)\.[jt]sx?$/.test(entry.name)
    ) {
      out.push(full);
    }
  }
  return out;
}

function readTranslation(file) {
  const data = JSON.parse(fs.readFileSync(file, "utf8"));
  return data.translation ?? data;
}

const localeFiles = fs
  .readdirSync(localesDir)
  .filter((name) => name.endsWith(".json"));
if (localeFiles.length === 0) {
  console.error(`未在 ${localesDir} 找到语言文件，覆盖度检查无法执行。`);
  process.exit(1);
}

// Collect literal keys and where they were used.
const usages = new Map();
for (const file of listSourceFiles(srcDir)) {
  const text = fs.readFileSync(file, "utf8");
  const rel = path.relative(defaultRoot, file);
  for (let line of text.split("\n")) {
    // Comments often quote t('key') as documentation; those are not call sites.
    const trimmed = line.trimStart();
    if (trimmed.startsWith("//") || trimmed.startsWith("*") || trimmed.startsWith("/*")) {
      continue;
    }
  for (const match of line.matchAll(KEY_CALL_PATTERN)) {
    const raw = match[1] ?? match[2];
    // Undo the source-level escapes so the key matches the JSON entry.
    const key = raw.replace(/\\(['"\\])/g, "$1");
    if (key.length === 0 || key.length > 300) continue;
    if (!usages.has(key)) usages.set(key, new Set());
    usages.get(key).add(rel);
  }
  }
}

const failures = [];

// 1. Every literal key must exist in every locale file.
for (const locale of localeFiles) {
  const translations = readTranslation(path.join(localesDir, locale));
  const missing = [...usages.keys()].filter((key) => !(key in translations));
  if (missing.length > 0) {
    failures.push(`${locale} 缺少 ${missing.length} 个代码中使用的键`);
    for (const key of missing.slice(0, 20)) {
      failures.push(`    ${JSON.stringify(key)} @ ${[...usages.get(key)][0]}`);
    }
    if (missing.length > 20) failures.push(`    ... 另有 ${missing.length - 20} 个`);
  }
}

// 2. Every registered static key must be in en.json.
const staticText = fs.readFileSync(staticKeysFile, "utf8");
const staticBody = staticText.slice(staticText.indexOf("["), staticText.lastIndexOf("]") + 1);
const staticKeys = [...staticBody.matchAll(/^\s*(['"])(.+?)\1,?\s*$/gm)].map((m) => m[2]);
const enTranslations = readTranslation(path.join(localesDir, "en.json"));
const staticMissing = staticKeys.filter((key) => !(key in enTranslations));
if (staticMissing.length > 0) {
  failures.push(`static-keys.ts 有 ${staticMissing.length} 个键不在 en.json`);
  for (const key of staticMissing.slice(0, 20)) failures.push(`    ${JSON.stringify(key)}`);
}

if (failures.length > 0) {
  console.error("web/default i18n 覆盖度检查失败：");
  for (const line of failures) console.error(`- ${line}`);
  console.error(
    "请在对应语言文件的 translation 命名空间补齐这些键。运行时以 en 为 fallback，缺键会原样显示键字符串。",
  );
  process.exit(1);
}

const dynamicNote =
  "（模板串/变量拼接的键无法静态识别，未计入）";
console.log(
  `web/default i18n 覆盖度检查通过：${usages.size} 个字面量键 × ${localeFiles.length} 个语言文件，` +
    `${staticKeys.length} 个静态键均已登记${dynamicNote}。`,
);
process.exit(0);