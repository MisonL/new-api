import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Code2, Loader2, Plus, RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { getChannels } from '@/features/channels/api'
import { channelsQueryKeys } from '@/features/channels/lib'
import {
  TEMPLATE_BIDIRECTIONAL,
  TEMPLATE_CHAT_TO_RESPONSES,
  TEMPLATE_RESPONSES_TO_CHAT,
  createProtocolRuleFromTemplate,
  isResponsesToChatRule,
  parseProtocolPolicy,
  serializeProtocolPolicy,
  type ProtocolRuleTemplate,
  type ProtocolRule,
} from './protocol-conversion-policy-utils'
import { reconcileProtocolRuleEditorKeys } from './protocol-conversion-policy-editor-keys'
import { ProtocolConversionRuleCard } from './protocol-conversion-rule-card'

type ProtocolConversionPolicyEditorProps = {
  value: string
  savedValue: string
  passThroughEnabled: boolean
  onChange: (value: string) => void
}

// The selector is an aid, not the source of truth; truncated results show a JSON fallback hint.
const CHANNEL_SELECTOR_PAGE_SIZE = 1000
const CHANNEL_SELECTOR_STALE_TIME_MS = 60_000
const CHANNEL_SELECTOR_GC_TIME_MS = 5 * 60_000
const EMPTY_PROTOCOL_RULES: ProtocolRule[] = []

let editorRuleKeyCounter = 0

function nextEditorRuleKey() {
  editorRuleKeyCounter += 1
  return `protocol-rule-editor-${Date.now()}-${editorRuleKeyCounter}`
}

function createEditorRuleKeys(ruleCount: number) {
  return Array.from({ length: ruleCount }, nextEditorRuleKey)
}

function fallbackEditorRuleKey(index: number) {
  return `external-rule-${index}`
}

