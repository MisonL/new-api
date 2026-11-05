import assert from "node:assert/strict";
import test from "node:test";

import { sanitizeDocumentCss } from "../classic/src/helpers/documentCss.js";

// DocumentRenderer 把管理员 HTML 里的 <style> 注入 document.head，等于让
// 管理员内容改写整站样式。DOMPurify 不解析 CSS，所以这段内容必须单独清洗：
// 闭合逃逸、外部样式表导入、IE 表达式与脚本协议都要剥掉。

test("keeps ordinary admin theme css intact", () => {
  const css = ".doc-title{color:#123;margin:0} @media (max-width:600px){.doc-title{font-size:14px}}";
  assert.equal(sanitizeDocumentCss(css), css);
});

test("strips style breakout tags", () => {
  const css = ".a{color:red}</style><script>alert(1)</script>";
  const sanitized = sanitizeDocumentCss(css);
  assert.ok(!/<\/?style/i.test(sanitized), sanitized);
  assert.ok(!/<script/i.test(sanitized), sanitized);
});

test("strips remote stylesheet imports and script protocols", () => {
  const sanitized = sanitizeDocumentCss(
    "@import url(https://evil.example/x.css); .a{background:url(javascript:alert(1))} .b{width:expression(alert(1))}",
  );
  assert.ok(!/@import/i.test(sanitized), sanitized);
  assert.ok(!/javascript\s*:/i.test(sanitized), sanitized);
  assert.ok(!/expression\s*\(/i.test(sanitized), sanitized);
});

test("caps pathological sizes", () => {
  const sanitized = sanitizeDocumentCss("a{color:red}".repeat(100), 100);
  assert.equal(sanitized.length, 100);
});

test("returns empty string for non-string or blank input", () => {
  assert.equal(sanitizeDocumentCss(null), "");
  assert.equal(sanitizeDocumentCss(undefined), "");
  assert.equal(sanitizeDocumentCss("   "), "");
});

// 静态契约：DocumentRenderer 必须在注入 document.head 之前调用清洗函数。
// 只测纯函数不够——调用点被删掉时纯函数测试仍然全绿。
test("DocumentRenderer sanitizes styles before injecting them into head", async () => {
  const fs = await import("node:fs/promises");
  const source = await fs.readFile(
    new URL(
      "../classic/src/components/common/DocumentRenderer/index.jsx",
      import.meta.url,
    ),
    "utf8",
  );
  assert.match(source, /sanitizeDocumentCss\s*\(/);
  assert.match(source, /import \{ sanitizeDocumentCss \}/);
});
