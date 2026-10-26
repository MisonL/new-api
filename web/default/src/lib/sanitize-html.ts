import { fromHtml } from 'hast-util-from-html'
import { sanitize } from 'hast-util-sanitize'
import { toHtml } from 'hast-util-to-html'
import type { Root } from 'hast'
import { sanitizeSchema } from './sanitize-schema'

/**
 * Parse an admin-configured HTML fragment and strip everything outside the
 * shared allowlist.
 *
 * The about page, legal documents and footer_html are parsed as HTML (not as
 * Markdown): Markdown would re-flow pretty-printed markup — blank lines and
 * four-space indents inside a block element become code blocks. Parsing with
 * parse5 and serializing with hast-util-to-html keeps the tree the admin
 * wrote; the only difference is that disallowed tags and attributes are gone.
 */
export function sanitizeHtmlTree(html: string): Root {
  // sanitize() is typed to return Nodes, but with a fragment root input it
  // returns that same root.
  return sanitize(fromHtml(html, { fragment: true }), sanitizeSchema) as Root
}

export function sanitizeHtml(html: string): string {
  return toHtml(sanitizeHtmlTree(html))
}
