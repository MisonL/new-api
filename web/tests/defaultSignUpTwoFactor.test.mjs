import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'

const readSource = (path) => readFileSync(new URL(path, import.meta.url), 'utf8')

// The server answers a two-factor login exit with
// {success:true,data:{require_2fa:true}} and no session
// (controller/user.go setupLoginWithResult). The sign-in wechat exit already
// branches on it; the sign-up wechat exit must too, or it shows a success
// toast and redirects to the dashboard with no session.
test('web/default sign-up wechat exit opens the 2FA challenge', () => {
  const source = readSource(
    '../default/src/features/auth/sign-up/components/sign-up-form.tsx'
  )

  assert.match(
    source,
    /redirectTo2FA/,
    'sign-up form must use the 2FA redirect helper'
  )
  assert.match(
    source,
    /if \(\(res\.data as \{ require_2fa\?: boolean \} \| null\)\?\.require_2fa\) \{[\s\S]*?redirectTo2FA\(\)[\s\S]*?return/,
    'the wechat exit must branch on require_2fa and return before signing in'
  )
})
