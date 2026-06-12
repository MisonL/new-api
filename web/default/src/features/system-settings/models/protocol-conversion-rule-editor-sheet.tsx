import { useState, type ReactNode } from 'react'
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Separator } from '@/components/ui/separator'
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
import { cn } from '@/lib/utils'
import { ProtocolConversionChannelScopeEditor } from './protocol-conversion-channel-scope-editor'
import { ProtocolConversionHitPreview } from './protocol-conversion-hit-preview'
import {
  createCommittedDraftText,
  createDraftTextChange,
  ENDPOINT_CHAT,
  ENDPOINT_RESPONSES,
  getDraftTextValue,
  getProtocolRuleDirection,
  isResponsesToChatRule,
  parseLines,
  TEMPLATE_CHAT_TO_RESPONSES,
  TEMPLATE_RESPONSES_TO_CHAT,
  type ProtocolRuleDirection,
  type ProtocolPreviewState,
  type ProtocolRule,
} from './protocol-conversion-policy-utils'
import { ProtocolConversionRuleImpactAlert } from './protocol-conversion-rule-impact-alert'

type ProtocolConversionRuleEditorSheetProps = {
  index: number
  rule: ProtocolRule
  channels: Channel[]
  channelsLoading: boolean
  channelsError: boolean
  passThroughEnabled: boolean
  onUpdate: (patch: Partial<ProtocolRule>) => void
  children: ReactNode
}

