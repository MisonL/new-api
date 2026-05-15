import { describe, expect, test } from 'bun:test'
import {
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
})
