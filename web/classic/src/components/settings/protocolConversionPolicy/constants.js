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

export const EDIT_MODE_VISUAL = 'visual';
export const EDIT_MODE_JSON = 'json';
export const ENDPOINT_CHAT = 'chat_completions';
export const ENDPOINT_RESPONSES = 'responses';
export const TEMPLATE_TYPE_CHAT_TO_RESPONSES = 'chat_to_responses';
export const TEMPLATE_TYPE_RESPONSES_TO_CHAT = 'responses_to_chat';
export const TEMPLATE_TYPE_BIDIRECTIONAL = 'bidirectional';

export const ENDPOINT_OPTIONS = [
  {
    label: '/v1/chat/completions',
    value: ENDPOINT_CHAT,
  },
  {
    label: '/v1/responses',
    value: ENDPOINT_RESPONSES,
  },
];

export const SUPPORTED_ENDPOINT_VALUES = new Set(
  ENDPOINT_OPTIONS.map((item) => item.value),
);

export const CHAT_TO_RESPONSES_TEMPLATE = {
  name: 'chat-to-responses',
  enabled: true,
  source_endpoint: ENDPOINT_CHAT,
  target_endpoint: ENDPOINT_RESPONSES,
  all_channels: false,
  channel_ids: [],
  channel_types: [1],
  model_patterns: ['^gpt-4o.*$', '^gpt-5.*$'],
};

export const RESPONSES_TO_CHAT_TEMPLATE = {
  name: 'responses-to-chat',
  enabled: true,
  source_endpoint: ENDPOINT_RESPONSES,
  target_endpoint: ENDPOINT_CHAT,
  all_channels: false,
  channel_ids: [],
  channel_types: [1],
  model_patterns: ['^gpt-5.*$', '^o[13].*$'],
};

export const POLICY_JSON_EXAMPLE = JSON.stringify(
  {
    rules: [CHAT_TO_RESPONSES_TEMPLATE, RESPONSES_TO_CHAT_TEMPLATE],
  },
  null,
  2,
);

export const panelStyle = {
  border: '1px solid var(--semi-color-border)',
  borderRadius: 12,
  padding: 14,
  background: 'var(--semi-color-fill-0)',
};
