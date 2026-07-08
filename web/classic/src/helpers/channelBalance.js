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

export const UNLIMITED_CHANNEL_BALANCE_THRESHOLD = 100000000;

export function isUnlimitedChannelBalance(balance, explicitUnlimited = false) {
  if (explicitUnlimited === true) {
    return true;
  }
  const numericBalance = Number(balance);
  return (
    Number.isFinite(numericBalance) &&
    numericBalance >= UNLIMITED_CHANNEL_BALANCE_THRESHOLD
  );
}
