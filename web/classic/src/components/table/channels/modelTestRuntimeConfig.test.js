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

import test from 'node:test';
import assert from 'node:assert/strict';

import { formatRuntimeResult } from './modelTestRuntimeConfig.js';

const translations = {
  Yes: '是',
  No: '否',
};

const t = (value) => translations[value] || value;

test('formatRuntimeResult includes compact capability observations', () => {
  const result = formatRuntimeResult(
    {
      runtime_config_enabled: true,
      header_config_enabled: true,
      header_applied: true,
      param_override_enabled: true,
      param_override_applied: true,
      proxy_enabled: false,
      request_path: '/v1/messages',
      final_request_path: '/v1/responses',
      upstream_request_path: '/v1/responses',
      request_conversion_chain: ['Claude Messages', 'OpenAI Responses'],
      channel_capability_snapshot: {
        source: 'observed_calls',
        profile: 'generic_openai',
        compact_mode_effective: 'native',
        supports_responses_compact: true,
        supports_rest_previous_response_id: false,
        supports_compaction_item_passthrough: false,
        strips_responses_encrypted_reasoning: true,
        observed: {
          observed_at: 1781660000,
          status_code: 400,
          error_code: 'invalid_request_error',
          reason: 'input item type compaction is not supported',
        },
        probe: {
          observed_at: 1781660060,
          status_code: 200,
          reason: 'probe ok',
        },
      },
    },
    t,
  );

  assert.match(result, /Capability: .*source=observed_calls/);
  assert.ok(result.includes('客户端路径: /v1/messages -> /v1/responses'));
  assert.ok(result.includes('上游路径: /v1/responses'));
  assert.ok(result.includes('协议: Claude Messages -> OpenAI Responses'));
  assert.match(result, /profile=generic_openai/);
  assert.match(result, /compact: 是/);
  assert.match(result, /previous_id: 否/);
  assert.match(result, /strip_reasoning: 是/);
  assert.match(result, /last fail: status=400/);
  assert.match(result, /code=invalid_request_error/);
  assert.match(result, /last probe: status=200/);
});
