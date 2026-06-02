import { describe, expect, test } from 'bun:test'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
} from '../src/features/channels/lib/channel-form'
import {
  RESPONSES_COMPACT_BADGE_KEYS,
  RESPONSES_COMPACT_AUTO_FALLBACK_RETRY_INTERVAL_HOURS_DEFAULT,
  RESPONSES_COMPACT_CONTEXT_FALLBACK_DEFAULT,
  RESPONSES_COMPACT_MODE_AUTO,
  RESPONSES_COMPACT_MODE_NATIVE,
  RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY,
  RESPONSES_COMPACT_SUMMARY_FALLBACK_MODELS_DEFAULT,
  RESPONSES_COMPACT_SUMMARY_MODEL_FALLBACK_DEFAULT,
  getResponsesCompactAutoFallbackReason,
  getResponsesCompactMode,
  isResponsesCompactAutoFallbackActive,
  normalizeResponsesCompactAutoFallbackRetryIntervalHours,
  normalizeResponsesCompactFallbackModels,
} from '../src/features/channels/lib/channel-utils'
import type { Channel } from '../src/features/channels/types'
import en from '../src/i18n/locales/en.json'
import fr from '../src/i18n/locales/fr.json'
import ja from '../src/i18n/locales/ja.json'
import ru from '../src/i18n/locales/ru.json'
import vi from '../src/i18n/locales/vi.json'
import zh from '../src/i18n/locales/zh.json'

const locales = { en, zh, fr, ja, ru, vi }

