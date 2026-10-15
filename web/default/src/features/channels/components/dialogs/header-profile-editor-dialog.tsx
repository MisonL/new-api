import { useEffect, useMemo, useState } from 'react'
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
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import {
  createUserHeaderProfile,
  updateUserHeaderProfile,
  type HeaderProfilePayload,
} from '../../api'
import type { HeaderProfile } from '../../lib/header-profile-utils'

type HeaderProfileEditorDialogProps = {
  open: boolean
  profile?: HeaderProfile | null
  profiles?: HeaderProfile[]
  importDraft?: boolean
  initialHeaders?: Record<string, string>
  onOpenChange: (open: boolean) => void
  onSaved: (profile: HeaderProfile) => void
}

type Draft = {
  name: string
  category: HeaderProfile['category']
  headers: string
  description: string
}

const EMPTY_DRAFT: Draft = {
  name: '',
  category: 'custom',
  headers: '',
  description: '',
}

function draftFromProfile(profile?: HeaderProfile | null): Draft {
  if (!profile) return EMPTY_DRAFT
  return {
    name: profile.name,
    category: profile.category,
    headers: JSON.stringify(profile.headers, null, 2),
    description: profile.description || '',
  }
}

function parseHeaders(raw: string): Record<string, string> {
  const parsed: unknown = JSON.parse(raw)
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('Header JSON must be an object.')
  }

  const result: Record<string, string> = {}
  for (const [key, value] of Object.entries(parsed)) {
    const normalizedKey = key.trim()
    if (!normalizedKey) throw new Error('Header names cannot be empty.')
    if (typeof value !== 'string') {
      throw new Error('Header values must be strings.')
    }
    if (!value.trim()) throw new Error(`Header value cannot be empty: ${key}`)
    result[normalizedKey] = value.trim()
  }
  if (Object.keys(result).length === 0) {
    throw new Error('At least one header is required.')
  }
  return result
}

function validateDraft(
  draft: Draft,
  profiles: HeaderProfile[],
  currentId?: string
): HeaderProfilePayload {
  const name = draft.name.trim()
  if (!name) throw new Error('Template name is required.')
  if (
    profiles.some(
      (profile) => profile.id !== currentId && profile.name.trim() === name
    )
  ) {
    throw new Error('A template with this name already exists.')
  }
  return {
    name,
    category: draft.category,
    headers: parseHeaders(draft.headers),
    description: draft.description.trim(),
  }
}

export function HeaderProfileEditorDialog(
  props: HeaderProfileEditorDialogProps
) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<Draft>(EMPTY_DRAFT)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!props.open) return
    if (props.profile) {
      setDraft(draftFromProfile(props.profile))
      return
    }
    if (props.initialHeaders) {
      setDraft({
        ...EMPTY_DRAFT,
        name: 'Legacy Header Override',
        headers: JSON.stringify(props.initialHeaders, null, 2),
      })
      return
    }
    setDraft(EMPTY_DRAFT)
  }, [props.open, props.profile, props.initialHeaders])

  const title = props.importDraft
    ? t('Import legacy request headers')
    : props.profile
      ? t('Edit client header template')
      : t('Create client header template')
  const canUseExample = useMemo(() => !draft.headers.trim(), [draft.headers])

  async function handleSave() {
    setSaving(true)
    try {
      const payload = validateDraft(
        draft,
        props.profiles || [],
        props.profile?.id
      )
      const response = props.profile
        ? await updateUserHeaderProfile(props.profile.id, payload)
        : await createUserHeaderProfile(payload)
      if (!response.success || !response.data) {
        throw new Error(
          response.message || t('Failed to save header template.')
        )
      }
      toast.success(
        props.importDraft
          ? t('Legacy headers imported and applied to this channel.')
          : props.profile
            ? t('Header template updated.')
            : t('Header template created.')
      )
      if (Array.isArray(response.data)) {
        props.onSaved(response.data[0])
      } else {
        props.onSaved(response.data)
      }
      props.onOpenChange(false)
    } catch (error) {
      toast.error(
        error instanceof Error
          ? t(error.message)
          : t('Failed to save header template.')
      )
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            {props.importDraft
              ? t(
                  'Save the legacy header_override as a reusable client template.'
                )
              : t('Manage a reusable set of concrete request headers.')}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-4'>
          <div className='grid gap-4 sm:grid-cols-2'>
            <div className='space-y-2'>
              <Label htmlFor='header-profile-name'>{t('Name')}</Label>
              <Input
                id='header-profile-name'
                value={draft.name}
                maxLength={128}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...current,
                    name: event.target.value,
                  }))
                }
                placeholder={t('Template name')}
              />
            </div>
            <div className='space-y-2'>
              <Label htmlFor='header-profile-category'>{t('Category')}</Label>
              <Select
                value={draft.category}
                onValueChange={(category: HeaderProfile['category']) =>
                  setDraft((current) => ({ ...current, category }))
                }
              >
                <SelectTrigger id='header-profile-category'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='browser'>{t('Browser')}</SelectItem>
                  <SelectItem value='ai_coding_cli'>
                    {t('AI Coding CLI')}
                  </SelectItem>
                  <SelectItem value='api_sdk'>
                    {t('API SDK / Debug')}
                  </SelectItem>
                  <SelectItem value='custom'>{t('Custom')}</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className='space-y-2'>
            <div className='flex items-center justify-between gap-2'>
              <Label htmlFor='header-profile-headers'>
                {t('Request headers JSON')}
              </Label>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={!canUseExample}
                onClick={() =>
                  setDraft((current) => ({
                    ...current,
                    headers: JSON.stringify(
                      {
                        'User-Agent': 'MyApp/1.0',
                        'X-Client-Name': 'my-client',
                      },
                      null,
                      2
                    ),
                  }))
                }
              >
                {t('Fill example')}
              </Button>
            </div>
            <Textarea
              id='header-profile-headers'
              value={draft.headers}
              rows={10}
              spellCheck={false}
              onChange={(event) =>
                setDraft((current) => ({
                  ...current,
                  headers: event.target.value,
                }))
              }
              placeholder={t('Enter a JSON object whose values are strings.')}
            />
          </div>

          <div className='space-y-2'>
            <Label htmlFor='header-profile-description'>
              {t('Description')}
            </Label>
            <Textarea
              id='header-profile-description'
              value={draft.description}
              rows={3}
              onChange={(event) =>
                setDraft((current) => ({
                  ...current,
                  description: event.target.value,
                }))
              }
            />
          </div>
        </div>

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            disabled={saving}
            onClick={() => void handleSave()}
          >
            {saving ? t('Saving') : t('Save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
