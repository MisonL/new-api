export function getPaginationTargetPage(
  value: string,
  totalPages: number
): number | null {
  if (totalPages <= 0) return null
  const token = value.trim()
  if (!/^\d+$/.test(token)) return null
  const parsed = Number.parseInt(token, 10)
  if (!Number.isSafeInteger(parsed) || parsed < 1) return null
  return Math.min(parsed, totalPages)
}
