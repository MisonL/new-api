import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Code2, Plus, RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'
import { getChannels } from '@/features/channels/api'
import { channelsQueryKeys } from '@/features/channels/lib'
import {
  createProtocolRule,
  isResponsesToChatRule,
  parseProtocolPolicy,
  serializeProtocolPolicy,
  type ProtocolRule,
} from './protocol-conversion-policy-utils'
import { ProtocolConversionRuleCard } from './protocol-conversion-rule-card'

type ProtocolConversionPolicyEditorProps = {
  value: string
  savedValue: string
  passThroughEnabled: boolean
  onChange: (value: string) => void
}

let editorRuleKeyCounter = 0

function nextEditorRuleKey() {
  editorRuleKeyCounter += 1
  return `protocol-rule-editor-${Date.now()}-${editorRuleKeyCounter}`
}

export function ProtocolConversionPolicyEditor({
  value,
  savedValue,
  passThroughEnabled,
  onChange,
}: ProtocolConversionPolicyEditorProps) {
  const { t } = useTranslation()
  const parsed = useMemo(() => parseProtocolPolicy(value), [value])
  const rules = parsed.ok ? parsed.rules : []
  const policyExtra = parsed.ok ? parsed.policyExtra : {}
  const [ruleKeys, setRuleKeys] = useState(() =>
    rules.map(() => nextEditorRuleKey())
  )
  const [jsonOpen, setJsonOpen] = useState(false)
  const [jsonDraft, setJsonDraft] = useState(value || '{}')

  const { data: channelsData } = useQuery({
    queryKey: channelsQueryKeys.list({ p: 1, page_size: 200 }),
    queryFn: () => getChannels({ p: 1, page_size: 200 }),
  })

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
    setRuleKeys(next.rules.map(() => nextEditorRuleKey()))
    onChange(serializeProtocolPolicy(next.rules, next.policyExtra))
    setJsonOpen(false)
  }

  const restoreSavedValue = () => {
    const nextValue = savedValue || '{}'
    const next = parseProtocolPolicy(nextValue)
    setRuleKeys(next.ok ? next.rules.map(() => nextEditorRuleKey()) : [])
    onChange(nextValue)
  }

  const addRule = () => {
    setRuleKeys((currentKeys) => [...currentKeys, nextEditorRuleKey()])
    commitRules([...rules, createProtocolRule()])
  }

  const removeRule = (ruleIndex: number) => {
    setRuleKeys((currentKeys) =>
      currentKeys.filter((_, index) => index !== ruleIndex)
    )
    commitRules(rules.filter((_, index) => index !== ruleIndex))
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
          {rules.map((rule, index) => (
            <ProtocolConversionRuleCard
              key={ruleKeys[index] ?? `external-rule-${index}`}
              index={index}
              rule={rule}
              channels={channelsData?.data?.items ?? []}
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
