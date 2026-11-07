import { useMemo, useState } from 'react'
import { ArrowDown, ArrowUp, ArrowDownUp, GripVertical } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { Column, Table } from '@/lib/table-compat'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  moveColumnKey,
  normalizeColumnOrder,
  reorderColumnKey,
} from '../lib/column-preferences'

interface ColumnOrderDialogProps<TData> {
  table: Table<TData>
  columnOrder: readonly string[]
  defaultColumnOrder: readonly string[]
  onColumnOrderChange: (order: string[]) => void
  fixedColumnIds?: readonly string[]
}

function getColumnLabel<TData>(column: Column<TData, unknown>) {
  return column.columnDef.meta?.label ?? column.id
}

export function ColumnOrderDialog<TData>(props: ColumnOrderDialogProps<TData>) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [draggingKey, setDraggingKey] = useState<string | null>(null)
  const [dropTarget, setDropTarget] = useState<{
    key: string
    position: 'before' | 'after'
  } | null>(null)
  const fixedColumnIds = useMemo(
    () => new Set(props.fixedColumnIds ?? []),
    [props.fixedColumnIds]
  )
  const columnsById = useMemo(
    () =>
      new Map(
        props.table.getAllLeafColumns().map((column) => [column.id, column])
      ),
    [props.table]
  )
  const defaultOrder = useMemo(
    () =>
      props.defaultColumnOrder.filter(
        (id, index, order) => order.indexOf(id) === index
      ),
    [props.defaultColumnOrder]
  )
  const orderedColumns = useMemo(() => {
    const order = normalizeColumnOrder(props.columnOrder, defaultOrder)
    return order
      .map((id) => columnsById.get(id))
      .filter((column): column is Column<TData, unknown> => column != null)
  }, [columnsById, defaultOrder, props.columnOrder])
  const movableKeys = useMemo(
    () =>
      orderedColumns
        .filter((column) => !fixedColumnIds.has(column.id))
        .map((column) => column.id),
    [fixedColumnIds, orderedColumns]
  )

  const move = (columnId: string, direction: 'up' | 'down') => {
    props.onColumnOrderChange(
      moveColumnKey(props.columnOrder, columnId, direction, movableKeys)
    )
  }

  const handleDrop = (targetKey: string, position: 'before' | 'after') => {
    if (!draggingKey) return
    props.onColumnOrderChange(
      reorderColumnKey(
        props.columnOrder,
        draggingKey,
        targetKey,
        position,
        movableKeys
      )
    )
    setDraggingKey(null)
    setDropTarget(null)
  }

  const reset = () => {
    props.onColumnOrderChange([...defaultOrder])
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button
          type='button'
          variant='outline'
          size='icon'
          className='size-8'
          aria-label={t('Move')}
          title={t('Move')}
        >
          <ArrowDownUp className='size-4' aria-hidden='true' />
          <span className='sr-only'>{t('Move')}</span>
        </Button>
      </DialogTrigger>
      <DialogContent className='max-h-[min(90vh,40rem)] sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{t('Move')}</DialogTitle>
          <DialogDescription>{t('Toggle columns')}</DialogDescription>
        </DialogHeader>
        <ScrollArea className='max-h-[min(60vh,30rem)] rounded-md border'>
          <div className='p-2'>
            {orderedColumns.map((column) => {
              const isFixed = fixedColumnIds.has(column.id)
              const isMovable = !isFixed && movableKeys.length > 1
              const movableIndex = movableKeys.indexOf(column.id)
              const canMoveUp = movableIndex > 0
              const canMoveDown =
                movableIndex >= 0 && movableIndex < movableKeys.length - 1
              const isDropTarget = dropTarget?.key === column.id

              return (
                <div
                  key={column.id}
                  draggable={isMovable}
                  data-column-key={column.id}
                  className={cn(
                    'flex items-center gap-2 border-b px-2 py-2 last:border-b-0',
                    draggingKey === column.id && 'opacity-50',
                    isDropTarget &&
                      dropTarget.position === 'before' &&
                      'border-t-primary border-t-2',
                    isDropTarget &&
                      dropTarget.position === 'after' &&
                      'border-b-primary border-b-2'
                  )}
                  onDragStart={(event) => {
                    if (!isMovable) return
                    event.dataTransfer.effectAllowed = 'move'
                    event.dataTransfer.setData('text/plain', column.id)
                    setDraggingKey(column.id)
                  }}
                  onDragOver={(event) => {
                    if (!draggingKey || !isMovable || draggingKey === column.id)
                      return
                    event.preventDefault()
                    const rect = event.currentTarget.getBoundingClientRect()
                    setDropTarget({
                      key: column.id,
                      position:
                        event.clientY - rect.top > rect.height / 2
                          ? 'after'
                          : 'before',
                    })
                  }}
                  onDrop={(event) => {
                    event.preventDefault()
                    handleDrop(column.id, dropTarget?.position ?? 'before')
                  }}
                  onDragEnd={() => {
                    setDraggingKey(null)
                    setDropTarget(null)
                  }}
                >
                  <GripVertical
                    className={cn(
                      'size-4 shrink-0',
                      isMovable
                        ? 'text-muted-foreground cursor-grab'
                        : 'text-muted-foreground/30'
                    )}
                    aria-hidden='true'
                  />
                  <Checkbox
                    checked={column.getIsVisible()}
                    disabled={!column.getCanHide()}
                    onCheckedChange={(checked) =>
                      column.toggleVisibility(checked === true)
                    }
                    aria-label={String(getColumnLabel(column))}
                  />
                  <span className='min-w-0 flex-1 truncate text-sm'>
                    {getColumnLabel(column)}
                  </span>
                  <div className='flex shrink-0 items-center gap-1'>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon-sm'
                      disabled={!isMovable || !canMoveUp}
                      aria-label={t('Move Up')}
                      title={t('Move Up')}
                      onClick={() => move(column.id, 'up')}
                    >
                      <ArrowUp className='size-4' aria-hidden='true' />
                    </Button>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon-sm'
                      disabled={!isMovable || !canMoveDown}
                      aria-label={t('Move Down')}
                      title={t('Move Down')}
                      onClick={() => move(column.id, 'down')}
                    >
                      <ArrowDown className='size-4' aria-hidden='true' />
                    </Button>
                  </div>
                </div>
              )
            })}
          </div>
        </ScrollArea>
        <DialogFooter>
          <Button type='button' variant='outline' onClick={reset}>
            {t('Reset')}
          </Button>
          <Button type='button' onClick={() => setOpen(false)}>
            {t('Done')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
