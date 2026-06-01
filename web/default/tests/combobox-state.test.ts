import { describe, expect, test } from 'bun:test'
import { shouldResetComboboxOnDisabledChange } from '../src/components/ui/combobox-state'

describe('combobox state helpers', () => {
  test('resets only when disabled changes from enabled to disabled', () => {
    expect(shouldResetComboboxOnDisabledChange(false, true)).toBe(true)
    expect(shouldResetComboboxOnDisabledChange(true, true)).toBe(false)
    expect(shouldResetComboboxOnDisabledChange(false, false)).toBe(false)
    expect(shouldResetComboboxOnDisabledChange(true, false)).toBe(false)
  })
})
