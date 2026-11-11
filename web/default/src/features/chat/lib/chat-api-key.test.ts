import assert from 'node:assert/strict'
import test from 'node:test'
import { resolveChatApiKey } from './chat-api-key'

type ListItem = { id: number; status: number }

const ENABLED = 1
const DISABLED = 2
const EXPIRED = 3

function deps(items: ListItem[], realKeys: Record<number, string | null>) {
  const requested: number[] = []
  return {
    requested,
    fetchKeys: async () => ({ success: true, items }),
    fetchRealKey: async (id: number) => {
      requested.push(id)
      const key = realKeys[id]
      if (key === null || key === undefined) {
        return { success: false, message: 'token key lookup failed' }
      }
      return { success: true, key }
    },
  }
}

test('returns the real key of the first enabled token, not a masked list value', async () => {
  const d = deps(
    [
      { id: 11, status: DISABLED },
      { id: 12, status: ENABLED },
    ],
    { 12: 'realkey12' }
  )

  const result = await resolveChatApiKey(d)

  assert.equal(result.ok, true)
  // 列表接口只返回掩码，真实 key 必须按 id 单独取；前缀由调用方补。
  assert.equal(result.ok && result.key, 'realkey12')
  assert.deepEqual(d.requested, [12], '不得为未选中的令牌取真实 key')
})

test('fails clearly when no enabled token exists', async () => {
  const d = deps([{ id: 21, status: EXPIRED }], {})

  const result = await resolveChatApiKey(d)

  assert.equal(result.ok, false)
  assert.match(result.ok ? '' : result.error, /enabled/i)
  assert.deepEqual(d.requested, [], '没有启用令牌时不得请求任何真实 key')
})

test('surfaces the backend message when the real key lookup fails', async () => {
  const d = deps([{ id: 31, status: ENABLED }], { 31: null })

  const result = await resolveChatApiKey(d)

  assert.equal(result.ok, false)
  assert.equal(result.ok ? '' : result.error, 'token key lookup failed')
})

test('propagates list failures instead of returning a stale key', async () => {
  const result = await resolveChatApiKey({
    fetchKeys: async () => ({
      success: false,
      message: 'list failed',
      items: [],
    }),
    fetchRealKey: async () => {
      throw new Error('must not be called')
    },
  })

  assert.equal(result.ok, false)
  assert.equal(result.ok ? '' : result.error, 'list failed')
})
