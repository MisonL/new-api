type MediaQueryListener = (event: MediaQueryListEvent) => void

type MutableGlobal = typeof globalThis & {
  structuredClone?: <T>(value: T) => T
}

function cloneSerializableValue<T>(
  value: T,
  seen = new WeakMap<object, unknown>()
): T {
  if (value === null || typeof value !== 'object') return value

  if (seen.has(value)) return seen.get(value) as T

  if (value instanceof Date) return new Date(value.getTime()) as T
  if (value instanceof RegExp) return new RegExp(value) as T
  if (value instanceof Map) {
    const copy = new Map()
    seen.set(value, copy)
    value.forEach((mapValue, mapKey) => {
      copy.set(
        cloneSerializableValue(mapKey, seen),
        cloneSerializableValue(mapValue, seen)
      )
    })
    return copy as T
  }
  if (value instanceof Set) {
    const copy = new Set()
    seen.set(value, copy)
    value.forEach((setValue) => {
      copy.add(cloneSerializableValue(setValue, seen))
    })
    return copy as T
  }
  if (Array.isArray(value)) {
    const copy: unknown[] = []
    seen.set(value, copy)
    value.forEach((item, index) => {
      copy[index] = cloneSerializableValue(item, seen)
    })
    return copy as T
  }

  const copy = Object.create(Object.getPrototypeOf(value))
  seen.set(value, copy)
  for (const key of Reflect.ownKeys(value)) {
    copy[key] = cloneSerializableValue(
      (value as Record<PropertyKey, unknown>)[key],
      seen
    )
  }
  return copy
}

function installStructuredCloneFallback() {
  const target = globalThis as MutableGlobal
  if (typeof target.structuredClone === 'function') return
  target.structuredClone = (value) => cloneSerializableValue(value)
}

function installMatchMediaFallback() {
  if (typeof window === 'undefined') return

  if (typeof window.matchMedia !== 'function') {
    window.matchMedia = (query: string) =>
      ({
        matches: false,
        media: query,
        onchange: null,
        addEventListener: () => undefined,
        removeEventListener: () => undefined,
        addListener: () => undefined,
        removeListener: () => undefined,
        dispatchEvent: () => false,
      }) as MediaQueryList
    return
  }

  const nativeMatchMedia = window.matchMedia.bind(window)
  window.matchMedia = (query: string) => {
    const queryList = nativeMatchMedia(query)
    const legacyQueryList = queryList as MediaQueryList & {
      addListener?: (listener: MediaQueryListener) => void
      removeListener?: (listener: MediaQueryListener) => void
    }

    if (
      typeof queryList.addEventListener !== 'function' &&
      typeof legacyQueryList.addListener === 'function'
    ) {
      queryList.addEventListener = (
        type: string,
        listener: EventListenerOrEventListenerObject
      ) => {
        if (type === 'change')
          legacyQueryList.addListener?.(listener as MediaQueryListener)
      }
      queryList.removeEventListener = (
        type: string,
        listener: EventListenerOrEventListenerObject
      ) => {
        if (type === 'change')
          legacyQueryList.removeListener?.(listener as MediaQueryListener)
      }
    }

    return queryList
  }
}

function installResizeObserverFallback() {
  if (
    typeof window === 'undefined' ||
    typeof window.ResizeObserver === 'function'
  ) {
    return
  }

  class CompatResizeObserver {
    private readonly callback: ResizeObserverCallback
    private readonly elements = new Set<Element>()
    private frame = 0

    constructor(callback: ResizeObserverCallback) {
      this.callback = callback
      window.addEventListener('resize', this.schedule)
    }

    observe = (target: Element) => {
      this.elements.add(target)
      this.schedule()
    }

    unobserve = (target: Element) => {
      this.elements.delete(target)
    }

    disconnect = () => {
      this.elements.clear()
      window.removeEventListener('resize', this.schedule)
      if (this.frame) cancelAnimationFrame(this.frame)
    }

    private schedule = () => {
      if (this.frame) cancelAnimationFrame(this.frame)
      this.frame = requestAnimationFrame(() => {
        this.frame = 0
        const entries = Array.from(this.elements).map((target) => ({
          target,
          contentRect: target.getBoundingClientRect(),
        }))
        this.callback(
          entries as ResizeObserverEntry[],
          this as unknown as ResizeObserver
        )
      })
    }
  }

  window.ResizeObserver =
    CompatResizeObserver as unknown as typeof ResizeObserver
}

function installIntersectionObserverFallback() {
  if (
    typeof window === 'undefined' ||
    typeof window.IntersectionObserver === 'function'
  ) {
    return
  }

  class CompatIntersectionObserver {
    private readonly callback: IntersectionObserverCallback
    private readonly elements = new Set<Element>()

    readonly root = null
    readonly rootMargin = '0px'
    readonly thresholds = [0]

    constructor(callback: IntersectionObserverCallback) {
      this.callback = callback
    }

    observe = (target: Element) => {
      this.elements.add(target)
      requestAnimationFrame(() => {
        if (!this.elements.has(target)) return
        this.callback(
          [
            {
              target,
              isIntersecting: true,
              intersectionRatio: 1,
              time: performance.now(),
              boundingClientRect: target.getBoundingClientRect(),
              intersectionRect: target.getBoundingClientRect(),
              rootBounds: null,
            } as IntersectionObserverEntry,
          ],
          this as unknown as IntersectionObserver
        )
      })
    }

    unobserve = (target: Element) => {
      this.elements.delete(target)
    }

    disconnect = () => {
      this.elements.clear()
    }

    takeRecords = () => []
  }

  window.IntersectionObserver =
    CompatIntersectionObserver as unknown as typeof IntersectionObserver
}

installStructuredCloneFallback()
installMatchMediaFallback()
installResizeObserverFallback()
installIntersectionObserverFallback()

if (typeof window !== 'undefined') {
  Object.defineProperty(window, '__NEW_API_BROWSER_COMPATIBILITY__', {
    configurable: true,
    value: {
      intersectionObserver: typeof window.IntersectionObserver === 'function',
      matchMedia: typeof window.matchMedia === 'function',
      resizeObserver: typeof window.ResizeObserver === 'function',
      structuredClone: typeof globalThis.structuredClone === 'function',
    },
  })
}
