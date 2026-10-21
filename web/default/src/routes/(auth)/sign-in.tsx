import { z } from 'zod'
import { createFileRoute, redirect } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { normalizeLoginRedirectTarget } from '@/features/auth/lib/redirect-target'
import { SignIn } from '@/features/auth/sign-in'

const searchSchema = z.object({
  redirect: z.string().optional(),
})

export const Route = createFileRoute('/(auth)/sign-in')({
  component: SignIn,
  validateSearch: searchSchema,
  beforeLoad: async ({ search }) => {
    const { auth } = useAuthStore.getState()

    // 如果已经有用户信息，说明已登录
    if (auth.user) {
      // 优先使用 redirect 参数（用户之前想去的地方）
      // 否则跳转到 dashboard。redirect 来自查询参数，必须先收敛成站内路径，
      // 否则已登录用户访问 /sign-in?redirect=https://evil 会被直接送出去。
      throw redirect({
        to: normalizeLoginRedirectTarget(search?.redirect) || '/dashboard',
      })
    }
  },
})
