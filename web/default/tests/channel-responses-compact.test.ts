import { describe, expect, test } from 'bun:test'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
} from '../src/features/channels/lib/channel-form'
import {
  RESPONSES_COMPACT_BADGE_KEYS,
  RESPONSES_COMPACT_DIAGNOSTIC_COMPACT_MODEL_DISABLED,
  RESPONSES_COMPACT_DIAGNOSTIC_MESSAGE_KEYS,
  RESPONSES_COMPACT_MODE_CONVERT,
  RESPONSES_COMPACT_MODE_DISABLED,
  RESPONSES_COMPACT_MODE_NATIVE,
  getResponsesCompactConfigurationDiagnostic,
  getResponsesCompactMode,
  hasResponsesCompactModelConfigured,
} from '../src/features/channels/lib/channel-utils'
import type { Channel } from '../src/features/channels/types'
import en from '../src/i18n/locales/en.json'
import fr from '../src/i18n/locales/fr.json'
import ja from '../src/i18n/locales/ja.json'
import ru from '../src/i18n/locales/ru.json'
import vi from '../src/i18n/locales/vi.json'
import zh from '../src/i18n/locales/zh.json'

const locales = { en, zh, fr, ja, ru, vi }

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
  test('defaults missing compact mode to native', () => {
    const defaults = transformChannelToFormDefaults(makeChannel())

    expect(defaults.responses_compact_mode).toBe(
      RESPONSES_COMPACT_MODE_NATIVE
    )
    expect(CHANNEL_FORM_DEFAULT_VALUES.responses_compact_mode).toBe(
      RESPONSES_COMPACT_MODE_NATIVE
    )
    expect(getResponsesCompactMode('{}')).toBe(RESPONSES_COMPACT_MODE_NATIVE)
    expect(getResponsesCompactMode('')).toBe(RESPONSES_COMPACT_MODE_NATIVE)
    expect(getResponsesCompactMode('{bad json')).toBe(
      RESPONSES_COMPACT_MODE_NATIVE
    )
    expect(getResponsesCompactMode('null')).toBe(RESPONSES_COMPACT_MODE_NATIVE)
    expect(getResponsesCompactMode('[]')).toBe(RESPONSES_COMPACT_MODE_NATIVE)
  })

  test('normalizes legacy and unknown compact modes', () => {
    expect(
      getResponsesCompactMode(
        JSON.stringify({ responses_compact_mode: 'unsupported' })
      )
    ).toBe(RESPONSES_COMPACT_MODE_DISABLED)
    expect(
      getResponsesCompactMode(
        JSON.stringify({ responses_compact_mode: 'unexpected' })
      )
    ).toBe(RESPONSES_COMPACT_MODE_CONVERT)
  })

  test('defaults existing Azure and empty records to native', () => {
    expect(
      transformChannelToFormDefaults(
        makeChannel({
          type: 3,
          settings: '{}',
        })
      ).responses_compact_mode
    ).toBe(RESPONSES_COMPACT_MODE_NATIVE)

    expect(
      transformChannelToFormDefaults(
        makeChannel({
          settings: '',
        })
      ).responses_compact_mode
    ).toBe(RESPONSES_COMPACT_MODE_NATIVE)
  })

  test('loads legacy and unknown compact modes into form defaults', () => {
    expect(
      transformChannelToFormDefaults(
        makeChannel({
          settings: JSON.stringify({ responses_compact_mode: 'unsupported' }),
        })
      ).responses_compact_mode
    ).toBe(RESPONSES_COMPACT_MODE_DISABLED)

    expect(
      transformChannelToFormDefaults(
        makeChannel({
          settings: JSON.stringify({ responses_compact_mode: 'unexpected' }),
        })
      ).responses_compact_mode
    ).toBe(RESPONSES_COMPACT_MODE_CONVERT)
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

  test('stores disabled compact mode for OpenAI channels', () => {
    const payload = transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: 1,
      responses_compact_mode: RESPONSES_COMPACT_MODE_DISABLED,
    })
    const stored = JSON.parse(String(payload.channel.settings))

    expect(stored.responses_compact_mode).toBe(
      RESPONSES_COMPACT_MODE_DISABLED
    )
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

  test('reports compact model configuration as safe in default native mode', () => {
    expect(
      getResponsesCompactConfigurationDiagnostic(
        1,
        '{}',
        'gpt-5.5-openai-compact',
        '{}'
      )
    ).toBeUndefined()
  })

  test('reports compact model configuration when compact is disabled', () => {
    expect(
      getResponsesCompactConfigurationDiagnostic(
        1,
        JSON.stringify({
          responses_compact_mode: RESPONSES_COMPACT_MODE_DISABLED,
        }),
        'gpt-5.5-openai-compact',
        '{}'
      )
    ).toEqual({
      code: RESPONSES_COMPACT_DIAGNOSTIC_COMPACT_MODEL_DISABLED,
      messageKey:
        RESPONSES_COMPACT_DIAGNOSTIC_MESSAGE_KEYS[
          RESPONSES_COMPACT_DIAGNOSTIC_COMPACT_MODEL_DISABLED
        ],
    })
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

  test('has translations for dynamic compact badge labels and tooltips', () => {
    for (const locale of Object.values(locales)) {
      for (const key of RESPONSES_COMPACT_BADGE_KEYS) {
        expect(locale.translation[key]).toBeDefined()
        expect(locale.translation[key]).not.toBe('')
      }
    }
  })

  test('has translations for compact diagnostic messages', () => {
    const diagnosticKeys = Object.values(
      RESPONSES_COMPACT_DIAGNOSTIC_MESSAGE_KEYS
    )

    for (const locale of Object.values(locales)) {
      for (const key of diagnosticKeys) {
        expect(locale.translation[key]).toBeDefined()
        expect(locale.translation[key]).not.toBe('')
      }
    }
  })
})
