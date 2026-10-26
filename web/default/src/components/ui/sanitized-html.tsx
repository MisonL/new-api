import { useMemo, type ReactNode } from 'react'
import { Fragment, jsx, jsxs } from 'react/jsx-runtime'
import { toJsxRuntime } from 'hast-util-to-jsx-runtime'
import { sanitizeHtmlTree } from '@/lib/sanitize-html'

interface SanitizedHtmlProps {
  html: string
  className?: string
}

/**
 * Render an admin-configured HTML string after stripping it to the shared
 * allowlist (lib/sanitize-schema.ts).
 *
 * The tree is converted to React elements instead of being handed back to
 * `dangerouslySetInnerHTML`, so scripts, event handlers, `javascript:` URLs
 * and iframes cannot reach the DOM at all.
 */
export function SanitizedHtml(props: SanitizedHtmlProps): ReactNode {
  const tree = useMemo(
    () => sanitizeHtmlTree(props.html),
    [props.html]
  )
  return (
    <div className={props.className}>
      {toJsxRuntime(tree, { Fragment, jsx, jsxs })}
    </div>
  )
}
