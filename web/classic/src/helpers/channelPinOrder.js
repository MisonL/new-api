/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

const PIN_ORDER_STEP = 1000;

export function toPinOrderNumber(value) {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}

export function isPinnedChannel(channel) {
  return toPinOrderNumber(channel?.pin_order) > 0;
}

export function sortPinnedChannels(channels) {
  const channelsById = new Map();
  let order = 0;

  for (const channel of channels || []) {
    if (!isPinnedChannel(channel)) continue;
    const existing = channelsById.get(channel.id);
    if (existing) {
      existing.channel = channel;
      continue;
    }
    channelsById.set(channel.id, { channel, order });
    order += 1;
  }

  return [...channelsById.values()]
    .sort((left, right) => {
      const pinOrderDiff =
        toPinOrderNumber(right.channel.pin_order) -
        toPinOrderNumber(left.channel.pin_order);
      return pinOrderDiff || left.order - right.order;
    })
    .map(({ channel }) => channel);
}

export function getNextChannelPinOrder(channels) {
  return (
    (channels || []).reduce(
      (maximum, channel) =>
        Math.max(maximum, toPinOrderNumber(channel?.pin_order)),
      0,
    ) + PIN_ORDER_STEP
  );
}

export function getChannelPinOrderMoveUpdates(channel, channels, direction) {
  const sorted = sortPinnedChannels(channels);
  const currentIndex = sorted.findIndex((item) => item.id === channel?.id);
  if (currentIndex < 0) return [];

  const targetIndex = direction === 'up' ? currentIndex - 1 : currentIndex + 1;
  if (!sorted[targetIndex]) return [];

  const reordered = [...sorted];
  const [moved] = reordered.splice(currentIndex, 1);
  reordered.splice(targetIndex, 0, moved);

  const originalOrder = new Map(
    sorted.map((item) => [item.id, toPinOrderNumber(item.pin_order)]),
  );
  return reordered
    .map((item, index) => ({
      id: item.id,
      pin_order: (reordered.length - index) * PIN_ORDER_STEP,
    }))
    .filter((update) => originalOrder.get(update.id) !== update.pin_order);
}
