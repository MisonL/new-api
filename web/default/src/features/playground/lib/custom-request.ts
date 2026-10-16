export type CustomRequestValidation = {
  valid: boolean
  message: string
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

export function validateCustomRequestBody(
  value: string
): CustomRequestValidation {
  if (!value.trim()) return { valid: true, message: '' }

  try {
    const parsed: unknown = JSON.parse(value)
    return isRecord(parsed)
      ? { valid: true, message: '' }
      : { valid: false, message: 'JSON must be an object' }
  } catch {
    return { valid: false, message: 'JSON format error' }
  }
}

export function formatCustomRequestBody(value: string): string {
  const parsed: unknown = JSON.parse(value)
  if (!isRecord(parsed)) throw new Error('JSON must be an object')
  return JSON.stringify(parsed, null, 2)
}

/**
 * Parse the custom request body for sending.
 *
 * Returns null when the text is empty or is not a JSON object. The caller must
 * refuse the request in that case: the settings panel promises the body is sent
 * as-is, so falling back to the generated payload would send something the user
 * did not ask for while showing no error.
 */
export function parseCustomRequestBody(
  value: string
): Record<string, unknown> | null {
  if (!value.trim()) return null
  try {
    const parsed: unknown = JSON.parse(value)
    return isRecord(parsed) ? parsed : null
  } catch {
    return null
  }
}
