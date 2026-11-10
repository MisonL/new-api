import assert from 'node:assert/strict'
import test from 'node:test'
import { resolveOAuthCallbackOutcome } from './oauth-callback-outcome'

test('keeps bind action on desktop confirm_required', () => {
  const outcome = resolveOAuthCallbackOutcome({
    desktop_handoff: true,
    action: 'bind',
    confirm_required: true,
    confirm_url: '/api/oauth/desktop/confirm?state=abc',
  })
  assert.deepEqual(outcome, {
    kind: 'desktop_confirm_required',
    confirmUrl: '/api/oauth/desktop/confirm?state=abc',
    action: 'bind',
  })
})

test('keeps login action on desktop confirm_required', () => {
  const outcome = resolveOAuthCallbackOutcome({
    desktop_handoff: true,
    action: 'login',
    confirm_required: true,
    confirm_url: '/api/oauth/desktop/confirm?state=abc',
  })
  assert.deepEqual(outcome, {
    kind: 'desktop_confirm_required',
    confirmUrl: '/api/oauth/desktop/confirm?state=abc',
    action: 'login',
  })
})

test('omits action when backend does not send one', () => {
  const outcome = resolveOAuthCallbackOutcome({
    desktop_handoff: true,
    confirm_required: true,
    confirm_url: '/api/oauth/desktop/confirm?state=abc',
  })
  assert.deepEqual(outcome, {
    kind: 'desktop_confirm_required',
    confirmUrl: '/api/oauth/desktop/confirm?state=abc',
  })
})

test('still resolves plain desktop handoff without confirmation', () => {
  const outcome = resolveOAuthCallbackOutcome({ desktop_handoff: true })
  assert.deepEqual(outcome, { kind: 'desktop_handoff' })
})
