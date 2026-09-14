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

import { useCallback, useEffect, useRef, useState } from 'react';
import { API } from '../../helpers';
import { getChannelHealthRuntimeRefreshDelay } from '../../helpers/channelHealthRuntime';

const RUNTIME_REQUEST_TIMEOUT_MS = 8000;
const runtimeRequestConfig = {
  timeout: RUNTIME_REQUEST_TIMEOUT_MS,
  skipBusinessError: true,
  skipErrorHandler: true,
};

export const useChannelHealthRuntime = ({ enabled }) => {
  const [runtime, setRuntime] = useState(null);
  const [runtimeUnavailable, setRuntimeUnavailable] = useState(false);
  const [runtimeUpdatedAt, setRuntimeUpdatedAt] = useState(null);
  const [runtimeLoading, setRuntimeLoading] = useState(false);
  const runtimeRequestRef = useRef(null);
  const runtimeRequestVisibleRef = useRef(false);
  const runtimeSnapshotRef = useRef(null);

  const refresh = useCallback(
    async ({ silent = false } = {}) => {
      if (!enabled) return;

      if (!silent) {
        runtimeRequestVisibleRef.current = true;
        setRuntimeLoading(true);
      }
      if (runtimeRequestRef.current) return runtimeRequestRef.current;

      const request = API.get(
        '/api/option/channel_health_status',
        runtimeRequestConfig,
      )
        .then((res) => {
          if (!res.data?.success || !res.data?.data) {
            throw new Error(
              res.data?.message || 'Failed to load channel health runtime',
            );
          }
          runtimeSnapshotRef.current = res.data.data;
          setRuntime(res.data.data);
          setRuntimeUnavailable(false);
          setRuntimeUpdatedAt(new Date());
        })
        .catch((error) => {
          runtimeSnapshotRef.current = null;
          setRuntime(null);
          setRuntimeUnavailable(true);
          setRuntimeUpdatedAt(null);
          throw error;
        })
        .finally(() => {
          runtimeRequestRef.current = null;
          const shouldClearRuntimeLoading = runtimeRequestVisibleRef.current;
          runtimeRequestVisibleRef.current = false;
          if (shouldClearRuntimeLoading) setRuntimeLoading(false);
        });

      runtimeRequestRef.current = request;
      return request;
    },
    [enabled],
  );

  useEffect(() => {
    if (enabled) return;
    queueMicrotask(() => {
      setRuntime(null);
      runtimeSnapshotRef.current = null;
      runtimeRequestVisibleRef.current = false;
      setRuntimeUnavailable(false);
      setRuntimeUpdatedAt(null);
      setRuntimeLoading(false);
    });
  }, [enabled]);

  useEffect(() => {
    if (!enabled) return;

    let disposed = false;
    let timer;

    const clearScheduledRefresh = () => {
      if (timer !== undefined) {
        window.clearTimeout(timer);
        timer = undefined;
      }
    };
    const scheduleRefresh = async () => {
      if (document.visibilityState === 'hidden') return;
      try {
        await refresh({ silent: true });
      } catch {
        // The UI clears stale state and renders the unavailable condition.
      }
      if (!disposed) {
        const refreshDelay = getChannelHealthRuntimeRefreshDelay(
          runtimeSnapshotRef.current,
        );
        timer = window.setTimeout(scheduleRefresh, refreshDelay);
      }
    };

    const handleVisibilityChange = () => {
      if (document.visibilityState === 'hidden') {
        clearScheduledRefresh();
        return;
      }
      clearScheduledRefresh();
      void scheduleRefresh();
    };

    document.addEventListener('visibilitychange', handleVisibilityChange);
    void scheduleRefresh();
    return () => {
      disposed = true;
      document.removeEventListener('visibilitychange', handleVisibilityChange);
      clearScheduledRefresh();
    };
  }, [enabled, refresh]);

  return {
    runtime,
    runtimeUnavailable,
    runtimeUpdatedAt,
    runtimeLoading,
    refresh,
  };
};
