export const DEFAULT_UNMOUNT_DELAY = 320

export function getLazyMountUpdateDelay(open: boolean, delay: number) {
  return open ? 0 : delay
}

export function shouldRenderLazyMount(open: boolean, mounted: boolean) {
  return open || mounted
}
