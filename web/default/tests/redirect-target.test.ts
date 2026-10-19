import { describe, expect, test } from 'bun:test'
import { normalizeLoginRedirectTarget } from '../src/features/auth/lib/redirect-target'

// 登录后的跳转目标来自 URL 查询参数（/oauth/<provider>?redirect=...、/otp?redirect=...）。
// 未校验时 window.location.replace(target) 会把用户带去外部站点，构成开放重定向。
// 期望：只接受站内绝对路径，其他一律回落到默认值。
describe('normalizeLoginRedirectTarget', () => {
  test('keeps a plain site-relative path', () => {
    expect(normalizeLoginRedirectTarget('/dashboard')).toBe('/dashboard')
    expect(normalizeLoginRedirectTarget('/console/log?a=1#b')).toBe(
      '/console/log?a=1#b'
    )
  })

  test('rejects absolute URLs on another origin', () => {
    expect(normalizeLoginRedirectTarget('https://evil.example/x')).toBe('')
    expect(normalizeLoginRedirectTarget('http://evil.example')).toBe('')
    expect(normalizeLoginRedirectTarget('HTTPS://EVIL.example/p')).toBe('')
  })

  test('rejects protocol-relative and backslash tricks', () => {
    expect(normalizeLoginRedirectTarget('//evil.example/x')).toBe('')
    expect(normalizeLoginRedirectTarget('/\\evil.example/x')).toBe('')
    expect(normalizeLoginRedirectTarget('\\\\evil.example/x')).toBe('')
  })

  test('rejects javascript and data schemes', () => {
    expect(normalizeLoginRedirectTarget('javascript:alert(1)')).toBe('')
    expect(normalizeLoginRedirectTarget('data:text/html,<script>x</script>')).toBe(
      ''
    )
    expect(normalizeLoginRedirectTarget(' /dashboard')).toBe('/dashboard')
  })

  test('keeps dot segments inside the site instead of escaping it', () => {
    // 这些值经过 URL 解析后仍是同源路径，不是开放重定向；关键是结果必须仍以
    // 单个 "/" 开头，不能变成 "//host" 或带 scheme。
    for (const target of ['/a/../../evil', '/..%2f..%2fevil', '/./x']) {
      const result = normalizeLoginRedirectTarget(target)
      expect(result === '' || (result.startsWith('/') && !result.startsWith('//'))).toBe(true)
    }
    expect(normalizeLoginRedirectTarget('/a/../../evil')).toBe('/evil')
  })

  test('rejects empty and non-string input', () => {
    expect(normalizeLoginRedirectTarget('')).toBe('')
    expect(normalizeLoginRedirectTarget('   ')).toBe('')
    expect(normalizeLoginRedirectTarget(undefined)).toBe('')
    expect(normalizeLoginRedirectTarget(null)).toBe('')
    expect(normalizeLoginRedirectTarget(42)).toBe('')
  })

  test('rejects the auth pages themselves to avoid a loop', () => {
    expect(normalizeLoginRedirectTarget('/sign-in')).toBe('')
    expect(normalizeLoginRedirectTarget('/sign-up')).toBe('')
  })
})
