import { create } from 'zustand'

export type UserPermissions = {
  sidebar_settings?: boolean
  sidebar_modules?: Record<string, unknown>
}

export interface AuthUser {
  id: number
  username: string
  display_name?: string
  email?: string
  role: number
  status?: number
  group?: string
  quota?: number
  used_quota?: number
  request_count?: number
  aff_code?: string
  aff_count?: number
  aff_quota?: number
  aff_history_quota?: number
  inviter_id?: number
  github_id?: string
  oidc_id?: string
  wechat_id?: string
  telegram_id?: string
  linux_do_id?: string
  setting?: Record<string, unknown> | string
  stripe_customer?: string
  sidebar_modules?: string
  permissions?: UserPermissions
}

interface AuthState {
  auth: {
    user: AuthUser | null
    setUser: (user: AuthUser | null) => void
    reset: () => void
  }
}

export const useAuthStore = create<AuthState>()((set) => {
  // Restore user info from localStorage
  const initUser = (() => {
    try {
      if (typeof window !== 'undefined') {
        const saved = window.localStorage.getItem('user')
        return saved ? JSON.parse(saved) : null
      }
    } catch {
      // Clear dirty data when parsing fails
      if (typeof window !== 'undefined') {
        window.localStorage.removeItem('user')
      }
    }
    return null
  })()

  return {
    auth: {
      user: initUser,
      setUser: (user) =>
        set((state) => {
          // 持久化失败不能拖垮状态更新：Safari 无痕、存储配额耗尽或被策略
          // 禁用时 setItem 会抛异常。旧写法让它从 set() 里冒出去，调用点在
          // beforeLoad 中就会整页进入 errorComponent，用户看到 500 而非登录页。
          if (typeof window !== 'undefined') {
            try {
              if (user) {
                window.localStorage.setItem('user', JSON.stringify(user))
              } else {
                window.localStorage.removeItem('user')
              }
            } catch (_error) {
              // 内存中的 user 仍然有效，只是本会话不再跨刷新保留。
              void _error
            }
          }
          return { ...state, auth: { ...state.auth, user } }
        }),
      reset: () =>
        set((state) => {
          if (typeof window !== 'undefined') {
            try {
              window.localStorage.removeItem('user')
            } catch (_error) {
              void _error
            }
          }
          return {
            ...state,
            auth: { ...state.auth, user: null },
          }
        }),
    },
  }
})
