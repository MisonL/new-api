/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumous.com
*/

// DocumentRenderer 会把管理员 HTML 里的 <style> 原样注入 document.head，
// 影响整个站点而不只是文档区。DOMPurify 不解析 CSS，<style> 内容等于未经
// 清洗；这里剥掉能逃出样式上下文或执行代码的构造：闭合标签逃逸、@import
// 外部样式表、IE expression()/behavior、以及 javascript:/vbscript: 协议。

const STYLE_BREAKOUT = /<\/?style[^>]*>/gi;
const SCRIPT_BLOCKS = /<script[\s\S]*?<\/script>/gi;
const ANGLE_BRACKET_MARKUP = /<\/?[a-z][^>]*>/gi;
const DANGEROUS_RULES = [
  /@import[^;]*;?/gi,
  /expression\s*\([^)]*\)/gi,
  /\bbehavior\s*:[^;}]*;?/gi,
  /(?:-moz-binding)\s*:[^;}]*;?/gi,
  /(?:javascript|vbscript)\s*:/gi,
];

export function sanitizeDocumentCss(raw, maxLength = 65536) {
  if (typeof raw !== "string" || raw.trim() === "") {
    return "";
  }
  let css = raw
    .replace(SCRIPT_BLOCKS, "")
    .replace(STYLE_BREAKOUT, "")
    .replace(ANGLE_BRACKET_MARKUP, "");
  for (const pattern of DANGEROUS_RULES) {
    css = css.replace(pattern, "");
  }
  if (css.length > maxLength) {
    css = css.slice(0, maxLength);
  }
  return css;
}

export default sanitizeDocumentCss;
