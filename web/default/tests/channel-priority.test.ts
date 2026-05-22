import { describe, expect, test } from 'bun:test'
import { getNextTopChannelPriority } from '../src/features/channels/lib/channel-priority'

describe('channel priority helpers', () => {
  test('raises the channel above the highest visible priority', () => {
    const nextPriority = getNextTopChannelPriority(
      { priority: 3 },
      [{ priority: 1 }, { priority: 8 }, { priority: 3 }]
    )

    expect(nextPriority).toBe(9)
  })

  test('raises an already top channel so the manual action is visible', () => {
    const nextPriority = getNextTopChannelPriority(
      { priority: 8 },
      [{ priority: 1 }, { priority: 8 }, { priority: 3 }]
    )

    expect(nextPriority).toBe(9)
  })

  test('normalizes empty priorities to zero before choosing the next value', () => {
    const nextPriority = getNextTopChannelPriority(
      { priority: null },
      [{ priority: null }, { priority: undefined }, { priority: -2 }]
    )

    expect(nextPriority).toBe(1)
  })

  test('keeps negative priority ranges ordered when every visible priority is negative', () => {
    const nextPriority = getNextTopChannelPriority(
      { priority: -8 },
      [{ priority: -10 }, { priority: -4 }, { priority: -8 }]
    )

    expect(nextPriority).toBe(-3)
  })
})
