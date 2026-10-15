import assert from 'node:assert/strict'
import test from 'node:test'
import {
  collectDirtyValues,
  expandDotPaths,
  flattenValues,
} from './use-settings-form'

// The settings API stores each option under its flat dotted key, while the form
// works on a nested object. These two helpers are the only bridge between them,
// and a section whose schema declares literal dotted keys silently drops every
// edit because zodResolver then returns only its own defaults.

test('expandDotPaths turns flat option keys into the nested form shape', () => {
  const flat = {
    'general_setting.log_filter_autocomplete_enabled': true,
    LogConsumeEnabled: false,
  }
  assert.deepEqual(expandDotPaths(flat), {
    general_setting: { log_filter_autocomplete_enabled: true },
    LogConsumeEnabled: false,
  })
})

test('flattenValues restores the flat option keys from the nested shape', () => {
  const nested = {
    general_setting: { log_filter_autocomplete_enabled: true },
    LogConsumeEnabled: false,
  }
  assert.deepEqual(flattenValues(nested), {
    'general_setting.log_filter_autocomplete_enabled': true,
    LogConsumeEnabled: false,
  })
})

test('the flat and nested shapes round-trip', () => {
  const flat = {
    'general_setting.responses_stream_bootstrap_recovery_enabled': false,
    'general_setting.responses_stream_bootstrap_grace_period_seconds': 180,
    'token_setting.max_user_tokens': 1000,
  }
  const nested = expandDotPaths(flat)
  assert.ok(nested, 'expandDotPaths must return the nested shape')
  assert.deepEqual(flattenValues(nested), flat)
})

test('collectDirtyValues reports changed nested fields under their dotted keys', () => {
  const dirty = { general_setting: { log_filter_autocomplete_enabled: true } }
  const values = {
    general_setting: { log_filter_autocomplete_enabled: true },
    LogConsumeEnabled: false,
  }
  assert.deepEqual(collectDirtyValues(dirty, values), {
    'general_setting.log_filter_autocomplete_enabled': true,
  })
})
