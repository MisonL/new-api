import { describe, expect, test } from 'bun:test'
import {
  optionWriteOrder,
  sortOptionWrites,
} from '../src/features/system-settings/models/utils'

// billing_mode and billing_expr are two separate option rows. Publishing
// tiered_expr before the expression exists makes the model reject every request
// until the second write lands, and the server refuses that ordering, so the
// client must always write the expression first.
describe('option write order', () => {
  test('billing_expr is ordered before billing_mode', () => {
    expect(optionWriteOrder('billing_setting.billing_expr')).toBeLessThan(
      optionWriteOrder('billing_setting.billing_mode')
    )
  })

  test('sorts mode after expr regardless of the input order', () => {
    const updates = [
      { key: 'billing_setting.billing_mode', value: 'mode' },
      { key: 'billing_setting.billing_expr', value: 'expr' },
    ]
    expect(sortOptionWrites(updates).map((u) => u.key)).toEqual([
      'billing_setting.billing_expr',
      'billing_setting.billing_mode',
    ])
  })

  test('leaves unrelated keys in their original relative order', () => {
    const updates = [
      { key: 'ModelRatio', value: '1' },
      { key: 'billing_setting.billing_mode', value: 'mode' },
      { key: 'CompletionRatio', value: '2' },
      { key: 'billing_setting.billing_expr', value: 'expr' },
    ]
    const sorted = sortOptionWrites(updates).map((u) => u.key)
    expect(sorted[0]).toBe('billing_setting.billing_expr')
    expect(sorted[sorted.length - 1]).toBe('billing_setting.billing_mode')
    // ModelRatio precedes CompletionRatio both before and after the sort.
    expect(sorted.indexOf('ModelRatio')).toBeLessThan(
      sorted.indexOf('CompletionRatio')
    )
  })

  test('does not mutate the input array', () => {
    const updates = [
      { key: 'billing_setting.billing_mode', value: 'mode' },
      { key: 'billing_setting.billing_expr', value: 'expr' },
    ]
    sortOptionWrites(updates)
    expect(updates[0].key).toBe('billing_setting.billing_mode')
  })

  test('an unknown key is not ordered before the expression', () => {
    const updates = [
      { key: 'billing_setting.billing_mode', value: 'mode' },
      { key: 'SomethingElse', value: 'x' },
      { key: 'billing_setting.billing_expr', value: 'expr' },
    ]
    expect(sortOptionWrites(updates)[0].key).toBe(
      'billing_setting.billing_expr'
    )
  })
})
