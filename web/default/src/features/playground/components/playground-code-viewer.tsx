import { useMemo, useState } from 'react'
import { Check, ChevronDown, ChevronUp, Copy } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { Button } from '@/components/ui/button'
import { CodeBlock } from '@/components/ai-elements/code-block'

const MAX_DISPLAY_LENGTH = 50_000
const PREVIEW_LENGTH = 5_000

function formatContent(value: unknown): string {
  if (value == null) return ''
  if (typeof value === 'string') {
    try {
      return JSON.stringify(JSON.parse(value), null, 2)
    } catch {
      return value
    }
  }
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

function placeholderFor(title: string, t: (key: string) => string): string {
  if (title === 'preview') return t('No preview available')
  if (title === 'request') return t('No request sent yet')
  if (title === 'response') return t('No response received yet')
  return t('No data')
}

interface PlaygroundCodeViewerProps {
  content: unknown
  title: string
}

export function PlaygroundCodeViewer({
  content,
  title,
}: PlaygroundCodeViewerProps) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  const { copiedText, copyToClipboard } = useCopyToClipboard({ notify: false })
  const formattedContent = useMemo(() => formatContent(content), [content])

  if (!formattedContent) {
    return (
      <div className='text-muted-foreground bg-background flex min-h-40 items-center justify-center rounded-md border p-6 text-center text-sm'>
        {placeholderFor(title, t)}
      </div>
    )
  }

  const isLarge = formattedContent.length > MAX_DISPLAY_LENGTH
  const displayContent =
    isLarge && !expanded
      ? `${formattedContent.slice(0, PREVIEW_LENGTH)}\n\n// ${t('Content truncated for performance')}`
      : formattedContent
  const isCopied = copiedText === formattedContent

  const handleCopy = async () => {
    const success = await copyToClipboard(formattedContent)
    if (success) toast.success(t('Copied'))
    else toast.error(t('Copy failed'))
  }

  return (
    <div className='space-y-2'>
      {isLarge && (
        <div className='text-muted-foreground bg-muted/40 rounded-md border px-3 py-2 text-xs'>
          {t('Content truncated for performance')}
        </div>
      )}
      <CodeBlock
        code={displayContent}
        language='json'
        className='max-h-[calc(100vh-14rem)] overflow-auto'
      >
        <Button
          type='button'
          variant='outline'
          size='icon-sm'
          aria-label={t('Copy code')}
          onClick={() => void handleCopy()}
        >
          {isCopied ? (
            <Check className='h-3.5 w-3.5 text-emerald-600' />
          ) : (
            <Copy className='h-3.5 w-3.5' />
          )}
        </Button>
      </CodeBlock>
      {isLarge && (
        <Button
          type='button'
          variant='outline'
          size='sm'
          className='mx-auto flex'
          onClick={() => setExpanded((value) => !value)}
        >
          {expanded ? (
            <ChevronUp className='mr-1.5 h-4 w-4' />
          ) : (
            <ChevronDown className='mr-1.5 h-4 w-4' />
          )}
          {expanded ? t('Collapse') : t('Expand')}
        </Button>
      )}
    </div>
  )
}
