import * as React from 'react'
import { Check, ChevronsUpDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { shouldResetComboboxOnDisabledChange } from './combobox-state'

export type ComboboxOption = {
  value: string
  label: string
  icon?: React.ReactNode
}

interface ComboboxProps {
  options: ComboboxOption[]
  value?: string
  onValueChange: (value: string) => void
  placeholder?: string
  searchPlaceholder?: string
  emptyText?: string
  className?: string
  allowCustomValue?: boolean
  disabled?: boolean
}

export function Combobox({
  options,
  value,
  onValueChange,
  placeholder = 'Select option...',
  searchPlaceholder = 'Search...',
  emptyText = 'No option found.',
  className,
  allowCustomValue = false,
  disabled = false,
}: ComboboxProps) {
  const { t } = useTranslation()
  const [open, setOpen] = React.useState(false)
  const [searchValue, setSearchValue] = React.useState('')
  const previousDisabledRef = React.useRef(disabled)

  const selectedOption = options.find((option) => option.value === value)
  const displayValue = selectedOption?.label || value || placeholder

  const filteredOptions = React.useMemo(() => {
    if (!searchValue) return options
    const search = searchValue.toLowerCase()
    return options.filter(
      (option) =>
        option.label.toLowerCase().includes(search) ||
        option.value.toLowerCase().includes(search)
    )
  }, [options, searchValue])

  React.useEffect(() => {
    if (
      shouldResetComboboxOnDisabledChange(
        previousDisabledRef.current,
        disabled
      )
    ) {
      setOpen(false)
      setSearchValue('')
    }
    previousDisabledRef.current = disabled
  }, [disabled])

  const handleSelect = (selectedValue: string) => {
    if (disabled) return
    onValueChange(selectedValue === value ? '' : selectedValue)
    setOpen(false)
    setSearchValue('')
  }

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (disabled) return
    if (allowCustomValue && e.key === 'Enter' && searchValue) {
      e.preventDefault()
      // Check if search value matches any existing option
      const exactMatch = options.find(
        (opt) => opt.value.toLowerCase() === searchValue.toLowerCase()
      )
      if (exactMatch) {
        handleSelect(exactMatch.value)
      } else {
        // Use custom value
        onValueChange(searchValue)
        setOpen(false)
        setSearchValue('')
      }
    }
  }

  const popoverOpen = disabled ? false : open

  return (
    <Popover
      open={popoverOpen}
      onOpenChange={(nextOpen) => {
        if (!disabled) setOpen(nextOpen)
      }}
    >
      <PopoverTrigger asChild>
        <Button
          variant='outline'
          role='combobox'
          aria-expanded={popoverOpen}
          disabled={disabled}
          className={cn('w-full justify-between', className)}
        >
          <span className='truncate'>
            {selectedOption?.icon && (
              <span className='mr-2 inline-block'>{selectedOption.icon}</span>
            )}
            {displayValue}
          </span>
          <ChevronsUpDown className='ml-2 h-4 w-4 shrink-0 opacity-50' />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        className='w-[var(--radix-popover-trigger-width)] p-0'
        onWheel={(e) => e.stopPropagation()}
        onTouchMove={(e) => e.stopPropagation()}
        onPointerDown={(e) => e.stopPropagation()}
      >
        <Command shouldFilter={false}>
          <CommandInput
            placeholder={searchPlaceholder}
            value={searchValue}
            onValueChange={setSearchValue}
            onKeyDown={handleKeyDown}
          />
          <CommandList>
            <CommandEmpty>
              {emptyText}
              {allowCustomValue && searchValue && (
                <div className='mt-2 text-xs'>
                  {t('Press Enter to use "{{value}}"', {
                    value: searchValue,
                  })}
                </div>
              )}
            </CommandEmpty>
            <CommandGroup>
              {filteredOptions.map((option) => (
                <CommandItem
                  key={option.value}
                  value={option.value}
                  onSelect={handleSelect}
                >
                  <Check
                    className={cn(
                      'mr-2 h-4 w-4',
                      value === option.value ? 'opacity-100' : 'opacity-0'
                    )}
                  />
                  {option.icon && <span className='mr-2'>{option.icon}</span>}
                  {option.label}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
