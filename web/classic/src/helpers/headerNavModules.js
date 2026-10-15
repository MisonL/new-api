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

export const DEFAULT_HEADER_NAV_MODULES = {
  home: true,
  console: true,
  pricing: { enabled: true, requireAuth: false },
  rankings: { enabled: true, requireAuth: false },
  docs: true,
  about: true,
};

function toBoolean(value, fallback) {
  if (typeof value === 'boolean') return value;
  if (typeof value === 'number') return value === 1;
  if (typeof value === 'string') {
    const normalized = value.trim().toLowerCase();
    if (normalized === 'true' || normalized === '1') return true;
    if (normalized === 'false' || normalized === '0') return false;
  }
  return fallback;
}

function normalizeAccessModule(value, fallback) {
  if (
    typeof value === 'boolean' ||
    typeof value === 'string' ||
    typeof value === 'number'
  ) {
    return {
      enabled: toBoolean(value, fallback.enabled),
      requireAuth: fallback.requireAuth,
    };
  }
  if (value && typeof value === 'object') {
    return {
      enabled: toBoolean(value.enabled, fallback.enabled),
      requireAuth: toBoolean(value.requireAuth, fallback.requireAuth),
    };
  }
  return { ...fallback };
}

export function parseHeaderNavModules(rawValue) {
  let parsed = rawValue;
  if (typeof rawValue === 'string') {
    if (!rawValue.trim()) return structuredClone(DEFAULT_HEADER_NAV_MODULES);
    try {
      parsed = JSON.parse(rawValue);
    } catch {
      return structuredClone(DEFAULT_HEADER_NAV_MODULES);
    }
  }
  if (!parsed || typeof parsed !== 'object') {
    return structuredClone(DEFAULT_HEADER_NAV_MODULES);
  }

  const result = structuredClone(DEFAULT_HEADER_NAV_MODULES);
  for (const [key, value] of Object.entries(parsed)) {
    if (key === 'pricing' || key === 'rankings') {
      result[key] = normalizeAccessModule(
        value,
        DEFAULT_HEADER_NAV_MODULES[key],
      );
      continue;
    }
    if (key in result) {
      result[key] = toBoolean(value, result[key]);
    }
  }
  return result;
}

export function getHeaderNavModuleAccess(rawValue, moduleName) {
  const modules = parseHeaderNavModules(rawValue);
  const moduleConfig = modules[moduleName];
  if (moduleConfig && typeof moduleConfig === 'object') {
    return {
      enabled: moduleConfig.enabled !== false,
      requireAuth: moduleConfig.requireAuth === true,
    };
  }
  return { enabled: moduleConfig !== false, requireAuth: false };
}
