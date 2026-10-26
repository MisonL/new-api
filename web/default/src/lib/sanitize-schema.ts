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
