import assert from 'node:assert/strict'
import test from 'node:test'
import { rewriteLegacyPath } from './legacy-route-rewrite'

function rewrite(path: string) {
  return rewriteLegacyPath(new URL(path, 'https://new-api.test'))
}

test('rewrites Classic console paths to Default paths', () => {
  assert.equal(rewrite('/console').pathname, '/dashboard')
  assert.equal(rewrite('/console/models/').pathname, '/models/metadata')
  assert.equal(rewrite('/console/deployment').pathname, '/models/deployments')
  assert.equal(rewrite('/console/channel').pathname, '/channels')
  assert.equal(rewrite('/console/token').pathname, '/keys')
  assert.equal(rewrite('/console/log').pathname, '/usage-logs/common')
  assert.equal(rewrite('/console/midjourney').pathname, '/usage-logs/drawing')
  assert.equal(rewrite('/console/task').pathname, '/usage-logs/task')
  assert.equal(rewrite('/login').pathname, '/sign-in')
  assert.equal(rewrite('/register').pathname, '/sign-up')
  assert.equal(rewrite('/forbidden').pathname, '/403')
})

test('maps legacy setting tabs and keeps unrelated search and hash', () => {
  const result = rewrite('/console/setting?tab=ratio&from=legacy#pricing')

  assert.equal(result.pathname, '/system-settings/models/ratio')
  assert.equal(result.search, '?from=legacy')
  assert.equal(result.hash, '#pricing')
})

test('maps legacy chat links and keeps their query and hash', () => {
  const result = rewrite('/console/chat/12?source=bookmark#messages')

  assert.equal(result.pathname, '/chat/12')
  assert.equal(result.search, '?source=bookmark')
  assert.equal(result.hash, '#messages')
})

test('uses the first chat preset for the old optional chat route', () => {
  assert.equal(rewrite('/console/chat').pathname, '/chat/0')
})
