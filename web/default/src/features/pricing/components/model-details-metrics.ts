export type LatencyTimePoint = {
  timestamp: string
  group: string
  ttft_ms: number
}

export type AvailabilityPoint = {
  date: string
  success_rate: number
  incidents: number
}

export function formatLatency(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return '-'
  if (ms < 1000) return `${Math.round(ms)} ms`
  return `${(ms / 1000).toFixed(2)} s`
}

export function formatPercent(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '-'
  return `${value.toFixed(2)}%`
}
