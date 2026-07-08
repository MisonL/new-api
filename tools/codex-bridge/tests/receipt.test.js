import test from 'node:test';
import assert from 'node:assert/strict';
import { buildReceipt, parseReceipt } from '../src/receipt.js';
import { TOOL_ID } from '../src/constants.js';

test('builds and parses a valid receipt', () => {
  const text = buildReceipt({
    providerBaseUrl: 'https://example.com/v1',
    defaultModel: 'gpt-5.5',
    selectedModels: ['gpt-5.5'],
    selectionMode: 'all',
    authMode: 'provider-token',
    backupId: '20260701-010203004Z',
  });
  const receipt = parseReceipt(text);

  assert.equal(receipt.providerBaseUrl, 'https://example.com/v1');
  assert.equal(receipt.defaultModel, 'gpt-5.5');
  assert.deepEqual(receipt.selectedModels, ['gpt-5.5']);
  assert.equal(receipt.selectionMode, 'all');
  assert.equal(receipt.authMode, 'provider-token');
  assert.equal(receipt.backupId, '20260701-010203004Z');
});

test('rejects malformed receipt backup id and explicit model selection', () => {
  const base = {
    schema: 1,
    tool: TOOL_ID,
    providerId: 'new-api',
    providerBaseUrl: 'https://example.com/v1',
    catalogFile: 'new-api-model-catalog.json',
    defaultModel: 'gpt-5.5',
    selectionMode: 'all',
    authMode: 'provider-token',
  };

  assert.throws(
    () => parseReceipt(JSON.stringify({ ...base, backupId: 123 })),
    /backupId 无效/,
  );
  assert.throws(
    () => parseReceipt(JSON.stringify({ ...base, backupId: 'bad-id' })),
    /backupId 无效/,
  );
  assert.throws(
    () => parseReceipt(JSON.stringify({ ...base, selectionMode: 'explicit' })),
    /selectedModels/,
  );
  assert.throws(
    () => parseReceipt(JSON.stringify({ ...base, catalogFile: undefined })),
    /catalogFile/,
  );
});

test('rejects env auth receipts without env key', () => {
  const base = {
    schema: 1,
    tool: TOOL_ID,
    providerId: 'new-api',
    providerBaseUrl: 'https://example.com/v1',
    catalogFile: 'new-api-model-catalog.json',
    defaultModel: 'gpt-5.5',
    selectionMode: 'all',
    authMode: 'env',
  };

  assert.throws(
    () => parseReceipt(JSON.stringify(base)),
    /envKey/,
  );
});

test('rejects receipts created by other tools', () => {
  const base = {
    schema: 1,
    tool: 'other-tool',
    providerId: 'new-api',
    providerBaseUrl: 'https://example.com/v1',
    catalogFile: 'new-api-model-catalog.json',
    defaultModel: 'gpt-5.5',
    selectionMode: 'all',
    authMode: 'provider-token',
  };

  assert.throws(
    () => parseReceipt(JSON.stringify(base)),
    /receipt 不是由/,
  );
});
