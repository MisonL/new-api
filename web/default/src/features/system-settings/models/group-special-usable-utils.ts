export const OP_ADD = 'add' as const
export const OP_REMOVE = 'remove' as const
export const OP_APPEND = 'append' as const

export type OpType = typeof OP_ADD | typeof OP_REMOVE | typeof OP_APPEND

export type Rule = {
  _id: string
  userGroup: string
  op: OpType
  targetGroup: string
  description: string
}

export const OP_BADGE_MAP: Record<
  OpType,
  { variant: 'info' | 'danger' | 'neutral'; label: string }
> = {
  [OP_ADD]: { variant: 'info', label: 'Add (+:)' },
  [OP_REMOVE]: { variant: 'danger', label: 'Remove (-:)' },
  [OP_APPEND]: { variant: 'neutral', label: 'Append' },
}

let _idCounter = 0
export function uid() {
  return `gsu_${++_idCounter}`
}

function parsePrefix(rawKey: string): { op: OpType; groupName: string } {
  if (rawKey.startsWith('+:')) return { op: OP_ADD, groupName: rawKey.slice(2) }
  if (rawKey.startsWith('-:'))
    return { op: OP_REMOVE, groupName: rawKey.slice(2) }
  return { op: OP_APPEND, groupName: rawKey }
}

function toRawKey(op: OpType, groupName: string): string {
  if (op === OP_ADD) return `+:${groupName}`
  if (op === OP_REMOVE) return `-:${groupName}`
  return groupName
}

export function safeParseJson(
  str: string
): Record<string, Record<string, string>> {
  if (!str || !str.trim()) return {}
  try {
    return JSON.parse(str) as Record<string, Record<string, string>>
  } catch {
    return {}
  }
}

export function flattenRules(
  nested: Record<string, Record<string, string>>
): Rule[] {
  const rules: Rule[] = []
  for (const [userGroup, inner] of Object.entries(nested)) {
    if (typeof inner !== 'object' || inner === null) continue
    for (const [rawKey, desc] of Object.entries(inner)) {
      const { op, groupName } = parsePrefix(rawKey)
      rules.push({
        _id: uid(),
        userGroup,
        op,
        targetGroup: groupName,
        description:
          op === OP_REMOVE ? 'remove' : typeof desc === 'string' ? desc : '',
      })
    }
  }
  return rules
}

function nestRules(rules: Rule[]): Record<string, Record<string, string>> {
  const result: Record<string, Record<string, string>> = {}
  for (const { userGroup, op, targetGroup, description } of rules) {
    if (!userGroup || !targetGroup) continue
    if (!result[userGroup]) result[userGroup] = {}
    result[userGroup][toRawKey(op, targetGroup)] = description
  }
  return result
}

export function serializeRules(rules: Rule[]): string {
  const nested = nestRules(rules)
  return Object.keys(nested).length === 0
    ? '{}'
    : JSON.stringify(nested, null, 2)
}
