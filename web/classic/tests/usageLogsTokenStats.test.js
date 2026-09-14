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

import { getLogPromptCacheStats } from '../src/helpers/log.js';

describe('usage log prompt cache statistics', () => {
  test('distinguishes missing cache telemetry from a measured zero', () => {
    expect(getLogPromptCacheStats(100, {}).cacheRate).toBeNull();
    expect(getLogPromptCacheStats(100, null).cacheRate).toBeNull();
    expect(
      getLogPromptCacheStats(100, { cache_tokens: -1 }).cacheRate,
    ).toBeNull();
    expect(getLogPromptCacheStats(100, { cache_tokens: 0 }).cacheRate).toBe(0);
    expect(getLogPromptCacheStats(0, { cache_tokens: 0 }).cacheRate).toBeNull();
  });
  test('uses input tokens as the denominator for OpenAI semantics', () => {
    expect(
      getLogPromptCacheStats(200, {
        cache_tokens: 50,
        usage_semantic: 'openai',
      }),
    ).toEqual({
      cacheReadTokens: 50,
      cacheWriteTokens: 0,
      cacheRate: 0.25,
    });
  });

  test('includes cache reads in the denominator for Anthropic semantics', () => {
    expect(
      getLogPromptCacheStats(100, {
        cache_tokens: 100,
        usage_semantic: 'anthropic',
      }),
    ).toEqual({
      cacheReadTokens: 100,
      cacheWriteTokens: 0,
      cacheRate: 0.5,
    });
  });

  test('prefers normalized cache writes and otherwise combines split writes', () => {
    expect(
      getLogPromptCacheStats(100, {
        cache_write_tokens: 80,
        cache_creation_tokens: 20,
        cache_creation_tokens_5m: 30,
        cache_creation_tokens_1h: 40,
      }).cacheWriteTokens,
    ).toBe(80);
    expect(
      getLogPromptCacheStats(100, {
        cache_creation_tokens: 20,
        cache_creation_tokens_5m: 30,
        cache_creation_tokens_1h: 40,
      }).cacheWriteTokens,
    ).toBe(70);
  });
});
