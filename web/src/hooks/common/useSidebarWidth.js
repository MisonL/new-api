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

import { useCallback, useEffect, useLayoutEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { normalizeLanguage } from '../../i18n/language';

const KEY = 'sidebar_width';
export const DEFAULT_SIDEBAR_WIDTH = 180;
export const ENGLISH_SIDEBAR_WIDTH = 224;
export const MIN_SIDEBAR_WIDTH = 160;
export const MAX_SIDEBAR_WIDTH = 320;

export const getDefaultSidebarWidth = (language) => {
  return normalizeLanguage(language) === 'en'
    ? ENGLISH_SIDEBAR_WIDTH
    : DEFAULT_SIDEBAR_WIDTH;
};

export const clampSidebarWidth = (
  value,
  defaultWidth = DEFAULT_SIDEBAR_WIDTH,
) => {
  if (value === null || value === undefined || value === '') {
    return defaultWidth;
  }
  const numericValue = Number(value);
  if (!Number.isFinite(numericValue)) {
    return defaultWidth;
  }
  return Math.min(
    MAX_SIDEBAR_WIDTH,
    Math.max(MIN_SIDEBAR_WIDTH, Math.round(numericValue)),
  );
};

export const useSidebarWidth = () => {
  const { i18n } = useTranslation();
  const defaultWidth = getDefaultSidebarWidth(
    i18n.resolvedLanguage || i18n.language,
  );
  const [width, setWidthState] = useState(() => {
    if (typeof window === 'undefined') {
      return defaultWidth;
    }
    return clampSidebarWidth(localStorage.getItem(KEY), defaultWidth);
  });

  useEffect(() => {
    if (typeof window === 'undefined') {
      return;
    }
    if (localStorage.getItem(KEY) === null) {
      setWidthState(defaultWidth);
    }
  }, [defaultWidth]);

  useLayoutEffect(() => {
    document.documentElement.style.setProperty('--sidebar-width', `${width}px`);
  }, [width]);

  const setWidth = useCallback((value) => {
    const nextWidth = clampSidebarWidth(value, defaultWidth);
    setWidthState(nextWidth);
    localStorage.setItem(KEY, String(nextWidth));
  }, [defaultWidth]);

  const resetWidth = useCallback(() => {
    setWidthState(defaultWidth);
    localStorage.removeItem(KEY);
  }, [defaultWidth]);

  return [width, setWidth, resetWidth, defaultWidth];
};
