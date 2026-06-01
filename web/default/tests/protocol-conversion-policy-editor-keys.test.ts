import { describe, expect, test } from 'bun:test'
import { reconcileProtocolRuleEditorKeys } from '../src/features/system-settings/models/protocol-conversion-policy-editor-keys'

describe('protocol conversion policy editor keys', () => {
  test('keeps keys stable when rule content changes without changing count', () => {
    const currentKeys = ['rule-a', 'rule-b']
    const nextKeys = reconcileProtocolRuleEditorKeys(currentKeys, 2, () => {
      throw new Error('next key should not be requested')
    })

    expect(nextKeys).toBe(currentKeys)
  })

  test('reconciles keys only when rule count changes', () => {
    let nextIndex = 1
    const nextKey = () => `rule-new-${nextIndex++}`

    expect(reconcileProtocolRuleEditorKeys(['rule-a'], 3, nextKey)).toEqual([
      'rule-a',
      'rule-new-1',
      'rule-new-2',
    ])
    expect(
      reconcileProtocolRuleEditorKeys(['rule-a', 'rule-b', 'rule-c'], 2, nextKey)
    ).toEqual(['rule-a', 'rule-b'])
    expect(reconcileProtocolRuleEditorKeys(['rule-a'], 0, nextKey)).toEqual([])
  })

  test('uses fallback keys for existing externally loaded rules', () => {
    const nextKeys = reconcileProtocolRuleEditorKeys(
      [],
      2,
      () => {
        throw new Error('next key should not be requested')
      },
      (index) => `external-rule-${index}`
    )

    expect(nextKeys).toEqual(['external-rule-0', 'external-rule-1'])
  })
})
