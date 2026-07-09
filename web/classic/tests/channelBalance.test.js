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
  isUnlimitedChannelBalance,
  UNLIMITED_CHANNEL_BALANCE_THRESHOLD,
} from '../src/helpers/channelBalance.js';

describe('classic channel balance display', () => {
  test('detects upstream unlimited balance placeholders', () => {
    expect(isUnlimitedChannelBalance(999999857.89)).toBe(true);
    expect(isUnlimitedChannelBalance(UNLIMITED_CHANNEL_BALANCE_THRESHOLD)).toBe(
      true,
    );
    expect(
      isUnlimitedChannelBalance(UNLIMITED_CHANNEL_BALANCE_THRESHOLD - 1),
    ).toBe(false);
  });

  test('honors explicit unlimited balance metadata', () => {
    expect(isUnlimitedChannelBalance(0, true)).toBe(true);
    expect(isUnlimitedChannelBalance(0, false)).toBe(false);
  });
});
