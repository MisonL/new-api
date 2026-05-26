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

export const RESPONSES_COMPACT_MODE_AUTO = 'auto';
export const RESPONSES_COMPACT_MODE_NATIVE = 'native';
export const RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY = 'synthetic_summary';
export const RESPONSES_COMPACT_MODE_DEFAULT = RESPONSES_COMPACT_MODE_AUTO;

export const RESPONSES_COMPACT_MODE_OPTIONS = [
  {
    label: '自动：优先原生，失败回退模拟摘要',
    value: RESPONSES_COMPACT_MODE_AUTO,
  },
  {
    label: '原生 /v1/responses/compact',
    value: RESPONSES_COMPACT_MODE_NATIVE,
  },
  {
    label: '模拟摘要兼容',
    value: RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY,
  },
];

export function normalizeResponsesCompactMode(mode) {
  if (mode === RESPONSES_COMPACT_MODE_AUTO) {
    return RESPONSES_COMPACT_MODE_AUTO;
  }
  if (mode === RESPONSES_COMPACT_MODE_NATIVE) {
    return RESPONSES_COMPACT_MODE_NATIVE;
  }
  if (mode === RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY) {
    return RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY;
  }
  if (mode === 'convert') {
    return RESPONSES_COMPACT_MODE_SYNTHETIC_SUMMARY;
  }
  if (mode === 'disabled' || mode === 'unsupported') {
    // Legacy disabled/unsupported meant no synthetic conversion; keep the native endpoint path.
    return RESPONSES_COMPACT_MODE_NATIVE;
  }
  return RESPONSES_COMPACT_MODE_DEFAULT;
}

export function buildResponsesCompactSettings(channelType, mode) {
  if (channelType !== 1) {
    return {};
  }
  return {
    responses_compact_mode: normalizeResponsesCompactMode(mode),
  };
}

export function clearResponsesCompactSettings(settings) {
  if (!settings || typeof settings !== 'object') {
    return;
  }
  delete settings.responses_compact_mode;
  delete settings.responses_compact_auto_fallback_date;
  delete settings.responses_compact_auto_fallback_reason;
}

export function resetResponsesCompactAutoFallbackOnModeChange(
  settings,
  mode,
  previousModeOverride,
) {
  if (!settings || typeof settings !== 'object') {
    return false;
  }
  const nextMode = normalizeResponsesCompactMode(mode);
  const previousMode = normalizeResponsesCompactMode(
    previousModeOverride ?? settings?.responses_compact_mode,
  );
  if (nextMode !== previousMode) {
    delete settings.responses_compact_auto_fallback_date;
    delete settings.responses_compact_auto_fallback_reason;
    return true;
  }
  return false;
}
