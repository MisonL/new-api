import type { Channel } from '../types'
import {
  getNextTopChannelPinOrder,
  getTopChannelPinOrderMovePlan,
  getTopChannelPinOrderMoveUpdates,
  isPinnedChannel,
  toPinOrderNumber,
  type TopChannelMoveDirection,
} from './channel-pin-order'

export type { TopChannelMoveDirection } from './channel-pin-order'

// Compatibility aliases for integrations that imported the pre-pin_order
// helper names. New code should import channel-pin-order directly.
export type TopChannelPriorityUpdate = {
  id: number
  priority: number
}

export type TopChannelPriorityMovePlan = {
  updates: TopChannelPriorityUpdate[]
  rollbackUpdates: TopChannelPriorityUpdate[]
}

type LegacyPriorityChannel = Pick<Channel, 'id' | 'priority'>

function toPinChannel(channel: LegacyPriorityChannel) {
  return { id: channel.id, pin_order: channel.priority }
}

function toLegacyUpdates(
  updates: Array<{ id: number; pin_order: number }>
): TopChannelPriorityUpdate[] {
  return updates.map((update) => ({
    id: update.id,
    priority: update.pin_order,
  }))
}

/** @deprecated Use toPinOrderNumber. */
export const toPriorityNumber = (priority: Channel['priority']): number =>
  toPinOrderNumber(priority)

/** @deprecated Use isPinnedChannel. */
export const isTopChannel = (channel: Pick<Channel, 'priority'>): boolean =>
  isPinnedChannel({ pin_order: channel.priority })

/** @deprecated Use getNextTopChannelPinOrder. */
export function getNextTopChannelPriority(
  channel: Pick<Channel, 'priority'>,
  visibleChannels: readonly Pick<Channel, 'priority'>[]
): number {
  return getNextTopChannelPinOrder(
    { pin_order: channel.priority },
    visibleChannels.map((visibleChannel) => ({
      pin_order: visibleChannel.priority,
    }))
  )
}

/** @deprecated Use getTopChannelPinOrderMovePlan. */
export function getTopChannelPriorityMovePlan(
  channel: LegacyPriorityChannel,
  topChannels: readonly LegacyPriorityChannel[],
  direction: TopChannelMoveDirection
): TopChannelPriorityMovePlan {
  const plan = getTopChannelPinOrderMovePlan(
    toPinChannel(channel),
    topChannels.map(toPinChannel),
    direction
  )
  return {
    updates: toLegacyUpdates(plan.updates),
    rollbackUpdates: toLegacyUpdates(plan.rollbackUpdates),
  }
}

/** @deprecated Use getTopChannelPinOrderMoveUpdates. */
export function getTopChannelPriorityMoveUpdates(
  channel: LegacyPriorityChannel,
  topChannels: readonly LegacyPriorityChannel[],
  direction: TopChannelMoveDirection
): TopChannelPriorityUpdate[] {
  return toLegacyUpdates(
    getTopChannelPinOrderMoveUpdates(
      toPinChannel(channel),
      topChannels.map(toPinChannel),
      direction
    )
  )
}
