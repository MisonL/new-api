import { useState, type Dispatch, type SetStateAction } from 'react'
import { ChevronDown, Settings } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { useSystemOptions } from '@/features/system-settings/hooks/use-system-options'
import {
  getModelTestRuntimeSnapshot,
  normalizeModelTestRuntimeConfig,
} from '../../lib/model-test-runtime-config'
import type { Channel, ModelTestRuntimeConfig } from '../../types'

type ModelTestRuntimePanelProps = {
  channel: Channel
  runtimeConfig: ModelTestRuntimeConfig
  setRuntimeConfig: Dispatch<SetStateAction<ModelTestRuntimeConfig>>
  selectedEndpointType: string
  disabled?: boolean
  onEditChannel?: () => void
}

type RuntimeToggleProps = {
  label: string
  configured: boolean
  checked: boolean
  disabled: boolean
  value?: string
  onCheckedChange: (checked: boolean) => void
}

function RuntimeToggle(props: RuntimeToggleProps) {
  const { t } = useTranslation()

  return (
    <div className='bg-background/60 flex items-center justify-between gap-3 rounded-md border px-3 py-2'>
      <div className='min-w-0'>
        <Label className='text-sm'>{props.label}</Label>
        <p
          className='text-muted-foreground truncate text-xs'
          title={props.value}
        >
          {props.value ||
            (props.configured ? t('Configured') : t('Not configured'))}
        </p>
      </div>
      <Switch
        checked={props.checked}
        disabled={props.disabled}
        onCheckedChange={props.onCheckedChange}
        aria-label={props.label}
      />
    </div>
  )
}

