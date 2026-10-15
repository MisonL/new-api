import assert from 'node:assert/strict'
import test from 'node:test'
import {
  mapLegacyUsageLogColumnOrder,
  moveColumnKey,
  normalizeColumnOrder,
  reorderColumnKey,
} from './column-preferences'

test('normalizes saved order, removes stale keys, and appends new columns', () => {
  assert.deepEqual(
    normalizeColumnOrder(
      ['model', 'model', 'removed'],
      ['time', 'model', 'cost']
    ),
    ['model', 'time', 'cost']
  )
  assert.deepEqual(normalizeColumnOrder('invalid', ['time']), ['time'])
})

test('moves columns while leaving fixed columns in place', () => {
  const order = ['time', 'model', 'cost', 'actions']
  const movable = ['time', 'model', 'cost']

  assert.deepEqual(moveColumnKey(order, 'cost', 'up', movable), [
    'time',
    'cost',
    'model',
    'actions',
  ])
  assert.deepEqual(moveColumnKey(order, 'actions', 'up', movable), order)
})

test('reorders a column around a target without crossing fixed columns', () => {
  const order = ['time', 'model', 'cost', 'actions']
  const movable = ['time', 'model', 'cost']

  assert.deepEqual(reorderColumnKey(order, 'time', 'cost', 'after', movable), [
    'model',
    'cost',
    'time',
    'actions',
  ])
  assert.deepEqual(
    reorderColumnKey(order, 'time', 'actions', 'after', movable),
    order
  )
})

test('maps Classic usage-log column keys to Default equivalents', () => {
  assert.deepEqual(
    mapLegacyUsageLogColumnOrder([
      'time',
      'completion',
      'details',
      'request_headers',
      'cost',
    ]),
    ['created_at', 'prompt_tokens', 'content', 'quota']
  )
})