export function ProtocolConversionRuleEditorSheet({
  index,
  rule,
  channels,
  channelsLoading,
  channelsError,
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
  const modelPatternsValue = rule.model_patterns.join('\n')
  const [modelPatternsDraft, setModelPatternsDraft] = useState(() =>
    createCommittedDraftText(modelPatternsValue)
  )
  const modelPatternsInput = getDraftTextValue(
    modelPatternsDraft,
    modelPatternsValue
  )
  const bridgeSupported = isResponsesToChatRule(rule)
  const direction = getProtocolRuleDirection(rule)
  const [savedEnableCustomToolBridge, setSavedEnableCustomToolBridge] =
    useState(rule.enable_custom_tool_bridge)

  const setDirection = (nextDirection: ProtocolRuleDirection) => {
    const isResponsesToChat = nextDirection === TEMPLATE_RESPONSES_TO_CHAT
    if (!isResponsesToChat && bridgeSupported) {
      setSavedEnableCustomToolBridge(rule.enable_custom_tool_bridge)
    }
    onUpdate({
      source_endpoint: isResponsesToChat ? ENDPOINT_RESPONSES : ENDPOINT_CHAT,
      target_endpoint: isResponsesToChat ? ENDPOINT_CHAT : ENDPOINT_RESPONSES,
      enable_custom_tool_bridge: isResponsesToChat
        ? savedEnableCustomToolBridge
        : false,
    })
  }

  const addSelectedChannel = () => {
    const id = Number.parseInt(selectedChannelId, 10)
    if (!Number.isInteger(id) || id <= 0 || rule.channel_ids.includes(id)) {
      return
    }
    onUpdate({ all_channels: false, channel_ids: [...rule.channel_ids, id] })
    setSelectedChannelId('')
  }

  const commitModelPatternsDraft = () => {
    const nextPatterns = parseLines(modelPatternsInput)
    const nextValue = nextPatterns.join('\n')
    setModelPatternsDraft(createCommittedDraftText(nextValue))
    onUpdate({ model_patterns: nextPatterns })
  }

  const updateModelPatternsDraft = (value: string) => {
    const nextPatterns = parseLines(value)
    const nextValue = nextPatterns.join('\n')
    setModelPatternsDraft(createDraftTextChange(value, nextValue))
    onUpdate({ model_patterns: nextPatterns })
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
          <EditorSection
            title={t('Rule identity')}
            description={t('Name the rule so audits can identify ownership.')}
          >
            <Field label={t('Rule name')}>
              <Input
                value={rule.name}
                onChange={(event) => onUpdate({ name: event.target.value })}
                placeholder={t('Rule name')}
              />
            </Field>
          </EditorSection>

          <EditorSection
            title={t('Protocol direction')}
            description={t(
              'Choose which client endpoint is converted before the upstream call.'
            )}
          >
            <div className='grid gap-3 md:grid-cols-2'>
              <DirectionOption
                active={direction === TEMPLATE_CHAT_TO_RESPONSES}
                title={t('Chat -> Responses')}
                source={t('Client Chat Completions')}
                target={t('Upstream Responses')}
                description={t(
                  'Use when a client sends Chat Completions but the upstream should receive Responses.'
                )}
                onClick={() => setDirection(TEMPLATE_CHAT_TO_RESPONSES)}
              />
              <DirectionOption
                active={direction === TEMPLATE_RESPONSES_TO_CHAT}
                title={t('Responses -> Chat')}
                source={t('Client Responses')}
                target={t('Upstream Chat Completions')}
                description={t(
                  'Use when a client sends Responses but the upstream only accepts Chat Completions.'
                )}
                onClick={() => setDirection(TEMPLATE_RESPONSES_TO_CHAT)}
              />
            </div>
          </EditorSection>

          <EditorSection
            title={t('Runtime scope')}
            description={t(
              'Limit the rule by channel ID or channel type before model checks run.'
            )}
          >
            <ProtocolConversionChannelScopeEditor
              rule={rule}
              channels={channels}
              selectedChannelId={selectedChannelId}
              channelsLoading={channelsLoading}
              channelsError={channelsError}
              onSelectedChannelIdChange={setSelectedChannelId}
              onAddSelectedChannel={addSelectedChannel}
              onUpdate={onUpdate}
            />
          </EditorSection>

          <EditorSection
            title={t('Model boundary')}
            description={t(
              'Leave empty to match all non-empty model names, or add one regular expression per line.'
            )}
          >
            <Field label={t('Model patterns')}>
              <Textarea
                rows={4}
                value={modelPatternsInput}
                onChange={(event) =>
                  updateModelPatternsDraft(event.target.value)
                }
                onBlur={commitModelPatternsDraft}
                placeholder={'^gpt-4o.*$\n^gpt-5.*$'}
              />
            </Field>
          </EditorSection>

          <EditorSection
            title={t('Runtime options')}
            description={t(
              'Options are evaluated only after direction, channel, and model match.'
            )}
          >
            <div className='flex flex-wrap items-center justify-between gap-3 rounded-md border bg-muted/20 p-3'>
              <div>
                <div className='flex flex-wrap items-center gap-2 text-sm font-medium'>
                  {t('Custom tool bridge')}
                  <Badge variant={bridgeSupported ? 'secondary' : 'outline'}>
                    {bridgeSupported ? t('Available') : t('Unavailable')}
                  </Badge>
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
                onCheckedChange={(checked) => {
                  setSavedEnableCustomToolBridge(checked)
                  onUpdate({ enable_custom_tool_bridge: checked })
                }}
              />
            </div>
          </EditorSection>

          <EditorSection
            title={t('Hit preview')}
            description={t(
              'Probe one sample channel and model without changing saved policy.'
            )}
          >
            <ProtocolConversionHitPreview
              rule={rule}
              preview={preview}
              channels={channels}
              passThroughEnabled={passThroughEnabled}
              onPreviewChange={setPreview}
            />
          </EditorSection>
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

type EditorSectionProps = {
  title: string
  description: string
  children: ReactNode
}

function EditorSection({
  title,
  description,
  children,
}: EditorSectionProps) {
  return (
    <section className='space-y-3 rounded-lg border bg-background p-4'>
      <div className='space-y-1'>
        <div className='text-sm font-semibold'>{title}</div>
        <div className='text-muted-foreground text-sm'>{description}</div>
      </div>
      <Separator />
      {children}
    </section>
  )
}

type DirectionOptionProps = {
  active: boolean
  title: string
  source: string
  target: string
  description: string
  onClick: () => void
}

function DirectionOption({
  active,
  title,
  source,
  target,
  description,
  onClick,
}: DirectionOptionProps) {
  const { t } = useTranslation()
  return (
    <button
      type='button'
      className={cn(
        'rounded-lg border p-3 text-left transition-colors',
        'hover:border-primary/60 hover:bg-accent',
        active
          ? 'border-primary bg-primary/5 ring-ring/30 ring-2'
          : 'bg-background'
      )}
      onClick={onClick}
    >
      <div className='flex items-center justify-between gap-3'>
        <div className='text-sm font-semibold'>{title}</div>
        <Badge variant={active ? 'default' : 'outline'}>
          {active ? t('Selected') : t('Option')}
        </Badge>
      </div>
      <div className='mt-3 flex flex-wrap items-center gap-2 text-xs'>
        <span className='rounded-md bg-muted px-2 py-1'>{source}</span>
        <ArrowRight className='text-muted-foreground size-3.5' />
        <span className='rounded-md bg-muted px-2 py-1'>{target}</span>
      </div>
      <div className='text-muted-foreground mt-3 text-xs'>{description}</div>
    </button>
  )
}
