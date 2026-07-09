import { describe, expect, test } from 'bun:test'
import { getRequestHeaderPolicy } from '../src/features/usage-logs/lib/request-header-policy'

describe('request header policy log fallback', () => {
  test('prefers structured request_header_policy', () => {
    expect(
      getRequestHeaderPolicy({
        request_header_policy: {
          mode: 'merge',
          applied_user_agent: 'structured-ua',
        },
        selected_user_agent: 'legacy-ua',
      })?.applied_user_agent
    ).toBe('structured-ua')
  })

  test('builds policy from legacy top-level UA audit fields', () => {
    expect(
      getRequestHeaderPolicy({
        header_policy_mode: 'merge',
        ua_strategy_mode: 'round_robin',
        selected_user_agent: 'codex-cli',
        applied_user_agent: 'codex-cli',
      })
    ).toEqual({
      mode: 'merge',
      ua_strategy_mode: 'round_robin',
      selected_user_agent: 'codex-cli',
      applied_user_agent: 'codex-cli',
    })
  })

  test('falls back to legacy top-level fields when structured policy is empty', () => {
    expect(
      getRequestHeaderPolicy({
        request_header_policy: {},
        header_policy_mode: 'prefer_channel',
        applied_user_agent: 'legacy-ua',
      })
    ).toEqual({
      mode: 'prefer_channel',
      applied_user_agent: 'legacy-ua',
    })
  })

  test('falls back to legacy top-level fields when structured policy is null', () => {
    expect(
      getRequestHeaderPolicy({
        request_header_policy: null,
        header_profile_applied: false,
        override_static_user_agent: false,
        user_agent_applied: false,
      })
    ).toEqual({
      header_profile_applied: false,
      override_static_user_agent: false,
      user_agent_applied: false,
    })
  })

  test('ignores empty legacy audit values', () => {
    expect(
      getRequestHeaderPolicy({
        selected_user_agent: ' ',
        applied_header_keys: [],
      })
    ).toBeUndefined()
  })

  test('keeps explicit false legacy boolean audit values', () => {
    expect(
      getRequestHeaderPolicy({
        header_profile_applied: false,
        override_static_user_agent: false,
        user_agent_applied: false,
      })
    ).toEqual({
      header_profile_applied: false,
      override_static_user_agent: false,
      user_agent_applied: false,
    })
  })

  test('ignores missing log metadata', () => {
    expect(getRequestHeaderPolicy(null)).toBeUndefined()
    expect(getRequestHeaderPolicy(undefined)).toBeUndefined()
  })
})
