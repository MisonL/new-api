export function reconcileProtocolRuleEditorKeys(
  currentKeys: string[],
  ruleCount: number,
  nextKey: () => string,
  fallbackKey?: (index: number) => string
): string[] {
  if (ruleCount <= 0) return []
  if (currentKeys.length === ruleCount) return currentKeys
  if (currentKeys.length > ruleCount) return currentKeys.slice(0, ruleCount)
  return [
    ...currentKeys,
    ...Array.from({ length: ruleCount - currentKeys.length }, (_, offset) => {
      const index = currentKeys.length + offset
      return fallbackKey?.(index) ?? nextKey()
    }),
  ]
}
