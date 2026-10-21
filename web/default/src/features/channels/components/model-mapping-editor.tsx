import { useState, useEffect, useRef } from 'react'
import { Code, Table, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import {
  isOwnModelMappingEcho,
  nextModelMappingRowId,
  parseModelMappingRows,
  rowsToModelMappingJson,
  type ModelMappingRow,
} from '../lib/model-mapping-rows'

type ModelMappingEditorProps = {
  value: string
  onChange: (value: string) => void
  disabled?: boolean
}

export function ModelMappingEditor({
  value,
  onChange,
  disabled = false,
}: ModelMappingEditorProps) {
  const { t } = useTranslation()
  const [mode, setMode] = useState<'visual' | 'json'>('visual')
  const [rows, setRows] = useState<ModelMappingRow[]>([])
  const [jsonValue, setJsonValue] = useState(value)
  // 记录本组件最近一次交给父级的值。父级把它作为 value 传回来时必须跳过，
  // 否则每次按键都重新解析行、行 id 变化、输入框重新挂载，焦点丢失。
  const lastEmittedRef = useRef<string | null>(null)

  const emit = (next: string) => {
    lastEmittedRef.current = next
    setJsonValue(next)
    onChange(next)
  }

  // Parse JSON to rows when value changes externally
  useEffect(() => {
    if (isOwnModelMappingEcho(value, lastEmittedRef.current)) {
      return
    }
    lastEmittedRef.current = null
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setJsonValue(value)
    setRows((current) => parseModelMappingRows(value) ?? current)
  }, [value])

  const handleAddRow = () => {
    const newRow: ModelMappingRow = {
      id: nextModelMappingRowId(),
      from: '',
      to: '',
    }
    setRows([...rows, newRow])
  }

  const handleDeleteRow = (id: string) => {
    const updatedRows = rows.filter((row) => row.id !== id)
    setRows(updatedRows)
    emit(rowsToModelMappingJson(updatedRows))
  }

  const handleRowChange = (
    id: string,
    field: 'from' | 'to',
    newValue: string
  ) => {
    const updatedRows = rows.map((row) =>
      row.id === id ? { ...row, [field]: newValue } : row
    )
    setRows(updatedRows)
    emit(rowsToModelMappingJson(updatedRows))
  }

  const handleJsonChange = (newJson: string) => {
    emit(newJson)
    const parsedRows = parseModelMappingRows(newJson)
    if (parsedRows) {
      setRows(parsedRows)
    }
  }

  const handleFillTemplate = () => {
    const template = JSON.stringify(
      { 'gpt-3.5-turbo': 'gpt-3.5-turbo-0125' },
      null,
      2
    )
    emit(template)
    setRows(parseModelMappingRows(template) ?? [])
  }

  const toggleMode = () => {
    if (mode === 'visual') {
      // Switching to JSON mode: sync rows to JSON
      emit(rowsToModelMappingJson(rows))
      setMode('json')
    } else {
      // Switching to visual mode: sync JSON to rows
      setRows(parseModelMappingRows(jsonValue) ?? rows)
      setMode('visual')
    }
  }

  return (
    <div className='space-y-2'>
      <div className='flex items-center justify-between'>
        <div className='flex gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={toggleMode}
            disabled={disabled}
          >
            {mode === 'visual' ? (
              <>
                <Code className='mr-2 h-4 w-4' />
                {t('JSON Mode')}
              </>
            ) : (
              <>
                <Table className='mr-2 h-4 w-4' />
                {t('Visual Mode')}
              </>
            )}
          </Button>
          <Button
            type='button'
            variant='link'
            size='sm'
            className='h-auto p-0'
            onClick={handleFillTemplate}
            disabled={disabled}
          >
            {t('Fill Template')}
          </Button>
        </div>
      </div>

      {mode === 'visual' ? (
        <div className='space-y-2'>
          {rows.length > 0 ? (
            <div className='space-y-2'>
              <div className='grid grid-cols-[1fr_1fr_auto] gap-2 text-sm font-medium'>
                <div>{t('Original Model')}</div>
                <div>{t('Replacement Model')}</div>
                <div className='w-10'></div>
              </div>
              {rows.map((row) => (
                <div
                  key={row.id}
                  className='grid grid-cols-[1fr_1fr_auto] gap-2'
                >
                  <Input
                    value={row.from}
                    onChange={(e) =>
                      handleRowChange(row.id, 'from', e.target.value)
                    }
                    placeholder='gpt-3.5-turbo'
                    disabled={disabled}
                  />
                  <Input
                    value={row.to}
                    onChange={(e) =>
                      handleRowChange(row.id, 'to', e.target.value)
                    }
                    placeholder='gpt-3.5-turbo-0125'
                    disabled={disabled}
                  />
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    onClick={() => handleDeleteRow(row.id)}
                    disabled={disabled}
                    className='h-10 w-10'
                  >
                    <Trash2 className='h-4 w-4' />
                  </Button>
                </div>
              ))}
            </div>
          ) : (
            <div className='text-muted-foreground flex h-24 items-center justify-center rounded-md border border-dashed text-sm'>
              {t(
                'No model mappings configured. Click "Add Mapping" to get started.'
              )}
            </div>
          )}
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={handleAddRow}
            disabled={disabled}
            className='w-full'
          >
            <Plus className='mr-2 h-4 w-4' />
            {t('Add Mapping')}
          </Button>
        </div>
      ) : (
        <Textarea
          value={jsonValue}
          onChange={(e) => handleJsonChange(e.target.value)}
          placeholder={t('{"original-model": "replacement-model"}')}
          disabled={disabled}
          rows={8}
          className={cn('font-mono text-sm')}
        />
      )}
    </div>
  )
}
