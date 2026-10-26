import { createFileRoute, redirect } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { getSelf } from '@/lib/api'
import { AuthenticatedLayout } from '@/components/layout'

// 内存中的验证标记，避免同一会话中重复验证
let sessionVerified = false

function statusOf(error: unknown): number | undefined {
  return (error as { response?: { status?: number } } | undefined)?.response
    ?.status
}

export const Route = createFileRoute('/_authenticated')({
  beforeLoad: async ({ location }) => {
    const { auth } = useAuthStore.getState()

    // 如果本地没有用户信息，直接跳转登录页
    if (!auth.user) {
      throw redirect({
        to: '/sign-in',
        search: { redirect: location.href },
      })
    }

    // 本地有用户信息，但需要验证 session 是否有效（每个会话只验证一次）
    if (!sessionVerified) {
      let res
      try {
        res = await getSelf()
      } catch (error) {
        // 只有服务端明确说"未认证"才清本地会话。网络抖动、超时、502
        // 都不代表会话失效，此时登出会让用户在自家网络抖动时被踢出，
        // 且服务端会话其实仍然有效。放行本次渲染，下次导航重试校验。
        if (statusOf(error) === 401) {
          auth.reset()
          throw redirect({
            to: '/sign-in',
            search: { redirect: location.href },
          })
        }
        return
      }

      if (res?.success && res.data) {
        // 验证成功，更新用户信息（可能有变化）
        auth.setUser(res.data)
        sessionVerified = true
        return
      }

      // 200 但业务失败（例如 success=false）按未登录处理。
      auth.reset()
      throw redirect({
        to: '/sign-in',
        search: { redirect: location.href },
      })
    }
  },
  component: AuthenticatedLayout,
})
