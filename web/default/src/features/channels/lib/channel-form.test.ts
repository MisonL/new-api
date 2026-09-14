import assert from 'node:assert/strict'
import test from 'node:test'
import type { Channel } from '../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  mergeChannelSubmitFormValues,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
  type ChannelFormValues,
} from './channel-form'
import {
  getResponsesCompactMode,
  normalizeResponsesUpstreamProfile,
  RESPONSES_COMPACT_MODE_AUTO,
  RESPONSES_UPSTREAM_PROFILE_GENERIC_OPENAI,
} from './channel-utils'
import {
  BUILTIN_HEADER_PROFILES,
  buildHeaderProfileStrategySettings,
} from './header-profile-utils'

const CLAUDE_CODE_UA = BUILTIN_HEADER_PROFILES.find(
  (profile) => profile.id === 'claude-code'
)?.headers['User-Agent']

const CLAUDE_CODE_PROFILE = BUILTIN_HEADER_PROFILES.find(
  (profile) => profile.id === 'claude-code'
)

assert.ok(CLAUDE_CODE_PROFILE)
assert.ok(CLAUDE_CODE_UA)

function makeChannelFormValues(
  values: Partial<ChannelFormValues>
): ChannelFormValues {
  return {
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'channel-a',
    key: '',
    models: 'gpt-5.5',
    group: ['default'],
    settings: '{}',
    ...values,
  }
}

test('mergeChannelSubmitFormValues preserves header profile state when editing name key and models', () => {
  const persistedSettings = buildHeaderProfileStrategySettings('{}', {
    enabled: true,
    mode: 'fixed',
    selectedProfileIds: ['claude-code'],
    profiles: [CLAUDE_CODE_PROFILE],
  })
  const persistedParamOverride =
    '{"operations":[{"mode":"pass_headers","value":["User-Agent"]}]}'
  const persistedHeaderOverride = '{"X-Test":"1"}'

  const persistedValues = makeChannelFormValues({
    name: 'channel-a',
    key: '',
    models: 'gpt-5.5',
    settings: persistedSettings,
    param_override: persistedParamOverride,
    header_override: persistedHeaderOverride,
  })
  const formValues = makeChannelFormValues({
    name: 'channel-renamed',
    key: 'new-key',
    models: 'gpt-5.5,gpt-5.4',
    settings: '{}',
    param_override: '',
    header_override: '{}',
  })

  const mergedValues = mergeChannelSubmitFormValues(
    formValues,
    persistedValues,
    {
      name: true,
      key: true,
      models: true,
    }
  )
  const payload = transformFormDataToUpdatePayload(mergedValues, 123)
  const payloadSettings = JSON.parse(String(payload.settings))

  assert.equal(payload.name, 'channel-renamed')
  assert.equal(payload.key, 'new-key')
  assert.equal(payload.models, 'gpt-5.5,gpt-5.4')
  assert.equal(
    payloadSettings.header_profile_strategy.profiles[0].headers['User-Agent'],
    CLAUDE_CODE_UA
  )
  assert.equal(payload.param_override, persistedParamOverride)
  assert.equal(payload.header_override, persistedHeaderOverride)
})

test('mergeChannelSubmitFormValues allows explicit header profile clearing', () => {
  const persistedSettings = buildHeaderProfileStrategySettings('{}', {
    enabled: true,
    mode: 'fixed',
    selectedProfileIds: ['claude-code'],
    profiles: [CLAUDE_CODE_PROFILE],
  })

  const mergedValues = mergeChannelSubmitFormValues(
    makeChannelFormValues({
      settings: '{}',
      param_override: '',
      header_override: '',
    }),
    makeChannelFormValues({
      settings: persistedSettings,
      param_override:
        '{"operations":[{"mode":"pass_headers","value":["User-Agent"]}]}',
      header_override: '{"X-Test":"1"}',
    }),
    {
      settings: true,
      param_override: true,
      header_override: true,
    }
  )
  const payload = transformFormDataToUpdatePayload(mergedValues, 123)
  const payloadSettings = JSON.parse(String(payload.settings))

  assert.equal(payloadSettings.header_profile_strategy, undefined)
  assert.equal(payload.param_override, '')
  assert.equal(payload.header_override, '')
})

