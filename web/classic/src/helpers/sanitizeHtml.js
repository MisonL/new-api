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

For commercial licensing, please contact support@quantumnous.com
*/

import DOMPurify from 'dompurify';

// 站点上所有"管理员可配置的 HTML"（关于、首页、页脚、公告、公告板、FAQ、
// 文档页）以及由 marked 渲染出来的 Markdown，最终都会进入
// dangerouslySetInnerHTML。管理员配置本身是可信边界之外的输入：它可能来自
// 被窃取的账号、被篡改的备份、或从别处复制的示例内容；Markdown 更是原样
// 保留内联 HTML。两者都必须先过一遍白名单，脚本、事件处理器、javascript:
// URL 与 iframe 才进不了 DOM。
//
// 这里用 DOMPurify 的默认白名单：它保留常见排版标签与链接，去掉脚本、
// 事件属性、危险协议。额外的 FORBID_TAGS 只用于屏蔽默认仍允许、但在本站
// 管理内容里没有合理用途的标签。
const SANITIZE_CONFIG = {
  // <style> 用于管理员给页脚、关于页等自带主题样式，与 web/default 的
  // 白名单保持一致；DOMPurify 会清洗 style 内容，不会放过闭合逃逸。
  ADD_TAGS: ['style'],
  FORBID_TAGS: ['form', 'input', 'button', 'textarea', 'select', 'option'],
  FORBID_ATTR: ['srcdoc'],
  // 允许在新窗口打开链接；rel=noopener 由浏览器默认的 noopener 行为兜底。
  ADD_ATTR: ['target'],
};

/**
 * Sanitize an admin-configured or Markdown-derived HTML string.
 *
 * Returns an empty string for non-string input so a caller that receives a
 * null/undefined payload renders nothing instead of throwing.
 */
export function sanitizeHtml(html) {
  if (typeof html !== 'string' || html === '') {
    return '';
  }
  // 用 <template> 解析：template 的内容是惰性的（脚本不执行、图片不加载），
  // 而且 <style> 会留在内容里而不是被解析器丢进 head —— 直接传字符串给
  // DOMPurify 时，<style> 落在 head 里会被整段移除，管理员给页脚/关于页配
  // 的主题样式就失效了。清洗本身仍是 DOMPurify 的白名单。
  const template = document.createElement('template');
  template.innerHTML = html;
  return DOMPurify.sanitize(template.content, SANITIZE_CONFIG);
}

/**
 * Render Markdown to HTML and sanitize the result.
 *
 * marked preserves inline HTML from the source, so the parsed output is just
 * as untrusted as the admin HTML above.
 */
export function sanitizeMarkdown(markdown, parse) {
  if (typeof markdown !== 'string' || markdown === '') {
    return '';
  }
  return sanitizeHtml(parse(markdown));
}

export default sanitizeHtml;