export function ProtocolConversionPolicyEditor({
  value,
  savedValue,
  passThroughEnabled,
  onChange,
}: ProtocolConversionPolicyEditorProps) {
  const { t } = useTranslation()
  const parsed = useMemo(() => parseProtocolPolicy(value), [value])
  const rules = parsed.ok ? parsed.rules : EMPTY_PROTOCOL_RULES
  const policyExtra = parsed.ok ? parsed.policyExtra : {}
  const [ruleKeys, setRuleKeys] = useState(() =>
    createEditorRuleKeys(rules.length)
  )
  const [jsonOpen, setJsonOpen] = useState(false)
  const [jsonDraft, setJsonDraft] = useState(value || '{}')
  const [ruleTemplate, setRuleTemplate] = useState<ProtocolRuleTemplate>(
    TEMPLATE_RESPONSES_TO_CHAT
  )

  const {
    data: channelsData,
    isLoading: isChannelsLoading,
    isError: isChannelsError,
  } = useQuery({
    queryKey: channelsQueryKeys.list({
      p: 1,
      page_size: CHANNEL_SELECTOR_PAGE_SIZE,
    }),
    queryFn: () => getChannels({ p: 1, page_size: CHANNEL_SELECTOR_PAGE_SIZE }),
    staleTime: CHANNEL_SELECTOR_STALE_TIME_MS,
    gcTime: CHANNEL_SELECTOR_GC_TIME_MS,
    refetchOnWindowFocus: false,
  })
  const channels = channelsData?.data?.items ?? []
  const channelTotal = channelsData?.data?.total ?? 0
  const channelsTruncated = channelTotal > channels.length

  const commitRules = (nextRules: ProtocolRule[]) => {
    onChange(serializeProtocolPolicy(nextRules, policyExtra))
  }

  const updateRule = (ruleIndex: number, patch: Partial<ProtocolRule>) => {
    commitRules(
      rules.map((rule, index) => {
        if (index !== ruleIndex) return rule
        const nextRule = { ...rule, ...patch }
        if (!isResponsesToChatRule(nextRule)) {
          nextRule.enable_custom_tool_bridge = false
        }
        return nextRule
      })
    )
  }

  const importJson = () => {
    const next = parseProtocolPolicy(jsonDraft)
    if (!next.ok) {
      toast.error(next.error)
      return
    }
    setRuleKeys(createEditorRuleKeys(next.rules.length))
    onChange(serializeProtocolPolicy(next.rules, next.policyExtra))
    setJsonOpen(false)
  }

  const restoreSavedValue = () => {
    const nextValue = savedValue || '{}'
    const next = parseProtocolPolicy(nextValue)
    if (!next.ok) {
      toast.error(next.error)
      return
    }
    setRuleKeys(createEditorRuleKeys(next.rules.length))
    onChange(nextValue)
  }

  const addRule = () => {
    const nextRules = createProtocolRuleFromTemplate(ruleTemplate, rules)
    setRuleKeys((currentKeys) =>
      [
        ...reconcileProtocolRuleEditorKeys(
          currentKeys,
          rules.length,
          nextEditorRuleKey,
          fallbackEditorRuleKey
        ),
        ...createEditorRuleKeys(nextRules.length),
      ]
    )
    const updatedRules = [...rules, ...nextRules]
    commitRules(updatedRules)
  }

  const removeRule = (ruleIndex: number) => {
    setRuleKeys((currentKeys) => {
      const currentRuleKeys = reconcileProtocolRuleEditorKeys(
        currentKeys,
        rules.length,
        nextEditorRuleKey,
        fallbackEditorRuleKey
      )
      return currentRuleKeys.filter((_, index) => index !== ruleIndex)
    })
    const updatedRules = rules.filter((_, index) => index !== ruleIndex)
    commitRules(updatedRules)
  }

  return (
    <div className='space-y-4 rounded-lg border p-4'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div className='space-y-1'>
          <div className='text-sm font-medium'>{t('Policy rules')}</div>
          <div className='text-muted-foreground text-sm'>
            {t(
              'Create explicit conversion rules without editing JSON by hand.'
            )}
          </div>
        </div>
        <div className='flex flex-wrap gap-2'>
          <Select
            value={ruleTemplate}
            onValueChange={(value) =>
              setRuleTemplate(value as ProtocolRuleTemplate)
            }
          >
            <SelectTrigger
              className='h-8 w-[180px]'
              aria-label={t('Rule template')}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={TEMPLATE_RESPONSES_TO_CHAT}>
                {t('Responses -> Chat')}
              </SelectItem>
              <SelectItem value={TEMPLATE_CHAT_TO_RESPONSES}>
                {t('Chat -> Responses')}
              </SelectItem>
              <SelectItem value={TEMPLATE_BIDIRECTIONAL}>
                {t('Bidirectional')}
              </SelectItem>
            </SelectContent>
          </Select>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => {
              setJsonDraft(value || '{}')
              setJsonOpen(true)
            }}
          >
            <Code2 className='size-4' />
            {t('Import or view JSON')}
          </Button>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={restoreSavedValue}
          >
            <RotateCcw className='size-4' />
            {t('Restore saved')}
          </Button>
          <Button type='button' size='sm' onClick={addRule}>
            <Plus className='size-4' />
            {t('Add rule')}
          </Button>
        </div>
      </div>

      {!parsed.ok ? (
        <div className='border-destructive text-destructive rounded-md border p-3 text-sm'>
          {t('Invalid JSON format')}: {parsed.error}
        </div>
      ) : null}

      {rules.length === 0 ? (
        <div className='text-muted-foreground rounded-md border border-dashed p-6 text-center text-sm'>
          {t('No protocol conversion rules configured.')}
        </div>
      ) : (
        <div className='space-y-3'>
          {isChannelsError ? (
            <Alert variant='destructive'>
              <AlertDescription>
                {t('Failed to load channels for selector.')}
              </AlertDescription>
            </Alert>
          ) : isChannelsLoading ? (
            <Alert>
              <Loader2 className='size-4 animate-spin' />
              <AlertDescription>{t('Loading channels...')}</AlertDescription>
            </Alert>
          ) : channelsTruncated ? (
            <Alert>
              <AlertDescription>
                {t(
                  'Only the first {{loaded}} of {{total}} channels are available in the selector. Use channel type or JSON for channels beyond this range.',
                  { loaded: channels.length, total: channelTotal }
                )}
              </AlertDescription>
            </Alert>
          ) : null}
          {rules.map((rule, index) => (
            <ProtocolConversionRuleCard
              key={ruleKeys[index] ?? fallbackEditorRuleKey(index)}
              index={index}
              rule={rule}
              channels={channels}
              channelsLoading={isChannelsLoading}
              channelsError={isChannelsError}
              passThroughEnabled={passThroughEnabled}
              onUpdate={(patch) => updateRule(index, patch)}
              onRemove={() => removeRule(index)}
            />
          ))}
        </div>
      )}

      <Dialog open={jsonOpen} onOpenChange={setJsonOpen}>
        <DialogContent className='sm:max-w-3xl'>
          <DialogHeader>
            <DialogTitle>{t('Import or view JSON')}</DialogTitle>
            <DialogDescription>
              {t('Unknown advanced fields are preserved when rules are saved.')}
            </DialogDescription>
          </DialogHeader>
          <Textarea
            rows={18}
            className='font-mono text-xs'
            value={jsonDraft}
            onChange={(event) => setJsonDraft(event.target.value)}
          />
          <DialogFooter>
            <Button
              type='button'
              variant='outline'
              onClick={() => setJsonOpen(false)}
            >
              {t('Cancel')}
            </Button>
            <Button type='button' onClick={importJson}>
              {t('Apply JSON')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