test('generic OpenAI Responses profile survives form transform and keeps auto compact', () => {
  const values = makeChannelFormValues({
    type: 1,
    responses_upstream_profile: RESPONSES_UPSTREAM_PROFILE_GENERIC_OPENAI,
    responses_compact_mode: RESPONSES_COMPACT_MODE_AUTO,
  })

  const payload = transformFormDataToUpdatePayload(values, 123)
  const settings = JSON.parse(String(payload.settings))

  assert.equal(
    normalizeResponsesUpstreamProfile(settings.responses_upstream_profile),
    RESPONSES_UPSTREAM_PROFILE_GENERIC_OPENAI
  )
  assert.equal(
    getResponsesCompactMode(payload.settings),
    RESPONSES_COMPACT_MODE_AUTO
  )
})

test('max concurrency is preserved by channel form transforms', () => {
  const values = makeChannelFormValues({ max_concurrency: 7 })
  const createPayload = transformFormDataToCreatePayload(values)
  const updatePayload = transformFormDataToUpdatePayload(values, 123)

  assert.equal(createPayload.channel.max_concurrency, 7)
  assert.equal(updatePayload.max_concurrency, 7)

  const defaults = transformChannelToFormDefaults({
    ...({} as Channel),
    type: 1,
    name: 'channel-a',
    max_concurrency: 9,
    channel_info: {
      is_multi_key: false,
      multi_key_size: 0,
      multi_key_polling_index: 0,
      multi_key_mode: 'random',
    },
  })
  assert.equal(defaults.max_concurrency, 9)
})

test('Codex folded SSE setting is read and written only for Codex channels', () => {
  const codexChannel = {
    type: 57,
    name: 'codex-channel',
    settings: JSON.stringify({ codex_folded_sse_enabled: true }),
    channel_info: {
      is_multi_key: false,
      multi_key_size: 0,
      multi_key_polling_index: 0,
      multi_key_mode: 'random',
    },
  } as Channel
  const codexValues = transformChannelToFormDefaults(codexChannel)
  assert.equal(codexValues.codex_folded_sse_enabled, true)
  const codexSettings = JSON.parse(
    String(transformFormDataToUpdatePayload(codexValues, 123).settings)
  )
  assert.equal(codexSettings.codex_folded_sse_enabled, true)

  const nonCodexChannel = {
    ...codexChannel,
    type: 1,
  } as Channel
  const nonCodexValues = transformChannelToFormDefaults(nonCodexChannel)
  assert.equal(nonCodexValues.codex_folded_sse_enabled, false)
  const nonCodexSettings = JSON.parse(
    String(transformFormDataToUpdatePayload(nonCodexValues, 123).settings)
  )
  assert.equal(nonCodexSettings.codex_folded_sse_enabled, undefined)
})

test('channel settings transform preserves capability registry observations', () => {
  const registry = {
    source: 'observed_calls',
    observed: {
      observed_at: 1781660000,
      status_code: 413,
      error_code: 'upstream_request_too_large',
      reason: 'payload too large',
    },
  }
  const values = makeChannelFormValues({
    type: 1,
    settings: JSON.stringify({
      responses_capability_registry: registry,
      responses_upstream_profile: RESPONSES_UPSTREAM_PROFILE_GENERIC_OPENAI,
    }),
    responses_upstream_profile: RESPONSES_UPSTREAM_PROFILE_GENERIC_OPENAI,
    responses_compact_mode: RESPONSES_COMPACT_MODE_AUTO,
  })

  const payload = transformFormDataToUpdatePayload(values, 123)
  const settings = JSON.parse(String(payload.settings))

  assert.deepEqual(settings.responses_capability_registry, registry)
  assert.equal(
    settings.responses_upstream_profile,
    RESPONSES_UPSTREAM_PROFILE_GENERIC_OPENAI
  )
})

