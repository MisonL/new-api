import assert from 'node:assert/strict'
import test from 'node:test'
import { mergeVisibility } from './use-persisted-table-visibility'

test('merges persisted visibility over defaults', () => {
  assert.deepEqual(
    mergeVisibility({ models: false, tag: false }, { priority: false }),
    { models: false, tag: false, priority: false }
  )
})

test('persisted values win over defaults for the same column', () => {
  assert.deepEqual(mergeVisibility({ models: false }, { models: true }), {
    models: true,
  })
})

test('required columns stay visible when persisted visibility hides them', () => {
  // A row-actions column carries enableHiding: false, so the toggle menu
  // cannot offer it back. A stale persisted entry must not hide it.
  assert.deepEqual(
    mergeVisibility({}, { actions: false, name: false }, ['actions']),
    { actions: true, name: false }
  )
})

test('required columns are visible even when absent from both inputs', () => {
  assert.deepEqual(mergeVisibility({}, {}, ['actions']), { actions: true })
})

test('ignores malformed persisted payloads', () => {
  assert.deepEqual(mergeVisibility({ models: false }, null, ['actions']), {
    models: false,
  })
  assert.deepEqual(mergeVisibility({ models: false }, 'nope', ['actions']), {
    models: false,
  })
  assert.deepEqual(
    mergeVisibility({ models: false }, ['models'], ['actions']),
    {
      models: false,
    }
  )
})
