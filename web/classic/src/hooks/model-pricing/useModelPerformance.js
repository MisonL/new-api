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

import { useEffect, useMemo, useState } from 'react';
import { API } from '../../helpers';

export function formatPerformanceLatency(value) {
  if (!Number.isFinite(Number(value)) || Number(value) <= 0) return '-';
  const milliseconds = Number(value);
  if (milliseconds < 1000) return `${Math.round(milliseconds)} ms`;
  return `${(milliseconds / 1000).toFixed(1)} s`;
}

export function formatPerformancePercent(value) {
  if (!Number.isFinite(Number(value))) return '-';
  return `${Number(value).toFixed(1)}%`;
}

export function formatPerformanceThroughput(value) {
  if (!Number.isFinite(Number(value)) || Number(value) <= 0) return '-';
  const numeric = Number(value);
  return `${numeric >= 100 ? Math.round(numeric) : numeric.toFixed(1)} tps`;
}

export function useModelPerformanceSummary() {
  const [summary, setSummary] = useState({});
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    let active = true;
    setLoading(true);
    API.get('/api/perf-metrics/summary?hours=24')
      .then((response) => {
        if (!active || !response.data?.success) return;
        const next = {};
        for (const item of response.data.data?.models || []) {
          if (item?.model_name && item.request_count > 0) {
            next[item.model_name] = item;
          }
        }
        setSummary(next);
      })
      .catch(() => {
        if (active) setSummary({});
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, []);

  return useMemo(() => ({ summary, loading }), [summary, loading]);
}

export function useModelPerformance(modelName, visible) {
  const [state, setState] = useState({
    loading: false,
    error: null,
    data: null,
  });

  useEffect(() => {
    if (!visible || !modelName) return undefined;
    let active = true;
    setState({ loading: true, error: null, data: null });
    const encodedModel = encodeURIComponent(modelName);
    API.get(`/api/perf-metrics?model=${encodedModel}&hours=24`)
      .then((response) => {
        if (!active) return;
        if (!response.data?.success) {
          setState({
            loading: false,
            error: response.data?.message || '加载失败',
            data: null,
          });
          return;
        }
        setState({
          loading: false,
          error: null,
          data: response.data.data || null,
        });
      })
      .catch((error) => {
        if (active) {
          setState({
            loading: false,
            error: error?.message || '加载失败',
            data: null,
          });
        }
      });
    return () => {
      active = false;
    };
  }, [modelName, visible]);

  return state;
}
