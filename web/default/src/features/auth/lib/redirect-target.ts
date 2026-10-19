/**
 * Normalises a post-login redirect target taken from a URL query parameter.
 *
 * Login flows accept `?redirect=<path>` so a deep link can survive the trip
 * through an identity provider. The value is attacker-controllable: it is just
 * a query parameter, and the callback page hands it to
 * `window.location.replace()`. Without validation that is an open redirect —
 * `?redirect=https://evil.example` turns a successful sign-in into a landing on
 * a lookalike page.
 *
 * Only a site-relative path is accepted. The value is resolved against a fixed
 * local base and then re-checked against the same origin, so percent-encoded
 * and `..` forms are caught after the URL parser has normalised them, and the
 * result is rebuilt from the parsed parts rather than passed through.
 *
 * Returns an empty string when the target must not be used; callers fall back
 * to their default destination.
 */
const REDIRECT_BASE_URL = 'https://new-api.local'

// Sign-in and sign-up must not be redirect targets: landing back on them right
// after a successful login either loops or shows a form the user no longer
// needs.
const REDIRECT_LOOP_PATHS = new Set(['/sign-in', '/sign-up'])

export function normalizeLoginRedirectTarget(target: unknown): string {
  if (typeof target !== 'string') {
    return ''
  }
  const trimmed = target.trim()
  // Must be a site-relative path. `//host` and `/\host` are protocol-relative
  // URLs that browsers treat as absolute, so both are rejected here rather
  // than left to the URL parser.
  if (!trimmed.startsWith('/') || trimmed.startsWith('//')) {
    return ''
  }
  if (trimmed.startsWith('/\\')) {
    return ''
  }

  let url: URL
  try {
    url = new URL(trimmed, REDIRECT_BASE_URL)
  } catch {
    return ''
  }
  if (url.origin !== REDIRECT_BASE_URL) {
    return ''
  }
  if (REDIRECT_LOOP_PATHS.has(url.pathname)) {
    return ''
  }
  return `${url.pathname}${url.search}${url.hash}`
}
