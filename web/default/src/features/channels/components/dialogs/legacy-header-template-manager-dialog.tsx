import { useCallback, useEffect, useMemo, useState } from 'react'
import { Loader2, RefreshCw, Save, Trash2, Upload } from 'lucide-react'
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
import { Input } from '@/components/ui/input'
import {
  createUserHeaderTemplate,
  deleteUserHeaderTemplate,
  getUserHeaderTemplates,
  updateUserHeaderTemplate,
  type UserHeaderTemplate,
} from '../../api'

type LegacyHeaderTemplateManagerDialogProps = {
  open: boolean
  value: string
  onOpenChange: (open: boolean) => void
  onApply: (content: string) => void
}

function normalizeTemplateContent(raw: string): string {
  const parsed: unknown = JSON.parse(raw)
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('Header override must be a JSON object')
  }
  const normalized: Record<string, string> = {}
  for (const [key, value] of Object.entries(parsed)) {
    const name = key.trim()
    if (!name || typeof value !== 'string' || !value.trim()) {
      throw new Error('Header names and values must be non-empty strings')
    }
    normalized[name] = value.trim()
  }
  if (Object.keys(normalized).length === 0) {
    throw new Error('Template content cannot be empty')
  }
  return JSON.stringify(normalized)
}

function formatTemplateTime(timestamp: number): string {
  return timestamp > 0 ? new Date(timestamp * 1000).toLocaleString() : '-'
}

function templatePreview(content: string): string {
  try {
    const parsed = JSON.parse(content) as Record<string, unknown>
    const entries = Object.entries(parsed)
    const preview = entries
      .slice(0, 2)
      .map(([key, value]) => `${key}: ${String(value)}`)
      .join(' | ')
    return entries.length > 2 ? `${preview} ...` : preview
  } catch {
    return content.replace(/\s+/g, ' ').slice(0, 96)
  }
}

