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

import { buildViewChannelKeyRequest } from './secureVerification.api.js';

test('view channel key requests bypass the global error handler before verification', () => {
  const request = buildViewChannelKeyRequest(179);

  assert.equal(request.url, '/api/channel/179/key');
  assert.deepEqual(request.data, {});
  assert.deepEqual(request.config, { skipErrorHandler: true });
});

test('view channel key requests reject invalid channel ids before building a URL', () => {
  for (const channelId of [undefined, null, 0, -1, 1.5, '179']) {
    assert.throws(
      () => buildViewChannelKeyRequest(channelId),
      /channelId must be a positive integer/,
    );
  }
});
