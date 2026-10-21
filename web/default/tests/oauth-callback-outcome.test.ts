import { describe, expect, test } from 'bun:test'
import { resolveOAuthCallbackOutcome } from '../src/features/auth/lib/oauth-callback-outcome'

// 后端 GET /api/oauth/:provider 的成功响应有四种形态，判定依据只能是 data 里的
// 标记字段：message 是本地化文案（"绑定成功" / "Binding successful"），
// 拿它做相等比较在中文站点上永远不成立，绑定会被当成登录。

describe('oauth callback outcome', () => {
  test('bind response is recognized from the action field, not the message', () => {
    expect(
      resolveOAuthCallbackOutcome({
        action: 'bind',
        message: '绑定成功',
      })
    ).toEqual({ kind: 'bind' })
    expect(
      resolveOAuthCallbackOutcome({
        action: 'bind',
        message: 'Binding successful',
      })
    ).toEqual({ kind: 'bind' })
  })

  // 后端对开启二次验证的 OAuth 登录返回 { require_2fa: true } 且不签发会话。
  // 当成用户对象写进 auth store 会让后续 getSelf 失败、被弹回登录页。
  test('require_2fa is not mistaken for a logged-in user', () => {
    expect(resolveOAuthCallbackOutcome({ require_2fa: true })).toEqual({
      kind: 'require_2fa',
    })
  })

  test('desktop handoff is not mistaken for a logged-in user', () => {
    expect(
      resolveOAuthCallbackOutcome({ desktop_handoff: true, action: 'login' })
    ).toEqual({ kind: 'desktop_handoff' })
  })

  test('a user payload resolves to login', () => {
    const user = { id: 7, username: 'alice', role: 1 }
    expect(resolveOAuthCallbackOutcome(user)).toEqual({
      kind: 'login',
      user,
    })
  })

  test('missing payload degrades to an empty login result', () => {
    expect(resolveOAuthCallbackOutcome(null)).toEqual({ kind: 'login', user: {} })
    expect(resolveOAuthCallbackOutcome(undefined)).toEqual({
      kind: 'login',
      user: {},
    })
    expect(resolveOAuthCallbackOutcome([])).toEqual({ kind: 'login', user: {} })
  })
})
