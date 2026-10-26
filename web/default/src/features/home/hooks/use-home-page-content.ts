import { useEffect, useState } from 'react'
import i18next from 'i18next'
import { toast } from 'sonner'
import { getHomePageContent } from '../api'
import type { HomePageContentResult } from '../types'

const STORAGE_KEY = 'home_page_content'

/**
 * Hook to load and manage custom home page content
 * Supports both Markdown/HTML content and iframe URLs
 */
export function useHomePageContent(): HomePageContentResult {
  const [content, setContent] = useState<string>('')
  const [isLoaded, setIsLoaded] = useState(false)

  useEffect(() => {
    let mounted = true

    const loadContent = async () => {
      // 缓存读写全部包在 try 内：存储被禁用时 getItem 会抛 SecurityError，
      // 旧写法把它放在 try 之前，Promise 直接 reject，finally 里的
      // setIsLoaded(true) 永不执行，首页永久停在 Loading。
      let cached: string | null = null
      try {
        cached = localStorage.getItem(STORAGE_KEY)
      } catch (_error) {
        void _error
      }
      if (cached && mounted) {
        setContent(cached)
      }

      try {
        const response = await getHomePageContent()
        const { success, data } = response

        if (!mounted) return

        if (success && data) {
          setContent(data)
          try {
            localStorage.setItem(STORAGE_KEY, data)
          } catch (_error) {
            void _error
          }
        } else {
          // Clear content if API returns empty
          setContent('')
          try {
            localStorage.removeItem(STORAGE_KEY)
          } catch (_error) {
            void _error
          }
        }
      } catch (error) {
        if (!mounted) return
        // eslint-disable-next-line no-console
        console.error('Failed to load home page content:', error)
        toast.error(i18next.t('Failed to load home page content'))
      } finally {
        if (mounted) {
          setIsLoaded(true)
        }
      }
    }

    loadContent()

    return () => {
      mounted = false
    }
  }, [])

  let isUrl = false
  try {
    const url = new URL(content)
    isUrl = url.protocol === 'http:' || url.protocol === 'https:'
  } catch {
    // not a URL
  }

  return { content, isLoaded, isUrl }
}
