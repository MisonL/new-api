import { describe, expect, test } from 'bun:test'

import { linkifyHtml } from '../src/components/ai-elements/code-block'

// A URL in code content becomes a link. The transform runs on Shiki's escaped
// output, so this takes that shape as input.
describe('linkifyHtml', () => {
  test('turns a bare URL into an anchor', () => {
    const out = linkifyHtml('<span>see https://example.com/page here</span>')
    expect(out).toContain('<a href="https://example.com/page"')
    expect(out).toContain('>https://example.com/page</a>')
  })

  // A query string must survive intact. `&` is legal unescaped inside an
  // attribute value, and treating it as a terminator truncated every URL with
  // parameters into a dead link.
  test('keeps a full query string', () => {
    const out = linkifyHtml('<span>https://example.com/p?a=1&b=2#frag</span>')
    expect(out).toContain('href="https://example.com/p?a=1&b=2#frag"')
    expect(out).toContain('>https://example.com/p?a=1&b=2#frag</a>')
  })

  test('leaves markup untouched', () => {
    const html = '<pre class="shiki"><span>const</span></pre>'
    expect(linkifyHtml(html)).toBe(html)
  })

  test('does not linkify text without a URL', () => {
    const html = '<span>no link here</span>'
    expect(linkifyHtml(html)).toBe(html)
  })

  // The escaped source can never contain a raw `<`, but the attribute must still
  // not be breakable by anything a match could carry. A quote ends the match
  // before it can close the attribute, so the payload stays inert text in the
  // surrounding span rather than becoming an attribute.
  test('cannot be broken out of the href attribute', () => {
    for (const payload of [
      'https://evil.com"onmouseover="alert(1)',
      "https://evil.com'onmouseover='alert(1)",
      'https://evil.com/><img src=x onerror=alert(1)>',
      'https://evil.com`onmouseover=alert(1)',
    ]) {
      const out = linkifyHtml(`<span>${payload}</span>`)
      const href = out.match(/href="([^"]*)"/)
      expect(href).not.toBeNull()
      // The captured href must contain no character that could close the
      // attribute or open a new tag.
      expect(href![1]).not.toMatch(/["'<>`]/)
      // The payload must not have been spliced into the tag: every `<a` in the
      // output has exactly the four attributes the transform writes.
      for (const tag of out.match(/<a\b[^>]*>/g) ?? []) {
        expect(tag).toMatch(
          /^<a href="[^"<>`]*" target="_blank" rel="noreferrer" class="underline underline-offset-2">$/
        )
      }
    }
  })
})
