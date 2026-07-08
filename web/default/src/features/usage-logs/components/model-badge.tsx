import type { ReactNode } from 'react'
import { Copy, Route, Check } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { Button } from '@/components/ui/button'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { StatusBadge } from '@/components/status-badge'

export interface ModelBadgeInfoRow {
  key: string
  label: string
  value: ReactNode
  mono?: boolean
  muted?: boolean
}

interface ModelBadgeProps {
  modelName: string
  actualModel?: string
  className?: string
  infoRows?: ModelBadgeInfoRow[]
  copyable?: boolean
}

interface ModelProvider {
  icon: string
  label: string
}

function resolveModelProvider(modelName: string): ModelProvider | null {
  const model = modelName.toLowerCase()
  const hasAny = (keywords: string[]) =>
    keywords.some((keyword) => model.includes(keyword))

  if (
    hasAny([
      'gpt-',
      'chatgpt-',
      'text-embedding-',
      'omni-moderation',
      'dall-e',
      'whisper',
      'tts-',
    ]) ||
    /\bo[134](?:-|$)/.test(model)
  ) {
    return { icon: 'OpenAI.Color', label: 'OpenAI' }
  }
  if (hasAny(['claude-', 'anthropic'])) {
    return { icon: 'Claude.Color', label: 'Claude' }
  }
  if (hasAny(['gemini-', 'learnlm-'])) {
    return { icon: 'Gemini.Color', label: 'Gemini' }
  }
  if (hasAny(['grok-', 'xai-'])) {
    return { icon: 'Grok.Color', label: 'Grok' }
  }
  if (hasAny(['deepseek-'])) {
    return { icon: 'DeepSeek.Color', label: 'DeepSeek' }
  }
  if (hasAny(['qwen', 'qwq-'])) {
    return { icon: 'Qwen.Color', label: 'Qwen' }
  }
  if (hasAny(['doubao-', 'volcengine'])) {
    return { icon: 'Doubao.Color', label: 'Doubao' }
  }
  if (hasAny(['moonshot-', 'kimi-'])) {
    return { icon: 'Moonshot.Color', label: 'Moonshot' }
  }
  if (hasAny(['mistral-', 'mixtral-'])) {
    return { icon: 'Mistral.Color', label: 'Mistral' }
  }
  if (hasAny(['llama-', 'meta-'])) {
    return { icon: 'Meta.Color', label: 'Meta' }
  }
  if (hasAny(['command-', 'cohere-'])) {
    return { icon: 'Cohere.Color', label: 'Cohere' }
  }

  return null
}

function ModelBadgeContent(props: ModelBadgeProps) {
  const provider = resolveModelProvider(props.modelName)

  return (
    <StatusBadge
      copyText={props.modelName}
      copyable={props.copyable ?? true}
      size='sm'
      showDot={!provider}
      autoColor={provider ? undefined : props.modelName}
      className={cn(
        'border-border/60 bg-muted/30 rounded-md border px-1.5 py-0.5 font-mono',
        provider && 'text-foreground',
        props.className
      )}
    >
      <span className='flex items-center gap-1.5'>
        {provider && (
          <span
            className='flex size-3.5 shrink-0 items-center justify-center'
            title={provider.label}
            aria-label={provider.label}
          >
            {getLobeIcon(provider.icon, 14)}
          </span>
        )}
        <span className='min-w-0 truncate'>{props.modelName}</span>
      </span>
    </StatusBadge>
  )
}

export function ModelBadge(props: ModelBadgeProps) {
  const { t } = useTranslation()
  const { copiedText, copyToClipboard } = useCopyToClipboard({ notify: false })
  const infoRows = props.infoRows ?? []
  const canCopy = props.copyable ?? true
  const rows: ModelBadgeInfoRow[] = [
    {
      key: 'request-model',
      label: t('Request Model'),
      value: props.modelName,
      mono: true,
    },
    ...(props.actualModel
      ? [
          {
            key: 'actual-model',
            label: t('Actual Model'),
            value: props.actualModel,
            mono: true,
          },
        ]
      : []),
    ...infoRows,
  ]

  if (!props.actualModel && infoRows.length === 0) {
    return <ModelBadgeContent {...props} />
  }

  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type='button'
          className='inline-flex min-w-0 items-center gap-1'
          aria-label={`${t('Model')} ${t('Details')}`}
        >
          <ModelBadgeContent {...props} copyable={false} />
          <Route className='text-muted-foreground size-3 shrink-0' />
        </button>
      </PopoverTrigger>
      <PopoverContent
        align='start'
        className='w-[min(32rem,calc(100vw-2rem))] p-0'
      >
        <div className='max-h-[min(22rem,calc(100vh-6rem))] overflow-y-auto p-3'>
          <div className='space-y-2'>
            {rows.map((row) => {
              const copyValue =
                canCopy && typeof row.value === 'string' && row.value
                  ? row.value
                  : ''

              return (
                <div
                  key={row.key}
                  className='grid min-w-0 grid-cols-[minmax(7rem,9rem)_minmax(0,1fr)_auto] items-start gap-2 text-xs'
                >
                  <span className='text-muted-foreground min-w-0'>
                    {row.label}
                  </span>
                  <div
                    className={cn(
                      'min-w-0 font-medium break-words',
                      row.mono && 'font-mono',
                      row.muted && 'text-muted-foreground font-normal'
                    )}
                  >
                    {row.value}
                  </div>
                  {copyValue ? (
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon'
                      className='text-muted-foreground size-6 shrink-0'
                      onClick={() => copyToClipboard(copyValue)}
                      title={`${t('Copy to clipboard')}: ${row.label}`}
                      aria-label={`${t('Copy to clipboard')}: ${row.label}`}
                    >
                      {copiedText === copyValue ? (
                        <Check className='size-3.5 text-emerald-600' />
                      ) : (
                        <Copy className='size-3.5' />
                      )}
                    </Button>
                  ) : (
                    <div aria-hidden='true' />
                  )}
                </div>
              )
            })}
          </div>
        </div>
      </PopoverContent>
    </Popover>
  )
}
