import assert from 'node:assert/strict'
import test from 'node:test'
import {
  buildDeploymentChannelPayload,
  normalizeDeploymentPublicUrl,
} from './deployment-channel'

test('normalizes an HTTP(S) container URL and drops query/hash', () => {
  assert.equal(
    normalizeDeploymentPublicUrl({
      public_url: 'https://example.test:8443/v1/?token=ignored#fragment',
    }),
    'https://example.test:8443/v1'
  )
})

test('rejects unsafe or malformed container URLs', () => {
  assert.equal(
    normalizeDeploymentPublicUrl({ public_url: 'javascript:alert(1)' }),
    null
  )
  assert.equal(
    normalizeDeploymentPublicUrl({
      public_url: 'https://user:pass@example.test',
    }),
    null
  )
  assert.equal(normalizeDeploymentPublicUrl({ public_url: 'not a URL' }), null)
})

test('builds an IO.NET channel payload with deployment metadata', () => {
  const payload = buildDeploymentChannelPayload(
    { id: 12, container_name: 'gpu-node' },
    { public_url: 'https://node.example.test/', container_id: 'ctr-1' },
    'ionet-test-key'
  )

  assert.deepEqual(payload, {
    mode: 'single',
    channel: {
      name: '[IO.NET] gpu-node',
      type: 4,
      key: 'ionet-test-key',
      base_url: 'https://node.example.test',
      group: 'default',
      tag: 'ionet',
      remark: '[IO.NET] Auto-synced from deployment 12',
      other_info: JSON.stringify({
        source: 'ionet',
        deployment_id: 12,
        deployment_name: 'gpu-node',
        container_id: 'ctr-1',
        public_url: 'https://node.example.test',
      }),
    },
  })
})

test('does not build a channel without a valid public URL or key', () => {
  const deployment = { id: 12 }
  assert.equal(
    buildDeploymentChannelPayload(
      deployment,
      { public_url: 'http://node.test' },
      ' '
    ),
    null
  )
  assert.equal(
    buildDeploymentChannelPayload(
      deployment,
      { public_url: 'ftp://node.test' },
      'key'
    ),
    null
  )
})
