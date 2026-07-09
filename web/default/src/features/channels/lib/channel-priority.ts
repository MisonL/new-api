import type { Channel } from '../types'

type PriorityChannel = Pick<Channel, 'id' | 'priority'>

export type TopChannelMoveDirection = 'up' | 'down'

export type TopChannelPriorityUpdate = {
  id: number
  priority: number
}

export type TopChannelPriorityMovePlan = {
  updates: TopChannelPriorityUpdate[]
  rollbackUpdates: TopChannelPriorityUpdate[]
}

const PRIORITY_STEP = 1_000

export function toPriorityNumber(priority: Channel['priority']): number {
  return typeof priority === 'number' && Number.isFinite(priority) ? priority : 0
}

export function isTopChannel(channel: Pick<Channel, 'priority'>): boolean {
  return toPriorityNumber(channel.priority) > 0
}

function sortTopChannels(channels: readonly PriorityChannel[]): PriorityChannel[] {
  const channelById = new Map<
    number,
    { channel: PriorityChannel; order: number }
  >()
  let order = 0
  for (const channel of channels) {
    if (isTopChannel(channel)) {
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
      const priorityDiff =
        toPriorityNumber(right.channel.priority) -
        toPriorityNumber(left.channel.priority)
      if (priorityDiff !== 0) return priorityDiff
      return left.order - right.order
    })
    .map(({ channel }) => channel)
}

export function getNextTopChannelPriority(
  channel: Pick<Channel, 'priority'>,
  visibleChannels: readonly Pick<Channel, 'priority'>[]
): number {
  const currentPriority = Math.max(0, toPriorityNumber(channel.priority))
  const maxPriority = visibleChannels.reduce(
    (max, visibleChannel) =>
      Math.max(max, Math.max(0, toPriorityNumber(visibleChannel.priority))),
    currentPriority
  )

  return maxPriority + PRIORITY_STEP
}

function createReindexedMovePlan(
  sortedChannels: PriorityChannel[],
  currentIndex: number,
  targetIndex: number
): TopChannelPriorityMovePlan {
  const reorderedChannels = [...sortedChannels]
  const [movedChannel] = reorderedChannels.splice(currentIndex, 1)
  reorderedChannels.splice(targetIndex, 0, movedChannel)

  const originalPriorityById = new Map(
    sortedChannels.map((item) => [item.id, toPriorityNumber(item.priority)])
  )
  const updates = reorderedChannels
    .map((item, index) => ({
      id: item.id,
      priority: (reorderedChannels.length - index) * PRIORITY_STEP,
    }))
    .filter((update) => originalPriorityById.get(update.id) !== update.priority)

  return {
    updates,
    rollbackUpdates: updates.map((update) => ({
      id: update.id,
      priority: originalPriorityById.get(update.id) ?? 0,
    })),
  }
}

export function getTopChannelPriorityMovePlan(
  channel: PriorityChannel,
  topChannels: readonly PriorityChannel[],
  direction: TopChannelMoveDirection
): TopChannelPriorityMovePlan {
  const sortedChannels = sortTopChannels(topChannels)
  const currentIndex = sortedChannels.findIndex((item) => item.id === channel.id)
  if (currentIndex === -1) return { updates: [], rollbackUpdates: [] }

  const targetIndex = direction === 'up' ? currentIndex - 1 : currentIndex + 1
  const targetChannel = sortedChannels[targetIndex]
  if (!targetChannel) return { updates: [], rollbackUpdates: [] }

  return createReindexedMovePlan(sortedChannels, currentIndex, targetIndex)
}

export function getTopChannelPriorityMoveUpdates(
  channel: PriorityChannel,
  topChannels: readonly PriorityChannel[],
  direction: TopChannelMoveDirection
): TopChannelPriorityUpdate[] {
  return getTopChannelPriorityMovePlan(channel, topChannels, direction).updates
}
