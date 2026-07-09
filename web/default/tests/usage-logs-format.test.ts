import { describe, expect, test } from 'bun:test'
import {
  getFirstResponseTimeColor,
  formatDurationFromMs,
  formatRatioDisplay,
  getResponseTimeColor,
  getTimeColor,
  getStreamStatusDisplayInfo,
} from '../src/features/usage-logs/lib/format'
import { formatUseTime } from '../src/lib/format'

describe('usage logs duration formatting', () => {
  test('formats use time while rejecting invalid values', () => {
    expect(formatUseTime(1.25)).toBe('1.3s')
    expect(formatUseTime(65)).toBe('1m 5s')
    expect(formatUseTime(-1)).toBe('-')
    expect(formatUseTime(Number.NaN)).toBe('-')
    expect(formatUseTime(Number.POSITIVE_INFINITY)).toBe('-')
  })

  test('keeps sub-second durations in millisecond precision', () => {
    expect(formatDurationFromMs(50)).toBe('0.05s')
    expect(formatDurationFromMs(120)).toBe('0.12s')
    expect(formatDurationFromMs(900)).toBe('0.9s')
    expect(formatDurationFromMs(1234)).toBe('1.234s')
  })

  test('rejects invalid and negative durations', () => {
    expect(formatDurationFromMs(Number.NaN)).toBe('-')
    expect(formatDurationFromMs(Number.POSITIVE_INFINITY)).toBe('-')
    expect(formatDurationFromMs(Number.NEGATIVE_INFINITY)).toBe('-')
    expect(formatDurationFromMs(-1)).toBe('-')
  })

  test('keeps zero duration visible', () => {
    expect(formatDurationFromMs(0)).toBe('0s')
  })

  test('trims whole-second durations without dropping the number', () => {
    expect(formatDurationFromMs(1000)).toBe('1s')
    expect(formatDurationFromMs(60000)).toBe('60s')
  })

  test('treats negative timing values as invalid', () => {
    expect(getTimeColor(-1)).toBe('danger')
    expect(getFirstResponseTimeColor(-1)).toBe('danger')
    expect(getResponseTimeColor(-5, 200)).toBe('danger')
  })

  test('preserves tiny but non-zero ratios', () => {
    expect(formatRatioDisplay(0.00004)).toBe('4e-5')
    expect(formatRatioDisplay(-0.00004)).toBe('-4e-5')
    expect(formatRatioDisplay(0.5)).toBe('0.5')
    expect(formatRatioDisplay(0)).toBe('0')
    expect(formatRatioDisplay(2)).toBe('2')
  })
})

describe('usage logs stream status formatting', () => {
  const t = (key: string) => key

  test('maps known status values consistently', () => {
    expect(getStreamStatusDisplayInfo({ status: 'ok' }, t)).toMatchObject({
      label: 'OK',
      variant: 'success',
      valueClassName: 'text-emerald-700 font-semibold dark:text-emerald-300',
      iconClassName: 'text-emerald-500',
    })
    expect(getStreamStatusDisplayInfo({ status: 'partial' }, t)).toMatchObject({
      label: 'Partial',
      variant: 'warning',
      valueClassName: 'text-amber-700 font-semibold dark:text-amber-300',
      iconClassName: 'text-amber-500',
    })
    expect(
      getStreamStatusDisplayInfo({ status: 'canceled' }, t)
    ).toMatchObject({
      label: 'Canceled',
      variant: 'disabled',
      valueClassName: 'text-muted-foreground font-semibold',
      iconClassName: 'text-muted-foreground',
    })
  })

  test('keeps unknown statuses visible', () => {
    expect(getStreamStatusDisplayInfo({ status: 'timeout' }, t)).toMatchObject({
      label: 'timeout',
      variant: 'red',
      iconClassName: 'text-red-500',
    })
    expect(getStreamStatusDisplayInfo({ end_reason: 'client_gone' }, t)).toMatchObject(
      {
        label: 'Canceled',
        variant: 'disabled',
      }
    )
  })
})
