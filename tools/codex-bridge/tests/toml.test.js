import test from 'node:test';
import assert from 'node:assert/strict';
import {
  extractManagedBlock,
  removeTableBlock,
  removeTopLevelAssignments,
  stripManagedBlock,
} from '../src/toml.js';

test('stripManagedBlock preserves text when markers are absent', () => {
  const text = '\nmodel = "keep"\n\n';
  assert.equal(stripManagedBlock(text, '# BEGIN managed', '# END managed'), text);
});

test('stripManagedBlock removes only the marked block', () => {
  const text = [
    'model = "before"',
    '# BEGIN managed',
    'model = "managed"',
    '# END managed',
    'approval_policy = "never"',
    '',
  ].join('\n');

  assert.equal(
    stripManagedBlock(text, '# BEGIN managed', '# END managed'),
    'model = "before"\napproval_policy = "never"\n'
  );
});

test('stripManagedBlock removes duplicate marked blocks', () => {
  const text = [
    'model = "before"',
    '# BEGIN managed',
    'model = "managed-a"',
    '# END managed',
    '',
    '# BEGIN managed',
    'model = "managed-b"',
    '# END managed',
    'approval_policy = "never"',
    '',
  ].join('\n');

  assert.equal(
    stripManagedBlock(text, '# BEGIN managed', '# END managed'),
    'model = "before"\n\napproval_policy = "never"\n'
  );
});

test('stripManagedBlock preserves unrelated multiline string whitespace', () => {
  const text = [
    'description = """',
    'first',
    '',
    '',
    'second',
    '"""',
    '# BEGIN managed',
    'model = "managed"',
    '# END managed',
    'approval_policy = "never"',
    '',
  ].join('\n');

  assert.equal(
    stripManagedBlock(text, '# BEGIN managed', '# END managed'),
    [
      'description = """',
      'first',
      '',
      '',
      'second',
      '"""',
      'approval_policy = "never"',
      '',
    ].join('\n')
  );
});

test('stripManagedBlock ignores marker text inside multiline strings', () => {
  const text = [
    'description = """',
    '# BEGIN managed',
    'model = "not managed"',
    '# END managed',
    '"""',
    '',
    'approval_policy = "never"',
    '',
  ].join('\n');

  assert.equal(stripManagedBlock(text, '# BEGIN managed', '# END managed'), text);
});

test('stripManagedBlock ignores markers after escaped triple quotes in multiline strings', () => {
  const text = [
    'description = """',
    'literal quote \\""" still inside',
    '# BEGIN managed',
    'model = "not managed"',
    '# END managed',
    '"""',
    '',
  ].join('\n');

  assert.equal(stripManagedBlock(text, '# BEGIN managed', '# END managed'), text);
});

test('extractManagedBlock returns the marker body', () => {
  const text = [
    '# BEGIN managed',
    'model = "managed"',
    '# END managed',
  ].join('\n');

  assert.equal(extractManagedBlock(text, '# BEGIN managed', '# END managed'), 'model = "managed"');
});

test('extractManagedBlock ignores marker text inside multiline strings', () => {
  const text = [
    'description = """',
    '# BEGIN managed',
    'model = "not managed"',
    '# END managed',
    '"""',
    '',
  ].join('\n');

  assert.equal(extractManagedBlock(text, '# BEGIN managed', '# END managed'), '');
});

test('removeTopLevelAssignments ignores array values and tables', () => {
  const text = [
    'model = "old"',
    'matrix = [',
    '  [1, 2],',
    ']',
    '[profiles.dev]',
    'model = "keep"',
  ].join('\n');

  assert.equal(
    removeTopLevelAssignments(text, ['model']),
    [
      'matrix = [',
      '  [1, 2],',
      ']',
      '[profiles.dev]',
      'model = "keep"',
    ].join('\n')
  );
});

test('removeTopLevelAssignments does not treat multiline arrays as tables', () => {
  const text = [
    'matrix = [',
    '  [1, 2],',
    '  [3, 4],',
    ']',
    'model = "old"',
    '',
  ].join('\n');

  assert.equal(
    removeTopLevelAssignments(text, ['model']),
    [
      'matrix = [',
      '  [1, 2],',
      '  [3, 4],',
      ']',
      '',
    ].join('\n')
  );
});

