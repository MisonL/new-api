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

import { normalizeRequestHeaderPolicyFromLogOther } from './headerAuditInfo.js';

test('normalizeRequestHeaderPolicyFromLogOther prefers structured policy', () => {
  assert.deepEqual(
    normalizeRequestHeaderPolicyFromLogOther({
      request_header_policy: {
        mode: 'merge',
        applied_user_agent: 'structured-ua',
      },
      applied_user_agent: 'legacy-ua',
    }),
    {
      mode: 'merge',
      applied_user_agent: 'structured-ua',
    },
  );
});

test('normalizeRequestHeaderPolicyFromLogOther falls back when structured policy is empty', () => {
  assert.deepEqual(
    normalizeRequestHeaderPolicyFromLogOther({
      request_header_policy: {},
      header_policy_mode: 'prefer_channel',
      applied_user_agent: 'legacy-ua',
    }),
    {
      mode: 'prefer_channel',
      applied_user_agent: 'legacy-ua',
    },
  );
});

test('normalizeRequestHeaderPolicyFromLogOther falls back when structured policy is null', () => {
  assert.deepEqual(
    normalizeRequestHeaderPolicyFromLogOther({
      request_header_policy: null,
      header_profile_applied: false,
      override_static_user_agent: false,
      user_agent_applied: false,
    }),
    {
      header_profile_applied: false,
      override_static_user_agent: false,
      user_agent_applied: false,
    },
  );
});

test('normalizeRequestHeaderPolicyFromLogOther ignores missing metadata', () => {
  assert.equal(normalizeRequestHeaderPolicyFromLogOther(null), null);
  assert.equal(normalizeRequestHeaderPolicyFromLogOther(undefined), null);
  assert.equal(normalizeRequestHeaderPolicyFromLogOther({}), null);
});
