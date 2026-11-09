import { useEffect } from 'react'
import { createFileRoute, useNavigate, useSearch } from '@tanstack/react-router'
import i18next from 'i18next'
import { toast } from 'sonner'
import { useAuthStore, type AuthUser } from '@/stores/auth-store'
import { getSelf } from '@/lib/api'
import { wechatLoginByCode } from '@/features/auth/api'
import { normalizeLoginRedirectTarget } from '@/features/auth/lib/redirect-target'

// 旧回调入口接受的 provider，与后端 OAuth 提供方保持一致。
const KNOWN_CALLBACK_PROVIDERS = new Set([
  'github',
  'discord',
  'oidc',
  'linuxdo',
  'telegram',
  'wechat',
])

function OAuthComponent() {
  const navigate = useNavigate()
  const search = useSearch({ from: '/(auth)/oauth' }) as {
    redirect?: string
    provider?: 'github' | 'discord' | 'oidc' | 'linuxdo' | 'telegram' | 'wechat'
    code?: string
    state?: string
  }

  useEffect(() => {
    ;(async () => {
      // 这是旧版回调入口：后端回调（github/discord/oidc 等）先建好会话，
      // 再让本页 getSelf 取用户。因此 provider 必须在已知回调列表里；
      // 缺失或伪造的 provider 不能拿当前浏览器已有的会话 setUser 后跳转，
      // 否则一条诱饵链接就能把人"送进"一个用户没有参与过的会话。
      if (!search?.provider || !KNOWN_CALLBACK_PROVIDERS.has(search.provider)) {
        toast.error(i18next.t('OAuth failed'))
        navigate({ to: '/sign-in', replace: true })
        return
      }
      try {
        if (search.provider === 'wechat') {
          // wechat 是客户端换码：没有 code 就无法完成登录。
          if (!search.code) {
            toast.error(i18next.t('OAuth failed'))
            navigate({ to: '/sign-in', replace: true })
            return
          }
          await wechatLoginByCode(search.code)
        }
        const res = await getSelf()
        if (res?.success) {
          useAuthStore.getState().auth.setUser(res.data as AuthUser)
          const target =
            normalizeLoginRedirectTarget(search?.redirect) || '/dashboard'
          navigate({ to: target, replace: true })
          return
        }
      } catch {
        /* empty */
      }
      toast.error(i18next.t('OAuth failed'))
      navigate({ to: '/sign-in', replace: true })
    })()
  }, [navigate, search])

  return null
}

export const Route = createFileRoute('/(auth)/oauth')({
  component: OAuthComponent,
})
