import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// Semi Design 的 Modal width 是像素数字，窄屏上会直接溢出视口。逐个弹窗加
// style={{ maxWidth: '90vw' }} 容易漏（仓库现有十几处固定宽度），改为一条全局
// 规则覆盖所有 Semi 弹窗：宽度仍按各弹窗的桌面语义指定，移动端用 calc 封顶。
// 与 web/default 的 dialog 组件 max-w-[calc(100%-2rem)] 同一口径。

const css = readFileSync(
  new URL("../classic/src/index.css", import.meta.url),
  "utf8",
);

test("global Semi modal rule caps width at the viewport", () => {
  const match = css.match(/\.semi-modal\s*\{[^}]*\}/);
  assert.ok(match, "index.css 必须存在全局 .semi-modal 规则");
  assert.match(
    match[0],
    /max-width:\s*calc\(100vw\s*-\s*32px\)/,
    "弹窗宽度必须封顶到视口内（左右各 16px 间距），实际：\n" + match[0],
  );
});
