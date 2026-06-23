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

import {
  getResponsesCompactModeFromSettings,
  normalizeResponsesUpstreamProfile,
  RESPONSES_COMPACT_MODE_AUTO,
  RESPONSES_COMPACT_MODE_NATIVE,
  RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY,
  RESPONSES_UPSTREAM_PROFILE_DEFAULT,
  RESPONSES_UPSTREAM_PROFILE_GENERIC_OPENAI,
  RESPONSES_UPSTREAM_PROFILE_GENERIC_PROXY,
  RESPONSES_UPSTREAM_PROFILE_OFFICIAL_OPENAI,
  RESPONSES_UPSTREAM_PROFILE_SUB2API_HTTP,
} from './responsesCompactSettings.js';

test('generic OpenAI profile is preserved and does not force synthetic compact', () => {
  assert.equal(
    normalizeResponsesUpstreamProfile(
      RESPONSES_UPSTREAM_PROFILE_GENERIC_OPENAI,
    ),
    RESPONSES_UPSTREAM_PROFILE_GENERIC_OPENAI,
  );
  assert.equal(
    getResponsesCompactModeFromSettings({
      responses_compact_mode: RESPONSES_COMPACT_MODE_AUTO,
      responses_upstream_profile: RESPONSES_UPSTREAM_PROFILE_GENERIC_OPENAI,
    }),
    RESPONSES_COMPACT_MODE_AUTO,
  );
});

test('known upstream profiles are normalized and unknown profiles use default', () => {
  assert.equal(
    normalizeResponsesUpstreamProfile(
      RESPONSES_UPSTREAM_PROFILE_OFFICIAL_OPENAI,
    ),
    RESPONSES_UPSTREAM_PROFILE_OFFICIAL_OPENAI,
  );
  assert.equal(
    normalizeResponsesUpstreamProfile(RESPONSES_UPSTREAM_PROFILE_SUB2API_HTTP),
    RESPONSES_UPSTREAM_PROFILE_SUB2API_HTTP,
  );
  assert.equal(
    normalizeResponsesUpstreamProfile(RESPONSES_UPSTREAM_PROFILE_GENERIC_PROXY),
    RESPONSES_UPSTREAM_PROFILE_GENERIC_PROXY,
  );
  assert.equal(
    normalizeResponsesUpstreamProfile(null),
    RESPONSES_UPSTREAM_PROFILE_DEFAULT,
  );
  assert.equal(
    normalizeResponsesUpstreamProfile('unknown_profile'),
    RESPONSES_UPSTREAM_PROFILE_DEFAULT,
  );
});

test('compact mode respects native and synthetic settings', () => {
  assert.equal(
    getResponsesCompactModeFromSettings({
      responses_compact_mode: RESPONSES_COMPACT_MODE_NATIVE,
      responses_upstream_profile: RESPONSES_UPSTREAM_PROFILE_OFFICIAL_OPENAI,
    }),
    RESPONSES_COMPACT_MODE_NATIVE,
  );
  assert.equal(
    getResponsesCompactModeFromSettings({
      responses_compact_mode: RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY,
      responses_upstream_profile: RESPONSES_UPSTREAM_PROFILE_SUB2API_HTTP,
    }),
    RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY,
  );
});

test('proxy compatibility profiles force synthetic compact mode', () => {
  assert.equal(
    getResponsesCompactModeFromSettings({
      responses_compact_mode: RESPONSES_COMPACT_MODE_NATIVE,
      responses_upstream_profile: RESPONSES_UPSTREAM_PROFILE_GENERIC_PROXY,
    }),
    RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY,
  );
});

test('undefined settings use auto compact mode', () => {
  assert.equal(
    getResponsesCompactModeFromSettings(),
    RESPONSES_COMPACT_MODE_AUTO,
  );
});
