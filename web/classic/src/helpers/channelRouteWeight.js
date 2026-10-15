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

export const ROUTE_WEIGHT_TOTAL_BPS = 10000;

function redistributeEqual(values, indexes, amount, subtract) {
  while (amount > 0) {
    const active = indexes.filter((index) =>
      subtract ? values[index] > 0 : values[index] < ROUTE_WEIGHT_TOTAL_BPS,
    );
    if (active.length === 0) return;
    const share = Math.max(1, Math.floor(amount / active.length));
    let remaining = amount;
    for (const index of active) {
      if (remaining === 0) break;
      const capacity = subtract
        ? values[index]
        : ROUTE_WEIGHT_TOTAL_BPS - values[index];
      const step = Math.min(share, remaining, capacity);
      if (step <= 0) continue;
      values[index] += subtract ? -step : step;
      remaining -= step;
    }
    if (remaining === amount) return;
    amount = remaining;
  }
}

function redistributeProportional(values, indexes, amount, add) {
  if (amount === 0 || indexes.length === 0) return;
  const numerators = indexes.map((index) => values[index]);
  const total = numerators.reduce((sum, value) => sum + value, 0);
  if (total === 0) {
    redistributeEqual(values, indexes, amount, !add);
    return;
  }

  const shares = numerators.map((value, index) => ({
    index,
    base: Math.floor((amount * value) / total),
    remainder: (amount * value) % total,
  }));
  const assigned = shares.reduce((sum, share) => sum + share.base, 0);
  shares.sort(
    (left, right) =>
      right.remainder - left.remainder || left.index - right.index,
  );
  for (let index = 0; index < amount - assigned; index += 1) {
    shares[index % shares.length].base += 1;
  }
  for (const share of shares) {
    const targetIndex = indexes[share.index];
    if (add) {
      values[targetIndex] += share.base;
    } else {
      values[targetIndex] = Math.max(0, values[targetIndex] - share.base);
    }
  }
}

export function adjustRouteWeights(
  current,
  targetIndex,
  target,
  mode = 'equal_adjust',
  locked = [],
) {
  if (
    !Number.isInteger(targetIndex) ||
    targetIndex < 0 ||
    targetIndex >= current.length ||
    !Number.isInteger(target) ||
    target < 0 ||
    target > ROUTE_WEIGHT_TOTAL_BPS ||
    (locked.length !== 0 && locked.length !== current.length) ||
    (locked.length !== 0 && locked[targetIndex]) ||
    current.some(
      (value) =>
        !Number.isInteger(value) || value < 0 || value > ROUTE_WEIGHT_TOTAL_BPS,
    ) ||
    current.reduce((sum, value) => sum + value, 0) !== ROUTE_WEIGHT_TOTAL_BPS
  ) {
    return null;
  }

  const result = [...current];
  const old = result[targetIndex];
  result[targetIndex] = target;
  const delta = target - old;
  if (delta === 0) return result;

  const participants = current
    .map((_value, index) => index)
    .filter(
      (index) =>
        index !== targetIndex && !(locked.length !== 0 && locked[index]),
    );
  if (participants.length === 0) return null;

  if (delta > 0) {
    const available = participants.reduce(
      (sum, index) => sum + result[index],
      0,
    );
    if (delta > available) return null;
    if (mode === 'proportional_adjust') {
      redistributeProportional(result, participants, delta, false);
    } else {
      redistributeEqual(result, participants, delta, true);
    }
  } else {
    const amount = -delta;
    if (mode === 'proportional_adjust') {
      redistributeProportional(result, participants, amount, true);
    } else {
      redistributeEqual(result, participants, amount, false);
    }
  }

  if (
    result.some(
      (value) =>
        !Number.isInteger(value) || value < 0 || value > ROUTE_WEIGHT_TOTAL_BPS,
    ) ||
    result.reduce((sum, value) => sum + value, 0) !== ROUTE_WEIGHT_TOTAL_BPS
  ) {
    return null;
  }
  return result;
}

export function parseRouteWeightPercent(value) {
  const normalized = String(value ?? '').trim();
  if (
    !normalized ||
    !/^\d{0,3}(?:\.\d{0,2})?$/.test(normalized) ||
    !/\d/.test(normalized)
  ) {
    return null;
  }

  const [wholePart = '', fractionPart = ''] = normalized.split('.');
  const whole = Number(wholePart || '0');
  const fraction = Number((fractionPart + '00').slice(0, 2));
  if (!Number.isInteger(whole) || !Number.isInteger(fraction) || whole > 100) {
    return null;
  }
  if (whole === 100 && fraction !== 0) return null;
  return whole * 100 + fraction;
}

export function formatRouteWeightPercent(bps) {
  if (!Number.isInteger(bps) || bps < 0 || bps > ROUTE_WEIGHT_TOTAL_BPS) {
    return '';
  }
  return (bps / 100).toFixed(2).replace(/\.?(0+)$/, '');
}
