import { describe, expect, test } from 'bun:test'
import {
  ENDPOINT_CHAT,
  ENDPOINT_RESPONSES,
  TEMPLATE_BIDIRECTIONAL,
  TEMPLATE_CHAT_TO_RESPONSES,
  TEMPLATE_RESPONSES_TO_CHAT,
  createCommittedDraftText,
  createDraftTextChange,
  createProtocolRuleFromTemplate,
  getDraftTextValue,
  getProtocolPreviewResult,
  getProtocolRuleWarningKeys,
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

  test('keeps explicit empty rules from falling back to legacy fields', () => {
    const parsed = parseProtocolPolicy(
      JSON.stringify({
        enabled: true,
        all_channels: true,
        model_patterns: ['^gpt-5.*$'],
        rules: [],
      })
    )
    expect(parsed.ok).toBe(true)
    if (!parsed.ok) return

    expect(parsed.rules).toEqual([])
    expect(JSON.parse(serializeProtocolPolicy(parsed.rules, parsed.policyExtra))).toEqual({})
  })

  test('normalizes endpoint aliases without changing rule semantics', () => {
    const parsed = parseProtocolPolicy(
      JSON.stringify({
        rules: [
          {
            name: 'alias-rule',
            source_endpoint: '/v1/chat/completions',
            target_endpoint: '/v1/responses',
            all_channels: true,
            model_patterns: ['^gpt-4o.*$'],
          },
        ],
      })
    )
    expect(parsed.ok).toBe(true)
    if (!parsed.ok) return

    expect(parsed.rules[0].source_endpoint).toBe(ENDPOINT_CHAT)
    expect(parsed.rules[0].target_endpoint).toBe(ENDPOINT_RESPONSES)
  })

  test('creates protocol rules from direction templates', () => {
    const responsesToChat = createProtocolRuleFromTemplate(
      TEMPLATE_RESPONSES_TO_CHAT
    )
    expect(responsesToChat).toHaveLength(1)
    expect(responsesToChat[0].name).toBe('responses-to-chat')
    expect(responsesToChat[0].source_endpoint).toBe(ENDPOINT_RESPONSES)
    expect(responsesToChat[0].target_endpoint).toBe(ENDPOINT_CHAT)
    expect(responsesToChat[0].all_channels).toBe(false)
    expect(responsesToChat[0].channel_types).toEqual([1])

    const chatToResponses = createProtocolRuleFromTemplate(
      TEMPLATE_CHAT_TO_RESPONSES
    )
    expect(chatToResponses[0].name).toBe('chat-to-responses')
    expect(chatToResponses[0].source_endpoint).toBe(ENDPOINT_CHAT)
    expect(chatToResponses[0].target_endpoint).toBe(ENDPOINT_RESPONSES)

    const bidirectional = createProtocolRuleFromTemplate(
      TEMPLATE_BIDIRECTIONAL,
      [responsesToChat[0], chatToResponses[0]]
    )
    expect(bidirectional.map((rule) => rule.name)).toEqual([
      'responses-to-chat-2',
      'chat-to-responses-2',
    ])
    expect(bidirectional.map((rule) => rule.source_endpoint)).toEqual([
      ENDPOINT_RESPONSES,
      ENDPOINT_CHAT,
    ])
  })

  test('reports required endpoint fields before alias validation', () => {
    const parsed = parseProtocolPolicy(
      JSON.stringify({
        rules: [
          {
            name: 'missing-source',
            target_endpoint: ENDPOINT_RESPONSES,
            all_channels: true,
            model_patterns: ['^gpt-4o.*$'],
          },
        ],
      })
    )

    expect(parsed.ok).toBe(false)
    if (parsed.ok) return
    expect(parsed.error).toContain('source_endpoint is required')
  })

  test('rejects non-object rules instead of dropping them', () => {
    const parsed = parseProtocolPolicy(
      JSON.stringify({
        rules: [
          {
            name: 'valid-rule',
            source_endpoint: ENDPOINT_CHAT,
            target_endpoint: ENDPOINT_RESPONSES,
            all_channels: true,
            model_patterns: ['^gpt-4o.*$'],
          },
          null,
        ],
      })
    )

    expect(parsed.ok).toBe(false)
    if (parsed.ok) return
    expect(parsed.error).toContain('rules[1]')
  })

  test('rejects invalid channel scope values instead of filtering them', () => {
    const parsed = parseProtocolPolicy(
      JSON.stringify({
        rules: [
          {
            name: 'invalid-channel',
            source_endpoint: ENDPOINT_CHAT,
            target_endpoint: ENDPOINT_RESPONSES,
            all_channels: false,
            channel_ids: [3, -1],
            channel_types: [1, 0],
            model_patterns: ['^gpt-4o.*$'],
          },
        ],
      })
    )

    expect(parsed.ok).toBe(false)
    if (parsed.ok) return
    expect(parsed.error).toContain('channel_ids')
  })

  test('reports rule field path when channel scope is not an array', () => {
    const parsed = parseProtocolPolicy(
      JSON.stringify({
        rules: [
          {
            name: 'invalid-channel-shape',
            source_endpoint: ENDPOINT_CHAT,
            target_endpoint: ENDPOINT_RESPONSES,
            all_channels: false,
            channel_ids: '3',
            model_patterns: ['^gpt-4o.*$'],
          },
        ],
      })
    )

    expect(parsed.ok).toBe(false)
    if (parsed.ok) return
    expect(parsed.error).toContain('rules[0].channel_ids')
  })

  test('rejects unsupported endpoints instead of falling back to defaults', () => {
    const parsed = parseProtocolPolicy(
      JSON.stringify({
        rules: [
          {
            name: 'invalid-endpoint',
            source_endpoint: 'custom-chat',
            target_endpoint: ENDPOINT_RESPONSES,
            all_channels: true,
            model_patterns: ['^gpt-4o.*$'],
          },
        ],
      })
    )

    expect(parsed.ok).toBe(false)
    if (parsed.ok) return
    expect(parsed.error).toContain('source_endpoint')
  })

  test('rejects invalid model regex instead of preview-only failure', () => {
    const parsed = parseProtocolPolicy(
      JSON.stringify({
        rules: [
          {
            name: 'invalid-regex',
            source_endpoint: ENDPOINT_CHAT,
            target_endpoint: ENDPOINT_RESPONSES,
            all_channels: true,
            model_patterns: ['['],
          },
        ],
      })
    )

    expect(parsed.ok).toBe(false)
    if (parsed.ok) return
    expect(parsed.error).toContain('model_patterns')
  })

  test('rejects custom tool bridge outside responses to chat direction', () => {
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

    expect(parsed.ok).toBe(false)
    if (parsed.ok) return
    expect(parsed.error).toContain('enable_custom_tool_bridge')
  })

  test('preview matches all non-empty models when model patterns are empty', () => {
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
      { channelId: '1', channelType: '1', model: 'deepseek-v4-flash' },
      false
    )
    expect(result).toEqual({
      matched: true,
      reason: 'Sample request matches this rule.',
    })
    expect(getProtocolRuleWarningKeys(parsed.rules[0])).not.toContain(
      'Model patterns are empty. This rule will not match.'
    )
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
    expect(parseIntegerText('1，2、3\n4 5')).toEqual([1, 2, 3, 4, 5])
    expect(parseIntegerText('1abc')).toEqual([])
    expect(parseIntegerText('1abc, 2, 3x')).toEqual([2])
  })

  test('preview rejects dirty channel identifiers', () => {
    const parsed = parseProtocolPolicy(
      JSON.stringify({
        rules: [
          {
            name: 'channel-scope',
            enabled: true,
            source_endpoint: ENDPOINT_CHAT,
            target_endpoint: ENDPOINT_RESPONSES,
            all_channels: false,
            channel_ids: [1],
            channel_types: [2],
            model_patterns: ['^gpt-5.*$'],
          },
        ],
      })
    )
    expect(parsed.ok).toBe(true)
    if (!parsed.ok) return

    expect(
      getProtocolPreviewResult(
        parsed.rules[0],
        { channelId: '1abc', channelType: '', model: 'gpt-5' },
        false
      )
    ).toEqual({
      matched: false,
      reason: 'Channel scope does not match.',
    })

    expect(
      getProtocolPreviewResult(
        parsed.rules[0],
        { channelId: '', channelType: '2abc', model: 'gpt-5' },
        false
      )
    ).toEqual({
      matched: false,
      reason: 'Channel scope does not match.',
    })
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
