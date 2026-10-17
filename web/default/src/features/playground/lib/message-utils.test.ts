import assert from 'node:assert/strict'
import test from 'node:test'
import type { MessageVersion } from '../types'
import {
  appendImageUrls,
  buildRegenerationMessages,
  createUserMessage,
  getTextContent,
} from './message-utils'
import { normalizeStoredMessages } from './storage'

test('does not duplicate existing image parts when text is empty', () => {
  const content = [
    { type: 'text' as const, text: '' },
    { type: 'image_url' as const, image_url: { url: 'https://old.test/a' } },
  ]

  assert.deepEqual(appendImageUrls(content, ['https://new.test/b']), [
    { type: 'text', text: '' },
    { type: 'image_url', image_url: { url: 'https://old.test/a' } },
    { type: 'image_url', image_url: { url: 'https://new.test/b' } },
  ])
})

test('preserves text, filters duplicate images, and appends new images', () => {
  const content = [
    { type: 'text' as const, text: 'hello' },
    { type: 'image_url' as const, image_url: { url: 'https://old.test/a' } },
  ]

  assert.deepEqual(
    appendImageUrls(content, [
      ' https://old.test/a ',
      'https://new.test/b',
      ' ',
    ]),
    [
      { type: 'text', text: 'hello' },
      { type: 'image_url', image_url: { url: 'https://old.test/a' } },
      { type: 'image_url', image_url: { url: 'https://new.test/b' } },
    ]
  )
})

test('preserves every text part when appending images', () => {
  const content = [
    { type: 'text' as const, text: 'first' },
    { type: 'text' as const, text: 'second' },
  ]

  assert.deepEqual(appendImageUrls(content, ['https://new.test/image']), [
    { type: 'text', text: 'first' },
    { type: 'text', text: 'second' },
    { type: 'image_url', image_url: { url: 'https://new.test/image' } },
  ])
})

test('joins every text part for display and reasoning parsing', () => {
  assert.equal(
    getTextContent([
      { type: 'text', text: 'first' },
      { type: 'text', text: 'second' },
    ]),
    'firstsecond'
  )
})

test('normalizes stored sources and drops malformed content parts', () => {
  const [message] = normalizeStoredMessages([
    {
      role: 'assistant',
      versions: [
        {
          id: 'version-1',
          content: [
            { type: 'text', text: 'answer' },
            { type: 'image_url', image_url: { url: 'https://example.test/a' } },
            null,
          ] as unknown as MessageVersion['content'],
        },
      ],
      sources: [
        { href: 'https://example.test/source', title: 'Source' },
        { href: 'javascript:alert(1)', title: 'Unsafe' },
      ],
    },
  ] as unknown as Parameters<typeof normalizeStoredMessages>[0])

  assert.deepEqual(message.versions[0]?.content, [
    { type: 'text', text: 'answer' },
    { type: 'image_url', image_url: { url: 'https://example.test/a' } },
  ])
  assert.deepEqual(message.sources, [
    { href: 'https://example.test/source', title: 'Source' },
  ])
})

test('retries a user message by preserving history and creating a new turn', () => {
  const first = createUserMessage('first')
  const second = createUserMessage('second')
  const regenerated = buildRegenerationMessages([first, second], second.key)

  assert.ok(regenerated)
  assert.equal(regenerated.length, 3)
  assert.equal(regenerated[0].versions[0].content, 'first')
  assert.equal(regenerated[1].from, 'user')
  assert.equal(regenerated[1].versions[0].content, 'second')
  assert.equal(regenerated[2].from, 'assistant')
  assert.equal(regenerated[2].status, 'loading')
  assert.notEqual(regenerated[1].key, second.key)
})

test('retries assistant or system messages from the nearest preceding user turn', () => {
  const user = createUserMessage('question')
  const assistant = {
    ...buildRegenerationMessages([user], user.key)![1],
    from: 'assistant' as const,
    status: 'complete' as const,
    versions: [{ id: 'assistant', content: 'answer' }],
  }
  const regenerated = buildRegenerationMessages(
    [user, assistant],
    assistant.key
  )

  assert.ok(regenerated)
  assert.equal(regenerated.length, 2)
  assert.equal(regenerated[0].key, user.key)
  assert.equal(regenerated[1].from, 'assistant')
  assert.equal(regenerated[1].status, 'loading')
})
