import assert from 'node:assert/strict'
import test from 'node:test'
import {
  formatCustomRequestBody,
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
