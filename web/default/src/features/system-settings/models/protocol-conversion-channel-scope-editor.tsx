import { useState, type ReactNode } from 'react'
import { Plus, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { CHANNEL_TYPE_OPTIONS } from '@/features/channels/constants'
import type { Channel } from '@/features/channels/types'
import {
  createCommittedDraftText,
  createDraftTextChange,
  getDraftTextValue,
  parseIntegerText,
  type ProtocolRule,
} from './protocol-conversion-policy-utils'

type ProtocolConversionChannelScopeEditorProps = {
  rule: ProtocolRule
  channels: Channel[]
  selectedChannelId: string
  channelsLoading: boolean
  channelsError: boolean
  onSelectedChannelIdChange: (value: string) => void
  onAddSelectedChannel: () => void
  onUpdate: (patch: Partial<ProtocolRule>) => void
}

export function ProtocolConversionChannelScopeEditor({
  rule,
  channels,
  selectedChannelId,
  channelsLoading,
  channelsError,
  onSelectedChannelIdChange,
  onAddSelectedChannel,
  onUpdate,
}: ProtocolConversionChannelScopeEditorProps) {
  const { t } = useTranslation()
  const channelIdsValue = rule.channel_ids.join(', ')
  const [channelIdsDraft, setChannelIdsDraft] = useState(() =>
    createCommittedDraftText(channelIdsValue)
  )
  const channelTypesValue = rule.channel_types.join(', ')
  const [channelTypesDraft, setChannelTypesDraft] = useState(() =>
    createCommittedDraftText(channelTypesValue)
  )
  const channelIdsInput = getDraftTextValue(channelIdsDraft, channelIdsValue)
  const channelTypesInput = getDraftTextValue(
    channelTypesDraft,
    channelTypesValue
  )
  const channelById = new Map(channels.map((channel) => [channel.id, channel]))
  const channelOptions = channels.map((channel) => ({
    value: String(channel.id),
    label: `#${channel.id} ${channel.name}`,
  }))
  const channelSelectorLabel = channelsLoading
    ? t('Loading channels...')
    : channelsError
      ? t('Failed to load channels for selector.')
      : null

  const commitChannelIdsDraft = () => {
    const nextChannelIds = parseIntegerText(channelIdsInput)
    const nextValue = nextChannelIds.join(', ')
    setChannelIdsDraft(createCommittedDraftText(nextValue))
    onUpdate({ channel_ids: nextChannelIds })
  }

  const updateChannelIdsDraft = (value: string) => {
    const nextChannelIds = parseIntegerText(value)
    const nextValue = nextChannelIds.join(', ')
    setChannelIdsDraft(createDraftTextChange(value, nextValue))
    onUpdate({ channel_ids: nextChannelIds })
  }

  const commitChannelTypesDraft = () => {
    const nextChannelTypes = parseIntegerText(channelTypesInput)
    const nextValue = nextChannelTypes.join(', ')
    setChannelTypesDraft(createCommittedDraftText(nextValue))
    onUpdate({ channel_types: nextChannelTypes })
  }

  const updateChannelTypesDraft = (value: string) => {
    const nextChannelTypes = parseIntegerText(value)
    const nextValue = nextChannelTypes.join(', ')
    setChannelTypesDraft(createDraftTextChange(value, nextValue))
    onUpdate({ channel_types: nextChannelTypes })
  }

  return (
    <div className='space-y-3 rounded-md border p-3'>
      <div className='flex h-9 items-center gap-2'>
        <Switch
          checked={rule.all_channels}
          onCheckedChange={(checked) => onUpdate({ all_channels: checked })}
        />
        <span className='text-sm'>
          {rule.all_channels ? t('All channels') : t('Limited scope')}
        </span>
      </div>
      <div className='grid gap-4 md:grid-cols-2'>
        <ScopeField label={t('Channel selector')}>
          <div className='flex gap-2'>
            <Combobox
              options={channelOptions}
              value={selectedChannelId}
              onValueChange={onSelectedChannelIdChange}
              placeholder={channelSelectorLabel ?? t('Select channel')}
              searchPlaceholder={t('Search channels')}
              emptyText={channelSelectorLabel ?? t('No channels found')}
              disabled={rule.all_channels || channelsLoading || channelsError}
            />
            <Button
              type='button'
              variant='outline'
              size='icon'
              disabled={
                rule.all_channels ||
                !selectedChannelId ||
                channelsLoading ||
                channelsError
              }
              onClick={onAddSelectedChannel}
              aria-label={t('Add channel')}
            >
              <Plus className='size-4' />
            </Button>
          </div>
          <div className='mt-2 flex flex-wrap gap-2'>
            {rule.channel_ids.map((id) => (
              <Badge key={id} variant='secondary' className='gap-1'>
                {channelById.get(id)?.name ?? `#${id}`}
                <button
                  type='button'
                  disabled={rule.all_channels}
                  onClick={() =>
                    onUpdate({
                      channel_ids: rule.channel_ids.filter(
                        (item) => item !== id
                      ),
                    })
                  }
                  aria-label={t('Remove channel')}
                >
                  <X className='size-3' />
                </button>
              </Badge>
            ))}
          </div>
        </ScopeField>
        <ScopeField label={t('Channel IDs')}>
          <Input
            disabled={rule.all_channels}
            value={channelIdsInput}
            onChange={(event) => updateChannelIdsDraft(event.target.value)}
            onBlur={commitChannelIdsDraft}
            placeholder='35, 36, 37'
          />
        </ScopeField>
        <ScopeField label={t('Channel types')}>
          <Input
            disabled={rule.all_channels}
            value={channelTypesInput}
            onChange={(event) => updateChannelTypesDraft(event.target.value)}
            onBlur={commitChannelTypesDraft}
            placeholder={CHANNEL_TYPE_OPTIONS.slice(0, 4)
              .map((item) => `${item.value}`)
              .join(', ')}
          />
        </ScopeField>
      </div>
    </div>
  )
}

function ScopeField(props: { label: string; children: ReactNode }) {
  return (
    <div className='space-y-2'>
      <Label>{props.label}</Label>
      {props.children}
    </div>
  )
}
