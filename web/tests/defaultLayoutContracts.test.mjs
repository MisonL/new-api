import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'

const readSource = (path) => readFileSync(new URL(path, import.meta.url), 'utf8')

test('SectionPageLayout keeps description slot hidden', () => {
  const source = readSource(
    '../default/src/components/layout/components/section-page-layout.tsx'
  )

  assert.match(
    source,
    /function SectionPageLayoutDescription\(_props: SlotProps\) \{\n  return null\n\}/
  )
  assert.doesNotMatch(source, /let description: ReactNode = null/)
  assert.doesNotMatch(
    source,
    /SectionPageLayoutDescription\)[\s\S]*description = child\.props\.children/
  )
})

test('sidebar header exposes system brand while preserving route switches', () => {
  const switcherSource = readSource(
    '../default/src/components/layout/components/workspace-switcher.tsx'
  )
  const sidebarSource = readSource(
    '../default/src/components/layout/components/app-sidebar.tsx'
  )

  assert.match(switcherSource, /type SystemBrandProps = \{/)
  assert.match(switcherSource, /export function SystemBrand\(/)
  assert.doesNotMatch(switcherSource, /export function WorkspaceSwitcher\(/)
  assert.doesNotMatch(switcherSource, /t\('Workspaces'\)/)
  assert.match(
    switcherSource,
    /navigate\(\{ to: '\/system-settings\/general' \}\)/
  )
  assert.match(switcherSource, /navigate\(\{ to: '\/dashboard' \}\)/)
  assert.match(sidebarSource, /import \{ SystemBrand \} from/)
  assert.match(sidebarSource, /<SystemBrand workspaces=\{sidebarData\.workspaces\} \/>/)
})
