import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { parseChannelSettings } from '@/features/channels/lib/channel-utils'
import type { Channel } from '@/features/channels/types'
import {
  getProtocolPreviewResult,
  type ProtocolPreviewState,
  type ProtocolRule,
} from './protocol-conversion-policy-utils'

type ProtocolConversionHitPreviewProps = {
  rule: ProtocolRule
  preview: ProtocolPreviewState
  channels: Channel[]
  passThroughEnabled: boolean
  onPreviewChange: (preview: ProtocolPreviewState) => void
}

export function ProtocolConversionHitPreview({
  rule,
  preview,
  channels,
  passThroughEnabled,
  onPreviewChange,
}: ProtocolConversionHitPreviewProps) {
  const { t } = useTranslation()
  const channelId = Number.parseInt(preview.channelId, 10)
  const previewChannel = channels.find((channel) => channel.id === channelId)
  const channelPassThroughEnabled =
    previewChannel != null
      ? parseChannelSettings(previewChannel.setting)
          .pass_through_body_enabled === true
      : false
  const result = getProtocolPreviewResult(
    rule,
    preview,
    passThroughEnabled || channelPassThroughEnabled
  )

  return (
    <div className='space-y-3 rounded-md border p-3'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div>
          <div className='text-sm font-medium'>{t('Hit preview')}</div>
          <div className='text-muted-foreground text-sm'>
            {t('Check whether a sample channel and model match this rule.')}
          </div>
        </div>
        <Badge variant={result.matched ? 'default' : 'secondary'}>
          {result.matched ? t('Matched') : t('Not matched')}
        </Badge>
      </div>
      <div className='grid gap-3 md:grid-cols-3'>
        <PreviewField label={t('Channel ID')}>
          <Input
            value={preview.channelId}
            onChange={(event) =>
              onPreviewChange({ ...preview, channelId: event.target.value })
            }
            placeholder='117'
          />
        </PreviewField>
        <PreviewField label={t('Channel type')}>
          <Input
            value={preview.channelType}
            onChange={(event) =>
              onPreviewChange({ ...preview, channelType: event.target.value })
            }
            placeholder='1'
          />
        </PreviewField>
        <PreviewField label={t('Model')}>
          <Input
            value={preview.model}
            onChange={(event) =>
              onPreviewChange({ ...preview, model: event.target.value })
            }
            placeholder='gpt-5.1'
          />
        </PreviewField>
      </div>
      <div className='text-muted-foreground text-sm'>{t(result.reason)}</div>
    </div>
  )
}

function PreviewField(props: { label: string; children: ReactNode }) {
  return (
    <div className='space-y-2'>
      <Label>{props.label}</Label>
      {props.children}
    </div>
  )
}