test('removeTopLevelAssignments removes multiline arrays assigned at top level', () => {
  const text = [
    'model = "old"',
    'matrix = [',
    '  [1, 2],',
    '  [3, 4],',
    ']',
    'profile = "keep"',
  ].join('\n');

  assert.equal(
    removeTopLevelAssignments(text, ['model', 'matrix']),
    ['profile = "keep"'].join('\n')
  );
});

test('removeTopLevelAssignments removes multiline inline tables at top level', () => {
  const text = [
    'model = "old"',
    'mapping = {',
    '  foo = "bar",',
    '  nested = [1, 2, 3],',
    '}',
    'profile = "keep"',
  ].join('\n');

  assert.equal(
    removeTopLevelAssignments(text, ['model', 'mapping']),
    ['profile = "keep"'].join('\n')
  );
});

test('removeTopLevelAssignments preserves kept multiline values with nested removal keys', () => {
  const text = [
    'model = "old"',
    'mapping = {',
    '  model = "nested-model",',
    '  provider = "keep"',
    '}',
    'providers = [',
    '  { model = "nested-array-model", provider = "keep" },',
    ']',
    'model_provider = "old-provider"',
    'approval_policy = "keep"',
  ].join('\n');

  assert.equal(
    removeTopLevelAssignments(text, ['model', 'model_provider']),
    [
      'mapping = {',
      '  model = "nested-model",',
      '  provider = "keep"',
      '}',
      'providers = [',
      '  { model = "nested-array-model", provider = "keep" },',
      ']',
      'approval_policy = "keep"',
    ].join('\n')
  );
});

test('removeTopLevelAssignments removes quoted top-level keys', () => {
  const text = [
    '"model" = "old"',
    '"model_provider" = "old-provider"',
    'model_catalog_json = "old.json"',
    'approval_policy = "keep"',
  ].join('\n');

  assert.equal(
    removeTopLevelAssignments(text, ['model', 'model_provider', 'model_catalog_json']),
    ['approval_policy = "keep"'].join('\n')
  );
});

test('removeTableBlock ignores bracketed values inside multiline arrays', () => {
  const text = [
    '[model_providers.new-api]',
    'name = "old"',
    '',
    '[model_providers.other]',
    'values = [',
    '  [1, 2],',
    '  { nested = [3] },',
    ']',
    'name = "keep"',
  ].join('\n');

  assert.equal(
    removeTableBlock(text, 'model_providers.new-api'),
    [
      '[model_providers.other]',
      'values = [',
      '  [1, 2],',
      '  { nested = [3] },',
      ']',
      'name = "keep"',
    ].join('\n')
  );
});

test('removeTableBlock removes quoted target tables', () => {
  const text = [
    '[model_providers."new-api"]',
    'name = "old"',
    '',
    '[model_providers."new-api".headers]',
    'x_old = "drop"',
    '',
    '[model_providers.other]',
    'name = "keep"',
  ].join('\n');

  assert.equal(
    removeTableBlock(text, 'model_providers.new-api'),
    [
      '[model_providers.other]',
      'name = "keep"',
    ].join('\n')
  );
});

test('removeTableBlock preserves quoted sibling provider names with dots', () => {
  const text = [
    '[model_providers."new-api"]',
    'name = "old"',
    '',
    '[model_providers."new-api.headers"]',
    'name = "keep"',
    '',
  ].join('\n');

  assert.equal(
    removeTableBlock(text, 'model_providers.new-api'),
    [
      '[model_providers."new-api.headers"]',
      'name = "keep"',
      '',
    ].join('\n')
  );
});

test('removeTableBlock handles quoted target headers containing brackets', () => {
  const text = [
    '[model_providers."new-api]prod"]',
    'name = "old"',
    '',
    '[model_providers."new-api"]',
    'name = "drop"',
    '',
    '[model_providers.other]',
    'name = "keep"',
    '',
  ].join('\n');

  assert.equal(
    removeTableBlock(text, 'model_providers.new-api'),
    [
      '[model_providers."new-api]prod"]',
      'name = "old"',
      '',
      '[model_providers.other]',
      'name = "keep"',
      '',
    ].join('\n')
  );
});

test('removeTableBlock removes target tables only', () => {
  const text = [
    '[model_providers.new-api]',
    'name = "old"',
    '',
    '[model_providers.new-api.headers]',
    'x_old = "drop"',
    '',
    '[model_providers.other]',
    'name = "keep"',
  ].join('\n');

  assert.equal(
    removeTableBlock(text, 'model_providers.new-api'),
    [
      '[model_providers.other]',
      'name = "keep"',
    ].join('\n')
  );
});
