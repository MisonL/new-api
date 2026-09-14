import type { Channel } from '../types'

type PinOrderChannel = Pick<Channel, 'id' | 'pin_order'>

export type TopChannelMoveDirection = 'up' | 'down'

export type TopChannelPinOrderUpdate = {
  id: number
  pin_order: number
}

export type TopChannelPinOrderMovePlan = {
  updates: TopChannelPinOrderUpdate[]
  rollbackUpdates: TopChannelPinOrderUpdate[]
}

const PIN_ORDER_STEP = 1_000

export function toPinOrderNumber(pinOrder: Channel['pin_order']): number {
  return typeof pinOrder === 'number' && Number.isFinite(pinOrder)
    ? pinOrder
    : 0
}

export function isPinnedChannel(channel: Pick<Channel, 'pin_order'>): boolean {
  return toPinOrderNumber(channel.pin_order) > 0
}

function sortPinnedChannels(
  channels: readonly PinOrderChannel[]
): PinOrderChannel[] {
  const channelById = new Map<
    number,
    { channel: PinOrderChannel; order: number }
  >()
  let order = 0
  for (const channel of channels) {
    if (isPinnedChannel(channel)) {
      const existing = channelById.get(channel.id)
      if (existing) {
        existing.channel = channel
      } else {
        channelById.set(channel.id, { channel, order })
        order += 1
      }
    }
  }

  return [...channelById.values()]
    .sort((left, right) => {
      const pinOrderDiff =
        toPinOrderNumber(right.channel.pin_order) -
        toPinOrderNumber(left.channel.pin_order)
      if (pinOrderDiff !== 0) return pinOrderDiff
      return left.order - right.order
    })
    .map(({ channel }) => channel)
}

export function getNextTopChannelPinOrder(
  channel: Pick<Channel, 'pin_order'>,
  visibleChannels: readonly Pick<Channel, 'pin_order'>[]
): number {
  const currentPinOrder = Math.max(0, toPinOrderNumber(channel.pin_order))
  const maxPinOrder = visibleChannels.reduce(
    (max, visibleChannel) =>
      Math.max(max, Math.max(0, toPinOrderNumber(visibleChannel.pin_order))),
    currentPinOrder
  )

  return maxPinOrder + PIN_ORDER_STEP
}

function createReindexedMovePlan(
  sortedChannels: PinOrderChannel[],
  currentIndex: number,
  targetIndex: number
): TopChannelPinOrderMovePlan {
  const reorderedChannels = [...sortedChannels]
  const [movedChannel] = reorderedChannels.splice(currentIndex, 1)
  reorderedChannels.splice(targetIndex, 0, movedChannel)

  const originalPinOrderById = new Map(
    sortedChannels.map((item) => [item.id, toPinOrderNumber(item.pin_order)])
  )
  const updates = reorderedChannels
    .map((item, index) => ({
      id: item.id,
      pin_order: (reorderedChannels.length - index) * PIN_ORDER_STEP,
    }))
    .filter(
      (update) => originalPinOrderById.get(update.id) !== update.pin_order
    )

  return {
    updates,
    rollbackUpdates: updates.map((update) => ({
      id: update.id,
      pin_order: originalPinOrderById.get(update.id) ?? 0,
    })),
  }
}

export function getTopChannelPinOrderMovePlan(
  channel: PinOrderChannel,
  pinnedChannels: readonly PinOrderChannel[],
  direction: TopChannelMoveDirection
): TopChannelPinOrderMovePlan {
  const sortedChannels = sortPinnedChannels(pinnedChannels)
  const currentIndex = sortedChannels.findIndex(
    (item) => item.id === channel.id
  )
  if (currentIndex === -1) return { updates: [], rollbackUpdates: [] }

  const targetIndex = direction === 'up' ? currentIndex - 1 : currentIndex + 1
  const targetChannel = sortedChannels[targetIndex]
  if (!targetChannel) return { updates: [], rollbackUpdates: [] }

  return createReindexedMovePlan(sortedChannels, currentIndex, targetIndex)
}

export function getTopChannelPinOrderMoveUpdates(
  channel: PinOrderChannel,
  pinnedChannels: readonly PinOrderChannel[],
  direction: TopChannelMoveDirection
): TopChannelPinOrderUpdate[] {
  return getTopChannelPinOrderMovePlan(channel, pinnedChannels, direction)
    .updates
}
