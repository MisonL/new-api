import { useEffect, useRef, useState } from 'react'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'
import { Input } from '@/components/ui/input'

type FilterAutoCompleteProps = {
  field: string
  value: string
  onValueChange: (value: string) => void
  endpoint: string
  buildParams?: () => Record<string, unknown>
  enabled?: boolean
  minLength?: number
  limit?: number
  placeholder?: string
  className?: string
  id?: string
  ariaLabel?: string
}

export function FilterAutoComplete(props: FilterAutoCompleteProps) {
  const { t } = useTranslation()
  const { buildParams, endpoint, enabled, field, limit, minLength, value } =
    props
  const [options, setOptions] = useState<string[]>([])
  const [open, setOpen] = useState(false)
  const [loading, setLoading] = useState(false)
  const cacheRef = useRef(new Map<string, string[]>())
  const requestSequenceRef = useRef(0)
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!enabled) {
      setOptions([])
      setOpen(false)
      return
    }

    const keyword = value.trim()
    if (keyword.length < (minLength ?? 2)) {
      setOptions([])
      setLoading(false)
      return
    }

    const params = {
      ...(buildParams?.() ?? {}),
      field,
      keyword,
      limit: limit ?? 10,
    }
    const cacheKey = JSON.stringify([endpoint, params])
    const cached = cacheRef.current.get(cacheKey)
    if (cached) {
      setOptions(cached)
      setOpen(true)
      // A cache hit skips the request whose `finally` clears the flag, so the
      // spinner from an earlier keystroke would spin forever.
      setLoading(false)
      return
    }

    const sequence = requestSequenceRef.current + 1
    requestSequenceRef.current = sequence
    const controller = new AbortController()
    const timer = window.setTimeout(async () => {
      setLoading(true)
      try {
        const response = await api.get(endpoint, {
          params,
          signal: controller.signal,
          disableDuplicate: true,
          skipBusinessError: true,
          skipErrorHandler: true,
        } as Record<string, unknown>)
        if (requestSequenceRef.current !== sequence) return
        const values = Array.isArray(response.data?.data)
          ? response.data.data.filter(
              (item: unknown): item is string =>
                typeof item === 'string' && item.length > 0
            )
          : []
        cacheRef.current.set(cacheKey, values)
        setOptions(values)
        setOpen(true)
      } catch {
        if (
          !controller.signal.aborted &&
          requestSequenceRef.current === sequence
        ) {
          setOptions([])
        }
      } finally {
        if (requestSequenceRef.current === sequence) setLoading(false)
      }
    }, 250)

    return () => {
      window.clearTimeout(timer)
      controller.abort()
    }
  }, [buildParams, endpoint, enabled, field, limit, minLength, value])

  useEffect(() => {
    if (!open) return
    const handleOutsideClick = (event: MouseEvent) => {
      if (!containerRef.current?.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handleOutsideClick)
    return () => document.removeEventListener('mousedown', handleOutsideClick)
  }, [open])

  return (
    <div ref={containerRef} className='relative'>
      <Input
        id={props.id ?? props.field}
        role='combobox'
        aria-label={props.ariaLabel}
        aria-autocomplete='list'
        aria-expanded={open && options.length > 0}
        aria-controls={`${props.field}-suggestions`}
        autoComplete='off'
        placeholder={props.placeholder}
        value={props.value}
        onChange={(event) => {
          props.onValueChange(event.target.value)
          if (props.enabled) setOpen(true)
        }}
        onFocus={() => {
          if (props.enabled && options.length > 0) setOpen(true)
        }}
        onKeyDown={(event) => {
          if (event.key === 'Escape') setOpen(false)
        }}
        className={cn('h-9', props.className)}
      />
      {loading && (
        <Loader2 className='text-muted-foreground absolute top-1/2 right-3 size-3.5 -translate-y-1/2 animate-spin' />
      )}
      {open && options.length > 0 && (
        <div
          id={`${props.field}-suggestions`}
          role='listbox'
          className='bg-popover text-popover-foreground absolute top-full z-50 mt-1 max-h-52 w-full overflow-y-auto rounded-md border p-1 shadow-md'
        >
          {options.map((option) => (
            <button
              key={option}
              type='button'
              role='option'
              aria-selected={option === props.value}
              className='hover:bg-accent hover:text-accent-foreground block w-full truncate rounded-sm px-2 py-1.5 text-start text-sm'
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => {
                props.onValueChange(option)
                setOpen(false)
              }}
            >
              {option}
            </button>
          ))}
        </div>
      )}
      {!props.enabled && props.value.trim() && (
        <span className='sr-only'>
          {t('Log filter suggestions are disabled')}
        </span>
      )}
    </div>
  )
}
