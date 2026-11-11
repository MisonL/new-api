import { useQuery } from '@tanstack/react-query'
import { resolveChatApiKeyFromApi } from '../lib/chat-api-key'

/**
 * Get the currently active API key for chat links.
 *
 * 列表接口返回的是掩码 key，不能直接当凭据；这里经 resolveChatApiKey
 * 选出启用的令牌并按 id 取真实 key，返回时补上 `sk-` 前缀（库内 key 不含前缀）。
 *
 * 真实 key 不长期驻留：staleTime 为 0，且一旦窗口失焦即视为过期，
 * 链接打开本身只会短暂复用同一次查询结果。
 */
export function useActiveChatKey(enabled: boolean) {
  return useQuery({
    queryKey: ['chat-active-key'],
    queryFn: async () => {
      const result = await resolveChatApiKeyFromApi()
      if (!result.ok) {
        throw new Error(result.error)
      }
      return `sk-${result.key}`
    },
    enabled,
    staleTime: 0,
    gcTime: 0,
    refetchOnWindowFocus: true,
  })
}
