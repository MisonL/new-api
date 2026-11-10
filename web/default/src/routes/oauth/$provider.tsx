import { useEffect, useState } from 'react'
import type { AxiosRequestConfig } from 'axios'
import {
  createFileRoute,
  useNavigate,
  useParams,
  useSearch,
} from '@tanstack/react-router'
import i18next from 'i18next'
import { toast } from 'sonner'
import { useAuthStore, type AuthUser } from '@/stores/auth-store'
import { api, getSelf } from '@/lib/api'
import { OAUTH_INTENT_STORAGE_KEY } from '@/lib/oauth'
import { OAuthCallbackScreen } from '@/features/auth/components/oauth-callback-screen'
import { OAUTH_BIND_STORAGE_KEY } from '@/features/auth/constants'
import { useAuthRedirect } from '@/features/auth/hooks/use-auth-redirect'
import {
  OAUTH_BIND_SUCCESS_PATH,
  resolveOAuthCallbackOutcome,
} from '@/features/auth/lib/oauth-callback-outcome'
import { normalizeLoginRedirectTarget } from '@/features/auth/lib/redirect-target'

// 回调页曾经用 window.opener 判断绑定/登录，这要求弹窗保留对打开者的
// 引用——正是 noopener 要去掉的东西。改读打开前写入的意图标记；标记缺失
// 时按登录处理（绑定流程由调用方自己发起，不会缺标记）。
function resolveOAuthMode(): 'login' | 'bind' {
  if (typeof window === 'undefined') return 'login'
  try {
    return window.localStorage.getItem(OAUTH_INTENT_STORAGE_KEY) === 'bind'
      ? 'bind'
      : 'login'
  } catch (_error) {
    void _error
    return 'login'
  }
}

function clearOAuthIntent(): void {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.removeItem(OAUTH_INTENT_STORAGE_KEY)
  } catch (_error) {
    void _error
  }
}

type OAuthRequestConfig = AxiosRequestConfig & {
  skipBusinessError?: boolean
}

