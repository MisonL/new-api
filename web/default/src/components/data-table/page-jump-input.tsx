import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Input } from '@/components/ui/input'
import { getPaginationTargetPage } from './page-jump-utils'

type PageJumpInputProps = {
  currentPage: number
  totalPages: number
  onPageChange: (page: number) => void
  disabled?: boolean
  showLabel?: boolean
  className?: string
}

export function PageJumpInput({
  currentPage,
  totalPages,
  onPageChange,
  disabled = false,
  showLabel = true,
  className,
}: PageJumpInputProps) {
  const { t } = useTranslation()
  const [pageInput, setPageInput] = useState(`${currentPage}`)

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setPageInput(`${currentPage}`)
  }, [currentPage])

  const commitPageInput = () => {
    const targetPage = getPaginationTargetPage(pageInput, totalPages)
    if (targetPage == null) {
      setPageInput(`${currentPage}`)
      return
    }
    if (targetPage !== currentPage) {
      onPageChange(targetPage)
    }
    setPageInput(`${targetPage}`)
  }

  return (
    <div
      className={cn(
        'flex h-8 items-center gap-1 whitespace-nowrap',
        className
      )}
    >
      {showLabel ? (
        <span className='text-muted-foreground hidden text-sm leading-none sm:inline'>
          {t('Page')}
        </span>
      ) : null}
      <Input
        aria-label={t('Page')}
        autoComplete='off'
        className='h-8 w-14 px-2 py-0 text-center text-sm leading-none tabular-nums'
        disabled={disabled}
        inputMode='numeric'
        name='page'
        value={pageInput}
        onBlur={commitPageInput}
        onChange={(event) => setPageInput(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Enter') {
            event.preventDefault()
            commitPageInput()
          }
        }}
      />
    </div>
  )
}
