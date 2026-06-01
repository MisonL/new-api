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

import { afterEach, describe, expect, test } from 'bun:test';

import {
  getIframeTargetOrigin,
  postMessageToIframe,
} from '../src/helpers/iframeMessaging.js';

const originalWindow = globalThis.window;

afterEach(() => {
  globalThis.window = originalWindow;
});

function setLocation(href) {
  globalThis.window = {
    location: {
      href,
    },
  };
}

describe('classic iframe messaging', () => {
  test('uses the iframe http origin as target origin', () => {
    setLocation('https://new-api.example/console');

    const iframe = {
      getAttribute: () => 'https://docs.example/path?x=1',
    };

    expect(getIframeTargetOrigin(iframe)).toBe('https://docs.example');
  });

  test('resolves relative iframe src against current location', () => {
    setLocation('https://new-api.example/console');

    const iframe = {
      getAttribute: () => '/home',
    };

    expect(getIframeTargetOrigin(iframe)).toBe('https://new-api.example');
  });

  test('rejects non web iframe targets', () => {
    setLocation('https://new-api.example/console');

    const iframe = {
      getAttribute: () => 'javascript:alert(1)',
    };

    expect(getIframeTargetOrigin(iframe)).toBeNull();
  });

  test('posts to a concrete target origin', () => {
    setLocation('https://new-api.example/console');

    const calls = [];
    const iframe = {
      getAttribute: () => 'https://docs.example/home',
      contentWindow: {
        postMessage: (message, targetOrigin) => {
          calls.push({ message, targetOrigin });
        },
      },
    };

    expect(postMessageToIframe(iframe, { themeMode: 'dark' })).toBe(true);
    expect(calls).toEqual([
      {
        message: { themeMode: 'dark' },
        targetOrigin: 'https://docs.example',
      },
    ]);
  });

  test('does not post without a usable target origin', () => {
    setLocation('https://new-api.example/console');

    const calls = [];
    const iframe = {
      getAttribute: () => 'about:blank',
      contentWindow: {
        postMessage: (message, targetOrigin) => {
          calls.push({ message, targetOrigin });
        },
      },
    };

    expect(postMessageToIframe(iframe, { lang: 'zh' })).toBe(false);
    expect(calls).toEqual([]);
  });
});
