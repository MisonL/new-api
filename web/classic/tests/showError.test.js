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

import { beforeEach, describe, expect, mock, test } from 'bun:test';

const toastError = mock(() => undefined);
const authExpired = mock(() => undefined);

mock.module('@douyinfe/semi-ui', () => ({
  Toast: {
    error: toastError,
    info: mock(() => undefined),
    warning: mock(() => undefined),
    success: mock(() => undefined),
  },
  Pagination: mock(() => null),
}));

mock.module('../src/helpers/authRedirect', () => ({
  handleAuthExpired: authExpired,
}));

// utils.jsx reads window.matchMedia at module scope to pick its toast options.
globalThis.window = {
  matchMedia: mock(() => ({ matches: false, addListener: mock(() => {}), addEventListener: mock(() => {}), removeListener: mock(() => {}), removeEventListener: mock(() => {}) })),
};

const { showError } = await import('../src/helpers/utils.jsx');

// An AxiosError raised before the response arrives (DNS failure, connection
// refused, offline) carries no `response`. Reading `error.response.status` then
// throws a TypeError inside the error handler, so the user sees nothing at all
// and the original failure never reaches the log either.
function networkAxiosError() {
  const error = new Error('Network Error');
  error.name = 'AxiosError';
  return error;
}

describe('showError', () => {
  beforeEach(() => {
    toastError.mockClear();
    authExpired.mockClear();
  });

  test('reports an axios network failure instead of throwing', () => {
    showError(networkAxiosError());

    expect(toastError).toHaveBeenCalledTimes(1);
    expect(toastError.mock.calls[0][0]).toContain('Network Error');
  });

  test('still routes a 401 to the auth-expired handler', () => {
    const error = new Error('Request failed with status code 401');
    error.name = 'AxiosError';
    error.response = { status: 401, data: { message: 'unauthorized' } };

    showError(error);

    expect(authExpired).toHaveBeenCalledTimes(1);
  });

  test('prefers the backend message over the generic axios text', () => {
    const error = new Error('Request failed with status code 400');
    error.name = 'AxiosError';
    error.response = {
      status: 400,
      data: { message: 'channel quota exhausted', success: false },
    };

    showError(error);

    expect(toastError.mock.calls[0][0]).toContain('channel quota exhausted');
  });
});