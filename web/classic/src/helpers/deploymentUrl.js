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

export function normalizeDeploymentPublicUrl(container) {
  const raw =
    typeof container?.public_url === 'string'
      ? container.public_url.trim()
      : '';
  if (!raw) return null;

  try {
    const url = new URL(raw);
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null;
    if (!url.hostname || url.username || url.password) return null;
    url.hash = '';
    url.search = '';
    return url.toString().replace(/\/$/, '');
  } catch {
    return null;
  }
}

export function generateDeploymentChannelKey(prefix = 'ionet') {
  const cryptoApi =
    typeof globalThis !== 'undefined' ? globalThis.crypto : undefined;
  if (!cryptoApi) {
    throw new Error('secure random source unavailable');
  }

  if (typeof cryptoApi.randomUUID === 'function') {
    return `${prefix}-${cryptoApi.randomUUID().replace(/-/g, '')}`;
  }

  if (typeof cryptoApi.getRandomValues === 'function') {
    const bytes = new Uint8Array(16);
    cryptoApi.getRandomValues(bytes);
    const suffix = Array.from(bytes, (byte) =>
      byte.toString(16).padStart(2, '0'),
    ).join('');
    return `${prefix}-${suffix}`;
  }

  throw new Error('secure random source unavailable');
}
