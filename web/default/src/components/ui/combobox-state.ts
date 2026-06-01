export function shouldResetComboboxOnDisabledChange(
  previousDisabled: boolean,
  disabled: boolean
) {
  return !previousDisabled && disabled
}
