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

export function getLogOther(otherStr) {
  if (otherStr === undefined || otherStr === null || otherStr === '') {
    return {};
  }
  if (typeof otherStr === 'object') {
    return otherStr;
  }
  try {
    return JSON.parse(otherStr);
  } catch (e) {
    console.error(`Failed to parse record.other: "${otherStr}".`, e);
    return null;
  }
}

function toLogTokenCount(value) {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

export function getLogPromptCacheStats(promptTokens, other) {
  const inputTokens = toLogTokenCount(promptTokens);
  const cacheReadTokens = toLogTokenCount(other?.cache_tokens);
  const normalizedCacheWriteTokens = toLogTokenCount(other?.cache_write_tokens);
  const cacheWrite5m = toLogTokenCount(other?.cache_creation_tokens_5m);
  const cacheWrite1h = toLogTokenCount(other?.cache_creation_tokens_1h);
  const cacheWriteTokens =
    normalizedCacheWriteTokens > 0
      ? normalizedCacheWriteTokens
      : cacheWrite5m > 0 || cacheWrite1h > 0
        ? cacheWrite5m + cacheWrite1h
        : toLogTokenCount(other?.cache_creation_tokens);
  const isAnthropic =
    String(other?.usage_semantic || '')
      .trim()
      .toLowerCase() === 'anthropic' || other?.claude === true;
  const denominator = isAnthropic ? inputTokens + cacheReadTokens : inputTokens;
  const rawCacheRead = other?.cache_tokens;
  const hasCacheRead =
    (typeof rawCacheRead === 'number' ||
      (typeof rawCacheRead === 'string' && rawCacheRead.trim() !== '')) &&
    Number.isFinite(Number(rawCacheRead)) &&
    Number(rawCacheRead) >= 0;

  return {
    cacheReadTokens,
    cacheWriteTokens,
    cacheRate:
      hasCacheRead && denominator > 0 ? cacheReadTokens / denominator : null,
  };
}
