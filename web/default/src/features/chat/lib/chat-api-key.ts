import { fetchTokenKey, getApiKeys } from '@/features/keys/api'
import { API_KEY_STATUS } from '@/features/keys/constants'

export type ChatApiKeyResult =
  { ok: true; key: string } | { ok: false; error: string }

type ChatApiKeyDeps = {
  fetchKeys: () => Promise<{
    success: boolean
    message?: string
    items: { id: number; status: number }[]
  }>
  fetchRealKey: (
    id: number
  ) => Promise<{ success: boolean; message?: string; key?: string }>
}

/**
 * 解析聊天链接要用的 API key。
 *
 * 令牌列表/详情接口只返回掩码 key，掩码拿去鉴权必然失败；真实 key 只能通过
 * 按当前用户校验归属的取 key 接口拿到。因此这里先选出第一个启用的令牌，
 * 只对它按 id 取真实 key，绝不把列表里的掩码值当成凭据。
 *
 * 返回时不补 `sk-` 前缀：库内 key 不含前缀，拼接由调用方按既有约定处理。
 */
export async function resolveChatApiKey(
  deps: ChatApiKeyDeps
): Promise<ChatApiKeyResult> {
  const list = await deps.fetchKeys()
  if (!list.success) {
    return {
      ok: false,
      error: list.message || 'Failed to load API keys',
    }
  }
  const active = list.items.find(
    (item) => item.status === API_KEY_STATUS.ENABLED
  )
  if (!active) {
    return {
      ok: false,
      error: 'No enabled API key available. Please enable an API key first.',
    }
  }
  const real = await deps.fetchRealKey(active.id)
  if (!real.success || !real.key) {
    return {
      ok: false,
      error: real.message || 'Failed to resolve the API key',
    }
  }
  return { ok: true, key: real.key }
}

export function resolveChatApiKeyFromApi(): Promise<ChatApiKeyResult> {
  return resolveChatApiKey({
    fetchKeys: async () => {
      const result = await getApiKeys({ p: 1, size: 50 })
      return {
        success: result.success,
        message: result.message,
        items: result.data?.items ?? [],
      }
    },
    fetchRealKey: async (id) => {
      const result = await fetchTokenKey(id)
      if (!result.success) {
        return { success: false, message: result.message }
      }
      return { success: result.success, key: result.data?.key }
    },
  })
}
