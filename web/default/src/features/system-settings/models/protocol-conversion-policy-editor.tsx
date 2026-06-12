import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Code2, Filter, Loader2, Plus, RotateCcw, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
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
import { Input } from '@/components/ui/input'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { getChannels } from '@/features/channels/api'
import { channelsQueryKeys } from '@/features/channels/lib'
import { reconcileProtocolRuleEditorKeys } from './protocol-conversion-policy-editor-keys'
import {
  PROTOCOL_FILTER_ALL,
  PROTOCOL_RULE_SCOPE_EMPTY,
  PROTOCOL_RULE_SCOPE_GLOBAL,
  PROTOCOL_RULE_SCOPE_LIMITED,
  PROTOCOL_RULE_STATE_ATTENTION,
  PROTOCOL_RULE_STATE_DISABLED,
  PROTOCOL_RULE_STATE_ENABLED,
  TEMPLATE_BIDIRECTIONAL,
  TEMPLATE_CHAT_TO_RESPONSES,
  TEMPLATE_RESPONSES_TO_CHAT,
  createProtocolRuleFromTemplate,
  filterProtocolRules,
  getProtocolPolicyStats,
  isResponsesToChatRule,
  parseProtocolPolicy,
  serializeProtocolPolicy,
  type ProtocolDirectionFilter,
  type ProtocolPolicyStats,
  type ProtocolRuleFilters,
  type ProtocolRuleScopeFilter,
  type ProtocolRuleStateFilter,
  type ProtocolRuleTemplate,
  type ProtocolRule,
} from './protocol-conversion-policy-utils'
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
const DEFAULT_PROTOCOL_RULE_FILTERS: ProtocolRuleFilters = {
  direction: PROTOCOL_FILTER_ALL,
  state: PROTOCOL_FILTER_ALL,
  scope: PROTOCOL_FILTER_ALL,
  query: '',
}

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
    TEMPLATE_CHAT_TO_RESPONSES
  )
  const [filters, setFilters] = useState<ProtocolRuleFilters>(
    DEFAULT_PROTOCOL_RULE_FILTERS
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
  const stats = useMemo(
    () => getProtocolPolicyStats(rules, passThroughEnabled),
    [rules, passThroughEnabled]
  )
  const filteredRules = useMemo(
    () => filterProtocolRules(rules, filters, passThroughEnabled),
    [rules, filters, passThroughEnabled]
  )
  const hasActiveFilters =
    filters.direction !== PROTOCOL_FILTER_ALL ||
    filters.state !== PROTOCOL_FILTER_ALL ||
    filters.scope !== PROTOCOL_FILTER_ALL ||
    filters.query.trim() !== ''

  const commitRules = (nextRules: ProtocolRule[]) => {
    onChange(serializeProtocolPolicy(nextRules, policyExtra))
  }

  const updateFilters = (patch: Partial<ProtocolRuleFilters>) => {
    setFilters((current) => ({ ...current, ...patch }))
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
    setRuleKeys((currentKeys) => [
      ...reconcileProtocolRuleEditorKeys(
        currentKeys,
        rules.length,
        nextEditorRuleKey,
        fallbackEditorRuleKey
      ),
      ...createEditorRuleKeys(nextRules.length),
    ])
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

      {parsed.ok ? (
        <ProtocolRuleControlPanel
          stats={stats}
          filters={filters}
          visibleCount={filteredRules.length}
          hasActiveFilters={hasActiveFilters}
          onFiltersChange={updateFilters}
          onResetFilters={() =>
            setFilters({ ...DEFAULT_PROTOCOL_RULE_FILTERS })
          }
        />
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
          {filteredRules.length === 0 ? (
            <div className='text-muted-foreground rounded-md border border-dashed p-6 text-center text-sm'>
              {t('No rules match the current filters.')}
            </div>
          ) : (
            filteredRules.map(({ rule, index }) => (
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
            ))
          )}
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

type ProtocolRuleControlPanelProps = {
  stats: ProtocolPolicyStats
  filters: ProtocolRuleFilters
  visibleCount: number
  hasActiveFilters: boolean
  onFiltersChange: (patch: Partial<ProtocolRuleFilters>) => void
  onResetFilters: () => void
}

function ProtocolRuleControlPanel({
  stats,
  filters,
  visibleCount,
  hasActiveFilters,
  onFiltersChange,
  onResetFilters,
}: ProtocolRuleControlPanelProps) {
  const { t } = useTranslation()
  return (
    <div className='space-y-4 rounded-xl border bg-muted/20 p-4'>
      <div className='grid gap-3 md:grid-cols-4'>
        <ProtocolMetric
          label={t('Rule inventory')}
          value={`${stats.enabled}/${stats.total}`}
          detail={t('{{count}} disabled', { count: stats.disabled })}
        />
        <ProtocolMetric
          label={t('Chat to Responses')}
          value={stats.chatToResponses}
          detail={t('Client chat requests')}
        />
        <ProtocolMetric
          label={t('Responses to Chat')}
          value={stats.responsesToChat}
          detail={t('Chat-only upstreams')}
        />
        <ProtocolMetric
          label={t('Needs attention')}
          value={stats.attention}
          detail={t('Disabled, empty scope, or passthrough')}
          intent={stats.attention > 0 ? 'warning' : 'default'}
        />
      </div>

      <div className='grid gap-3 lg:grid-cols-[1.2fr_1fr_1fr_1.2fr_auto]'>
        <div className='space-y-2'>
          <div className='text-muted-foreground flex items-center gap-2 text-xs font-medium'>
            <Filter className='size-3.5' />
            {t('Direction')}
          </div>
          <Tabs
            value={filters.direction}
            onValueChange={(value) =>
              onFiltersChange({
                direction: value as ProtocolDirectionFilter,
              })
            }
          >
            <TabsList className='grid w-full grid-cols-3'>
              <TabsTrigger value={PROTOCOL_FILTER_ALL}>
                {t('All')}
              </TabsTrigger>
              <TabsTrigger value={TEMPLATE_CHAT_TO_RESPONSES}>
                {t('Chat -> Responses')}
              </TabsTrigger>
              <TabsTrigger value={TEMPLATE_RESPONSES_TO_CHAT}>
                {t('Responses -> Chat')}
              </TabsTrigger>
            </TabsList>
          </Tabs>
        </div>

        <ProtocolFilterSelect
          label={t('State')}
          value={filters.state}
          onValueChange={(value) =>
            onFiltersChange({ state: value as ProtocolRuleStateFilter })
          }
          options={[
            { value: PROTOCOL_FILTER_ALL, label: t('All states') },
            { value: PROTOCOL_RULE_STATE_ENABLED, label: t('Enabled') },
            { value: PROTOCOL_RULE_STATE_DISABLED, label: t('Disabled') },
            {
              value: PROTOCOL_RULE_STATE_ATTENTION,
              label: t('Needs attention'),
            },
          ]}
        />

        <ProtocolFilterSelect
          label={t('Scope')}
          value={filters.scope}
          onValueChange={(value) =>
            onFiltersChange({ scope: value as ProtocolRuleScopeFilter })
          }
          options={[
            { value: PROTOCOL_FILTER_ALL, label: t('All scopes') },
            { value: PROTOCOL_RULE_SCOPE_GLOBAL, label: t('All channels') },
            { value: PROTOCOL_RULE_SCOPE_LIMITED, label: t('Limited scope') },
            { value: PROTOCOL_RULE_SCOPE_EMPTY, label: t('Empty scope') },
          ]}
        />

        <div className='space-y-2'>
          <div className='text-muted-foreground text-xs font-medium'>
            {t('Search')}
          </div>
          <div className='relative'>
            <Search className='text-muted-foreground pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2' />
            <Input
              value={filters.query}
              onChange={(event) =>
                onFiltersChange({ query: event.target.value })
              }
              className='pl-9'
              placeholder={t('Rule name, channel, or model')}
            />
          </div>
        </div>

        <div className='flex items-end gap-2'>
          <Badge variant='secondary' className='h-9 px-3'>
            {t('{{visible}} visible', { visible: visibleCount })}
          </Badge>
          <Button
            type='button'
            variant='outline'
            className='h-9'
            disabled={!hasActiveFilters}
            onClick={onResetFilters}
          >
            {t('Reset')}
          </Button>
        </div>
      </div>

      <div className='text-muted-foreground flex flex-wrap gap-x-4 gap-y-1 text-xs'>
        <span>
          {t('{{count}} all-channel rules', { count: stats.allChannels })}
        </span>
        <span>
          {t('{{count}} limited-scope rules', { count: stats.limitedScope })}
        </span>
        <span>
          {t('{{count}} empty-scope rules', { count: stats.emptyScope })}
        </span>
      </div>
    </div>
  )
}

type ProtocolMetricProps = {
  label: string
  value: string | number
  detail: string
  intent?: 'default' | 'warning'
}

function ProtocolMetric({
  label,
  value,
  detail,
  intent = 'default',
}: ProtocolMetricProps) {
  const intentClassName =
    intent === 'warning'
      ? 'border-amber-200 bg-amber-50 text-amber-950 dark:border-amber-900/60 dark:bg-amber-950/20 dark:text-amber-100'
      : 'border-border bg-background'
  return (
    <div className={`rounded-lg border p-3 ${intentClassName}`}>
      <div className='text-muted-foreground text-xs font-medium'>{label}</div>
      <div className='mt-1 text-2xl font-semibold tracking-tight'>{value}</div>
      <div className='text-muted-foreground mt-1 text-xs'>{detail}</div>
    </div>
  )
}

type ProtocolFilterSelectOption = {
  value: string
  label: string
}

type ProtocolFilterSelectProps = {
  label: string
  value: string
  options: ProtocolFilterSelectOption[]
  onValueChange: (value: string) => void
}

function ProtocolFilterSelect({
  label,
  value,
  options,
  onValueChange,
}: ProtocolFilterSelectProps) {
  return (
    <div className='space-y-2'>
      <div className='text-muted-foreground text-xs font-medium'>{label}</div>
      <Select value={value} onValueChange={onValueChange}>
        <SelectTrigger className='h-9 w-full'>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {options.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}
