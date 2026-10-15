import assert from 'node:assert/strict'
import test from 'node:test'
import {
  getLegacyConsoleSettingValues,
  hasLegacyConsoleSettings,
} from './legacy-console-migration'

test('detects every backend migration key and keeps its values', () => {
  const options = [
    { key: 'ApiInfo', value: '[{"url":"https://api.example.com"}]' },
    { key: 'Announcements', value: '[]' },
    { key: 'FAQ', value: '[{"question":"q","answer":"a"}]' },
    { key: 'UptimeKumaUrl', value: 'https://status.example.com' },
    { key: 'UptimeKumaSlug', value: 'new-api' },
  ]

  assert.deepEqual(getLegacyConsoleSettingValues(options), {
    ApiInfo: '[{"url":"https://api.example.com"}]',
    Announcements: '[]',
    FAQ: '[{"question":"q","answer":"a"}]',
    UptimeKumaUrl: 'https://status.example.com',
    UptimeKumaSlug: 'new-api',
  })
  assert.equal(hasLegacyConsoleSettings(options), true)
})

test('ignores absent and empty values but detects a partial Uptime Kuma pair', () => {
  assert.deepEqual(
    getLegacyConsoleSettingValues([
      { key: 'ApiInfo', value: '' },
      { key: 'UptimeKumaUrl', value: 'https://status.example.com' },
    ]),
    { UptimeKumaUrl: 'https://status.example.com' }
  )
  assert.equal(hasLegacyConsoleSettings([{ key: 'ApiInfo', value: '' }]), false)
})
