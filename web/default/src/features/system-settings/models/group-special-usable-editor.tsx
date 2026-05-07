import { useCallback, useMemo, useState } from 'react'
import { Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  flattenRules,
  OP_APPEND,
  OP_REMOVE,
  safeParseJson,
  serializeRules,
  uid,
  type Rule,
} from './group-special-usable-utils'
import { GroupSpecialUsableSection } from './group-special-usable-section'

const sectionCardClassName =
  'relative shadow-sm ring-0 before:pointer-events-none before:absolute before:inset-0 before:rounded-xl before:border before:border-border/90'
const sectionHeaderClassName = 'border-b bg-muted/20'

type GroupSpecialUsableRulesEditorProps = {
  value: string
  onChange: (value: string) => void
}

export function GroupSpecialUsableRulesEditor(
  props: GroupSpecialUsableRulesEditorProps
) {
  const { t } = useTranslation()
  const [rules, setRules] = useState<Rule[]>(() =>
    flattenRules(safeParseJson(props.value))
  )
  const [newGroupName, setNewGroupName] = useState('')

  const { onChange } = props
  const emitChange = useCallback(
    (newRules: Rule[]) => {
      setRules(newRules)
      onChange(serializeRules(newRules))
    },
    [onChange]
  )

  const updateRule = useCallback(
    (id: string, field: keyof Rule, val: string) => {
      emitChange(
        rules.map((r) => {
          if (r._id !== id) return r
          const updated = { ...r, [field]: val }
          if (field === 'op' && val === OP_REMOVE)
            updated.description = 'remove'
          else if (field === 'op' && r.op === OP_REMOVE && val !== OP_REMOVE) {
            if (updated.description === 'remove') updated.description = ''
          }
          return updated
        })
      )
    },
    [rules, emitChange]
  )

  const removeRule = useCallback(
    (id: string) => emitChange(rules.filter((r) => r._id !== id)),
    [rules, emitChange]
  )

  const removeGroup = useCallback(
    (groupName: string) =>
      emitChange(rules.filter((r) => r.userGroup !== groupName)),
    [rules, emitChange]
  )

  const addRuleToGroup = useCallback(
    (groupName: string) => {
      emitChange([
        ...rules,
        {
          _id: uid(),
          userGroup: groupName,
          op: OP_APPEND,
          targetGroup: '',
          description: '',
        },
      ])
    },
    [rules, emitChange]
  )

  const addNewGroup = useCallback(() => {
    const name = newGroupName.trim()
    if (!name) return
    emitChange([
      ...rules,
      {
        _id: uid(),
        userGroup: name,
        op: OP_APPEND,
        targetGroup: '',
        description: '',
      },
    ])
    setNewGroupName('')
  }, [rules, emitChange, newGroupName])

  const grouped = useMemo(() => {
    const map: Record<string, Rule[]> = {}
    const order: string[] = []
    for (const r of rules) {
      if (!r.userGroup) continue
      if (!map[r.userGroup]) {
        map[r.userGroup] = []
        order.push(r.userGroup)
      }
      map[r.userGroup].push(r)
    }
    return order.map((name) => ({ name, items: map[name] }))
  }, [rules])

  return (
    <Card className={sectionCardClassName}>
      <CardHeader className={sectionHeaderClassName}>
        <CardTitle>{t('Special usable group rules')}</CardTitle>
        <CardDescription>
          {t(
            'Define per-group rules to add, remove, or append selectable groups for specific user groups.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className='space-y-3'>
          {grouped.length === 0 ? (
            <p className='text-muted-foreground py-4 text-center text-sm'>
              {t('No rules yet. Add a group below to get started.')}
            </p>
          ) : (
            grouped.map((group) => (
              <GroupSpecialUsableSection
                key={group.name}
                groupName={group.name}
                items={group.items}
                onUpdate={updateRule}
                onRemove={removeRule}
                onAdd={addRuleToGroup}
                onRemoveGroup={removeGroup}
              />
            ))
          )}

          <div className='flex items-center justify-center gap-2 pt-2'>
            <Input
              className='w-[200px]'
              value={newGroupName}
              placeholder={t('User group name')}
              onChange={(e) => setNewGroupName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  addNewGroup()
                }
              }}
            />
            <Button variant='outline' size='sm' onClick={addNewGroup}>
              <Plus className='mr-1 h-4 w-4' />
              {t('Add group rules')}
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
