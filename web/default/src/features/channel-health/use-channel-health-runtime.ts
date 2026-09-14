import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '@/lib/api'
import {
  getChannelHealthRuntimeRefreshDelay,
  type ChannelHealthRuntimeStatus,
} from '@/features/system-settings/general/channel-health-runtime'

type ChannelHealthRuntimeResponse = {
  success: boolean
  message?: string
  data?: ChannelHealthRuntimeStatus
}

const RUNTIME_REQUEST_TIMEOUT_MS = 8000

const channelHealthRuntimeRequestConfig = {
  timeout: RUNTIME_REQUEST_TIMEOUT_MS,
  skipBusinessError: true,
  skipErrorHandler: true,
}

export function useChannelHealthRuntime(props: { enabled: boolean }) {
  const [runtime, setRuntime] = useState<ChannelHealthRuntimeStatus | null>(
    null
  )
  const [runtimeUnavailable, setRuntimeUnavailable] = useState(false)
  const [runtimeUpdatedAt, setRuntimeUpdatedAt] = useState<Date | null>(null)
  const [runtimeLoading, setRuntimeLoading] = useState(false)
  const runtimeRequestRef = useRef<Promise<void> | null>(null)
  const runtimeRequestVisibleRef = useRef(false)
  const runtimeSnapshotRef = useRef<ChannelHealthRuntimeStatus | null>(null)

  const refresh = useCallback(
    async (options: { silent?: boolean } = {}): Promise<void> => {
      if (!props.enabled) return

      if (!options.silent) {
        runtimeRequestVisibleRef.current = true
        setRuntimeLoading(true)
      }
      if (runtimeRequestRef.current) return runtimeRequestRef.current

      const request = api
        .get<ChannelHealthRuntimeResponse>(
          '/api/option/channel_health_status',
          channelHealthRuntimeRequestConfig
        )
        .then((response) => {
          if (!response.data.success || !response.data.data) {
            throw new Error(
              response.data.message || 'Failed to load channel health runtime'
            )
          }
          runtimeSnapshotRef.current = response.data.data
          setRuntime(response.data.data)
          setRuntimeUnavailable(false)
          setRuntimeUpdatedAt(new Date())
        })
        .catch((error: unknown) => {
          runtimeSnapshotRef.current = null
          setRuntime(null)
          setRuntimeUnavailable(true)
          setRuntimeUpdatedAt(null)
          throw error
        })
        .finally(() => {
          runtimeRequestRef.current = null
          const shouldClearRuntimeLoading = runtimeRequestVisibleRef.current
          runtimeRequestVisibleRef.current = false
          if (shouldClearRuntimeLoading) setRuntimeLoading(false)
        })

      runtimeRequestRef.current = request
      return request
    },
    [props.enabled]
  )

  useEffect(() => {
    if (props.enabled) return
    queueMicrotask(() => {
      setRuntime(null)
      runtimeSnapshotRef.current = null
      runtimeRequestVisibleRef.current = false
      setRuntimeUnavailable(false)
      setRuntimeUpdatedAt(null)
      setRuntimeLoading(false)
    })
  }, [props.enabled])

  useEffect(() => {
    if (!props.enabled) return

    let disposed = false
    let timer: number | undefined

    const clearScheduledRefresh = () => {
      if (timer !== undefined) {
        window.clearTimeout(timer)
        timer = undefined
      }
    }

    const scheduleRefresh = async () => {
      if (document.visibilityState === 'hidden') return
      try {
        await refresh({ silent: true })
      } catch {
        // The UI clears stale state and renders the unavailable condition.
      }
      if (!disposed) {
        const refreshDelay = getChannelHealthRuntimeRefreshDelay(
          runtimeSnapshotRef.current
        )
        timer = window.setTimeout(scheduleRefresh, refreshDelay)
      }
    }

    const handleVisibilityChange = () => {
      if (document.visibilityState === 'hidden') {
        clearScheduledRefresh()
        return
      }
      clearScheduledRefresh()
      void scheduleRefresh()
    }

    document.addEventListener('visibilitychange', handleVisibilityChange)
    void scheduleRefresh()
    return () => {
      disposed = true
      document.removeEventListener('visibilitychange', handleVisibilityChange)
      clearScheduledRefresh()
    }
  }, [props.enabled, refresh])

  return {
    runtime,
    runtimeUnavailable,
    runtimeUpdatedAt,
    runtimeLoading,
    refresh,
  }
}
