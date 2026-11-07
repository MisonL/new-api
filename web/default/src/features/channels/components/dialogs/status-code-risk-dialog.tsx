import { useState } from 'react'
import { AlertTriangle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
import { Markdown } from '@/components/ui/markdown'

interface StatusCodeRiskDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  detailItems: string[]
  onConfirm: () => void
}

// 风险确认强度与 classic（StatusCodeRiskGuardModal）保持一致：完整风险告知、
// 四条逐项确认、以及需要逐字输入且禁止粘贴的确认文本。只勾选不带输入，
// 或者允许直接粘贴，都等于把这道闸门降级成一次普通点击。
const CHECKLIST_KEYS = [
  'High-risk status code retry risk check 1',
  'High-risk status code retry risk check 2',
  'High-risk status code retry risk check 3',
  'High-risk status code retry risk check 4',
] as const

export function StatusCodeRiskDialog(props: StatusCodeRiskDialogProps) {
  const { t } = useTranslation()
  const [checkedItems, setCheckedItems] = useState<Set<number>>(new Set())
  const [confirmText, setConfirmText] = useState('')

  const requiredText = t('High-risk status code retry confirmation text')
  const allChecked = checkedItems.size === CHECKLIST_KEYS.length
  const textMatches = confirmText.trim() === requiredText.trim()
  const canConfirm = allChecked && textMatches

  const resetInputs = () => {
    setCheckedItems(new Set())
    setConfirmText('')
  }

  const handleConfirm = () => {
    if (!canConfirm) return
    resetInputs()
    props.onConfirm()
  }

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen) resetInputs()
    props.onOpenChange(nextOpen)
  }

  const toggleCheck = (idx: number) => {
    setCheckedItems((prev) => {
      const next = new Set(prev)
      if (next.has(idx)) next.delete(idx)
      else next.add(idx)
      return next
    })
  }

  return (
    <Dialog open={props.open} onOpenChange={handleOpenChange}>
      <DialogContent className='max-w-3xl'>
        <DialogHeader>
          <DialogTitle className='text-destructive flex items-center gap-2'>
            <AlertTriangle className='h-5 w-5' />
            {t('High-risk operation confirmation')}
          </DialogTitle>
          <DialogDescription>
            {t('High-risk status code retry risk disclaimer')}
          </DialogDescription>
        </DialogHeader>

        <div className='max-h-[70vh] space-y-4 overflow-y-auto pr-1'>
          <div className='border-destructive/30 bg-destructive/5 rounded-lg border p-3'>
            <Markdown>
              {t('High-risk status code retry risk notice markdown')}
            </Markdown>
          </div>

          {props.detailItems.length > 0 && (
            <div className='border-destructive/30 bg-destructive/5 rounded-lg border p-3'>
              <p className='mb-2 text-sm font-medium'>
                {t('Detected high-risk status code redirect rules')}
              </p>
              <ul className='list-inside list-disc text-sm'>
                {props.detailItems.map((item) => (
                  <li key={item} className='font-mono text-xs'>
                    {item}
                  </li>
                ))}
              </ul>
            </div>
          )}

          <div className='space-y-2 rounded-lg border p-3'>
            {CHECKLIST_KEYS.map((key, idx) => (
              <div key={key} className='flex items-start gap-2'>
                <Checkbox
                  id={`risk-check-${idx}`}
                  checked={checkedItems.has(idx)}
                  onCheckedChange={() => toggleCheck(idx)}
                />
                <Label
                  htmlFor={`risk-check-${idx}`}
                  className='text-sm leading-tight'
                >
                  {t(key)}
                </Label>
              </div>
            ))}
          </div>

          <div className='border-destructive/40 bg-destructive/5 space-y-1.5 rounded-lg border p-3'>
            <Label className='text-sm'>
              {t('Action confirmation')}:{' '}
              <code className='bg-muted rounded px-1 text-xs'>
                {requiredText}
              </code>
            </Label>
            <Input
              value={confirmText}
              onChange={(e) => setConfirmText(e.target.value)}
              placeholder={t('High-risk status code retry input placeholder')}
              onCopy={(e) => e.preventDefault()}
              onCut={(e) => e.preventDefault()}
              onPaste={(e) => e.preventDefault()}
              onDrop={(e) => e.preventDefault()}
            />
            {confirmText && !textMatches && (
              <p className='text-destructive text-xs'>
                {t('High-risk status code retry input mismatch')}
              </p>
            )}
          </div>
        </div>

        <DialogFooter>
          <Button variant='outline' onClick={() => handleOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button
            variant='destructive'
            disabled={!canConfirm}
            onClick={handleConfirm}
          >
            {t('I confirm enabling high-risk retry')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
