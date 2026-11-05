import assert from "node:assert/strict";
import fs from "node:fs/promises";
import test from "node:test";

// zh-CN 是 classic 的 fallbackLng。en 基线里存在的 key 若在中文语言包中缺失，
// i18next 会把 key 本身当作文案显示（returnEmptyString: false），中文界面
// 直接露出英文键。这里以 en 为基线，要求两个中文语言包全覆盖。
// 复数变体按 i18next 规则豁免：中文只有 _other，不要求 _one。

const localesDir = new URL(
  "../classic/src/i18n/locales/",
  import.meta.url,
);

async function loadTranslation(name) {
  const raw = await fs.readFile(new URL(`${name}.json`, localesDir), "utf8");
  return JSON.parse(raw).translation;
}

function missingKeys(baseline, candidate) {
  return Object.keys(baseline).filter((key) => {
    if (key in candidate) return false;
    if (key.endsWith("_one") && `${key.slice(0, -4)}_other` in candidate) {
      return false;
    }
    return true;
  });
}

test("zh-CN and zh-TW cover every en translation key", async () => {
  const en = await loadTranslation("en");
  for (const locale of ["zh-CN", "zh-TW"]) {
    const translation = await loadTranslation(locale);
    const missing = missingKeys(en, translation);
    assert.deepEqual(missing, [], `${locale} 缺少 ${missing.length} 个 key: ${missing.join(" | ")}`);
  }
});
