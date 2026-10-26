import assert from 'node:assert/strict'
import test from 'node:test'
import { sanitizeHtml } from './sanitize-html'

// The three admin-configurable rich-text fields (about, legal documents,
// footer_html) were rendered with dangerouslySetInnerHTML, i.e. raw, while the
// Markdown component next to them already ran rehype-sanitize. These tests pin
// the sanitizer that both paths now share.

test('drops script elements but keeps their siblings', () => {
  assert.equal(
    sanitizeHtml('<p>before</p><script>alert(1)</script><p>after</p>'),
    '<p>before</p><p>after</p>'
  )
})

test('drops event handler attributes', () => {
  assert.equal(
    sanitizeHtml('<img src="https://x/y.png" onerror="alert(1)" alt="logo">'),
    '<img src="https://x/y.png" alt="logo">'
  )
})

test('drops javascript: urls while keeping the anchor text', () => {
  assert.equal(sanitizeHtml('<a href="javascript:alert(1)">click</a>'), '<a>click</a>')
})

test('drops iframes', () => {
  assert.equal(sanitizeHtml('<iframe src="https://evil.example"></iframe>'), '')
})

test('keeps the markup the admin is expected to write', () => {
  const html =
    '<h2>1. Acceptance</h2>\n' +
    '<p>Contact <a href="mailto:legal@example.com">legal@example.com</a>.</p>\n' +
    '<ul><li>one</li></ul>\n' +
    '<table><thead><tr><th>A</th></tr></thead><tbody><tr><td>b</td></tr></tbody></table>\n' +
    '<p style="color:#666">gray</p>\n' +
    '<img src="https://x/logo.png" width="120" alt="logo">'
  const out = sanitizeHtml(html)
  assert.match(out, /<h2>1\. Acceptance<\/h2>/)
  assert.match(out, /<a href="mailto:legal@example\.com">legal@example\.com<\/a>/)
  assert.match(out, /<table><thead><tr><th>A<\/th><\/tr><\/thead><tbody><tr><td>b<\/td><\/tr><\/tbody><\/table>/)
  assert.match(out, /style="color:#666"/)
  assert.match(out, /<img src="https:\/\/x\/logo\.png" width="120" alt="logo">/)
})

test('preserves indentation and blank lines instead of re-flowing them', () => {
  // Indented HTML inside a block element is a Markdown code block, so this
  // content cannot be routed through the Markdown renderer.
  const html = '<div class="terms">\n\n    <h2>Section</h2>\n\n    <p>Body.</p>\n\n</div>'
  assert.equal(sanitizeHtml(html), html)
})

test('keeps class attributes so admin CSS selectors still apply', () => {
  // hast stores the HTML `class` attribute under `className`; an allowlist
  // entry named `class` silently does nothing.
  assert.equal(
    sanitizeHtml('<p class="custom-footer">x</p>'),
    '<p class="custom-footer">x</p>'
  )
})

test('repairs malformed markup rather than dropping it', () => {
  assert.equal(sanitizeHtml('<p>unclosed <b>bold'), '<p>unclosed <b>bold</b></p>')
})
