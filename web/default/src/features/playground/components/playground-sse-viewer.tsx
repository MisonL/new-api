/* eslint-disable react-refresh/only-export-components */
import { useEffect, useMemo, useState } from 'react'
import {
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Clipboard,
  Copy,
  FileJson2,
  TriangleAlert,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { StatusBadge } from '@/components/status-badge'

type ParsedSseEvent = {
  key: string
  raw: string
  parsed: unknown | null
  error: string | null
  isDone: boolean
  label: string
  summary: string
}

function objectRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
  return value as Record<string, unknown>
}

function stringValue(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function summarizeEvent(value: unknown): { label: string; summary: string } {
  const event = objectRecord(value)
  if (!event) return { label: 'JSON', summary: '' }

  const eventType = stringValue(event.type) || stringValue(event.object)
  const choices = Array.isArray(event.choices) ? event.choices : []
  const choice = objectRecord(choices[0])
  const delta = objectRecord(choice?.delta)
  const message = objectRecord(choice?.message)
  const usage = objectRecord(event.usage)
  const reasoning =
    stringValue(delta?.reasoning_content) ||
    stringValue(delta?.reasoning) ||
    stringValue(message?.reasoning_content) ||
    stringValue(message?.reasoning)
  const content =
    stringValue(delta?.content) || stringValue(message?.content) || ''
  const finishReason = stringValue(choice?.finish_reason)

  if (reasoning) {
    return { label: eventType || 'reasoning', summary: reasoning }
  }
  if (content) {
    return { label: eventType || 'content', summary: content }
  }
  if (finishReason) {
    return { label: eventType || 'finish', summary: finishReason }
  }
  if (usage) {
    const total = usage.total_tokens
    return {
      label: eventType || 'usage',
      summary: typeof total === 'number' ? `total_tokens: ${total}` : 'usage',
    }
  }
  return { label: eventType || 'JSON', summary: '' }
}

export function parsePlaygroundSseMessages(
  messages: readonly string[]
): ParsedSseEvent[] {
  return messages.map((rawMessage, index) => {
    const raw = String(rawMessage ?? '').trim()
    const data = raw.startsWith('data:') ? raw.slice(5).trimStart() : raw
    if (data === '[DONE]') {
      return {
        key: `sse-${index}`,
        raw,
        parsed: null,
        error: null,
        isDone: true,
        label: 'DONE',
        summary: '',
      }
    }

    try {
      const parsed: unknown = JSON.parse(data)
      const summary = summarizeEvent(parsed)
      return {
        key: `sse-${index}`,
        raw,
        parsed,
        error: null,
        isDone: false,
        ...summary,
      }
    } catch (error) {
      return {
        key: `sse-${index}`,
        raw,
        parsed: null,
        error: error instanceof Error ? error.message : 'Invalid JSON',
        isDone: false,
        label: 'RAW',
        summary: data,
      }
    }
  })
}

async function copyText(value: string): Promise<void> {
  await navigator.clipboard.writeText(value)
}

function SseEventRow(props: {
  event: ParsedSseEvent
  index: number
  expandSignal: number
  collapseSignal: number
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  useEffect(() => {
    // The signal is an imperative control from the parent viewer toolbar.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (props.expandSignal > 0) setOpen(true)
  }, [props.expandSignal])

  useEffect(() => {
    // The signal is an imperative control from the parent viewer toolbar.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (props.collapseSignal > 0) setOpen(false)
  }, [props.collapseSignal])

  const formatted = props.event.parsed
    ? JSON.stringify(props.event.parsed, null, 2)
    : props.event.raw

  return (
    <div className='bg-background overflow-hidden rounded-md border'>
      <button
        type='button'
        className='hover:bg-muted/60 focus-visible:ring-ring flex w-full items-center gap-2 px-3 py-2 text-left focus-visible:ring-2 focus-visible:outline-none'
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        {props.event.isDone ? (
          <CheckCircle2 className='h-4 w-4 shrink-0 text-emerald-600' />
        ) : props.event.error ? (
          <TriangleAlert className='text-destructive h-4 w-4 shrink-0' />
        ) : (
          <FileJson2 className='text-primary h-4 w-4 shrink-0' />
        )}
        <span className='text-muted-foreground w-7 shrink-0 font-mono text-xs'>
          {props.index + 1}
        </span>
        <span className='min-w-0 flex-1'>
          <span className='block truncate text-xs font-medium'>
            {props.event.isDone
              ? t('Stream completed')
              : props.event.error
                ? t('Parse error')
                : props.event.label}
          </span>
          {props.event.summary && (
            <span className='text-muted-foreground block truncate text-xs'>
              {props.event.summary}
            </span>
          )}
        </span>
        {open ? (
          <ChevronUp className='h-4 w-4 shrink-0' />
        ) : (
          <ChevronDown className='h-4 w-4 shrink-0' />
        )}
      </button>
      {open && (
        <div className='relative border-t'>
          <Button
            type='button'
            variant='outline'
            size='icon-sm'
            className='absolute top-2 right-2'
            aria-label={t('Copy event')}
            onClick={async () => {
              try {
                await copyText(formatted)
                toast.success(t('Copied'))
              } catch {
                toast.error(t('Copy failed'))
              }
            }}
          >
            <Copy className='h-3.5 w-3.5' />
          </Button>
          <pre className='max-h-80 overflow-auto p-3 pr-12 text-xs break-all whitespace-pre-wrap'>
            {formatted}
          </pre>
        </div>
      )}
    </div>
  )
}

export function PlaygroundSseViewer(props: { messages: readonly string[] }) {
  const { t } = useTranslation()
  const [expandSignal, setExpandSignal] = useState(0)
  const [collapseSignal, setCollapseSignal] = useState(0)
  const events = useMemo(
    () => parsePlaygroundSseMessages(props.messages),
    [props.messages]
  )
  const errors = events.filter((event) => event.error).length
  const done = events.filter((event) => event.isDone).length
  const valid = events.length - errors - done

  if (events.length === 0) {
    return (
      <div className='text-muted-foreground bg-background min-h-40 rounded-md border border-dashed p-6 text-center text-sm'>
        {t('No SSE events received yet')}
      </div>
    )
  }

  return (
    <div className='space-y-3'>
      <div className='flex flex-wrap items-center gap-2'>
        <StatusBadge variant='neutral' copyable={false}>
          {t('Events')}: {events.length}
        </StatusBadge>
        <StatusBadge variant='success' copyable={false}>
          {t('Valid')}: {valid}
        </StatusBadge>
        {done > 0 && (
          <StatusBadge variant='success' copyable={false}>
            DONE: {done}
          </StatusBadge>
        )}
        {errors > 0 && (
          <StatusBadge variant='danger' copyable={false}>
            {t('Errors')}: {errors}
          </StatusBadge>
        )}
        <div className='ml-auto flex gap-1'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => setExpandSignal((value) => value + 1)}
          >
            {t('Expand all')}
          </Button>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => setCollapseSignal((value) => value + 1)}
          >
            {t('Collapse all')}
          </Button>
          <Button
            type='button'
            variant='outline'
            size='icon-sm'
            aria-label={t('Copy all events')}
            onClick={async () => {
              const text = events
                .map((event) =>
                  event.parsed
                    ? JSON.stringify(event.parsed, null, 2)
                    : event.raw
                )
                .join('\n\n')
              try {
                await copyText(text)
                toast.success(t('Copied'))
              } catch {
                toast.error(t('Copy failed'))
              }
            }}
          >
            <Clipboard className='h-3.5 w-3.5' />
          </Button>
        </div>
      </div>
      <div className='space-y-2'>
        {events.map((event, index) => (
          <SseEventRow
            key={event.key}
            event={event}
            index={index}
            expandSignal={expandSignal}
            collapseSignal={collapseSignal}
          />
        ))}
      </div>
    </div>
  )
}
