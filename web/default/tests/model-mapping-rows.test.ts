import { describe, expect, test } from 'bun:test'
import {
  isOwnModelMappingEcho,
  nextModelMappingRowId,
  parseModelMappingRows,
  rowsToModelMappingJson,
} from '../src/features/channels/lib/model-mapping-rows'

describe('model mapping rows', () => {
  test('parses a mapping object into rows', () => {
    const rows = parseModelMappingRows(
      '{"gpt-3.5-turbo":"gpt-3.5-turbo-0125"}'
    )
    expect(rows).toHaveLength(1)
    expect(rows?.[0].from).toBe('gpt-3.5-turbo')
    expect(rows?.[0].to).toBe('gpt-3.5-turbo-0125')
  })

  test('empty text means no rows', () => {
    expect(parseModelMappingRows('   ')).toEqual([])
  })

  test('invalid JSON keeps the current rows', () => {
    expect(parseModelMappingRows('{not json')).toBeNull()
  })

  test('non-object JSON keeps the current rows', () => {
    expect(parseModelMappingRows('[1,2]')).toBeNull()
    expect(parseModelMappingRows('"text"')).toBeNull()
  })

  test('row ids are unique and never derived from a clock', () => {
    const first = nextModelMappingRowId()
    const second = nextModelMappingRowId()
    expect(first).not.toBe(second)
    expect(first.startsWith('model-mapping-')).toBe(true)
  })

  test('serializes rows back to JSON, skipping empty keys', () => {
    const json = rowsToModelMappingJson([
      { id: 'a', from: ' gpt-4 ', to: ' gpt-4o ' },
      { id: 'b', from: '', to: 'ignored' },
    ])
    expect(JSON.parse(json)).toEqual({ 'gpt-4': 'gpt-4o' })
  })

  test('rows with no keys serialize to empty string', () => {
    expect(rowsToModelMappingJson([{ id: 'a', from: '', to: 'x' }])).toBe('')
    expect(rowsToModelMappingJson([])).toBe('')
  })

  // 这是丢焦点的核心回归：组件把自己的值回传给父级后，父级再把这个值传回来，
  // 不能当成外部变更重新解析。
  test('recognizes its own emitted value as an echo', () => {
    const emitted = '{\n  "gpt-4": "gpt-4o"\n}'
    expect(isOwnModelMappingEcho(emitted, emitted)).toBe(true)
    expect(isOwnModelMappingEcho(emitted, null)).toBe(false)
    expect(isOwnModelMappingEcho('{"other":"x"}', emitted)).toBe(false)
  })
})
