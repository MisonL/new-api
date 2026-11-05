import assert from 'node:assert/strict'
import test from 'node:test'
import { installReadonlyXHRGuard } from './readonly-guard'

// 只读联调环境要求前端自己阻断写入（AGENTS.md 第 7 节）。此前这一层只 patch 了
// fetch 与 axios：playground 的流式请求走 sse.js，它内部用的是
// new XMLHttpRequest()，POST /v1/chat/completions 因此绕过了前端这一层，只剩
// nginx 代理一层保护。守卫必须是"一层"而不是"选择性接入"。

interface StubXHR {
  opened: { method: string; url: string } | null
  open(method: string, url: string): void
}

// 每个用例用全新的类：守卫是装在 XMLHttpRequest.prototype 上的，同一个类跨用例
// 复用会让前一个用例装好的包装继续生效，"未启用即无副作用"就无法被验证。
function makeStubXHR(): new () => StubXHR {
  return class {
    opened: { method: string; url: string } | null = null
    open(method: string, url: string): void {
      this.opened = { method, url }
    }
  }
}

function withStubXHR<T>(run: (Ctor: new () => StubXHR) => T): T {
  const Ctor = makeStubXHR()
  const original = (globalThis as { XMLHttpRequest?: unknown }).XMLHttpRequest
  ;(globalThis as { XMLHttpRequest?: unknown }).XMLHttpRequest = Ctor
  try {
    return run(Ctor)
  } finally {
    ;(globalThis as { XMLHttpRequest?: unknown }).XMLHttpRequest = original
  }
}

test('blocks unsafe XHR methods before the socket is opened', () => {
  withStubXHR((Ctor) => {
    installReadonlyXHRGuard({ enabled: true })
    const xhr = new Ctor()
    assert.throws(
      () => xhr.open('POST', '/v1/chat/completions'),
      /只读/,
      'POST 必须在 open 阶段就被拒绝'
    )
    assert.equal(xhr.opened, null, '被拒绝的请求不得进入原始 open')
  })
})

test('blocks GET endpoints that mutate state', () => {
  withStubXHR((Ctor) => {
    installReadonlyXHRGuard({ enabled: true })
    const xhr = new Ctor()
    // 这些端点注册为 GET 但会写库/打上游，代理的方法检查挡不住。
    assert.throws(() => xhr.open('GET', '/api/channel/test/7'), /只读/)
    assert.throws(() => xhr.open('GET', '/api/user/token'), /只读/)
    assert.throws(() => xhr.open('GET', '/api/oauth/github'), /只读/)
  })
})

test('leaves safe same-origin reads untouched', () => {
  withStubXHR((Ctor) => {
    installReadonlyXHRGuard({ enabled: true })
    const xhr = new Ctor()
    xhr.open('GET', '/api/status')
    assert.deepEqual(xhr.opened, { method: 'GET', url: '/api/status' })

    const head = new Ctor()
    head.open('HEAD', '/api/user/self')
    assert.deepEqual(head.opened, { method: 'HEAD', url: '/api/user/self' })
  })
})

test('is inert when read-only mode is off', () => {
  withStubXHR((Ctor) => {
    installReadonlyXHRGuard({ enabled: false })
    const xhr = new Ctor()
    xhr.open('POST', '/api/user/self')
    assert.deepEqual(xhr.opened, { method: 'POST', url: '/api/user/self' })
  })
})