export function LegacyHeaderTemplateManagerDialog(
  props: LegacyHeaderTemplateManagerDialogProps
) {
  const { t } = useTranslation()
  const [templates, setTemplates] = useState<UserHeaderTemplate[]>([])
  const [templateName, setTemplateName] = useState('')
  const [loading, setLoading] = useState(false)
  const [savingId, setSavingId] = useState<number | 'new' | null>(null)

  const sortedTemplates = useMemo(
    () =>
      [...templates].sort(
        (left, right) =>
          right.updated_at - left.updated_at || right.id - left.id
      ),
    [templates]
  )

  const loadTemplates = useCallback(async () => {
    setLoading(true)
    try {
      const response = await getUserHeaderTemplates()
      if (!response.success) {
        throw new Error(response.message || t('Failed to load templates'))
      }
      setTemplates(response.data || [])
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('Failed to load templates')
      )
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    if (props.open) void loadTemplates()
  }, [loadTemplates, props.open])

  function getCurrentContent(): string | null {
    try {
      return normalizeTemplateContent(props.value)
    } catch (error) {
      toast.error(
        error instanceof Error ? t(error.message) : t('Invalid header JSON')
      )
      return null
    }
  }

  async function createTemplate() {
    const name = templateName.trim()
    if (!name) {
      toast.error(t('Template name is required.'))
      return
    }
    const content = getCurrentContent()
    if (!content) return
    setSavingId('new')
    try {
      const response = await createUserHeaderTemplate({ name, content })
      if (!response.success || !response.data) {
        throw new Error(
          response.message || t('Failed to save header template.')
        )
      }
      setTemplates((current) => [
        response.data!,
        ...current.filter((item) => item.id !== response.data!.id),
      ])
      setTemplateName('')
      toast.success(t('Header template created.'))
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to save header template.')
      )
    } finally {
      setSavingId(null)
    }
  }

  async function overwriteTemplate(template: UserHeaderTemplate) {
    const content = getCurrentContent()
    if (!content) return
    setSavingId(template.id)
    try {
      const response = await updateUserHeaderTemplate(template.id, {
        name: template.name,
        content,
      })
      if (!response.success || !response.data) {
        throw new Error(
          response.message || t('Failed to save header template.')
        )
      }
      setTemplates((current) =>
        current.map((item) =>
          item.id === response.data!.id ? response.data! : item
        )
      )
      toast.success(t('Header template updated.'))
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to save header template.')
      )
    } finally {
      setSavingId(null)
    }
  }

  async function removeTemplate(template: UserHeaderTemplate) {
    if (!window.confirm(t('Delete this template?'))) return
    setSavingId(template.id)
    try {
      const response = await deleteUserHeaderTemplate(template.id)
      if (!response.success) {
        throw new Error(response.message || t('Failed to delete template'))
      }
      setTemplates((current) =>
        current.filter((item) => item.id !== template.id)
      )
      toast.success(t('Template deleted'))
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('Failed to delete template')
      )
    } finally {
      setSavingId(null)
    }
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='max-h-[85vh] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{t('Legacy header templates')}</DialogTitle>
          <DialogDescription>
            {t(
              'Compatibility library for reusable header_override JSON. New channel configurations should use client header templates.'
            )}
          </DialogDescription>
        </DialogHeader>

        <div className='flex flex-wrap items-end gap-2'>
          <div className='min-w-48 flex-1 space-y-2'>
            <label
              className='text-sm font-medium'
              htmlFor='legacy-template-name'
            >
              {t('Template name')}
            </label>
            <Input
              id='legacy-template-name'
              value={templateName}
              maxLength={128}
              onChange={(event) => setTemplateName(event.target.value)}
            />
          </div>
          <Button
            type='button'
            onClick={createTemplate}
            disabled={savingId !== null || !templateName.trim()}
          >
            {savingId === 'new' ? (
              <Loader2 className='mr-2 h-4 w-4 animate-spin' />
            ) : (
              <Save className='mr-2 h-4 w-4' />
            )}
            {t('Save current JSON')}
          </Button>
          <Button
            type='button'
            variant='outline'
            size='icon'
            aria-label={t('Refresh')}
            onClick={loadTemplates}
            disabled={loading}
          >
            <RefreshCw
              className={loading ? 'h-4 w-4 animate-spin' : 'h-4 w-4'}
            />
          </Button>
        </div>

        {loading ? (
          <div className='flex justify-center py-8'>
            <Loader2 className='h-5 w-5 animate-spin' />
          </div>
        ) : sortedTemplates.length === 0 ? (
          <div className='text-muted-foreground rounded-md border border-dashed p-8 text-center text-sm'>
            {t('No templates')}
          </div>
        ) : (
          <div className='space-y-2'>
            {sortedTemplates.map((template) => (
              <div
                key={template.id}
                className='flex flex-col gap-3 rounded-md border p-3 sm:flex-row sm:items-center'
              >
                <div className='min-w-0 flex-1'>
                  <div className='font-medium'>{template.name}</div>
                  <div
                    className='bg-muted mt-1 truncate rounded px-2 py-1 font-mono text-xs'
                    title={template.content}
                  >
                    {templatePreview(template.content)}
                  </div>
                  <div className='text-muted-foreground mt-1 text-xs'>
                    {t('Updated')} {formatTemplateTime(template.updated_at)}
                  </div>
                </div>
                <div className='flex flex-wrap gap-2'>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => props.onApply(template.content)}
                  >
                    <Upload className='mr-2 h-4 w-4' />
                    {t('Apply')}
                  </Button>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    disabled={savingId !== null}
                    onClick={() => overwriteTemplate(template)}
                  >
                    <Save className='mr-2 h-4 w-4' />
                    {t('Overwrite')}
                  </Button>
                  <Button
                    type='button'
                    variant='destructive'
                    size='icon-sm'
                    aria-label={t('Delete')}
                    disabled={savingId !== null}
                    onClick={() => removeTemplate(template)}
                  >
                    <Trash2 className='h-4 w-4' />
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
