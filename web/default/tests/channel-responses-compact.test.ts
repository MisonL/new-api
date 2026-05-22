import { describe, expect, test } from 'bun:test'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
} from '../src/features/channels/lib/channel-form'
import {
  RESPONSES_COMPACT_MODE_NATIVE,
  RESPONSES_COMPACT_MODE_UNSUPPORTED,
  getResponsesCompactConfigurationDiagnostic,
  getResponsesCompactMode,
  hasResponsesCompactModelConfigured,
} from '../src/features/channels/lib/channel-utils'
import type { Channel } from '../src/features/channels/types'

function makeChannel(overrides: Partial<Channel> = {}): Channel {
  return {
    id: 1,
    type: 1,
    key: '',
    openai_organization: null,
    test_model: null,
    status: 1,
    name: 'openai',
    weight: 0,
    created_time: 0,
    test_time: 0,
    response_time: 0,
    base_url: null,
    other: '',
    balance: 0,
    balance_updated_time: 0,
    models: 'gpt-5.5',
    group: 'default',
    used_quota: 0,
    model_mapping: null,
    status_code_mapping: null,
    priority: 0,
    auto_ban: 1,
    other_info: '',
    tag: null,
    setting: '{}',
    param_override: null,
    header_override: null,
    remark: '',
    max_input_tokens: 0,
    channel_info: {
      is_multi_key: false,
      multi_key_size: 0,
      multi_key_polling_index: 0,
      multi_key_mode: 'random',
    },
    settings: '{}',
    ...overrides,
  }
}

describe('channel responses compact settings', () => {
  test('defaults missing compact mode to unsupported', () => {
    const defaults = transformChannelToFormDefaults(makeChannel())

    expect(defaults.responses_compact_mode).toBe(
      RESPONSES_COMPACT_MODE_UNSUPPORTED
    )
    expect(getResponsesCompactMode('{}')).toBe(
      RESPONSES_COMPACT_MODE_UNSUPPORTED
    )
  })

  test('loads and stores native compact mode for OpenAI channels', () => {
    const defaults = transformChannelToFormDefaults(
      makeChannel({
        settings: JSON.stringify({
          responses_compact_mode: RESPONSES_COMPACT_MODE_NATIVE,
        }),
      })
    )

    expect(defaults.responses_compact_mode).toBe(
      RESPONSES_COMPACT_MODE_NATIVE
    )

    const payload = transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: 1,
      responses_compact_mode: RESPONSES_COMPACT_MODE_NATIVE,
    })
    const stored = JSON.parse(String(payload.channel.settings))

    expect(stored.responses_compact_mode).toBe(
      RESPONSES_COMPACT_MODE_NATIVE
    )
  })

  test('drops compact mode from non OpenAI channel settings', () => {
    const payload = transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: 14,
      responses_compact_mode: RESPONSES_COMPACT_MODE_NATIVE,
    })
    const stored = JSON.parse(String(payload.channel.settings))

    expect(stored.responses_compact_mode).toBeUndefined()
  })

  test('detects compact model configuration in models and mappings', () => {
    expect(
      hasResponsesCompactModelConfigured(
        'gpt-5.5-openai-compact,gpt-5.5',
        ''
      )
    ).toBe(true)
    expect(
      hasResponsesCompactModelConfigured(
        'gpt-5.5',
        '{"gpt-5.5-openai-compact":"gpt-5.5"}'
      )
    ).toBe(true)
    expect(hasResponsesCompactModelConfigured('gpt-5.5', '{}')).toBe(false)
  })

  test('reports risky compact model configuration when native compact is disabled', () => {
    expect(
      getResponsesCompactConfigurationDiagnostic(
        1,
        '{}',
        'gpt-5.5-openai-compact',
        '{}'
      )
    ).toBe('Compact model configured but native compact disabled')
    expect(
      getResponsesCompactConfigurationDiagnostic(
        1,
        JSON.stringify({ responses_compact_mode: 'native' }),
        'gpt-5.5-openai-compact',
        '{}'
      )
    ).toBeUndefined()
    expect(
      getResponsesCompactConfigurationDiagnostic(
        14,
        '{}',
        'gpt-5.5-openai-compact',
        '{}'
      )
    ).toBeUndefined()
  })
})
