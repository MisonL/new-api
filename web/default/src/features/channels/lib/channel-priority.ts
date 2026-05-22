import type { Channel } from '../types'

function toPriorityNumber(priority: Channel['priority']): number {
  return typeof priority === 'number' && Number.isFinite(priority) ? priority : 0
}

export function getNextTopChannelPriority(
  channel: Pick<Channel, 'priority'>,
  visibleChannels: readonly Pick<Channel, 'priority'>[]
): number {
  const currentPriority = toPriorityNumber(channel.priority)
  const maxPriority = visibleChannels.reduce(
    (max, visibleChannel) =>
      Math.max(max, toPriorityNumber(visibleChannel.priority)),
    currentPriority
  )

  return maxPriority + 1
}
