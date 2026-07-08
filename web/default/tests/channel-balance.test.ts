import { describe, expect, test } from 'bun:test'
import {
  formatBalance,
  isUnlimitedChannelBalance,
  UNLIMITED_CHANNEL_BALANCE_THRESHOLD,
} from '../src/features/channels/lib/channel-utils'

describe('channel balance display', () => {
  test('detects upstream unlimited balance placeholders', () => {
    expect(isUnlimitedChannelBalance(999999857.89)).toBe(true)
    expect(isUnlimitedChannelBalance(UNLIMITED_CHANNEL_BALANCE_THRESHOLD)).toBe(
      true
    )
    expect(
      isUnlimitedChannelBalance(UNLIMITED_CHANNEL_BALANCE_THRESHOLD - 1)
    ).toBe(false)
  })

  test('honors explicit unlimited balance metadata', () => {
    expect(isUnlimitedChannelBalance(0, true)).toBe(true)
    expect(formatBalance(0, { unlimited: true })).toBe('Unlimited')
    expect(formatBalance(undefined, { unlimited: true })).toBe('Unlimited')
  })

  test('formats unlimited balances without exposing placeholder digits', () => {
    expect(
      formatBalance(999999857.89, {
        unlimitedLabel: 'Unlimited',
      })
    ).toBe('Unlimited')
  })
})
