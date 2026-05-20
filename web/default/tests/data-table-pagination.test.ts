import { describe, expect, test } from 'bun:test'
import { getPaginationTargetPage } from '../src/components/data-table/page-jump-utils'

describe('data table pagination', () => {
  test('parses and clamps page jump input', () => {
    expect(getPaginationTargetPage('2', 5)).toBe(2)
    expect(getPaginationTargetPage(' 3 ', 5)).toBe(3)
    expect(getPaginationTargetPage('9', 5)).toBe(5)
  })

  test('rejects invalid page jump input', () => {
    expect(getPaginationTargetPage('', 5)).toBeNull()
    expect(getPaginationTargetPage('0', 5)).toBeNull()
    expect(getPaginationTargetPage('-1', 5)).toBeNull()
    expect(getPaginationTargetPage('1abc', 5)).toBeNull()
    expect(getPaginationTargetPage('1', 0)).toBeNull()
  })
})
