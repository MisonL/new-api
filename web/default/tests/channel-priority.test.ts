import { describe, expect, test } from 'bun:test'
import {
  getNextTopChannelPriority,
  getTopChannelPriorityMovePlan,
  getTopChannelPriorityMoveUpdates,
  isTopChannel,
} from '../src/features/channels/lib/channel-priority'

describe('channel priority helpers', () => {
  test('raises the channel above the highest visible priority', () => {
    const nextPriority = getNextTopChannelPriority(
      { priority: 3 },
      [{ priority: 1 }, { priority: 8 }, { priority: 3 }]
    )

    expect(nextPriority).toBe(1008)
  })

  test('raises an already top channel so the manual action is visible', () => {
    const nextPriority = getNextTopChannelPriority(
      { priority: 8 },
      [{ priority: 1 }, { priority: 8 }, { priority: 3 }]
    )

    expect(nextPriority).toBe(1008)
  })

  test('normalizes empty priorities to zero before choosing the next value', () => {
    const nextPriority = getNextTopChannelPriority(
      { priority: null },
      [{ priority: null }, { priority: undefined }, { priority: -2 }]
    )

    expect(nextPriority).toBe(1000)
  })

  test('pins above zero when every visible priority is negative', () => {
    const nextPriority = getNextTopChannelPriority(
      { priority: -8 },
      [{ priority: -10 }, { priority: -4 }, { priority: -8 }]
    )

    expect(nextPriority).toBe(1000)
  })

  test('detects only positive priorities as pinned channels', () => {
    expect(isTopChannel({ priority: 1 })).toBe(true)
    expect(isTopChannel({ priority: 0 })).toBe(false)
    expect(isTopChannel({ priority: -1 })).toBe(false)
    expect(isTopChannel({ priority: null })).toBe(false)
  })

  test('moves a pinned channel up by reindexing the pinned order', () => {
    const plan = getTopChannelPriorityMovePlan(
      { id: 2, priority: 20 },
      [
        { id: 1, priority: 30 },
        { id: 2, priority: 20 },
        { id: 3, priority: 10 },
      ],
      'up'
    )

    expect(plan.updates).toEqual([
      { id: 2, priority: 3000 },
      { id: 1, priority: 2000 },
      { id: 3, priority: 1000 },
    ])
    expect(plan.rollbackUpdates).toEqual([
      { id: 2, priority: 20 },
      { id: 1, priority: 30 },
      { id: 3, priority: 10 },
    ])
  })

  test('moves a pinned channel down by reindexing the pinned order', () => {
    const plan = getTopChannelPriorityMovePlan(
      { id: 2, priority: 20 },
      [
        { id: 1, priority: 30 },
        { id: 2, priority: 20 },
        { id: 3, priority: 10 },
      ],
      'down'
    )

    expect(plan.updates).toEqual([
      { id: 1, priority: 3000 },
      { id: 3, priority: 2000 },
      { id: 2, priority: 1000 },
    ])
    expect(plan.rollbackUpdates).toEqual([
      { id: 1, priority: 30 },
      { id: 3, priority: 10 },
      { id: 2, priority: 20 },
    ])
  })

  test('reindexes equal-priority neighbors so movement is stable after refresh', () => {
    const updates = getTopChannelPriorityMoveUpdates(
      { id: 2, priority: 20 },
      [
        { id: 3, priority: 20 },
        { id: 2, priority: 20 },
      ],
      'up'
    )

    expect(updates).toEqual([
      { id: 2, priority: 2000 },
      { id: 3, priority: 1000 },
    ])
  })

  test('does not update when a pinned channel has no neighbor in that direction', () => {
    const updates = getTopChannelPriorityMoveUpdates(
      { id: 1, priority: 30 },
      [
        { id: 1, priority: 30 },
        { id: 2, priority: 20 },
      ],
      'up'
    )

    expect(updates).toEqual([])
  })

  test('does not update when the channel is missing from pinned channels', () => {
    const plan = getTopChannelPriorityMovePlan(
      { id: 9, priority: 10 },
      [
        { id: 1, priority: 30 },
        { id: 2, priority: 20 },
      ],
      'up'
    )

    expect(plan).toEqual({ updates: [], rollbackUpdates: [] })
  })

  test('uses fresh listed priority when the row snapshot is stale', () => {
    const updates = getTopChannelPriorityMoveUpdates(
      { id: 2, priority: 5 },
      [
        { id: 1, priority: 30 },
        { id: 2, priority: 20 },
        { id: 3, priority: 10 },
      ],
      'up'
    )

    expect(updates).toEqual([
      { id: 2, priority: 3000 },
      { id: 1, priority: 2000 },
      { id: 3, priority: 1000 },
    ])
  })

  test('moves a pinned channel down in an equal-priority block by reindexing pinned order', () => {
    const plan = getTopChannelPriorityMovePlan(
      { id: 2, priority: 20 },
      [
        { id: 3, priority: 20 },
        { id: 2, priority: 20 },
        { id: 1, priority: 20 },
      ],
      'down'
    )

    expect(plan.updates).toEqual([
      { id: 3, priority: 3000 },
      { id: 1, priority: 2000 },
      { id: 2, priority: 1000 },
    ])
    expect(plan.rollbackUpdates).toEqual([
      { id: 3, priority: 20 },
      { id: 1, priority: 20 },
      { id: 2, priority: 20 },
    ])
  })

  test('keeps equal-priority down moves pinned by assigning positive priorities', () => {
    const plan = getTopChannelPriorityMovePlan(
      { id: 2, priority: 1 },
      [
        { id: 3, priority: 1 },
        { id: 2, priority: 1 },
        { id: 1, priority: 1 },
      ],
      'down'
    )

    expect(plan.updates).toEqual([
      { id: 3, priority: 3000 },
      { id: 1, priority: 2000 },
      { id: 2, priority: 1000 },
    ])
    expect(plan.rollbackUpdates).toEqual([
      { id: 3, priority: 1 },
      { id: 1, priority: 1 },
      { id: 2, priority: 1 },
    ])
  })

  test('moves a pinned channel down inside an equal-priority block by one listed position', () => {
    const plan = getTopChannelPriorityMovePlan(
      { id: 3, priority: 20 },
      [
        { id: 3, priority: 20 },
        { id: 2, priority: 20 },
        { id: 1, priority: 20 },
      ],
      'down'
    )

    expect(plan.updates).toEqual([
      { id: 2, priority: 3000 },
      { id: 3, priority: 2000 },
      { id: 1, priority: 1000 },
    ])
    expect(plan.rollbackUpdates).toEqual([
      { id: 2, priority: 20 },
      { id: 3, priority: 20 },
      { id: 1, priority: 20 },
    ])
  })

  test('keeps the listed order when equal-priority channels are not id-sorted', () => {
    const plan = getTopChannelPriorityMovePlan(
      { id: 2, priority: 20 },
      [
        { id: 7, priority: 20 },
        { id: 2, priority: 20 },
        { id: 10, priority: 20 },
      ],
      'down'
    )

    expect(plan.updates).toEqual([
      { id: 7, priority: 3000 },
      { id: 10, priority: 2000 },
      { id: 2, priority: 1000 },
    ])
    expect(plan.rollbackUpdates).toEqual([
      { id: 7, priority: 20 },
      { id: 10, priority: 20 },
      { id: 2, priority: 20 },
    ])
  })
})
