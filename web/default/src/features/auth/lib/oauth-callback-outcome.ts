// OAuth 回调响应的语义判定。
//
// 统一 OAuth 路由 GET /api/oauth/:provider 有三种成功形态：
//   - 登录：data 是用户对象（id/username/role/...）
//   - 绑定：data 是 { action: 'bind' }，message 是本地化文案（"绑定成功" /
//     "Binding successful"），因此不能拿 message 判断
//   - 二次验证挑战：data 是 { require_2fa: true }，后端没有签发会话
//
// 外加桌面端 login 的 { desktop_handoff: true }：回调跑在系统浏览器里，
// 会话由桌面应用轮询 handoff 自行签发。

export type OAuthCallbackUserPayload = {
  id?: number | string
  username?: string
  role?: number
  [key: string]: unknown
}

export type OAuthCallbackOutcome =
  | { kind: 'bind' }
  | { kind: 'require_2fa' }
  | { kind: 'desktop_handoff' }
  | {
      kind: 'desktop_confirm_required'
      confirmUrl: string
      action?: 'login' | 'bind'
    }
  | { kind: 'login'; user: OAuthCallbackUserPayload }

export function resolveOAuthCallbackOutcome(
  data: unknown
): OAuthCallbackOutcome {
  if (!data || typeof data !== 'object' || Array.isArray(data)) {
    return { kind: 'login', user: {} }
  }
  const payload = data as Record<string, unknown>
  // 桌面端 handoff 必须排在最前：它的载荷里 action 可能是 'bind'，但这里
  // 不是「已完成的绑定」，而是「浏览器需先整页跳到确认页，确认后桌面端
  // 才拿到会话」。先判 bind 会让桌面绑定流程跳过确认、窗口也不会关闭。
  if (payload.desktop_handoff === true) {
    if (
      payload.confirm_required === true &&
      typeof payload.confirm_url === 'string' &&
      payload.confirm_url !== ''
    ) {
      const action =
        payload.action === 'bind' || payload.action === 'login'
          ? payload.action
          : undefined
      return {
        kind: 'desktop_confirm_required',
        confirmUrl: payload.confirm_url,
        ...(action ? { action } : {}),
      }
    }
    return { kind: 'desktop_handoff' }
  }
  if (payload.action === 'bind') {
    return { kind: 'bind' }
  }
  if (payload.require_2fa === true) {
    return { kind: 'require_2fa' }
  }
  return { kind: 'login', user: payload }
}

// 绑定成功后的落点。TanStack Router 的 navigate 接收的是路径，
// /_authenticated/profile/ 是路由 id 不是路径，直接跳会落到 NotFound。
export const OAUTH_BIND_SUCCESS_PATH = '/profile'
