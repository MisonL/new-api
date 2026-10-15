import { Bug, Eye, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { PlaygroundDebugData } from '../types'
import { PlaygroundCodeViewer } from './playground-code-viewer'
import { PlaygroundSseViewer } from './playground-sse-viewer'

interface PlaygroundDebugPanelProps {
  data: PlaygroundDebugData
  previewPayload: Record<string, unknown> | null
  onClose?: () => void
}

export function PlaygroundDebugPanel({
  data,
  previewPayload,
  onClose,
}: PlaygroundDebugPanelProps) {
  const { t } = useTranslation()

  return (
    <section className='bg-muted/20 flex min-h-0 flex-col border-t lg:border-t-0 lg:border-l'>
      <div className='flex items-center justify-between gap-2 border-b px-3 py-2'>
        <div className='flex items-center gap-2 text-sm font-medium'>
          <Bug className='h-4 w-4' />
          {t('Debug')}
          {data.isStreaming && (
            <span className='text-muted-foreground text-xs'>
              {t('Streaming')}
            </span>
          )}
        </div>
        {onClose && (
          <Button
            type='button'
            variant='ghost'
            size='icon-sm'
            aria-label={t('Close')}
            onClick={onClose}
          >
            <X className='h-4 w-4' />
          </Button>
        )}
      </div>
      <Tabs defaultValue='preview' className='flex min-h-0 flex-1 flex-col'>
        <TabsList className='mx-3 mt-2 grid grid-cols-4'>
          <TabsTrigger value='preview'>
            <Eye className='mr-1 h-3.5 w-3.5' />
            {t('Preview')}
          </TabsTrigger>
          <TabsTrigger value='request'>{t('Request')}</TabsTrigger>
          <TabsTrigger value='response'>{t('Response')}</TabsTrigger>
          <TabsTrigger value='sse'>{t('SSE')}</TabsTrigger>
        </TabsList>
        <div className='min-h-0 flex-1 overflow-auto p-3'>
          <TabsContent value='preview' className='mt-0'>
            <PlaygroundCodeViewer content={previewPayload} title='preview' />
          </TabsContent>
          <TabsContent value='request' className='mt-0'>
            <PlaygroundCodeViewer content={data.request} title='request' />
          </TabsContent>
          <TabsContent value='response' className='mt-0'>
            <PlaygroundCodeViewer content={data.response} title='response' />
          </TabsContent>
          <TabsContent value='sse' className='mt-0'>
            <PlaygroundSseViewer messages={data.sseMessages} />
          </TabsContent>
        </div>
      </Tabs>
      {(data.previewTimestamp || data.timestamp) && (
        <div className='text-muted-foreground flex shrink-0 flex-wrap gap-x-4 gap-y-1 border-t px-3 py-2 text-xs'>
          {data.previewTimestamp && (
            <span>
              {t('Preview updated')}:{' '}
              {new Date(data.previewTimestamp).toLocaleString()}
            </span>
          )}
          {data.timestamp && (
            <span>
              {t('Last request')}: {new Date(data.timestamp).toLocaleString()}
            </span>
          )}
        </div>
      )}
    </section>
  )
}
