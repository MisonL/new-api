import { describe, expect, test } from 'bun:test'
import {
  ENDPOINT_CHAT,
  ENDPOINT_RESPONSES,
  createCommittedDraftText,
  createDraftTextChange,
  getDraftTextValue,
  getProtocolPreviewResult,
  parseIntegerText,
  parseLines,
  parseProtocolPolicy,
  serializeProtocolPolicy,
} from '../src/features/system-settings/models/protocol-conversion-policy-utils'

describe('protocol conversion policy utils', () => {
  test('preserves policy, rule, and options extra fields', () => {
    const raw = JSON.stringify({
      vendor_policy_flag: 'keep',
      rules: [
        {
          name: 'responses-to-chat',
          enabled: true,
          source_endpoint: 'responses',
          target_endpoint: 'chat_completions',
          all_channels: false,
          channel_ids: [1],
          model_patterns: ['^gpt-5.*$'],
          vendor_rule_flag: 7,
          options: {
            enable_custom_tool_bridge: true,
            vendor_option_flag: 'keep',
          },
        },
      ],
    })

    const parsed = parseProtocolPolicy(raw)
    expect(parsed.ok).toBe(true)
    if (!parsed.ok) return

    const serialized = JSON.parse(
      serializeProtocolPolicy(parsed.rules, parsed.policyExtra)
    )

    expect(serialized.vendor_policy_flag).toBe('keep')
    expect(serialized.rules[0].vendor_rule_flag).toBe(7)
    expect(serialized.rules[0].options.vendor_option_flag).toBe('keep')
    expect(serialized.rules[0].options.enable_custom_tool_bridge).toBe(true)
  })

  test('upgrades legacy policy to rules shape', () => {
    const parsed = parseProtocolPolicy(
      JSON.stringify({
        enabled: true,
        all_channels: false,
        channel_ids: [2],
        channel_types: [1],
        model_patterns: ['^gpt-4o.*$'],
      })
    )
    expect(parsed.ok).toBe(true)
    if (!parsed.ok) return

    const serialized = JSON.parse(
      serializeProtocolPolicy(parsed.rules, parsed.policyExtra)
    )
    expect(serialized.rules).toHaveLength(1)
    expect(serialized.rules[0].source_endpoint).toBe('chat_completions')
    expect(serialized.rules[0].target_endpoint).toBe('responses')
    expect(serialized.rules[0].channel_ids).toEqual([2])
  })

  test('drops custom tool bridge outside responses to chat direction', () => {
    const parsed = parseProtocolPolicy(
      JSON.stringify({
        rules: [
          {
            name: 'chat-to-responses',
            source_endpoint: 'chat_completions',
            target_endpoint: 'responses',
            all_channels: true,
            options: {
              enable_custom_tool_bridge: true,
            },
          },
        ],
      })
    )
    expect(parsed.ok).toBe(true)
    if (!parsed.ok) return

    const serialized = JSON.parse(
      serializeProtocolPolicy(parsed.rules, parsed.policyExtra)
    )
    expect(serialized.rules[0].options).toBeUndefined()
  })

  test('preview does not match when model patterns are empty', () => {
    const parsed = parseProtocolPolicy(
      JSON.stringify({
        rules: [
          {
            name: 'empty-model-patterns',
            enabled: true,
            source_endpoint: ENDPOINT_CHAT,
            target_endpoint: ENDPOINT_RESPONSES,
            all_channels: true,
            model_patterns: [],
          },
        ],
      })
    )
    expect(parsed.ok).toBe(true)
    if (!parsed.ok) return

    const result = getProtocolPreviewResult(
      parsed.rules[0],
      { channelId: '1', channelType: '1', model: 'gpt-5' },
      false
    )
    expect(result).toEqual({
      matched: false,
      reason: 'Model patterns are empty. This rule will not match.',
    })
  })

  test('parses draft model patterns without requiring committed input cleanup', () => {
    expect(parseLines('^gpt-5.*$\n')).toEqual(['^gpt-5.*$'])
    expect(parseLines('^gpt-5.*$\n\n^gpt-4o.*$')).toEqual([
      '^gpt-5.*$',
      '^gpt-4o.*$',
    ])
  })

  test('parses draft channel types after users type separators', () => {
    expect(parseIntegerText('1, ')).toEqual([1])
    expect(parseIntegerText('1, 2,')).toEqual([1, 2])
  })

  test('keeps draft text visible while the parsed parent value is already synchronized', () => {
    const channelDraft = createDraftTextChange('1, ', '1')
    expect(getDraftTextValue(channelDraft, '1')).toBe('1, ')
    expect(getDraftTextValue(channelDraft, '1, 2')).toBe('1, 2')

    const patternDraft = createDraftTextChange('^gpt-5.*$\n', '^gpt-5.*$')
    expect(getDraftTextValue(patternDraft, '^gpt-5.*$')).toBe('^gpt-5.*$\n')
    expect(getDraftTextValue(patternDraft, '^gpt-4o.*$')).toBe('^gpt-4o.*$')

    expect(createCommittedDraftText('1, 2')).toEqual({
      source: '1, 2',
      value: '1, 2',
    })
  })
})
