import assert from 'node:assert/strict'
import test from 'node:test'
import {
  buildCustomOAuthStartUrl,
  isInteractiveCustomOAuthProvider,
  resolveCustomOAuthAction,
} from './custom-oauth-action'

type Provider = {
  name: string
  slug: string
  kind?: string
  browser_login_supported?: boolean
  client_id?: string
  authorization_endpoint?: string
  scopes?: string
}

function provider(overrides: Partial<Provider> = {}): Provider {
  return { name: 'Corp', slug: 'corp', ...overrides }
}

test('oauth_code provider with endpoint and client id starts the authorization code flow', () => {
  const action = resolveCustomOAuthAction(
    provider({
      kind: 'oauth_code',
      browser_login_supported: true,
      client_id: 'client-1',
      authorization_endpoint: 'https://idp.example.com/authorize',
      scopes: 'openid profile',
    })
  )

  assert.equal(action.type, 'oauth_code')
  assert.equal(
    action.type === 'oauth_code' && action.authorizationEndpoint,
    'https://idp.example.com/authorize'
  )
})

test('cas provider starts the server-side CAS start route with the current state', () => {
  const action = resolveCustomOAuthAction(
    provider({ kind: 'cas', browser_login_supported: true, slug: 'corp-cas' })
  )

  assert.equal(action.type, 'cas_start')
  assert.equal(
    action.type === 'cas_start' && action.startPath,
    '/api/auth/external/corp-cas/cas/start'
  )
})

test('trusted_header provider cannot start an interactive login', () => {
  const action = resolveCustomOAuthAction(
    provider({ kind: 'trusted_header', browser_login_supported: false })
  )

  assert.equal(action.type, 'unsupported')
  assert.match(
    action.type === 'unsupported' ? action.reason : '',
    /trusted header/i
  )
})

test('provider that reports browser login unsupported is never treated as oauth_code', () => {
  const action = resolveCustomOAuthAction(
    provider({
      kind: 'jwt_direct',
      browser_login_supported: false,
      client_id: '',
      authorization_endpoint: '',
    })
  )

  assert.equal(action.type, 'unsupported')
})

test('oauth_code provider missing endpoint or client id fails visibly instead of returning silently', () => {
  const action = resolveCustomOAuthAction(
    provider({
      kind: 'oauth_code',
      browser_login_supported: false,
      client_id: '',
      authorization_endpoint: '',
    })
  )

  assert.equal(action.type, 'misconfigured')
  assert.match(
    action.type === 'misconfigured' ? action.reason : '',
    /client id|authorization endpoint/i
  )
})

test('unknown kind is reported as unsupported rather than guessed', () => {
  const action = resolveCustomOAuthAction(
    provider({ kind: 'saml', browser_login_supported: true })
  )

  assert.equal(action.type, 'unsupported')
})
test('only interactive kinds produce a login button', () => {
  assert.equal(
    isInteractiveCustomOAuthProvider(
      provider({
        kind: 'oauth_code',
        browser_login_supported: true,
        client_id: 'client-1',
        authorization_endpoint: 'https://idp.example.com/authorize',
      })
    ),
    true
  )
  assert.equal(
    isInteractiveCustomOAuthProvider(
      provider({ kind: 'cas', browser_login_supported: true })
    ),
    true
  )
  // 缺配置的 oauth_code 不应生成按钮：点了也发不出请求。
  assert.equal(
    isInteractiveCustomOAuthProvider(
      provider({ kind: 'oauth_code', browser_login_supported: false })
    ),
    false
  )
  assert.equal(
    isInteractiveCustomOAuthProvider(
      provider({ kind: 'trusted_header', browser_login_supported: false })
    ),
    false
  )
  assert.equal(
    isInteractiveCustomOAuthProvider(provider({ kind: 'jwt_direct' })),
    false
  )
})

test('oauth_code start URL carries client id, redirect, response type and state', () => {
  const url = buildCustomOAuthStartUrl(
    provider({
      kind: 'oauth_code',
      client_id: 'client-1',
      authorization_endpoint: 'https://idp.example.com/authorize',
      scopes: 'openid profile',
    }),
    'state-123',
    'http://localhost:5173/oauth/corp?bind=true'
  )

  assert.ok(url)
  const parsed = new URL(url)
  assert.equal(
    parsed.origin + parsed.pathname,
    'https://idp.example.com/authorize'
  )
  assert.equal(parsed.searchParams.get('client_id'), 'client-1')
  assert.equal(parsed.searchParams.get('state'), 'state-123')
  assert.equal(parsed.searchParams.get('response_type'), 'code')
  assert.equal(
    parsed.searchParams.get('redirect_uri'),
    'http://localhost:5173/oauth/corp?bind=true'
  )
  assert.equal(parsed.searchParams.get('scope'), 'openid profile')
})

test('cas start URL uses the slug-keyed server route with the state', () => {
  const url = buildCustomOAuthStartUrl(
    provider({
      kind: 'cas',
      browser_login_supported: true,
      slug: 'corp-cas',
    }),
    'state-123',
    'http://localhost:5173/oauth/corp-cas'
  )

  assert.equal(url, '/api/auth/external/corp-cas/cas/start?state=state-123')
})

test('start URL is null for providers without a browser entry', () => {
  assert.equal(
    buildCustomOAuthStartUrl(
      provider({ kind: 'trusted_header' }),
      's',
      'http://localhost/oauth/corp'
    ),
    null
  )
  assert.equal(
    buildCustomOAuthStartUrl(
      provider({ kind: 'jwt_direct', browser_login_supported: false }),
      's',
      'http://localhost/oauth/corp'
    ),
    null
  )
  assert.equal(
    buildCustomOAuthStartUrl(
      provider({ kind: 'oauth_code', browser_login_supported: false }),
      's',
      'http://localhost/oauth/corp'
    ),
    null
  )
})
