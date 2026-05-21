import { useEffect, useState } from 'react'
import {
  DEFAULT_UNMOUNT_DELAY,
  getLazyMountUpdateDelay,
  shouldRenderLazyMount,
} from './lazy-mount-state'

type LazyMountProps = {
  open: boolean
  delay?: number
  children: React.ReactNode
}

export function LazyMount({
  open,
  delay = DEFAULT_UNMOUNT_DELAY,
  children,
}: LazyMountProps) {
  const [mounted, setMounted] = useState(open)

  useEffect(() => {
    const timer = window.setTimeout(
      () => {
        setMounted(open)
      },
      getLazyMountUpdateDelay(open, delay)
    )

    return () => window.clearTimeout(timer)
  }, [delay, open])

  return shouldRenderLazyMount(open, mounted) ? children : null
}