test('channel settings transform resets request body limit metadata when max bytes changes', () => {
  const now = Date.now
  Date.now = () => 1781661234000

  try {
    const values = makeChannelFormValues({
      settings: JSON.stringify({
        request_body_limit: {
          max_bytes: 1024,
          observed_at: 1781660000,
          source: 'upstream_413',
          reason: 'payload too large',
        },
      }),
      request_body_limit_max_bytes: 2048,
    })

    const payload = transformFormDataToUpdatePayload(values, 123)
    const settings = JSON.parse(String(payload.settings))

    assert.deepEqual(settings.request_body_limit, {
      max_bytes: 2048,
      observed_at: 1781661234,
      source: 'manual',
      reason: 'manual override',
    })
  } finally {
    Date.now = now
  }
})

test('channel settings transform preserves request body limit metadata when max bytes is unchanged', () => {
  const values = makeChannelFormValues({
    settings: JSON.stringify({
      request_body_limit: {
        max_bytes: 1024,
        observed_at: 1781660000,
        source: 'upstream_413',
        reason: 'payload too large',
      },
    }),
    request_body_limit_max_bytes: 1024,
  })

  const payload = transformFormDataToUpdatePayload(values, 123)
  const settings = JSON.parse(String(payload.settings))

  assert.deepEqual(settings.request_body_limit, {
    max_bytes: 1024,
    observed_at: 1781660000,
    source: 'upstream_413',
    reason: 'payload too large',
  })
})

test('channel settings transform removes request body limit when max bytes is zero', () => {
  const values = makeChannelFormValues({
    settings: JSON.stringify({
      request_body_limit: {
        max_bytes: 1024,
        observed_at: 1781660000,
        source: 'upstream_413',
        reason: 'payload too large',
      },
    }),
    request_body_limit_max_bytes: 0,
  })

  const payload = transformFormDataToUpdatePayload(values, 123)
  const settings = JSON.parse(String(payload.settings))

  assert.equal(settings.request_body_limit, undefined)
})

test('remote compaction first data timeout is bounded for OpenAI and removed elsewhere', () => {
  const openAIValues = makeChannelFormValues({
    type: 1,
    settings: JSON.stringify({ preserved_setting: 'keep' }),
    responses_remote_compaction_first_data_timeout_seconds: 301,
  })
  const openAIPayload = transformFormDataToUpdatePayload(openAIValues, 123)
  const openAISettings = JSON.parse(String(openAIPayload.settings))

  assert.equal(
    openAISettings.responses_remote_compaction_first_data_timeout_seconds,
    300
  )
  assert.equal(openAISettings.preserved_setting, 'keep')

  const disabledValues = makeChannelFormValues({
    type: 1,
    settings: JSON.stringify({
      responses_remote_compaction_first_data_timeout_seconds: 0,
    }),
    responses_remote_compaction_first_data_timeout_seconds: 0,
  })
  const disabledPayload = transformFormDataToUpdatePayload(disabledValues, 123)
  const disabledSettings = JSON.parse(String(disabledPayload.settings))
  assert.equal(
    disabledSettings.responses_remote_compaction_first_data_timeout_seconds,
    0
  )
  assert.equal(
    transformChannelToFormDefaults({
      type: 1,
      name: 'channel-a',
      settings: JSON.stringify({
        responses_remote_compaction_first_data_timeout_seconds: 0,
      }),
      channel_info: {
        is_multi_key: false,
        multi_key_size: 0,
        multi_key_polling_index: 0,
        multi_key_mode: 'random',
      },
    } as Channel).responses_remote_compaction_first_data_timeout_seconds,
    0
  )

  const nonOpenAIValues = makeChannelFormValues({
    type: 14,
    settings: JSON.stringify({
      responses_remote_compaction_first_data_timeout_seconds: 30,
    }),
    responses_remote_compaction_first_data_timeout_seconds: 30,
  })
  const nonOpenAIPayload = transformFormDataToUpdatePayload(
    nonOpenAIValues,
    123
  )
  const nonOpenAISettings = JSON.parse(String(nonOpenAIPayload.settings))

  assert.equal(
    nonOpenAISettings.responses_remote_compaction_first_data_timeout_seconds,
    undefined
  )
})
