import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import type { Channel } from '@/features/channels/types'
import { ProtocolConversionChannelScopeEditor } from './protocol-conversion-channel-scope-editor'
import {
  ProtocolConversionHitPreview,
  type ProtocolPreviewState,
} from './protocol-conversion-hit-preview'
import {
  ENDPOINT_CHAT,
  ENDPOINT_RESPONSES,
  isResponsesToChatRule,
  parseLines,
  type ProtocolEndpoint,
  type ProtocolRule,
} from './protocol-conversion-policy-utils'
import { ProtocolConversionRuleImpactAlert } from './protocol-conversion-rule-impact-alert'

type ProtocolConversionRuleEditorSheetProps = {
  index: number
  rule: ProtocolRule
  channels: Channel[]
  passThroughEnabled: boolean
  onUpdate: (patch: Partial<ProtocolRule>) => void
  children: ReactNode
}

const endpointOptions: { value: ProtocolEndpoint; label: string }[] = [
  { value: ENDPOINT_CHAT, label: 'Chat Completions' },
  { value: ENDPOINT_RESPONSES, label: 'Responses' },
]

export function ProtocolConversionRuleEditorSheet({
  index,
  rule,
  channels,
  passThroughEnabled,
  onUpdate,
  children,
}: ProtocolConversionRuleEditorSheetProps) {
  const { t } = useTranslation()
  const [selectedChannelId, setSelectedChannelId] = useState('')
  const [preview, setPreview] = useState<ProtocolPreviewState>({
    channelId: '',
    channelType: '',
    model: '',
  })
  const bridgeSupported = isResponsesToChatRule(rule)

  const setEndpoint = (field: 'source_endpoint' | 'target_endpoint') => {
    return (value: string) => {
      const endpoint = value as ProtocolEndpoint
      const otherField =
        field === 'source_endpoint' ? 'target_endpoint' : 'source_endpoint'
      const nextOther =
        rule[otherField] === endpoint
          ? endpoint === ENDPOINT_CHAT
            ? ENDPOINT_RESPONSES
            : ENDPOINT_CHAT
          : rule[otherField]
      onUpdate({ [field]: endpoint, [otherField]: nextOther })
    }
  }

  const addSelectedChannel = () => {
    const id = Number.parseInt(selectedChannelId, 10)
    if (!Number.isInteger(id) || id <= 0 || rule.channel_ids.includes(id)) {
      return
    }
    onUpdate({ all_channels: false, channel_ids: [...rule.channel_ids, id] })
    setSelectedChannelId('')
  }

  return (
    <Sheet>
      <SheetTrigger asChild>{children}</SheetTrigger>
      <SheetContent className='flex h-dvh w-full flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl'>
        <SheetHeader className='border-b'>
          <SheetTitle>{t('Edit protocol conversion rule')}</SheetTitle>
          <SheetDescription>
            {t('Rule {{index}}', { index: index + 1 })}
          </SheetDescription>
        </SheetHeader>

        <div className='flex-1 space-y-5 overflow-y-auto p-4'>
          <ProtocolConversionRuleImpactAlert
            rule={rule}
            passThroughEnabled={passThroughEnabled}
          />
          <div className='grid gap-4 md:grid-cols-2'>
            <Field label={t('Rule name')}>
              <Input
                value={rule.name}
                onChange={(event) => onUpdate({ name: event.target.value })}
                placeholder={t('Rule name')}
              />
            </Field>
            <div className='grid grid-cols-2 gap-3'>
              <EndpointSelect
                label={t('Source endpoint')}
                value={rule.source_endpoint}
                onValueChange={setEndpoint('source_endpoint')}
              />
              <EndpointSelect
                label={t('Target endpoint')}
                value={rule.target_endpoint}
                onValueChange={setEndpoint('target_endpoint')}
              />
            </div>
          </div>

          <ProtocolConversionChannelScopeEditor
            rule={rule}
            channels={channels}
            selectedChannelId={selectedChannelId}
            onSelectedChannelIdChange={setSelectedChannelId}
            onAddSelectedChannel={addSelectedChannel}
            onUpdate={onUpdate}
          />

          <Field label={t('Model patterns')}>
            <Textarea
              rows={4}
              value={rule.model_patterns.join('\n')}
              onChange={(event) =>
                onUpdate({ model_patterns: parseLines(event.target.value) })
              }
              placeholder={'^gpt-4o.*$\n^gpt-5.*$'}
            />
          </Field>

          <div className='flex flex-wrap items-center justify-between gap-3 rounded-md border p-3'>
            <div>
              <div className='text-sm font-medium'>
                {t('Custom tool bridge')}
              </div>
              <div className='text-muted-foreground text-sm'>
                {bridgeSupported
                  ? t(
                      'Bridge Responses custom tools into Chat Completions tools.'
                    )
                  : t(
                      'Custom tool bridge only applies to Responses to Chat Completions.'
                    )}
              </div>
            </div>
            <Switch
              disabled={!bridgeSupported}
              checked={bridgeSupported && rule.enable_custom_tool_bridge}
              onCheckedChange={(checked) =>
                onUpdate({ enable_custom_tool_bridge: checked })
              }
            />
          </div>

          <ProtocolConversionHitPreview
            rule={rule}
            preview={preview}
            passThroughEnabled={passThroughEnabled}
            onPreviewChange={setPreview}
          />
        </div>
      </SheetContent>
    </Sheet>
  )
}

type FieldProps = {
  label: string
  children: ReactNode
}

function Field({ label, children }: FieldProps) {
  return (
    <div className='space-y-2'>
      <Label>{label}</Label>
      {children}
    </div>
  )
}

type EndpointSelectProps = {
  label: string
  value: ProtocolEndpoint
  onValueChange: (value: string) => void
}

function EndpointSelect({ label, value, onValueChange }: EndpointSelectProps) {
  const { t } = useTranslation()
  return (
    <Field label={label}>
      <Select value={value} onValueChange={onValueChange}>
        <SelectTrigger className='w-full'>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {endpointOptions.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              {t(option.label)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </Field>
  )
}
