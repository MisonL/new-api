import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { ProtocolRule } from './protocol-conversion-policy-utils'

export type ProtocolPreviewState = {
  channelId: string
  channelType: string
  model: string
}

type ProtocolConversionHitPreviewProps = {
  rule: ProtocolRule
  preview: ProtocolPreviewState
  passThroughEnabled: boolean
  onPreviewChange: (preview: ProtocolPreviewState) => void
}

export function ProtocolConversionHitPreview({
  rule,
  preview,
  passThroughEnabled,
  onPreviewChange,
}: ProtocolConversionHitPreviewProps) {
  const { t } = useTranslation()
  const result = getPreviewResult(rule, preview, passThroughEnabled)

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

function getPreviewResult(
  rule: ProtocolRule,
  preview: ProtocolPreviewState,
  passThroughEnabled: boolean
) {
  if (!rule.enabled) return { matched: false, reason: 'Rule is disabled.' }

  const channelId = Number.parseInt(preview.channelId, 10)
  const channelType = Number.parseInt(preview.channelType, 10)

  if (!rule.all_channels) {
    const idMatched =
      Number.isInteger(channelId) && rule.channel_ids.includes(channelId)
    const typeMatched =
      Number.isInteger(channelType) && rule.channel_types.includes(channelType)
    if (!idMatched && !typeMatched) {
      return { matched: false, reason: 'Channel scope does not match.' }
    }
  }

  const model = preview.model.trim()
  if (rule.model_patterns.length > 0) {
    if (!model)
      return { matched: false, reason: 'Model is required for preview.' }
    const matched = rule.model_patterns.some((pattern) => {
      try {
        return new RegExp(pattern).test(model)
      } catch {
        return false
      }
    })
    if (!matched) {
      return { matched: false, reason: 'Model pattern does not match.' }
    }
  }

  if (passThroughEnabled) {
    return {
      matched: true,
      reason:
        'Sample request matches this rule, but passthrough will skip conversion.',
    }
  }

  return { matched: true, reason: 'Sample request matches this rule.' }
}
