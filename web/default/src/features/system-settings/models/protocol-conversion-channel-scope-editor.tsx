import type { ReactNode } from 'react'
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
  parseIntegerText,
  type ProtocolRule,
} from './protocol-conversion-policy-utils'

type ProtocolConversionChannelScopeEditorProps = {
  rule: ProtocolRule
  channels: Channel[]
  selectedChannelId: string
  onSelectedChannelIdChange: (value: string) => void
  onAddSelectedChannel: () => void
  onUpdate: (patch: Partial<ProtocolRule>) => void
}

export function ProtocolConversionChannelScopeEditor({
  rule,
  channels,
  selectedChannelId,
  onSelectedChannelIdChange,
  onAddSelectedChannel,
  onUpdate,
}: ProtocolConversionChannelScopeEditorProps) {
  const { t } = useTranslation()
  const channelById = new Map(channels.map((channel) => [channel.id, channel]))
  const channelOptions = channels.map((channel) => ({
    value: String(channel.id),
    label: `#${channel.id} ${channel.name}`,
  }))

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
              placeholder={t('Select channel')}
              searchPlaceholder={t('Search channels')}
              emptyText={t('No channels found')}
            />
            <Button
              type='button'
              variant='outline'
              size='icon'
              disabled={rule.all_channels || !selectedChannelId}
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
        <ScopeField label={t('Channel types')}>
          <Input
            disabled={rule.all_channels}
            value={rule.channel_types.join(', ')}
            onChange={(event) =>
              onUpdate({ channel_types: parseIntegerText(event.target.value) })
            }
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
