import test from 'node:test';
import assert from 'node:assert/strict';
import { modelsEndpoint, normalizeProviderBaseUrl } from '../src/url.js';

test('规范化带 /v1 和不带 /v1 的基础 URL', () => {
  assert.equal(normalizeProviderBaseUrl('https://example.com'), 'https://example.com/v1');
  assert.equal(normalizeProviderBaseUrl('https://example.com/'), 'https://example.com/v1');
  assert.equal(normalizeProviderBaseUrl('https://example.com/v1'), 'https://example.com/v1');
  assert.equal(normalizeProviderBaseUrl('https://example.com/V1'), 'https://example.com/V1');
  assert.equal(normalizeProviderBaseUrl('https://example.com/api/v1/'), 'https://example.com/api/v1');
});

test('strips query strings and hashes from base URLs', () => {
  assert.equal(
    normalizeProviderBaseUrl('https://example.com/v1?foo=bar#frag'),
    'https://example.com/v1',
  );
});

test('builds models endpoint', () => {
  assert.equal(modelsEndpoint('https://example.com'), 'https://example.com/v1/models');
});

test('rejects full endpoint paths', () => {
  assert.throws(
    () => normalizeProviderBaseUrl('https://example.com/v1/models'),
    /具体接口路径/,
  );
  assert.throws(
    () => normalizeProviderBaseUrl('https://example.com/models'),
    /具体接口路径/,
  );
  assert.throws(
    () => normalizeProviderBaseUrl('https://example.com/V1/Models'),
    /具体接口路径/,
  );
  assert.throws(
    () => normalizeProviderBaseUrl('https://example.com/images/generations'),
    /具体接口路径/,
  );
});

test('rejects invalid protocol', () => {
  assert.throws(() => normalizeProviderBaseUrl('file:///tmp/api'), /http/);
});

test('rejects embedded credentials', () => {
  assert.throws(
    () => normalizeProviderBaseUrl('https://user:pass@example.com'),
    /不能包含用户名或密码/,
  );
});

test('rejects endpoint-like suffixes only at the tail segment', () => {
  assert.equal(
    normalizeProviderBaseUrl('https://example.com/models/root'),
    'https://example.com/models/root/v1',
  );
  assert.throws(
    () => normalizeProviderBaseUrl('https://example.com/projects/models'),
    /具体接口路径/,
  );
});
