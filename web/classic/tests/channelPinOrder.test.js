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

import { describe, expect, test } from 'bun:test';

import {
  getChannelPinOrderMoveUpdates,
  getNextChannelPinOrder,
  isPinnedChannel,
  sortPinnedChannels,
} from '../src/helpers/channelPinOrder.js';

describe('classic channel pin ordering', () => {
  const channels = [
    { id: 1, pin_order: 1000, priority: 90 },
    { id: 2, pin_order: 3000, priority: 10 },
    { id: 3, pin_order: 2000, priority: 50 },
    { id: 4, pin_order: 0, priority: 100 },
  ];

  test('uses pin_order independently from route priority', () => {
    expect(sortPinnedChannels(channels).map((channel) => channel.id)).toEqual([
      2, 3, 1,
    ]);
    expect(isPinnedChannel(channels[3])).toBe(false);
    expect(getNextChannelPinOrder(channels)).toBe(4000);
  });

  test('reindexes the pinned subset when moving a channel', () => {
    expect(getChannelPinOrderMoveUpdates(channels[2], channels, 'up')).toEqual([
      { id: 3, pin_order: 3000 },
      { id: 2, pin_order: 2000 },
    ]);
    expect(channels.map((channel) => channel.priority)).toEqual([
      90, 10, 50, 100,
    ]);
  });

  test('does not emit updates outside the pinned order boundaries', () => {
    expect(getChannelPinOrderMoveUpdates(channels[1], channels, 'up')).toEqual(
      [],
    );
    expect(
      getChannelPinOrderMoveUpdates(channels[0], channels, 'down'),
    ).toEqual([]);
  });
});
