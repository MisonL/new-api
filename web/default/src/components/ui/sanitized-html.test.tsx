import { createElement } from 'react'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { renderToStaticMarkup } from 'react-dom/server'
import { SanitizedHtml } from './sanitized-html'

function render(html: string): string {
  return renderToStaticMarkup(createElement(SanitizedHtml, { html }))
}

// The about page, legal documents and footer_html all render through this
// component. The tests below assert on the rendered markup, not on the
// sanitizer internals, so a call site that goes back to
// dangerouslySetInnerHTML is caught by the source guard at the bottom.

test('renders allowed markup and keeps admin classes', () => {
  const out = render('<p class="custom-footer">hello</p>')
  assert.match(out, /<p class="custom-footer">hello<\/p>/)
})

test('never renders a script element', () => {
  const out = render('<p>before</p><script>alert(1)</script><p>after</p>')
  assert.ok(!out.includes('script'), out)
  assert.match(out, /<p>before<\/p><p>after<\/p>/)
})

test('never renders an event handler attribute', () => {
  const out = render('<img src="https://x/y.png" onerror="alert(1)">')
  assert.ok(!out.includes('onerror'), out)
})

test('never renders a javascript: url', () => {
  const out = render('<a href="javascript:alert(1)">click</a>')
  assert.ok(!out.includes('javascript:'), out)
  assert.match(out, /<a>click<\/a>/)
})

test('keeps pretty-printed HTML as written', () => {
  const html =
    '<div class="terms">\n\n    <h2>Section</h2>\n\n    <p>Body.</p>\n\n</div>'
  assert.equal(render(html), `<div>${html}</div>`)
})

test('no admin-content call site injects raw html', () => {
  const callSites = [
    '../../features/about/index.tsx',
    '../../features/legal/legal-document.tsx',
    '../layout/components/footer.tsx',
  ]
  for (const relative of callSites) {
    const source = readFileSync(new URL(relative, import.meta.url), 'utf8')
    assert.ok(
      !source.includes('dangerouslySetInnerHTML'),
      `${relative} bypasses the sanitizer`
    )
  }
})
