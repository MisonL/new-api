import { describe, expect, test } from 'bun:test'
import {
  getNextTopChannelPinOrder,
  getTopChannelPinOrderMovePlan,
  isPinnedChannel,
} from '../src/features/channels/lib/channel-pin-order'
import {
  getNextTopChannelPriority,
  getTopChannelPriorityMovePlan,
} from '../src/features/channels/lib/channel-priority'

describe('channel pin order helpers', () => {
  test('assigns the next pin order without consulting route priority', () => {
    expect(
      getNextTopChannelPinOrder(
        { pin_order: 10 },
        [{ pin_order: 30 }, { pin_order: 20 }]
      )
    ).toBe(1030)
  })

  test('treats only positive pin orders as pinned', () => {
    expect(isPinnedChannel({ pin_order: 1 })).toBe(true)
    expect(isPinnedChannel({ pin_order: 0 })).toBe(false)
    expect(isPinnedChannel({ pin_order: null })).toBe(false)
  })

  test('reindexes pinned channels using pin_order fields', () => {
    const plan = getTopChannelPinOrderMovePlan(
      { id: 2, pin_order: 20 },
      [
        { id: 1, pin_order: 30 },
        { id: 2, pin_order: 20 },
        { id: 3, pin_order: 10 },
      ],
      'up'
    )

    expect(plan.updates).toEqual([
      { id: 2, pin_order: 3000 },
      { id: 1, pin_order: 2000 },
      { id: 3, pin_order: 1000 },
    ])
    expect(plan.rollbackUpdates).toEqual([
      { id: 2, pin_order: 20 },
      { id: 1, pin_order: 30 },
      { id: 3, pin_order: 10 },
    ])
  })

  test('keeps the legacy helper exports as pin-order compatibility aliases', () => {
    expect(
      getNextTopChannelPriority(
        { priority: 10 },
        [{ priority: 30 }]
      )
    ).toBe(1030)

    expect(
      getTopChannelPriorityMovePlan(
        { id: 2, priority: 20 },
        [
          { id: 1, priority: 30 },
          { id: 2, priority: 20 },
        ],
        'up'
      ).updates
    ).toEqual([
      { id: 2, priority: 2000 },
      { id: 1, priority: 1000 },
    ])
  })
})
