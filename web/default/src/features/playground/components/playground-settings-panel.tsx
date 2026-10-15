import { useRef } from 'react'
import {
  AlertCircle,
  Check,
  Download,
  FileJson,
  ImagePlus,
  Pencil,
  RotateCcw,
  Settings2,
  Trash2,
  Upload,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import {
  formatCustomRequestBody,
  validateCustomRequestBody,
} from '../lib/custom-request'
import { normalizePlaygroundConfig } from '../lib/storage'
import type {
  GroupOption,
  Message,
  ModelOption,
  ParameterEnabled,
  PlaygroundConfig,
} from '../types'

interface PlaygroundSettingsPanelProps {
  config: PlaygroundConfig
  parameterEnabled: ParameterEnabled
  messages: Message[]
  models: ModelOption[]
  groups: GroupOption[]
  previewPayload: Record<string, unknown> | null
  onConfigChange: <K extends keyof PlaygroundConfig>(
    key: K,
    value: PlaygroundConfig[K]
  ) => void
  onParameterToggle: (key: keyof ParameterEnabled, value: boolean) => void
  onImportMessages: (value: unknown) => void
  onReset: () => void
  onClearMessages: () => void
  onCustomRequestModeChange: (enabled: boolean) => void
}

const parameterMeta: Array<{
  key: keyof ParameterEnabled
  label: string
  min: number
  max: number
  step: number
}> = [
  { key: 'temperature', label: 'Temperature', min: 0, max: 2, step: 0.1 },
  { key: 'top_p', label: 'Top P', min: 0, max: 1, step: 0.05 },
  {
    key: 'frequency_penalty',
    label: 'Frequency penalty',
    min: -2,
    max: 2,
    step: 0.1,
  },
  {
    key: 'presence_penalty',
    label: 'Presence penalty',
    min: -2,
    max: 2,
    step: 0.1,
  },
  { key: 'max_tokens', label: 'Max tokens', min: 1, max: 1_000_000, step: 1 },
  { key: 'seed', label: 'Seed', min: 0, max: 2_147_483_647, step: 1 },
]

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

export function PlaygroundSettingsPanel({
  config,
  parameterEnabled,
  messages,
  models,
  groups,
  previewPayload,
  onConfigChange,
  onParameterToggle,
  onImportMessages,
  onReset,
  onClearMessages,
  onCustomRequestModeChange,
}: PlaygroundSettingsPanelProps) {
  const { t } = useTranslation()
  const importRef = useRef<HTMLInputElement>(null)

  const customRequestValidation = validateCustomRequestBody(
    config.customRequestMode ? config.customRequestBody : ''
  )

  const handleFormatCustomRequest = () => {
    try {
      onConfigChange(
        'customRequestBody',
        formatCustomRequestBody(config.customRequestBody)
      )
    } catch (error) {
      if (
        error instanceof Error &&
        error.message === 'JSON must be an object'
      ) {
        toast.error(t('JSON must be an object'))
        return
      }
      toast.error(
        `${t('JSON format error')}: ${
          error instanceof Error ? error.message : t('Invalid JSON')
        }`
      )
    }
  }

  const handleImport = (file: File | undefined) => {
    if (!file) return
    const reader = new FileReader()
    reader.onload = () => {
      try {
        const value: unknown = JSON.parse(String(reader.result || ''))
        if (!isRecord(value)) throw new Error(t('Invalid configuration file'))
        importValue(value)
        toast.success(t('Configuration imported'))
      } catch (error) {
        toast.error(error instanceof Error ? error.message : t('Invalid JSON'))
      } finally {
        if (importRef.current) importRef.current.value = ''
      }
    }
    reader.onerror = () => toast.error(t('Failed to read configuration file'))
    reader.readAsText(file)
  }

  const handleExport = () => {
    const payload = {
      config,
      parameterEnabled,
      messages,
      exportedAt: new Date().toISOString(),
      version: 1,
    }
    const blob = new Blob([JSON.stringify(payload, null, 2)], {
      type: 'application/json',
    })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `playground-config-${new Date().toISOString().slice(0, 10)}.json`
    link.click()
    URL.revokeObjectURL(url)
  }

  const importValue = (value: unknown) => {
    if (!isRecord(value)) return
    const importedConfig = isRecord(value.config)
      ? value.config
      : isRecord(value.inputs)
        ? { ...value, ...value.inputs }
        : value
    const normalizedConfig = normalizePlaygroundConfig(importedConfig)
    if (!normalizedConfig) return
    const importedParameters = isRecord(value.parameterEnabled)
      ? value.parameterEnabled
      : isRecord(importedConfig.parameterEnabled)
        ? importedConfig.parameterEnabled
        : null

    for (const key of [
      'model',
      'group',
      'temperature',
      'top_p',
      'max_tokens',
      'frequency_penalty',
      'presence_penalty',
      'seed',
      'stream',
      'imageEnabled',
      'imageUrls',
      'customRequestMode',
      'customRequestBody',
      'showDebugPanel',
    ] as const) {
      if (key in normalizedConfig) {
        onConfigChange(
          key,
          normalizedConfig[key] as PlaygroundConfig[typeof key]
        )
      }
    }
    if (importedParameters) {
      for (const item of parameterMeta) {
        if (typeof importedParameters[item.key] === 'boolean') {
          onParameterToggle(item.key, importedParameters[item.key] as boolean)
        }
      }
    }
    if (Array.isArray(value.messages)) onImportMessages(value.messages)
  }

  return (
    <div className='flex min-h-0 flex-col gap-4 overflow-y-auto p-4'>
      <div className='flex items-center gap-2'>
        <Settings2 className='text-muted-foreground h-4 w-4' />
        <div>
          <h2 className='text-sm font-semibold'>{t('Playground settings')}</h2>
          <p className='text-muted-foreground text-xs'>
            {t('Configure the request sent by the playground.')}
          </p>
        </div>
      </div>

      <div className='grid gap-3'>
        <div className='grid gap-1.5'>
          <Label>{t('Model')}</Label>
          <Select
            value={config.model}
            onValueChange={(value) => onConfigChange('model', value)}
            disabled={config.customRequestMode}
          >
            <SelectTrigger>
              <SelectValue placeholder={t('Choose model')} />
            </SelectTrigger>
            <SelectContent>
              {models.map((model) => (
                <SelectItem key={model.value} value={model.value}>
                  {model.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className='grid gap-1.5'>
          <Label>{t('Group')}</Label>
          <Select
            value={config.group}
            onValueChange={(value) => onConfigChange('group', value)}
            disabled={config.customRequestMode}
          >
            <SelectTrigger>
              <SelectValue placeholder={t('Choose group')} />
            </SelectTrigger>
            <SelectContent>
              {groups.map((group) => (
                <SelectItem key={group.value} value={group.value}>
                  {group.label}
                  {group.desc ? ` (${group.desc})` : ''}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      <div className='flex items-center justify-between gap-3 rounded-md border p-3'>
        <div>
          <Label>{t('Stream response')}</Label>
          <p className='text-muted-foreground text-xs'>
            {t('Receive response tokens as an SSE stream.')}
          </p>
        </div>
        <Switch
          checked={config.stream}
          disabled={config.customRequestMode}
          onCheckedChange={(value) => onConfigChange('stream', value)}
        />
      </div>

      <Separator />
      <div className='space-y-3'>
        <div>
          <h3 className='text-sm font-medium'>{t('Parameters')}</h3>
          <p className='text-muted-foreground text-xs'>
            {t('Only enabled parameters are included in the request.')}
          </p>
        </div>
        {parameterMeta.map((item) => {
          const value = config[item.key]
          const enabled = parameterEnabled[item.key]
          const inputValue = value == null ? '' : String(value)
          return (
            <div className='grid gap-2 rounded-md border p-3' key={item.key}>
              <div className='flex items-center justify-between gap-2'>
                <Label htmlFor={`playground-${item.key}`}>
                  {t(item.label)}
                </Label>
                <Switch
                  checked={enabled}
                  disabled={config.customRequestMode}
                  onCheckedChange={(checked) =>
                    onParameterToggle(item.key, checked)
                  }
                />
              </div>
              <Input
                id={`playground-${item.key}`}
                type='number'
                min={item.min}
                max={item.max}
                step={item.step}
                value={inputValue}
                disabled={!enabled || config.customRequestMode}
                onChange={(event) => {
                  const raw = event.target.value
                  if (item.key === 'seed') {
                    onConfigChange(
                      item.key,
                      raw === ''
                        ? null
                        : (Number(raw) as PlaygroundConfig[typeof item.key])
                    )
                    return
                  }
                  onConfigChange(
                    item.key,
                    Number(raw) as PlaygroundConfig[typeof item.key]
                  )
                }}
              />
            </div>
          )
        })}
      </div>

      <Separator />
      <div className='space-y-3'>
        <div className='flex items-center justify-between gap-3'>
          <div>
            <h3 className='text-sm font-medium'>{t('Image input')}</h3>
            <p className='text-muted-foreground text-xs'>
              {t('Add image URLs or paste an image into the message box.')}
            </p>
          </div>
          <Switch
            checked={config.imageEnabled}
            disabled={config.customRequestMode}
            onCheckedChange={(value) => onConfigChange('imageEnabled', value)}
          />
        </div>
        {config.imageEnabled && (
          <div className='space-y-2'>
            {config.imageUrls.map((url, index) => (
              // Keyed by index only: including the value remounts the input
              // on every keystroke, which drops focus.
              <div className='flex items-center gap-2' key={index}>
                <Input
                  value={url}
                  placeholder={t('Image URL')}
                  onChange={(event) => {
                    const next = [...config.imageUrls]
                    next[index] = event.target.value
                    onConfigChange('imageUrls', next)
                  }}
                />
                <Button
                  type='button'
                  variant='ghost'
                  size='icon'
                  aria-label={t('Remove image')}
                  onClick={() =>
                    onConfigChange(
                      'imageUrls',
                      config.imageUrls.filter(
                        (_, itemIndex) => itemIndex !== index
                      )
                    )
                  }
                >
                  <Trash2 className='text-destructive h-4 w-4' />
                </Button>
              </div>
            ))}
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() =>
                onConfigChange('imageUrls', [...config.imageUrls, ''])
              }
            >
              <ImagePlus className='mr-1.5 h-4 w-4' />
              {t('Add image URL')}
            </Button>
          </div>
        )}
      </div>

      <Separator />
      <div className='space-y-2'>
        <div className='flex items-center justify-between gap-2'>
          <div>
            <h3 className='text-sm font-medium'>{t('Custom request body')}</h3>
            <p className='text-muted-foreground text-xs'>
              {t('When enabled, this JSON object is sent as-is.')}
            </p>
          </div>
          <Switch
            checked={config.customRequestMode}
            onCheckedChange={onCustomRequestModeChange}
          />
        </div>
        {config.customRequestMode && (
          <>
            <div className='flex items-center justify-between gap-2'>
              <div className='flex items-center gap-1.5 text-xs'>
                {customRequestValidation.valid ? (
                  <Check className='h-3.5 w-3.5 text-emerald-600' />
                ) : (
                  <AlertCircle className='text-destructive h-3.5 w-3.5' />
                )}
                <span
                  className={
                    customRequestValidation.valid
                      ? 'text-muted-foreground'
                      : 'text-destructive'
                  }
                >
                  {customRequestValidation.valid
                    ? t('Valid')
                    : customRequestValidation.message}
                </span>
              </div>
              <Button
                type='button'
                variant='ghost'
                size='sm'
                onClick={handleFormatCustomRequest}
                disabled={!config.customRequestBody.trim()}
              >
                <Pencil className='mr-1.5 h-3.5 w-3.5' />
                {t('Format JSON')}
              </Button>
            </div>
            <Textarea
              className={`min-h-44 font-mono text-xs ${
                customRequestValidation.valid ? '' : 'border-destructive'
              }`}
              value={config.customRequestBody}
              onChange={(event) =>
                onConfigChange('customRequestBody', event.target.value)
              }
              aria-label={t('Custom request body')}
            />
            {!customRequestValidation.valid && (
              <p className='text-destructive text-xs'>
                {customRequestValidation.message}
              </p>
            )}
          </>
        )}
        {config.customRequestMode && previewPayload && (
          <details className='rounded-md border p-2'>
            <summary className='cursor-pointer text-xs font-medium'>
              {t('Request preview')}
            </summary>
            <pre className='text-muted-foreground mt-2 max-h-48 overflow-auto text-xs break-all whitespace-pre-wrap'>
              {JSON.stringify(previewPayload, null, 2)}
            </pre>
          </details>
        )}
      </div>

      <Separator />
      <div className='grid grid-cols-2 gap-2'>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => importRef.current?.click()}
        >
          <Upload className='mr-1.5 h-4 w-4' />
          {t('Import')}
        </Button>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={handleExport}
        >
          <Download className='mr-1.5 h-4 w-4' />
          {t('Export')}
        </Button>
        <Button type='button' variant='outline' size='sm' onClick={onReset}>
          <RotateCcw className='mr-1.5 h-4 w-4' />
          {t('Reset settings')}
        </Button>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={onClearMessages}
        >
          <Trash2 className='mr-1.5 h-4 w-4' />
          {t('Clear conversation')}
        </Button>
      </div>
      <input
        ref={importRef}
        type='file'
        accept='application/json,.json'
        className='hidden'
        onChange={(event) => handleImport(event.target.files?.[0])}
      />
      <div className='text-muted-foreground flex items-center gap-2 text-xs'>
        <FileJson className='h-3.5 w-3.5' />
        {t('Configuration includes the current conversation.')}
      </div>
    </div>
  )
}
