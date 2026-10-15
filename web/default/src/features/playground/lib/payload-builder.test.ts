import assert from 'node:assert/strict'
import test from 'node:test'
import { payloadToMessages } from './payload-builder'

test('rejects custom messages containing malformed content parts', () => {
  assert.deepEqual(
    payloadToMessages({
      messages: [
        {
          role: 'user',
          content: [{ type: 'text', text: 'ok' }, null],
        },
      ],
    }),
    []
  )
})

test('normalizes valid custom message content arrays', () => {
  const [message] = payloadToMessages({
    messages: [
      {
        role: 'user',
        content: [
          { type: 'text', text: 'hello' },
          { type: 'image_url', image_url: { url: 'https://example.test/a' } },
        ],
      },
    ],
  })

  assert.deepEqual(message.versions[0]?.content, [
    { type: 'text', text: 'hello' },
    { type: 'image_url', image_url: { url: 'https://example.test/a' } },
  ])
})
