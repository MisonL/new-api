import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'

const readSource = (path) => readFileSync(new URL(path, import.meta.url), 'utf8')

test('default theme exposes semantic status tokens and Tailwind mappings', () => {
  const source = readSource('../default/src/styles/theme.css')

  for (const token of ['success', 'warning', 'info', 'neutral']) {
    assert.match(source, new RegExp(`--${token}:\\s*oklch\\(`))
    assert.match(source, new RegExp(`--${token}-foreground:\\s*oklch\\(`))
    assert.match(source, new RegExp(`--color-${token}:\\s*var\\(--${token}\\)`))
    assert.match(
      source,
      new RegExp(
        `--color-${token}-foreground:\\s*var\\(--${token}-foreground\\)`
      )
    )
  }
})

test('default theme includes preset surface bridge without requiring runtime preset support', () => {
  const source = readSource('../default/src/styles/theme.css')

  assert.match(
    source,
    /\[data-theme-preset\]:not\(\[data-theme-preset='default'\]\)/
  )
  assert.match(
    source,
    /\.dark \[data-theme-preset\]:not\(\[data-theme-preset='default'\]\)/
  )
  assert.match(
    source,
    /\.dark\[data-theme-preset\]:not\(\[data-theme-preset='default'\]\)/
  )

  for (const token of [
    'card',
    'popover',
    'muted',
    'accent',
    'border',
    'input',
    'sidebar',
    'sidebar-accent',
    'sidebar-border',
  ]) {
    assert.match(
      source,
      new RegExp(`--${token}:\\s*color-mix\\(in oklch, var\\(--primary\\)`)
    )
  }

  assert.match(source, /--success:\s*var\(--chart-2\)/)
  assert.match(source, /--warning:\s*var\(--chart-4\)/)
  assert.match(source, /--info:\s*var\(--chart-1\)/)
  assert.match(source, /--neutral:\s*var\(--muted-foreground\)/)
})

test('dashboard charts read series colors from CSS chart tokens with fallbacks', () => {
  const source = readSource('../default/src/features/dashboard/lib/charts.ts')

  assert.match(source, /const THEME_CHART_COLOR_VARIABLES = \[/)
  for (const token of ['--chart-1', '--chart-2', '--chart-3', '--chart-4', '--chart-5']) {
    assert.match(source, new RegExp(`'${token}'`))
  }
  assert.match(source, /function getThemeChartColors\(themeKey\?: string\): string\[\]/)
  assert.match(source, /window\.getComputedStyle\(document\.body\)/)
  assert.match(source, /window\.getComputedStyle\(document\.documentElement\)/)
  assert.match(
    source,
    /function getVChartDefaultColors\(domainLength: number, themeKey\?: string\)/
  )
  assert.match(source, /const themeColors = getThemeChartColors\(themeKey\)/)
  assert.match(
    source,
    /export function processChartData\([\s\S]*themeKey\?: string[\s\S]*\): ProcessedChartData/
  )
  assert.match(
    source,
    /export function processUserChartData\([\s\S]*themeKey\?: string[\s\S]*\): ProcessedUserChartData/
  )
})
