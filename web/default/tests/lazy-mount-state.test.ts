import { describe, expect, test } from 'bun:test'
import {
  DEFAULT_UNMOUNT_DELAY,
  getLazyMountUpdateDelay,
  shouldRenderLazyMount,
} from '../src/components/lazy-mount-state'

describe('lazy mount state helpers', () => {
  test('renders immediately while opening or already mounted', () => {
    expect(shouldRenderLazyMount(true, false)).toBe(true)
    expect(shouldRenderLazyMount(false, true)).toBe(true)
    expect(shouldRenderLazyMount(false, false)).toBe(false)
  })

  test('uses immediate open update and delayed close update', () => {
    expect(getLazyMountUpdateDelay(true, DEFAULT_UNMOUNT_DELAY)).toBe(0)
    expect(getLazyMountUpdateDelay(false, DEFAULT_UNMOUNT_DELAY)).toBe(
      DEFAULT_UNMOUNT_DELAY
    )
    expect(getLazyMountUpdateDelay(false, 150)).toBe(150)
  })
})
