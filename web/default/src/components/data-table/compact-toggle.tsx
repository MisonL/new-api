import { List } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

interface CompactTableToggleProps {
  compact: boolean
  onCompactChange: (compact: boolean) => void
}

export function CompactTableToggle(props: CompactTableToggleProps) {
  const { t } = useTranslation()
  const label = props.compact
    ? t('Use comfortable table spacing')
    : t('Compact')

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type='button'
          variant='outline'
          size='icon'
          className='size-8'
          aria-label={label}
          aria-pressed={props.compact}
          title={label}
          onClick={() => props.onCompactChange(!props.compact)}
        >
          <List className='size-4' />
          <span className='sr-only'>{label}</span>
        </Button>
      </TooltipTrigger>
      <TooltipContent>
        <p>{label}</p>
      </TooltipContent>
    </Tooltip>
  )
}
