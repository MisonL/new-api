import { ArrowRight, Pencil, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import type { Channel } from '@/features/channels/types'
import {
  ENDPOINT_CHAT,
  ENDPOINT_RESPONSES,
  TEMPLATE_CHAT_TO_RESPONSES,
  getProtocolRuleAttentionKeys,
  getProtocolRuleDirection,
  isResponsesToChatRule,
  type ProtocolRule,
} from './protocol-conversion-policy-utils'
import { ProtocolConversionRuleEditorSheet } from './protocol-conversion-rule-editor-sheet'

type ProtocolConversionRuleCardProps = {
  index: number
  rule: ProtocolRule
  channels: Channel[]
  channelsLoading: boolean
  channelsError: boolean
  passThroughEnabled: boolean
  onUpdate: (patch: Partial<ProtocolRule>) => void
  onRemove: () => void
}

export function ProtocolConversionRuleCard({
  index,
  rule,
  channels,
  channelsLoading,
  channelsError,
  passThroughEnabled,
  onUpdate,
  onRemove,
}: ProtocolConversionRuleCardProps) {
  const { t } = useTranslation()
  const extraCount =
    Object.keys(rule.extra).length + Object.keys(rule.optionsExtra).length
  const bridgeEnabled =
    isResponsesToChatRule(rule) && rule.enable_custom_tool_bridge
  const attentionKeys = getProtocolRuleAttentionKeys(rule, passThroughEnabled)
  const direction = getProtocolRuleDirection(rule)
  const sourceLabel = getEndpointLabel(rule.source_endpoint, t)
  const targetLabel = getEndpointLabel(rule.target_endpoint, t)
  const scopeState = rule.all_channels
    ? t('All channels')
    : rule.channel_ids.length === 0 && rule.channel_types.length === 0
      ? t('Empty scope')
      : t('Limited scope')
  const scopeDetail = rule.all_channels
    ? t('This rule can match any channel.')
    : t('{{channelCount}} channel IDs, {{typeCount}} channel types', {
        channelCount: rule.channel_ids.length,
        typeCount: rule.channel_types.length,
      })
  const modelDetail =
    rule.model_patterns.length === 0
      ? t('All non-empty models')
      : t('{{count}} model patterns', {
          count: rule.model_patterns.length,
        })

  return (
    <div className='space-y-4 rounded-xl border bg-background p-4 shadow-sm'>
      <div className='flex flex-wrap items-start justify-between gap-3'>
        <div className='min-w-0 space-y-2'>
          <div className='flex flex-wrap items-center gap-2'>
            <Badge variant='outline'>
              {t('Rule {{index}}', { index: index + 1 })}
            </Badge>
            <Badge variant={rule.enabled ? 'default' : 'secondary'}>
              {rule.enabled ? t('Enabled') : t('Disabled')}
            </Badge>
            {bridgeEnabled ? (
              <Badge variant='secondary'>{t('Custom tool bridge')}</Badge>
            ) : null}
            {extraCount > 0 ? (
              <Badge variant='outline'>{t('Advanced fields preserved')}</Badge>
            ) : null}
            {attentionKeys.length > 0 ? (
              <Badge variant='secondary' className='bg-amber-100 text-amber-900'>
                {t('{{count}} attention items', {
                  count: attentionKeys.length,
                })}
              </Badge>
            ) : null}
          </div>
          <div className='truncate text-sm font-medium'>
            {rule.name.trim() || t('Unnamed rule')}
          </div>
        </div>
        <div className='flex items-center gap-2'>
          <Switch
            checked={rule.enabled}
            onCheckedChange={(checked) => onUpdate({ enabled: checked })}
            aria-label={t('Toggle rule')}
          />
          <ProtocolConversionRuleEditorSheet
            index={index}
            rule={rule}
            channels={channels}
            channelsLoading={channelsLoading}
            channelsError={channelsError}
            passThroughEnabled={passThroughEnabled}
            onUpdate={onUpdate}
          >
            <Button type='button' variant='outline' size='sm'>
              <Pencil className='size-4' />
              {t('Edit')}
            </Button>
          </ProtocolConversionRuleEditorSheet>
          <AlertDialog>
            <AlertDialogTrigger asChild>
              <Button
                type='button'
                variant='ghost'
                size='icon'
                aria-label={t('Delete rule')}
              >
                <Trash2 className='size-4' />
              </Button>
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>{t('Delete rule')}</AlertDialogTitle>
                <AlertDialogDescription>
                  {t('The rule will be removed after you save the settings.')}
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
                <AlertDialogAction onClick={onRemove}>
                  {t('Delete')}
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        </div>
      </div>

      <div className='grid gap-3 lg:grid-cols-[1fr_auto_1fr_1.1fr]'>
        <ProtocolRuleCardPanel
          label={t('Incoming request')}
          title={sourceLabel}
          detail={
            direction === TEMPLATE_CHAT_TO_RESPONSES
              ? t('Client uses Chat Completions.')
              : t('Client uses Responses.')
          }
        />
        <div className='text-muted-foreground hidden items-center justify-center lg:flex'>
          <ArrowRight className='size-4' />
        </div>
        <ProtocolRuleCardPanel
          label={t('Upstream protocol')}
          title={targetLabel}
          detail={
            rule.target_endpoint === ENDPOINT_RESPONSES
              ? t('Forward upstream as Responses.')
              : t('Forward upstream as Chat Completions.')
          }
        />
        <ProtocolRuleCardPanel
          label={t('Runtime scope')}
          title={scopeState}
          detail={scopeDetail}
        />
      </div>

      <div className='grid gap-3 md:grid-cols-2'>
        <ProtocolRuleCardPanel
          label={t('Model boundary')}
          title={modelDetail}
          detail={
            rule.model_patterns.length === 0
              ? t('Model names still must be non-empty.')
              : rule.model_patterns.slice(0, 2).join(', ')
          }
        />
        <ProtocolRuleCardPanel
          label={t('Execution notes')}
          title={
            attentionKeys.length === 0
              ? t('Ready to evaluate')
              : t('Needs attention')
          }
          detail={
            attentionKeys[0] != null
              ? t(attentionKeys[0])
              : t(
                  'Rule will be evaluated when request direction and scope match.'
                )
          }
        />
      </div>
    </div>
  )
}

type ProtocolRuleCardPanelProps = {
  label: string
  title: string
  detail: string
}

function ProtocolRuleCardPanel({
  label,
  title,
  detail,
}: ProtocolRuleCardPanelProps) {
  return (
    <div className='rounded-lg border bg-muted/20 p-3'>
      <div className='text-muted-foreground text-xs font-medium'>{label}</div>
      <div className='mt-1 text-sm font-semibold'>{title}</div>
      <div className='text-muted-foreground mt-1 line-clamp-2 text-xs'>
        {detail}
      </div>
    </div>
  )
}

function getEndpointLabel(endpoint: string, t: (key: string) => string) {
  if (endpoint === ENDPOINT_CHAT) return t('Chat Completions')
  if (endpoint === ENDPOINT_RESPONSES) return t('Responses')
  return endpoint
}
