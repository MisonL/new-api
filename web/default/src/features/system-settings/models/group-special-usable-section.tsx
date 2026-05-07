import { useState } from 'react'
import { ChevronDown, ChevronUp, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { StatusBadge } from '@/components/status-badge'
import {
  OP_ADD,
  OP_APPEND,
  OP_BADGE_MAP,
  OP_REMOVE,
  type Rule,
} from './group-special-usable-utils'

type GroupSectionProps = {
  groupName: string
  items: Rule[]
  onUpdate: (id: string, field: keyof Rule, val: string) => void
  onRemove: (id: string) => void
  onAdd: (groupName: string) => void
  onRemoveGroup: (groupName: string) => void
}

export function GroupSpecialUsableSection(props: GroupSectionProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <div className='rounded-lg border'>
        <div className='flex items-center justify-between p-3'>
          <div className='flex items-center gap-2'>
            <CollapsibleTrigger asChild>
              <Button variant='ghost' size='sm' className='h-6 w-6 p-0'>
                {open ? (
                  <ChevronUp className='h-4 w-4' />
                ) : (
                  <ChevronDown className='h-4 w-4' />
                )}
              </Button>
            </CollapsibleTrigger>
            <span className='font-semibold'>{props.groupName}</span>
            <StatusBadge variant='neutral' copyable={false}>
              {props.items.length} {t('rules')}
            </StatusBadge>
          </div>
          <div className='flex items-center gap-1'>
            <Button
              variant='ghost'
              size='sm'
              className='h-7 w-7 p-0'
              onClick={() => props.onAdd(props.groupName)}
            >
              <Plus className='h-4 w-4' />
            </Button>
            <Button
              variant='ghost'
              size='sm'
              className='text-destructive h-7 w-7 p-0'
              onClick={() => props.onRemoveGroup(props.groupName)}
            >
              <Trash2 className='h-4 w-4' />
            </Button>
          </div>
        </div>
        <CollapsibleContent>
          <div className='space-y-2 border-t p-3'>
            {props.items.map((rule) => (
              <div key={rule._id} className='flex items-center gap-2'>
                <Select
                  value={rule.op}
                  onValueChange={(v) => props.onUpdate(rule._id, 'op', v)}
                >
                  <SelectTrigger className='w-[130px]'>
                    <SelectValue>
                      <StatusBadge
                        label={t(OP_BADGE_MAP[rule.op].label)}
                        variant={OP_BADGE_MAP[rule.op].variant}
                        copyable={false}
                      />
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={OP_ADD}>
                      <StatusBadge
                        label={t(OP_BADGE_MAP[OP_ADD].label)}
                        variant={OP_BADGE_MAP[OP_ADD].variant}
                        copyable={false}
                      />
                    </SelectItem>
                    <SelectItem value={OP_REMOVE}>
                      <StatusBadge
                        label={t(OP_BADGE_MAP[OP_REMOVE].label)}
                        variant={OP_BADGE_MAP[OP_REMOVE].variant}
                        copyable={false}
                      />
                    </SelectItem>
                    <SelectItem value={OP_APPEND}>
                      <StatusBadge
                        label={t(OP_BADGE_MAP[OP_APPEND].label)}
                        variant={OP_BADGE_MAP[OP_APPEND].variant}
                        copyable={false}
                      />
                    </SelectItem>
                  </SelectContent>
                </Select>
                <Input
                  className='flex-1'
                  value={rule.targetGroup}
                  placeholder={t('Group name')}
                  onChange={(e) =>
                    props.onUpdate(rule._id, 'targetGroup', e.target.value)
                  }
                />
                {rule.op !== OP_REMOVE ? (
                  <Input
                    className='flex-1'
                    value={rule.description}
                    placeholder={t('Description')}
                    onChange={(e) =>
                      props.onUpdate(rule._id, 'description', e.target.value)
                    }
                  />
                ) : (
                  <div className='text-muted-foreground flex-1 px-3 text-sm'>
                    -
                  </div>
                )}
                <Button
                  variant='ghost'
                  size='sm'
                  className='text-destructive h-8 w-8 p-0'
                  onClick={() => props.onRemove(rule._id)}
                >
                  <Trash2 className='h-4 w-4' />
                </Button>
              </div>
            ))}
          </div>
        </CollapsibleContent>
      </div>
    </Collapsible>
  )
}
