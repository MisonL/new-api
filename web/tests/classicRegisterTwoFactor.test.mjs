import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'

const readSource = (path) => readFileSync(new URL(path, import.meta.url), 'utf8')

// The server answers a two-factor login exit with
// {success:true,data:{require_2fa:true}} and does not issue a session
// (controller/user.go setupLoginWithResult). Every classic login exit must
// recognise that body and open the challenge modal instead of dispatching a
// session; RegisterForm was the one exit still treating the challenge as a
// completed login.
test('classic RegisterForm opens the 2FA challenge on every login exit', () => {
  const source = readSource('../classic/src/components/auth/RegisterForm.jsx')

  assert.match(
    source,
    /require_2fa/,
    'RegisterForm must recognise the require_2fa challenge body'
  )
  assert.match(
    source,
    /setShowTwoFA\(true\)/,
    'RegisterForm must open the two-factor modal on the challenge'
  )
  assert.match(
    source,
    /<TwoFAVerification/,
    'RegisterForm must render the two-factor verification component'
  )

  // Each exit that dispatches a session must first branch on the challenge:
  // the wechat code exchange, the browser/desktop OAuth result, and telegram.
  const dispatchSites = source.match(/userDispatch\(\{ type: 'login'/g) ?? []
  const challengeSites = source.match(/require_2fa/g) ?? []
  assert.ok(
    dispatchSites.length >= 3,
    `expected at least three session-dispatch exits, found ${dispatchSites.length}`
  )
  assert.ok(
    challengeSites.length >= dispatchSites.length,
    `every session-dispatch exit needs a challenge guard: ${challengeSites.length} guards for ${dispatchSites.length} exits`
  )
})

// The signed-in dispatch must not run on the challenge body. The guarded
// branches return before dispatching, so a session is never written for a
// response that carries no session.
test('classic RegisterForm returns before dispatching on the challenge', () => {
  const source = readSource('../classic/src/components/auth/RegisterForm.jsx')

  assert.match(
    source,
    /if \(data && data\.require_2fa\) \{[\s\S]*?setShowTwoFA\(true\)[\s\S]*?return/,
    'the wechat exit must return before dispatching a session'
  )
  assert.match(
    source,
    /if \(result\.user\.require_2fa\) \{[\s\S]*?setShowTwoFA\(true\)[\s\S]*?return/,
    'the browser OAuth exit must return before dispatching a session'
  )
})
