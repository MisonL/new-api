import { Pencil, Trash2 } from 'lucide-react'
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
  getProtocolRuleWarningKeys,
  isResponsesToChatRule,
  type ProtocolRule,
} from './protocol-conversion-policy-utils'
import { ProtocolConversionRuleEditorSheet } from './protocol-conversion-rule-editor-sheet'

type ProtocolConversionRuleCardProps = {
  index: number
  rule: ProtocolRule
  channels: Channel[]
  passThroughEnabled: boolean
  onUpdate: (patch: Partial<ProtocolRule>) => void
  onRemove: () => void
}

export function ProtocolConversionRuleCard({
  index,
  rule,
  channels,
  passThroughEnabled,
  onUpdate,
  onRemove,
}: ProtocolConversionRuleCardProps) {
  const { t } = useTranslation()
  const extraCount =
    Object.keys(rule.extra).length + Object.keys(rule.optionsExtra).length
  const bridgeEnabled =
    isResponsesToChatRule(rule) && rule.enable_custom_tool_bridge
  const warningCount =
    getProtocolRuleWarningKeys(rule).length + (passThroughEnabled ? 1 : 0)
  const scopeLabel = rule.all_channels
    ? t('All channels')
    : t('{{channelCount}} channels, {{typeCount}} types', {
        channelCount: rule.channel_ids.length,
        typeCount: rule.channel_types.length,
      })

  return (
    <div className='space-y-3 rounded-md border p-4'>
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
            {warningCount > 0 ? (
              <Badge variant='secondary'>
                {t('{{count}} warnings', { count: warningCount })}
              </Badge>
            ) : null}
          </div>
          <div className='truncate text-sm font-medium'>
            {rule.name.trim() || t('Unnamed rule')}
          </div>
          <div className='text-muted-foreground flex flex-wrap gap-x-3 gap-y-1 text-xs'>
            <span>
              {rule.source_endpoint} {'->'} {rule.target_endpoint}
            </span>
            <span>{scopeLabel}</span>
            <span>
              {t('{{count}} model patterns', {
                count: rule.model_patterns.length,
              })}
            </span>
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
    </div>
  )
}
