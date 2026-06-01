import { describe, expect, test } from 'bun:test'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
} from '../src/features/channels/lib/channel-form'
import {
  RESPONSES_COMPACT_BADGE_KEYS,
  RESPONSES_COMPACT_CONTEXT_FALLBACK_DEFAULT,
  RESPONSES_COMPACT_MODE_AUTO,
  RESPONSES_COMPACT_MODE_NATIVE,
  RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY,
  RESPONSES_COMPACT_SUMMARY_FALLBACK_MODELS_DEFAULT,
  RESPONSES_COMPACT_SUMMARY_MODEL_FALLBACK_DEFAULT,
  getResponsesCompactAutoFallbackReason,
  getResponsesCompactMode,
  isResponsesCompactAutoFallbackActive,
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
    expect(defaults.responses_compact_context_fallback).toBe(true)
    expect(defaults.responses_compact_summary_model_fallback).toBe(true)
    expect(defaults.responses_compact_summary_fallback_models).toBe('gpt-5.4')

    const payload = transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: 1,
      responses_compact_mode: RESPONSES_COMPACT_MODE_NATIVE,
      responses_compact_context_fallback: false,
      responses_compact_summary_model_fallback: true,
      responses_compact_summary_fallback_models: 'gpt-5.4,gpt-5.4-large',
    })
    const stored = JSON.parse(String(payload.channel.settings))

    expect(stored.responses_compact_mode).toBe(RESPONSES_COMPACT_MODE_NATIVE)
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

    const payload = transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: 1,
      responses_compact_mode: RESPONSES_COMPACT_MODE_AUTO,
    })
    const stored = JSON.parse(String(payload.channel.settings))

    expect(stored.responses_compact_mode).toBe(RESPONSES_COMPACT_MODE_AUTO)
    expect(stored.responses_compact_auto_fallback_date).toBeUndefined()
  })

  test('preserves auto fallback state unless compact mode changes', () => {
    const base = {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: 1,
      settings: JSON.stringify({
        responses_compact_mode: RESPONSES_COMPACT_MODE_AUTO,
        responses_compact_auto_fallback_date: 20260526,
        responses_compact_auto_fallback_reason: 'status_code=404',
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

    const changed = JSON.parse(
      String(
        transformFormDataToCreatePayload({
          ...base,
          responses_compact_mode: RESPONSES_COMPACT_MODE_NATIVE,
        }).channel.settings
      )
    )
    expect(changed.responses_compact_auto_fallback_date).toBeUndefined()
    expect(changed.responses_compact_auto_fallback_reason).toBeUndefined()

    const synthetic = JSON.parse(
      String(
        transformFormDataToCreatePayload({
          ...base,
          responses_compact_mode: RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY,
        }).channel.settings
      )
    )
    expect(synthetic.responses_compact_auto_fallback_date).toBeUndefined()
    expect(synthetic.responses_compact_auto_fallback_reason).toBeUndefined()
  })

  test('drops compact metadata from non OpenAI channel settings', () => {
    const payload = transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: 14,
      responses_compact_mode: RESPONSES_COMPACT_MODE_NATIVE,
      settings: JSON.stringify({
        responses_compact_auto_fallback_date: 20260526,
        responses_compact_auto_fallback_reason: 'status_code=404',
        responses_compact_context_fallback: true,
        responses_compact_summary_model_fallback: true,
        responses_compact_summary_fallback_models: ['gpt-5.4'],
      }),
    })
    const stored = JSON.parse(String(payload.channel.settings))

    expect(stored.responses_compact_mode).toBeUndefined()
    expect(stored.responses_compact_auto_fallback_date).toBeUndefined()
    expect(stored.responses_compact_auto_fallback_reason).toBeUndefined()
    expect(stored.responses_compact_context_fallback).toBeUndefined()
    expect(stored.responses_compact_summary_model_fallback).toBeUndefined()
    expect(stored.responses_compact_summary_fallback_models).toBeUndefined()
  })

  test('detects auto fallback state by UTC date', () => {
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
