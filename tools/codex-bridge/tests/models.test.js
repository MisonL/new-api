import test from 'node:test';
import assert from 'node:assert/strict';
import { discoverModels, parseModelIds } from '../src/models.js';

test('parses unique model ids', () => {
  assert.deepEqual(parseModelIds({ data: [{ id: 'gpt-5.5' }, { id: 'gpt-5.5' }, 'gpt-5.4'] }), [
    'gpt-5.5',
    'gpt-5.4',
  ]);
});

test('rejects empty model list', () => {
  assert.throws(() => parseModelIds({ data: [] }), /空模型列表/);
});

test('discovers models through fetch', async () => {
  const seen = {};
  const result = await discoverModels({
    baseUrl: 'https://example.com/',
    apiKey: 'test-key',
    fetchImpl: async (url, options) => {
      seen.url = url;
      seen.authorization = options.headers.Authorization;
      return {
        ok: true,
        status: 200,
        text: async () => JSON.stringify({ data: [{ id: 'gpt-5.5' }] }),
      };
    },
  });

  assert.equal(seen.url, 'https://example.com/v1/models');
  assert.equal(seen.authorization, 'Bearer test-key');
  assert.deepEqual(result.modelIds, ['gpt-5.5']);
});

test('trims whitespace from api key before sending', async () => {
  const seen = {};
  await discoverModels({
    baseUrl: 'https://example.com/',
    apiKey: '  test-key\n',
    fetchImpl: async (url, options) => {
      seen.url = url;
      seen.authorization = options.headers.Authorization;
      return {
        ok: true,
        status: 200,
        text: async () => JSON.stringify({ data: [{ id: 'gpt-5.5' }] }),
      };
    },
  });

  assert.equal(seen.authorization, 'Bearer test-key');
});

test('reports HTTP failures explicitly', async () => {
  await assert.rejects(
    () => discoverModels({
      baseUrl: 'https://example.com',
      apiKey: 'bad-key',
      fetchImpl: async () => ({
        ok: false,
        status: 401,
        text: async () => 'invalid token',
      }),
    }),
    /HTTP 401/,
  );
});

test('reports non JSON responses', async () => {
  await assert.rejects(
    () => discoverModels({
      baseUrl: 'https://example.com',
      apiKey: 'test-key',
      fetchImpl: async () => ({
        ok: true,
        status: 200,
        text: async () => '<html>',
      }),
    }),
    /有效 JSON/,
  );
});

test('rejects empty api keys before issuing fetch', async () => {
  await assert.rejects(
    () =>
      discoverModels({
        baseUrl: 'https://example.com',
        apiKey: '   ',
        fetchImpl: async () => {
          throw new Error('should not be called');
        },
      }),
    /需要 API Key/,
  );
});

test('keeps timeout active until response body is read', async () => {
  let cleared = false;
  const originalAbortSignal = globalThis.AbortSignal;
  const originalClearTimeout = globalThis.clearTimeout;
  globalThis.AbortSignal = undefined;
  globalThis.clearTimeout = (timer) => {
    cleared = true;
    return originalClearTimeout(timer);
  };

  try {
    const result = await discoverModels({
      baseUrl: 'https://example.com',
      apiKey: 'test-key',
      fetchImpl: async () => ({
        ok: true,
        status: 200,
        text: async () => {
          assert.equal(cleared, false);
          return JSON.stringify({ data: [{ id: 'gpt-5.5' }] });
        },
      }),
    });

    assert.deepEqual(result.modelIds, ['gpt-5.5']);
    assert.equal(cleared, true);
  } finally {
    globalThis.AbortSignal = originalAbortSignal;
    globalThis.clearTimeout = originalClearTimeout;
  }
});

test('supports explicit timeout overrides', async () => {
  const result = await discoverModels({
    baseUrl: 'https://example.com',
    apiKey: 'test-key',
    timeoutMs: 0,
    fetchImpl: async (url, options) => {
      assert.equal(url, 'https://example.com/v1/models');
      assert.equal(options.signal, undefined);
      return {
        ok: true,
        status: 200,
        text: async () => JSON.stringify({ data: [{ id: 'gpt-5.5' }] }),
      };
    },
  });

  assert.deepEqual(result.modelIds, ['gpt-5.5']);
});

test('reports fetch aborts as explicit timeouts', async () => {
  const abortError = Object.assign(new Error('The operation was aborted'), {
    name: 'AbortError',
  });

  await assert.rejects(
    () =>
      discoverModels({
        baseUrl: 'https://example.com',
        apiKey: 'test-key',
        timeoutMs: 25,
        fetchImpl: async () => {
          throw abortError;
        },
      }),
    /请求超时，超过 25ms/,
  );
});
