import { useEffect, useState } from 'react'

export function useRetainedValue<T>(value: T, active: boolean) {
  const [retainedValue, setRetainedValue] = useState(value)

  useEffect(() => {
    if (!active) {
      return
    }

    const timer = window.setTimeout(() => {
      setRetainedValue(value)
    }, 0)

    return () => window.clearTimeout(timer)
  }, [active, value])

  return active ? value : retainedValue
}
