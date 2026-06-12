/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import { describe, expect, test } from 'bun:test';

import {
  CHAT_TO_RESPONSES_TEMPLATE,
  PROTOCOL_FILTER_ALL,
  PROTOCOL_RULE_SCOPE_EMPTY,
  PROTOCOL_RULE_STATE_ATTENTION,
  RESPONSES_TO_CHAT_TEMPLATE,
  TEMPLATE_TYPE_CHAT_TO_RESPONSES,
  TEMPLATE_TYPE_RESPONSES_TO_CHAT,
} from '../src/components/settings/protocolConversionPolicy/constants.js';
import {
  buildTemplateRule,
  deserializePolicy,
  filterProtocolRules,
  getEndpointLabel,
  getProtocolPolicyStats,
  getProtocolPreviewResult,
  getRuleModelSummary,
  parseIntegerList,
  serializeRules,
} from '../src/components/settings/protocolConversionPolicy/utils.js';

describe('classic protocol conversion policy utils', () => {
  test('preserves custom tool bridge and unknown fields through visual round trip', () => {
    const raw = JSON.stringify({
      future_policy: { keep: true },
      rules: [
        {
          name: 'responses-to-chat',
          enabled: true,
          source_endpoint: 'responses',
          target_endpoint: 'chat_completions',
          all_channels: false,
          channel_ids: [117],
          model_patterns: ['^gpt-5.*$'],
          future_rule: 'rule-extra',
          options: {
            enable_custom_tool_bridge: true,
            future_option: 'option-extra',
          },
        },
      ],
    });

    const parsed = deserializePolicy(raw);
    expect(parsed).not.toBeNull();

    const serialized = JSON.parse(
      serializeRules(parsed.rules, parsed.policyExtra),
    );
    expect(serialized.future_policy).toEqual({ keep: true });
    expect(serialized.rules[0].future_rule).toBe('rule-extra');
    expect(serialized.rules[0].options.enable_custom_tool_bridge).toBe(true);
    expect(serialized.rules[0].options.future_option).toBe('option-extra');
  });

  test('rejects custom tool bridge when visual direction is not responses to chat', () => {
    const raw = JSON.stringify({
      rules: [
        {
          name: 'chat-to-responses',
          enabled: true,
          source_endpoint: 'chat_completions',
          target_endpoint: 'responses',
          all_channels: true,
          model_patterns: ['^gpt-5.*$'],
          options: {
            enable_custom_tool_bridge: true,
          },
        },
      ],
    });

    const parsed = deserializePolicy(raw);
    expect(parsed).toBeNull();
  });

  test('keeps explicit empty rules from falling back to legacy fields', () => {
    const parsed = deserializePolicy(
      JSON.stringify({
        enabled: true,
        all_channels: true,
        model_patterns: ['^gpt-5.*$'],
        rules: [],
      }),
    );

    expect(parsed).not.toBeNull();
    expect(parsed.rules).toEqual([]);
    expect(
      JSON.parse(serializeRules(parsed.rules, parsed.policyExtra)),
    ).toEqual({});
  });

  test('template labels and generated rules keep the same direction', () => {
    const chatToResponses = buildTemplateRule(CHAT_TO_RESPONSES_TEMPLATE, []);
    const responsesToChat = buildTemplateRule(RESPONSES_TO_CHAT_TEMPLATE, []);

    expect(chatToResponses.name).toBe('chat-to-responses');
    expect(
      `${getEndpointLabel(chatToResponses.source_endpoint)} -> ${getEndpointLabel(chatToResponses.target_endpoint)}`,
    ).toBe('/v1/chat/completions -> /v1/responses');

    expect(responsesToChat.name).toBe('responses-to-chat');
    expect(
      `${getEndpointLabel(responsesToChat.source_endpoint)} -> ${getEndpointLabel(responsesToChat.target_endpoint)}`,
    ).toBe('/v1/responses -> /v1/chat/completions');
  });

  test('rejects non-object options instead of silently dropping them', () => {
    const parsed = deserializePolicy(
      JSON.stringify({
        rules: [
          {
            name: 'invalid-options',
            enabled: true,
            source_endpoint: 'responses',
            target_endpoint: 'chat_completions',
            all_channels: true,
            model_patterns: ['^gpt-5.*$'],
            options: null,
          },
        ],
      }),
    );

    expect(parsed).toBeNull();
  });

  test('rejects invalid rule and scope shapes instead of filtering them', () => {
    expect(
      deserializePolicy(
        JSON.stringify({
          rules: [null],
        }),
      ),
    ).toBeNull();

    expect(
      deserializePolicy(
        JSON.stringify({
          rules: [
            {
              name: 'invalid-channel',
              enabled: true,
              source_endpoint: 'responses',
              target_endpoint: 'chat_completions',
              all_channels: false,
              channel_ids: [117, -1],
              model_patterns: ['^gpt-5.*$'],
            },
          ],
        }),
      ),
    ).toBeNull();
  });

  test('parses channel id text with common separators', () => {
    expect(parseIntegerList('35,36, 37')).toEqual([35, 36, 37]);
    expect(parseIntegerList('35，36、37\n38 39')).toEqual([35, 36, 37, 38, 39]);
    expect(parseIntegerList('35,')).toEqual([35]);
  });

  test('summarizes empty model patterns as all models', () => {
    expect(getRuleModelSummary({ model_patterns: [] }, (value) => value)).toBe(
      '全部非空模型',
    );
  });

  test('computes stats and filters rules by direction state scope and query', () => {
    const rules = [
      buildTemplateRule(CHAT_TO_RESPONSES_TEMPLATE, []),
      {
        ...buildTemplateRule(RESPONSES_TO_CHAT_TEMPLATE, []),
        enabled: false,
        all_channels: false,
        channel_ids: [],
        channel_types: [],
        model_patterns: ['^deepseek.*$'],
      },
    ];

    const stats = getProtocolPolicyStats(rules, true);
    expect(stats).toMatchObject({
      total: 2,
      enabled: 1,
      disabled: 1,
      chatToResponses: 1,
      responsesToChat: 1,
      limitedScope: 1,
      emptyScope: 1,
      attention: 2,
    });

    expect(
      filterProtocolRules(
        rules,
        {
          direction: TEMPLATE_TYPE_RESPONSES_TO_CHAT,
          state: PROTOCOL_RULE_STATE_ATTENTION,
          scope: PROTOCOL_RULE_SCOPE_EMPTY,
          query: 'deepseek',
        },
        false,
      ).map((item) => item.index),
    ).toEqual([1]);

    expect(
      filterProtocolRules(
        rules,
        {
          direction: TEMPLATE_TYPE_CHAT_TO_RESPONSES,
          state: PROTOCOL_FILTER_ALL,
          scope: PROTOCOL_FILTER_ALL,
          query: '',
        },
        false,
      ).map((item) => item.index),
    ).toEqual([0]);
  });

  test('previews rule hits with empty model patterns matching non-empty models', () => {
    const rule = {
      ...buildTemplateRule(CHAT_TO_RESPONSES_TEMPLATE, []),
      all_channels: false,
      channel_ids: [145],
      channel_types: [],
      model_patterns: [],
    };

    expect(
      getProtocolPreviewResult(
        rule,
        { channelId: '145', channelType: '', model: 'gpt-5' },
        false,
      ),
    ).toEqual({ matched: true, reason: '示例请求命中这条规则。' });

    expect(
      getProtocolPreviewResult(
        rule,
        { channelId: '146', channelType: '', model: 'gpt-5' },
        false,
      ),
    ).toEqual({ matched: false, reason: '渠道范围不匹配。' });

    expect(
      getProtocolPreviewResult(
        { ...rule, enabled: false },
        { channelId: '145', channelType: '', model: 'gpt-5' },
        false,
      ),
    ).toEqual({ matched: false, reason: '规则已停用。' });
  });
});
