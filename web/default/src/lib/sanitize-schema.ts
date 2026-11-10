import { defaultSchema } from 'rehype-sanitize'

/**
 * Allowlist shared by the Markdown renderer and the raw-HTML renderer.
 *
 * Both paths render admin-configured rich text (home page, announcements,
 * about, legal documents, footer). GitHub's default schema is the baseline;
 * the additions cover what those fields legitimately need:
 * - `style` tags and inline `style` attributes for admin theming,
 * - `class`/`className` so admin CSS selectors keep working (hast stores the
 *   HTML `class` attribute as `className`),
 * - `img` sizing attributes plus `data:` image sources for inline logos.
 *
 * Everything else — scripts, event handlers, `javascript:` URLs, iframes —
 * stays stripped.
 */
export const sanitizeSchema = {
  ...defaultSchema,
  // The default schema rewrites element ids to `user-content-<id>` to defeat
  // DOM clobbering. Admin stylesheets in <style> blocks address elements by
  // their original id, and the stylesheet text is not rewritten, so every
  // selector stops matching and the admin theme silently disappears. Keep ids
  // intact; `name` stays clobbered, which is the attribute that can shadow
  // document properties.
  clobber: (defaultSchema.clobber ?? []).filter(
    (attribute) => attribute !== 'id'
  ),
  tagNames: [...(defaultSchema.tagNames ?? []), 'style'],
  attributes: {
    ...defaultSchema.attributes,
    '*': [
      ...(defaultSchema.attributes?.['*'] ?? []),
      'style',
      'class',
      'className',
    ],
    img: [...(defaultSchema.attributes?.img ?? []), 'src', 'srcSet', 'sizes'],
  },
  protocols: {
    ...defaultSchema.protocols,
    href: ['http', 'https', 'mailto', 'tel'],
    src: ['http', 'https', 'data'],
  },
}
