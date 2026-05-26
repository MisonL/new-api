import { describe, expect, test } from 'bun:test'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
} from '../src/features/channels/lib/channel-form'
import {
  RESPONSES_COMPACT_BADGE_KEYS,
  RESPONSES_COMPACT_MODE_NATIVE,
  RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY,
  getResponsesCompactMode,
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
        JSON.stringify({ responses_compact_mode: 'synthetic_summary' })
      )
    ).toBe(RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY)

    expect(
      getResponsesCompactMode(
        JSON.stringify({ responses_compact_mode: 'convert' })
      )
    ).toBe(RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY)

    for (const mode of ['auto', 'disabled', 'unsupported', 'unexpected']) {
      expect(
        getResponsesCompactMode(
          JSON.stringify({ responses_compact_mode: mode })
        )
      ).toBe(RESPONSES_COMPACT_MODE_NATIVE)
    }
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

  test('loads synthetic and normalizes legacy compact modes into form defaults', () => {
    expect(
      transformChannelToFormDefaults(
        makeChannel({
          settings: JSON.stringify({
            responses_compact_mode: RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY,
          }),
        })
      ).responses_compact_mode
    ).toBe(RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY)

    expect(
      transformChannelToFormDefaults(
        makeChannel({
          settings: JSON.stringify({ responses_compact_mode: 'convert' }),
        })
      ).responses_compact_mode
    ).toBe(RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY)

    for (const mode of ['auto', 'disabled', 'unsupported', 'unexpected']) {
      expect(
        transformChannelToFormDefaults(
          makeChannel({
            settings: JSON.stringify({ responses_compact_mode: mode }),
          })
        ).responses_compact_mode
      ).toBe(RESPONSES_COMPACT_MODE_NATIVE)
    }
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

    expect(stored.responses_compact_mode).toBe(RESPONSES_COMPACT_MODE_NATIVE)
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

  test('normalizes legacy convert compact mode to synthetic before storing', () => {
    const payload = transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: 1,
      responses_compact_mode: 'convert' as never,
    })
    const stored = JSON.parse(String(payload.channel.settings))

    expect(stored.responses_compact_mode).toBe(
      RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY
    )
  })

  test('stores synthetic summary compact mode for OpenAI channels', () => {
    const payload = transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: 1,
      responses_compact_mode: RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY,
    })
    const stored = JSON.parse(String(payload.channel.settings))

    expect(stored.responses_compact_mode).toBe(
      RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY
    )
  })

  test('has translations for dynamic compact badge labels and tooltips', () => {
    for (const locale of Object.values(locales)) {
      for (const key of RESPONSES_COMPACT_BADGE_KEYS) {
        expect(locale.translation[key]).toBeDefined()
        expect(locale.translation[key]).not.toBe('')
      }
    }
  })
})
