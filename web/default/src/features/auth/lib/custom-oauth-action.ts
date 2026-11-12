export type CustomOAuthProviderKind =
  'oauth_code' | 'cas' | 'jwt_direct' | 'trusted_header'

export type CustomOAuthAction =
  | {
      type: 'oauth_code'
      authorizationEndpoint: string
      clientId: string
      scopes: string
    }
  | { type: 'cas_start'; startPath: string }
  | { type: 'unsupported'; reason: string }
  | { type: 'misconfigured'; reason: string }

export type CustomOAuthProviderLike = {
  slug: string
  kind?: string
  browser_login_supported?: boolean
  client_id?: string
  authorization_endpoint?: string
  scopes?: string
}

/**
 * 决定自定义 OAuth provider 的浏览器登录入口。
 *
 * 后端按 kind 区分 oauth_code / cas / jwt_direct / trusted_header，并通过
 * /api/status 暴露 kind 与 browser_login_supported。此前前端对所有 provider
 * 一律拼 OAuth code URL：非 code 的兄弟要么静默无响应，要么被当成 code 流程
 * 处理。这里把分流规则收敛成一个纯函数，供登录按钮与点击处理共用。
 *
 * 不支持的入口返回 unsupported 而不是静默返回，让调用方给出可见提示。
 */
export function resolveCustomOAuthAction(
  provider: CustomOAuthProviderLike
): CustomOAuthAction {
  const kind = provider.kind ?? ''
  const browserLoginSupported = provider.browser_login_supported === true

  switch (kind) {
    case 'oauth_code': {
      const endpoint = (provider.authorization_endpoint ?? '').trim()
      const clientId = (provider.client_id ?? '').trim()
      if (!endpoint || !clientId) {
        return {
          type: 'misconfigured',
          reason: 'Missing authorization endpoint or client id',
        }
      }
      return {
        type: 'oauth_code',
        authorizationEndpoint: endpoint,
        clientId,
        scopes: (provider.scopes ?? '').trim(),
      }
    }
    case 'cas':
      if (!browserLoginSupported) {
        return {
          type: 'unsupported',
          reason: 'CAS provider is not available for browser login',
        }
      }
      return {
        type: 'cas_start',
        startPath: `/api/auth/external/${encodeURIComponent(provider.slug)}/cas/start`,
      }
    case 'trusted_header':
      return {
        type: 'unsupported',
        reason:
          'Trusted header login is provided by the upstream proxy, not this page',
      }
    case 'jwt_direct':
      if (!browserLoginSupported) {
        return {
          type: 'unsupported',
          reason: 'JWT provider is not available for browser login',
        }
      }
      return {
        type: 'unsupported',
        reason: 'JWT provider requires the client application to sign in',
      }
    default:
      return {
        type: 'unsupported',
        reason: 'Unsupported custom OAuth provider type',
      }
  }
}
/**
 * 该 provider 是否应生成可点击的浏览器登录按钮。
 *
 * 只放行真正能走通的交互入口（授权码流程与带浏览器登录能力的 CAS）：
 * 其余 kind 或配置不全的 provider 生成按钮就是死入口，点击后既无跳转
 * 也无提示。
 */
export function isInteractiveCustomOAuthProvider(
  provider: CustomOAuthProviderLike
): boolean {
  const action = resolveCustomOAuthAction(provider)
  return action.type === 'oauth_code' || action.type === 'cas_start'
}

/**
 * 构造自定义 OAuth provider 的起始跳转 URL，供登录与绑定共用。
 *
 * 绑定与登录走同一条回调链：回调页 /oauth/{slug} 用 slug 反查 provider，
 * 再请求 /api/oauth/{slug}。因此这里必须用 slug 而不是数字 id（后端按
 * slug 注册 GenericOAuthProvider，用 id 只会得到 unknown provider）。
 *
 * state 由调用方先取（/api/oauth/state），保证它已写入会话；CAS 的起始
 * 路径由服务端完成身份解析，同样需要携带 state。返回 null 表示该 provider
 * 没有可用的浏览器入口，调用方应给出可见失败而不是静默。
 */
export function buildCustomOAuthStartUrl(
  provider: CustomOAuthProviderLike,
  state: string,
  redirectUri: string
): string | null {
  const action = resolveCustomOAuthAction(provider)
  if (action.type === 'cas_start') {
    return `${action.startPath}?state=${encodeURIComponent(state)}`
  }
  if (action.type !== 'oauth_code') {
    return null
  }
  const url = new URL(action.authorizationEndpoint)
  url.searchParams.set('client_id', action.clientId)
  url.searchParams.set('redirect_uri', redirectUri)
  url.searchParams.set('response_type', 'code')
  url.searchParams.set('state', state)
  if (action.scopes) {
    url.searchParams.set('scope', action.scopes)
  }
  return url.toString()
}
