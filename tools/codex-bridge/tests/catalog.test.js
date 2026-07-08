import test from 'node:test';
import assert from 'node:assert/strict';
import { buildModelCatalog } from '../src/catalog.js';

test('builds Codex-compatible model entries', () => {
  const catalog = buildModelCatalog(['gpt-5.5']);
  const model = catalog.models[0];
  const nextCatalog = buildModelCatalog(['gpt-5.5', 'gpt-5.4']);

  assert.equal(model.slug, 'gpt-5.5');
  assert.equal(model.display_name, 'gpt-5.5');
  assert.equal(model.base_instructions, 'You are Codex, a coding agent.');
  assert.equal(model.supports_reasoning_summaries, true);
  assert.equal(model.support_verbosity, true);
  assert.deepEqual(model.input_modalities, ['text', 'image']);
  assert.equal(model.truncation_policy.mode, 'tokens');
  assert.equal(nextCatalog.models[1].priority, 51);
});

test('copies supported reasoning levels per model', () => {
  const catalog = buildModelCatalog(['gpt-5.5', 'gpt-5.4']);
  catalog.models[0].supported_reasoning_levels.push({
    effort: 'custom',
    description: 'Custom',
  });

  assert.equal(catalog.models[1].supported_reasoning_levels.length, 3);
});
