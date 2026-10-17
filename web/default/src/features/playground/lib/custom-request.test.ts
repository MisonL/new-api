import assert from 'node:assert/strict'
import test from 'node:test'
import {
  formatCustomRequestBody,
  parseCustomRequestBody,
  validateCustomRequestBody,
} from './custom-request'

test('accepts JSON objects and formats them', () => {
  assert.deepEqual(validateCustomRequestBody('{"model":"gpt-4o"}'), {
    valid: true,
    message: '',
  })
  assert.equal(
    formatCustomRequestBody('{"model":"gpt-4o"}'),
    '{\n  "model": "gpt-4o"\n}'
  )
})

test('rejects malformed JSON and non-object JSON', () => {
  assert.deepEqual(validateCustomRequestBody('{'), {
    valid: false,
    message: 'JSON format error',
  })
  assert.deepEqual(validateCustomRequestBody('[]'), {
    valid: false,
    message: 'JSON must be an object',
  })
  assert.throws(() => formatCustomRequestBody('[]'), {
    message: 'JSON must be an object',
  })
})

test('parses a JSON object body for sending', () => {
  assert.deepEqual(
    parseCustomRequestBody('{"model":"gpt-4o","stream":false}'),
    {
      model: 'gpt-4o',
      stream: false,
    }
  )
})

// The send path must refuse rather than silently fall back to the generated
// payload: the panel promises the body is sent as-is, so a null here has to
// become a visible error, not a request the user did not ask for.
test('returns null for bodies that cannot be sent as-is', () => {
  assert.equal(parseCustomRequestBody(''), null)
  assert.equal(parseCustomRequestBody('   '), null)
  assert.equal(parseCustomRequestBody('{'), null)
  assert.equal(parseCustomRequestBody('[]'), null)
  assert.equal(parseCustomRequestBody('"text"'), null)
})