// Retry interval rules: default 3 hours, minimum 1 hour, maximum 168 hours.

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
  test('defaults missing compact mode to auto', () => {
    const defaults = transformChannelToFormDefaults(makeChannel())

    expect(defaults.responses_compact_mode).toBe(
      RESPONSES_COMPACT_MODE_AUTO
    )
    expect(CHANNEL_FORM_DEFAULT_VALUES.responses_compact_mode).toBe(
      RESPONSES_COMPACT_MODE_AUTO
    )
    expect(CHANNEL_FORM_DEFAULT_VALUES.responses_compact_context_fallback).toBe(
      RESPONSES_COMPACT_CONTEXT_FALLBACK_DEFAULT
    )
    expect(
      CHANNEL_FORM_DEFAULT_VALUES.responses_compact_summary_model_fallback
    ).toBe(RESPONSES_COMPACT_SUMMARY_MODEL_FALLBACK_DEFAULT)
    expect(
      CHANNEL_FORM_DEFAULT_VALUES.responses_compact_summary_fallback_models
    ).toBe(RESPONSES_COMPACT_SUMMARY_FALLBACK_MODELS_DEFAULT.join(','))
    expect(
      CHANNEL_FORM_DEFAULT_VALUES.responses_compact_auto_fallback_retry_interval_hours
    ).toBe(RESPONSES_COMPACT_AUTO_FALLBACK_RETRY_INTERVAL_HOURS_DEFAULT)
    expect(getResponsesCompactMode('{}')).toBe(RESPONSES_COMPACT_MODE_AUTO)
    expect(getResponsesCompactMode('')).toBe(RESPONSES_COMPACT_MODE_AUTO)
    expect(getResponsesCompactMode('{bad json')).toBe(
      RESPONSES_COMPACT_MODE_AUTO
    )
    expect(getResponsesCompactMode('null')).toBe(RESPONSES_COMPACT_MODE_AUTO)
    expect(getResponsesCompactMode('[]')).toBe(RESPONSES_COMPACT_MODE_AUTO)
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

    expect(
      getResponsesCompactMode(JSON.stringify({ responses_compact_mode: 'auto' }))
    ).toBe(RESPONSES_COMPACT_MODE_AUTO)

    for (const mode of ['disabled', 'unsupported']) {
      expect(
        getResponsesCompactMode(
          JSON.stringify({ responses_compact_mode: mode })
        )
      ).toBe(RESPONSES_COMPACT_MODE_NATIVE)
    }
    expect(
      getResponsesCompactMode(
        JSON.stringify({ responses_compact_mode: 'unexpected' })
      )
    ).toBe(RESPONSES_COMPACT_MODE_AUTO)
    for (const mode of ['Auto', 'AUTO', 0]) {
      expect(
        getResponsesCompactMode(
          JSON.stringify({ responses_compact_mode: mode })
        )
      ).toBe(RESPONSES_COMPACT_MODE_AUTO)
    }
  })

  test('normalizes synthetic summary fallback models safely', () => {
    expect(
      normalizeResponsesCompactFallbackModels(
        ' gpt-5.4, gpt-5.4, gpt-5.4-large '
      )
    ).toEqual(['gpt-5.4', 'gpt-5.4-large'])
    expect(normalizeResponsesCompactFallbackModels([' ', ''])).toEqual([
      'gpt-5.4',
    ])

    const defaults = normalizeResponsesCompactFallbackModels(undefined)
    defaults.push('mutated')
    expect(normalizeResponsesCompactFallbackModels(undefined)).toEqual([
      'gpt-5.4',
    ])
    expect(normalizeResponsesCompactAutoFallbackRetryIntervalHours()).toBe(3)
    expect(normalizeResponsesCompactAutoFallbackRetryIntervalHours(2)).toBe(2)
    expect(normalizeResponsesCompactAutoFallbackRetryIntervalHours(3)).toBe(3)
    expect(normalizeResponsesCompactAutoFallbackRetryIntervalHours(0)).toBe(3)
    expect(normalizeResponsesCompactAutoFallbackRetryIntervalHours(-1)).toBe(1)
    expect(normalizeResponsesCompactAutoFallbackRetryIntervalHours(167)).toBe(
      167
    )
    expect(normalizeResponsesCompactAutoFallbackRetryIntervalHours(168)).toBe(
      168
    )
    expect(normalizeResponsesCompactAutoFallbackRetryIntervalHours(169)).toBe(
      168
    )
  })

  test('defaults existing Azure and empty records to auto', () => {
    expect(
      transformChannelToFormDefaults(
        makeChannel({
          type: 3,
          settings: '{}',
        })
      ).responses_compact_mode
    ).toBe(RESPONSES_COMPACT_MODE_AUTO)

    expect(
      transformChannelToFormDefaults(
        makeChannel({
          settings: '',
        })
      ).responses_compact_mode
    ).toBe(RESPONSES_COMPACT_MODE_AUTO)
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

    for (const mode of ['disabled', 'unsupported']) {
      expect(
        transformChannelToFormDefaults(
          makeChannel({
            settings: JSON.stringify({ responses_compact_mode: mode }),
          })
        ).responses_compact_mode
      ).toBe(RESPONSES_COMPACT_MODE_NATIVE)
    }
    expect(
      transformChannelToFormDefaults(
        makeChannel({
          settings: JSON.stringify({ responses_compact_mode: 'unexpected' }),
        })
      ).responses_compact_mode
    ).toBe(RESPONSES_COMPACT_MODE_AUTO)
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
    expect(
      defaults.responses_compact_auto_fallback_retry_interval_hours
    ).toBe(3)
    expect(defaults.responses_compact_context_fallback).toBe(true)
    expect(defaults.responses_compact_summary_model_fallback).toBe(true)
    expect(defaults.responses_compact_summary_fallback_models).toBe('gpt-5.4')

    const payload = transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: 1,
      responses_compact_mode: RESPONSES_COMPACT_MODE_NATIVE,
      responses_compact_auto_fallback_retry_interval_hours: 6,
      responses_compact_context_fallback: false,
      responses_compact_summary_model_fallback: true,
      responses_compact_summary_fallback_models: 'gpt-5.4,gpt-5.4-large',
    })
    const stored = JSON.parse(String(payload.channel.settings))

    expect(stored.responses_compact_mode).toBe(RESPONSES_COMPACT_MODE_NATIVE)
    expect(
      stored.responses_compact_auto_fallback_retry_interval_hours
    ).toBe(6)
    expect(stored.responses_compact_context_fallback).toBe(false)
    expect(stored.responses_compact_summary_model_fallback).toBe(true)
    expect(stored.responses_compact_summary_fallback_models).toEqual([
      'gpt-5.4',
      'gpt-5.4-large',
    ])
  })

  test('loads and stores auto compact mode for OpenAI channels', () => {
    const defaults = transformChannelToFormDefaults(
      makeChannel({
        settings: JSON.stringify({
          responses_compact_mode: RESPONSES_COMPACT_MODE_AUTO,
        }),
      })
    )

    expect(defaults.responses_compact_mode).toBe(RESPONSES_COMPACT_MODE_AUTO)
    expect(
      defaults.responses_compact_auto_fallback_retry_interval_hours
    ).toBe(3)

    const payload = transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: 1,
      responses_compact_mode: RESPONSES_COMPACT_MODE_AUTO,
      responses_compact_auto_fallback_retry_interval_hours: 6,
    })
    const stored = JSON.parse(String(payload.channel.settings))

    expect(stored.responses_compact_mode).toBe(RESPONSES_COMPACT_MODE_AUTO)
    expect(
      stored.responses_compact_auto_fallback_retry_interval_hours
    ).toBe(6)
    expect(stored.responses_compact_auto_fallback_date).toBeUndefined()
  })

  test('preserves auto fallback state unless compact mode changes', () => {
    const base = {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: 1,
      responses_compact_auto_fallback_retry_interval_hours: 6,
      settings: JSON.stringify({
        responses_compact_mode: RESPONSES_COMPACT_MODE_AUTO,
        responses_compact_auto_fallback_date: 20260526,
        responses_compact_auto_fallback_at: 1780000000,
        responses_compact_auto_fallback_reason: 'status_code=404',
        responses_compact_auto_fallback_retry_interval_hours: 6,
      }),
    }

    const unchanged = JSON.parse(
      String(
        transformFormDataToCreatePayload({
          ...base,
          responses_compact_mode: RESPONSES_COMPACT_MODE_AUTO,
        }).channel.settings
      )
    )
    expect(unchanged.responses_compact_auto_fallback_date).toBe(20260526)
    expect(unchanged.responses_compact_auto_fallback_at).toBe(1780000000)
    expect(
      unchanged.responses_compact_auto_fallback_retry_interval_hours
    ).toBe(6)

    const changed = JSON.parse(
      String(
        transformFormDataToCreatePayload({
          ...base,
          responses_compact_mode: RESPONSES_COMPACT_MODE_NATIVE,
        }).channel.settings
      )
    )
    expect(changed.responses_compact_auto_fallback_date).toBeUndefined()
    expect(changed.responses_compact_auto_fallback_at).toBeUndefined()
    expect(changed.responses_compact_auto_fallback_reason).toBeUndefined()
    expect(changed.responses_compact_auto_fallback_retry_interval_hours).toBe(6)

    const synthetic = JSON.parse(
      String(
        transformFormDataToCreatePayload({
          ...base,
          responses_compact_mode: RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY,
        }).channel.settings
      )
    )
    expect(synthetic.responses_compact_auto_fallback_date).toBeUndefined()
    expect(synthetic.responses_compact_auto_fallback_at).toBeUndefined()
    expect(synthetic.responses_compact_auto_fallback_reason).toBeUndefined()
    expect(synthetic.responses_compact_auto_fallback_retry_interval_hours).toBe(
      6
    )
  })

  test('drops compact metadata from non OpenAI channel settings', () => {
    const payload = transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: 14,
      responses_compact_mode: RESPONSES_COMPACT_MODE_NATIVE,
      settings: JSON.stringify({
        responses_compact_auto_fallback_date: 20260526,
        responses_compact_auto_fallback_at: 1780000000,
        responses_compact_auto_fallback_reason: 'status_code=404',
        responses_compact_auto_fallback_retry_interval_hours: 6,
        responses_compact_context_fallback: true,
        responses_compact_summary_model_fallback: true,
        responses_compact_summary_fallback_models: ['gpt-5.4'],
      }),
    })
    const stored = JSON.parse(String(payload.channel.settings))

    expect(stored.responses_compact_mode).toBeUndefined()
    expect(stored.responses_compact_auto_fallback_date).toBeUndefined()
    expect(stored.responses_compact_auto_fallback_at).toBeUndefined()
    expect(stored.responses_compact_auto_fallback_reason).toBeUndefined()
    expect(
      stored.responses_compact_auto_fallback_retry_interval_hours
    ).toBeUndefined()
    expect(stored.responses_compact_context_fallback).toBeUndefined()
    expect(stored.responses_compact_summary_model_fallback).toBeUndefined()
    expect(stored.responses_compact_summary_fallback_models).toBeUndefined()
  })

  test('detects auto fallback state by retry interval and legacy UTC date', () => {
    const fallbackAt = Date.parse('2026-05-26T23:30:00.000Z') / 1000
    const intervalSettings = JSON.stringify({
      responses_compact_mode: RESPONSES_COMPACT_MODE_AUTO,
      responses_compact_auto_fallback_at: fallbackAt,
      responses_compact_auto_fallback_retry_interval_hours: 3,
      responses_compact_auto_fallback_reason: 'status_code=404',
    })

    expect(
      isResponsesCompactAutoFallbackActive(
        intervalSettings,
        new Date('2026-05-27T02:29:59.000Z')
      )
    ).toBe(true)
    expect(
      isResponsesCompactAutoFallbackActive(
        intervalSettings,
        new Date('2026-05-27T02:30:00.000Z')
      )
    ).toBe(false)
    expect(
      isResponsesCompactAutoFallbackActive(
        JSON.stringify({
          responses_compact_mode: RESPONSES_COMPACT_MODE_AUTO,
          responses_compact_auto_fallback_at: fallbackAt,
          responses_compact_auto_fallback_retry_interval_hours: 6,
        }),
        new Date('2026-05-27T05:29:59.000Z')
      )
    ).toBe(true)
    expect(
      isResponsesCompactAutoFallbackActive(
        JSON.stringify({
          responses_compact_mode: RESPONSES_COMPACT_MODE_AUTO,
          responses_compact_auto_fallback_at: Date.parse(
            '2026-05-26T23:31:00.000Z'
          ) / 1000,
        }),
        new Date('2026-05-26T23:30:00.000Z')
      )
    ).toBe(false)
    expect(
      isResponsesCompactAutoFallbackActive(
        JSON.stringify({
          responses_compact_mode: RESPONSES_COMPACT_MODE_AUTO,
          responses_compact_auto_fallback_at: Date.parse(
            '2026-05-26T23:31:00.000Z'
          ) / 1000,
          responses_compact_auto_fallback_date: 20260526,
        }),
        new Date('2026-05-26T23:31:00.000Z')
      )
    ).toBe(true)
    expect(
      isResponsesCompactAutoFallbackActive(
        JSON.stringify({
          responses_compact_mode: RESPONSES_COMPACT_MODE_AUTO,
          responses_compact_auto_fallback_at: Date.parse(
            '2026-05-27T00:01:00.000Z'
          ) / 1000,
          responses_compact_auto_fallback_date: 20260526,
        }),
        new Date('2026-05-26T23:30:00.000Z')
      )
    ).toBe(true)

    const settings = JSON.stringify({
      responses_compact_mode: RESPONSES_COMPACT_MODE_AUTO,
      responses_compact_auto_fallback_date: 20260526,
      responses_compact_auto_fallback_reason: 'status_code=404',
    })

    expect(
      isResponsesCompactAutoFallbackActive(
        settings,
        new Date('2026-05-26T23:30:00.000Z')
      )
    ).toBe(true)
    expect(
      isResponsesCompactAutoFallbackActive(
        settings,
        new Date('2026-05-27T00:00:00.000Z')
      )
    ).toBe(false)
    expect(getResponsesCompactAutoFallbackReason(settings)).toBe(
      'status_code=404'
    )
    expect(getResponsesCompactAutoFallbackReason('{}')).toBe('')
    expect(
      isResponsesCompactAutoFallbackActive(
        JSON.stringify({
          responses_compact_mode: RESPONSES_COMPACT_MODE_NATIVE,
          responses_compact_auto_fallback_date: 20260526,
        }),
        new Date('2026-05-26T12:00:00.000Z')
      )
    ).toBe(false)
    for (const fallbackDate of [undefined, 'bad', -1]) {
      expect(
        isResponsesCompactAutoFallbackActive(
          JSON.stringify({
            responses_compact_mode: RESPONSES_COMPACT_MODE_AUTO,
            responses_compact_auto_fallback_date: fallbackDate,
          }),
          new Date('2026-05-26T12:00:00.000Z')
        )
      ).toBe(false)
    }
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