function OAuthCallback() {
  const navigate = useNavigate()
  const { redirectTo2FA } = useAuthRedirect()
  const { provider } = useParams({ from: '/oauth/$provider' }) as {
    provider: string
  }
  const search = useSearch({ from: '/oauth/$provider' }) as {
    code?: string
    state?: string
    redirect?: string
  }
  const [mode, setMode] = useState<'login' | 'bind'>(resolveOAuthMode)

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setMode(resolveOAuthMode())
  }, [])

  useEffect(() => {
    ;(async () => {
      const safeNavigate = (target: string) => {
        navigate({ to: target as never, replace: true })
        if (typeof window !== 'undefined') {
          setTimeout(() => {
            const normalizedTarget = target.startsWith('/')
              ? target
              : `/${target}`
            const currentPath =
              window.location.pathname + window.location.search
            if (
              currentPath !== normalizedTarget &&
              currentPath !== `${normalizedTarget}/`
            ) {
              // target 来自 ?redirect= 查询参数，未经校验时会跳到外部站点。
              // 这里只接受站内路径，其余回落到默认页。
              const safeTarget = normalizeLoginRedirectTarget(target)
              window.location.replace(safeTarget || '/dashboard')
            }
          }, 100)
        }
      }

      if (!search?.code) {
        toast.error(i18next.t('Missing code'))
        safeNavigate('/sign-in')
        return
      }
      const isBindingFlow = resolveOAuthMode() === 'bind'
      if (isBindingFlow && mode !== 'bind') {
        setMode('bind')
      } else if (!isBindingFlow && mode !== 'login') {
        setMode('login')
      }
      const notifyBindingResult = (status: 'success' | 'error') => {
        if (typeof window === 'undefined') return
        try {
          window.localStorage.setItem(
            OAUTH_BIND_STORAGE_KEY,
            JSON.stringify({
              provider,
              status,
              timestamp: Date.now(),
            })
          )
        } catch (_error) {
          // ignore storage write failures
          void _error
        }
      }

      const closeBindingWindow = () => {
        if (typeof window === 'undefined') return
        window.close()
        setTimeout(() => {
          if (!window.closed) {
            window.location.replace('/_authenticated/profile/')
          }
        }, 200)
      }

      const finalizeLogin = async (): Promise<boolean> => {
        try {
          const selfResponse = (await getSelf()) as {
            success?: boolean
            data?: AuthUser | null
          }
          if (selfResponse?.success && selfResponse.data) {
            useAuthStore.getState().auth.setUser(selfResponse.data)
            try {
              if (
                typeof window !== 'undefined' &&
                selfResponse.data?.id != null
              ) {
                window.localStorage.setItem('uid', String(selfResponse.data.id))
              }
            } catch (_error) {
              void _error
            }
            return true
          }
        } catch (_error) {
          void _error
        }
        return false
      }

      const redirectAfterLogin = (target?: string) => {
        const to =
          normalizeLoginRedirectTarget(target || search?.redirect) ||
          '/dashboard'
        safeNavigate(to)
        toast.success(i18next.t('Signed in successfully!'))
      }

      const handleBindingFailure = (message: string) => {
        notifyBindingResult('error')
        clearOAuthIntent()
        toast.error(message)
      }

      const handleLoginFailure = async (message: string) => {
        if (await finalizeLogin()) {
          redirectAfterLogin()
          return
        }
        toast.error(message)
        safeNavigate('/sign-in')
      }

      try {
        const config: OAuthRequestConfig = {
          params: { code: search.code, state: search.state },
          skipBusinessError: true,
        }
        const res = await api.get(`/api/oauth/${provider}`, config)
        if (res?.data?.success) {
          const outcome = resolveOAuthCallbackOutcome(res.data?.data)
          // 桌面端 login 的回调跑在系统浏览器里，后端不在这里签发会话；
          // 会话由桌面应用自己轮询 handoff 拿到。提示用户切回应用即可。
          if (outcome.kind === 'desktop_confirm_required') {
            // 桌面端登录/绑定要求浏览器侧确认：整页跳到网关确认页，
            // 由用户输入桌面应用中显示的确认码。确认页由后端渲染，
            // SPA 管不到它，因此把「这次是绑定」的意图写进 localStorage，
            // 由确认页在成功后读标记并尝试关闭窗口。
            if (outcome.action === 'bind') {
              try {
                window.localStorage.setItem(
                  OAUTH_INTENT_STORAGE_KEY,
                  'bind'
                )
              } catch (_error) {
                void _error
              }
            }
            window.location.assign(outcome.confirmUrl)
            return
          }
          if (outcome.kind === 'desktop_handoff') {
            toast.success(
              i18next.t('Authorization complete. Return to the desktop app.')
            )
            return
          }
          // 二次验证挑战：后端没有签发会话，data 只是 { require_2fa: true }。
          // 把它当用户对象写进 auth store 会让后续 getSelf 失败、被弹回登录页。
          if (outcome.kind === 'require_2fa') {
            redirectTo2FA(search?.redirect)
            return
          }
          // 绑定成功的判定只能用 data.action：message 是本地化文案，
          // 比较 message === 'bind' 在中文站点上永远不成立。
          if (outcome.kind === 'bind') {
            toast.success(i18next.t('Binding successful!'))
            notifyBindingResult('success')
            clearOAuthIntent()
            if (isBindingFlow) {
              // Close the callback window if we opened a new tab for binding
              closeBindingWindow()
            } else {
              safeNavigate(OAUTH_BIND_SUCCESS_PATH)
            }
            return
          }
          // Otherwise it's a login. 只有带 id 与 username 的载荷才是用户对象；
          // 空的 {} 表示后端没回用户，交给 finalizeLogin 用会话再确认一次。
          const loginUser = outcome.user
          const hasUserPayload =
            typeof loginUser.id === 'number' &&
            typeof loginUser.username === 'string'
          if (hasUserPayload) {
            useAuthStore.getState().auth.setUser(loginUser as AuthUser)
            try {
              if (typeof window !== 'undefined') {
                window.localStorage.setItem('uid', String(loginUser.id))
              }
            } catch (_error) {
              void _error
            }
            redirectAfterLogin()
            return
          }
          if (await finalizeLogin()) {
            redirectAfterLogin()
            return
          }
          toast.error(res?.data?.message || i18next.t('OAuth failed'))
          safeNavigate('/sign-in')
          return
        }
        const message = res?.data?.message || 'OAuth failed'
        if (!res?.data?.success && !isBindingFlow) {
          // When logging in with an already bound GitHub account, backend may return this message
          if (message === '该 GitHub 账户已被绑定') {
            if (await finalizeLogin()) {
              redirectAfterLogin()
              return
            }
          }
        }
        if (isBindingFlow) {
          handleBindingFailure(message)
        } else {
          await handleLoginFailure(message)
        }
        return
      } catch (error) {
        const message = ((error &&
          typeof error === 'object' &&
          'response' in error &&
          (error as { response?: { data?: { message?: string } } }).response
            ?.data?.message) ??
          (error instanceof Error ? error.message : undefined) ??
          'OAuth failed') as string

        if (isBindingFlow) {
          handleBindingFailure(message)
          return
        }
        await handleLoginFailure(message)
        return
      }
    })()
  }, [mode, navigate, provider, search, redirectTo2FA])

  return <OAuthCallbackScreen provider={provider} mode={mode} />
}

export const Route = createFileRoute('/oauth/$provider')({
  component: OAuthCallback,
})
