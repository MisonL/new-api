import assert from 'node:assert/strict'
import test from 'node:test'
import { parseHeaderNavModuleAccess } from './header-nav-access'

test('parses object access modules', () => {
  assert.deepEqual(
    parseHeaderNavModuleAccess(
      JSON.stringify({ pricing: { enabled: true, requireAuth: true } }),
      'pricing'
    ),
    { enabled: true, requireAuth: true }
  )
})

test('keeps boolean access modules backward compatible', () => {
  assert.deepEqual(
    parseHeaderNavModuleAccess(JSON.stringify({ rankings: false }), 'rankings'),
    { enabled: false, requireAuth: false }
  )
})

test('uses public defaults for invalid configuration', () => {
  assert.deepEqual(parseHeaderNavModuleAccess('{', 'pricing'), {
    enabled: true,
    requireAuth: false,
  })
})
