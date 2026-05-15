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

export function ProtocolConversionPolicyEditor({
  value,
  savedValue,
  passThroughEnabled,
  onChange,
}: ProtocolConversionPolicyEditorProps) {
  const { t } = useTranslation()
  const [jsonOpen, setJsonOpen] = useState(false)
  const [jsonDraft, setJsonDraft] = useState(value || '{}')

  const parsed = useMemo(() => parseProtocolPolicy(value), [value])
  const rules = parsed.ok ? parsed.rules : []
  const policyExtra = parsed.ok ? parsed.policyExtra : {}

  const { data: channelsData } = useQuery({
    queryKey: channelsQueryKeys.list({ p: 1, page_size: 200 }),
    queryFn: () => getChannels({ p: 1, page_size: 200 }),
  })

  const commitRules = (nextRules: ProtocolRule[]) => {
    onChange(serializeProtocolPolicy(nextRules, policyExtra))
  }

  const updateRule = (clientKey: string, patch: Partial<ProtocolRule>) => {
    commitRules(
      rules.map((rule) => {
        if (rule.clientKey !== clientKey) return rule
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
    onChange(serializeProtocolPolicy(next.rules, next.policyExtra))
    setJsonOpen(false)
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
            onClick={() => onChange(savedValue || '{}')}
          >
            <RotateCcw className='size-4' />
            {t('Restore saved')}
          </Button>
          <Button
            type='button'
            size='sm'
            onClick={() => commitRules([...rules, createProtocolRule()])}
          >
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
              key={rule.clientKey}
              index={index}
              rule={rule}
              channels={channelsData?.data?.items ?? []}
              passThroughEnabled={passThroughEnabled}
              onUpdate={(patch) => updateRule(rule.clientKey, patch)}
              onRemove={() =>
                commitRules(
                  rules.filter((item) => item.clientKey !== rule.clientKey)
                )
              }
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
