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

import { useCallback, useEffect, useState } from 'react';

const KEY = 'sidebar_width';
export const DEFAULT_SIDEBAR_WIDTH = 180;
export const MIN_SIDEBAR_WIDTH = 160;
export const MAX_SIDEBAR_WIDTH = 320;

export const clampSidebarWidth = (value) => {
  const numericValue = Number(value);
  if (!Number.isFinite(numericValue)) {
    return DEFAULT_SIDEBAR_WIDTH;
  }
  return Math.min(
    MAX_SIDEBAR_WIDTH,
    Math.max(MIN_SIDEBAR_WIDTH, Math.round(numericValue)),
  );
};

export const useSidebarWidth = () => {
  const [width, setWidthState] = useState(() => {
    if (typeof window === 'undefined') {
      return DEFAULT_SIDEBAR_WIDTH;
    }
    return clampSidebarWidth(localStorage.getItem(KEY));
  });

  useEffect(() => {
    document.documentElement.style.setProperty('--sidebar-width', `${width}px`);
  }, [width]);

  const setWidth = useCallback((value) => {
    const nextWidth = clampSidebarWidth(value);
    setWidthState(nextWidth);
    localStorage.setItem(KEY, String(nextWidth));
  }, []);

  const resetWidth = useCallback(() => {
    setWidth(DEFAULT_SIDEBAR_WIDTH);
  }, [setWidth]);

  return [width, setWidth, resetWidth];
};
