import { expect, test } from 'bun:test'
import { parseDashboardTokenStats } from '../src/features/dashboard/lib/token-stats'

test('preserves a measured zero and an unavailable cache rate', () => {
  const data = { input_tokens: 100, cache_read_tokens: 0, cache_write_tokens: 20, output_tokens: 30, cache_rate: 0 }
  expect(parseDashboardTokenStats({ success: true, data })).toEqual(data)
  expect(parseDashboardTokenStats({ success: true, data: { ...data, cache_rate: null } }).cache_rate).toBeNull()
})

test('rejects failed or incomplete statistics instead of fabricating zero usage', () => {
  expect(() => parseDashboardTokenStats({ success: false })).toThrow()
  expect(() => parseDashboardTokenStats({ success: true, data: {} })).toThrow()
  expect(() => parseDashboardTokenStats({ success: true, data: { input_tokens: -1 } })).toThrow()
})