export function ModelTestRuntimePanel(props: ModelTestRuntimePanelProps) {
  const { t } = useTranslation()
  const normalized = normalizeModelTestRuntimeConfig(props.runtimeConfig)
  const snapshot = getModelTestRuntimeSnapshot(props.channel)
  const { data: systemOptionsData } = useSystemOptions()
  const [showAdvanced, setShowAdvanced] = useState(false)

  const updateConfig = <K extends keyof ModelTestRuntimeConfig>(
    key: K,
    value: ModelTestRuntimeConfig[K]
  ) => {
    props.setRuntimeConfig((previous) => ({
      ...normalizeModelTestRuntimeConfig(previous),
      [key]: value,
    }))
  }

  const subOptionDisabled = props.disabled || !normalized.enabled
  const chatConversionAllowed =
    props.selectedEndpointType === 'auto' ||
    props.selectedEndpointType === 'openai-response' ||
    props.selectedEndpointType === 'openai-response-compact'
  const globalPassThroughEnabled =
    systemOptionsData?.data?.find(
      (option) => option.key === 'global.pass_through_request_enabled'
    )?.value === 'true'

  return (
    <section className='bg-muted/20 space-y-3 rounded-md border p-3'>
      <div className='flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between'>
        <div>
          <div className='flex items-center gap-2'>
            <h3 className='text-sm font-medium'>
              {t('Model test runtime configuration')}
            </h3>
            <span className='text-muted-foreground text-xs'>
              {normalized.enabled ? t('Enabled') : t('Disabled')}
            </span>
            {globalPassThroughEnabled && (
              <span className='text-xs text-amber-600 dark:text-amber-400'>
                {t('Global pass-through enabled')}
              </span>
            )}
          </div>
          <p className='text-muted-foreground text-xs'>
            {t('Choose which channel settings are applied to this test.')}
          </p>
        </div>
        <div className='flex items-center gap-3'>
          <Label htmlFor='model-test-runtime-enabled' className='text-xs'>
            {t('Apply channel configuration')}
          </Label>
          <Switch
            id='model-test-runtime-enabled'
            checked={normalized.enabled}
            disabled={props.disabled}
            onCheckedChange={(checked) => updateConfig('enabled', checked)}
          />
          {props.onEditChannel && (
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={props.onEditChannel}
              disabled={props.disabled}
            >
              <Settings className='mr-1 h-3.5 w-3.5' />
              {t('Edit channel')}
            </Button>
          )}
        </div>
      </div>

      <div className='grid gap-2 sm:grid-cols-2'>
        <RuntimeToggle
          label={t('Header configuration')}
          configured={snapshot.headerConfigured}
          checked={normalized.enabled && normalized.headerConfig}
          disabled={subOptionDisabled}
          value={snapshot.headerValue}
          onCheckedChange={(checked) => updateConfig('headerConfig', checked)}
        />
        <RuntimeToggle
          label={t('Parameter override')}
          configured={snapshot.paramConfigured}
          checked={normalized.enabled && normalized.paramOverride}
          disabled={subOptionDisabled}
          value={snapshot.paramValue}
          onCheckedChange={(checked) => updateConfig('paramOverride', checked)}
        />
        <RuntimeToggle
          label={t('Channel proxy')}
          configured={snapshot.proxyConfigured}
          checked={normalized.enabled && normalized.proxy}
          disabled={subOptionDisabled}
          value={snapshot.proxyValue}
          onCheckedChange={(checked) => updateConfig('proxy', checked)}
        />
        <RuntimeToggle
          label={t('Model mapping')}
          configured={snapshot.modelMappingConfigured}
          checked={normalized.enabled && normalized.modelMapping}
          disabled={subOptionDisabled}
          value={snapshot.modelMappingValue}
          onCheckedChange={(checked) => updateConfig('modelMapping', checked)}
        />
      </div>

      <div className='grid gap-3 sm:grid-cols-[minmax(0,1fr)_12rem]'>
        <div className='grid gap-2'>
          <Label htmlFor='model-test-prompt'>{t('Test content')}</Label>
          <Textarea
            id='model-test-prompt'
            value={normalized.testPrompt}
            disabled={props.disabled}
            onChange={(event) => updateConfig('testPrompt', event.target.value)}
            rows={2}
          />
        </div>
        <div className='grid content-start gap-2'>
          <Label htmlFor='model-test-max-tokens'>
            {t('Maximum output tokens')}
          </Label>
          <Input
            id='model-test-max-tokens'
            type='number'
            min={1}
            max={8192}
            step={1}
            value={normalized.maxTokens}
            disabled={props.disabled}
            onChange={(event) =>
              updateConfig('maxTokens', Number(event.target.value))
            }
          />
        </div>
      </div>

      <div className='grid gap-2 sm:max-w-sm'>
        <Label htmlFor='model-test-response-protocol'>
          {t('Response protocol')}
        </Label>
        <Select
          value={normalized.responseProtocol}
          disabled={props.disabled}
          onValueChange={(value) =>
            updateConfig(
              'responseProtocol',
              value as ModelTestRuntimeConfig['responseProtocol']
            )
          }
        >
          <SelectTrigger id='model-test-response-protocol'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='native'>{t('Native Responses')}</SelectItem>
            <SelectItem
              value='chat_completions'
              disabled={!chatConversionAllowed}
            >
              {t('Chat Completions conversion')}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div className='border-t pt-2'>
        <Button
          type='button'
          variant='ghost'
          size='sm'
          className='text-muted-foreground px-0'
          onClick={() => setShowAdvanced((value) => !value)}
          aria-expanded={showAdvanced}
        >
          <ChevronDown
            className={`mr-1 h-4 w-4 transition-transform ${showAdvanced ? 'rotate-180' : ''}`}
          />
          {showAdvanced ? t('Hide') : t('Advanced Options')}
        </Button>
        {showAdvanced && (
          <div className='text-muted-foreground mt-2 grid gap-1 text-xs'>
            <span>
              {t('Header configuration')}:{' '}
              {snapshot.headerValue || t('Not configured')}
            </span>
            <span>
              {t('Parameter override')}:{' '}
              {snapshot.paramValue || t('Not configured')}
            </span>
            <span>
              {t('Channel proxy')}: {snapshot.proxyValue || t('Not configured')}
            </span>
            <span>
              {t('Model mapping')}:{' '}
              {snapshot.modelMappingValue || t('Not configured')}
            </span>
          </div>
        )}
      </div>
    </section>
  )
}
