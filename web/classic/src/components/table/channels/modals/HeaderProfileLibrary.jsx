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

import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Empty,
  Select,
  Space,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import {
  IconDelete,
  IconEdit,
  IconPlus,
  IconRefresh,
} from '@douyinfe/semi-icons';

import { API } from '../../../../helpers';
import { getHeaderProfileCategoryLabel } from './headerProfile.helpers.js';
import {
  AI_CODING_CLI_DEFAULT_PLATFORM,
  AI_CODING_CLI_PLATFORM_OPTIONS,
  areAiCodingCliProfileSnapshotsEqual,
  buildRefreshedAiCodingCliProfileSnapshot,
  buildNpmCliFallbackVersionOptions,
  buildVersionedAiCodingCliProfile,
  fetchNpmCliVersionOptionsResult,
  getAiCodingCliVersionSource,
  getNpmCliVersionOptionSource,
  NPM_VERSION_AUTH_ERROR_CODE,
  NPM_VERSION_EMPTY_ERROR_CODE,
  NPM_VERSION_FORBIDDEN_CODE,
  NPM_VERSION_LATEST_ALIAS,
  NPM_VERSION_LOAD_ERROR_CODE,
  NPM_VERSION_NOT_RECORDED_CODE,
  NPM_VERSION_RATE_LIMITED_CODE,
  normalizeAiCodingCliPlatform,
  normalizeNpmCliVersionPayloadErrorCode,
} from './headerProfile.constants.js';

const { Text } = Typography;
const EMPTY_VERSION_OPTIONS = [];
const CLI_VERSION_OPTIONS_CACHE_TTL_MS = 10 * 60 * 1000;
const CLI_VERSION_REFRESH_TIMEOUT_MS = 10_000;
const NPM_VERSION_KNOWN_ERROR_CODES = new Set([
  NPM_VERSION_AUTH_ERROR_CODE,
  NPM_VERSION_EMPTY_ERROR_CODE,
  NPM_VERSION_FORBIDDEN_CODE,
  NPM_VERSION_LOAD_ERROR_CODE,
  NPM_VERSION_NOT_RECORDED_CODE,
  NPM_VERSION_RATE_LIMITED_CODE,
]);
const cliVersionOptionsRequestCache = new Map();

function ensureSelectedVersionOption(options, selectedVersion) {
  const normalizedVersion = String(selectedVersion || '').trim();
  if (
    !normalizedVersion ||
    normalizedVersion === NPM_VERSION_LATEST_ALIAS ||
    options.some((option) => option.value === normalizedVersion)
  ) {
    return options;
  }
  return [
    ...options,
    {
      value: normalizedVersion,
      label: normalizedVersion,
      isLatest: false,
      resolvedVersion: normalizedVersion,
      source: 'retained',
    },
  ];
}

function loadCliVersionOptions(packageName, options = {}) {
  const cacheKey = String(packageName || '').trim();
  const cached = cliVersionOptionsRequestCache.get(cacheKey);
  const now = Date.now();
  if (!options.force && !options.refresh && cached && cached.expiresAt > now) {
    return cached.request;
  }
  if (cached) {
    cliVersionOptionsRequestCache.delete(cacheKey);
  }
  const requestImpl = options.refresh
    ? (url, requestOptions) => API.post(url, null, requestOptions)
    : API.get.bind(API);
  const request = fetchNpmCliVersionOptionsResult(cacheKey, requestImpl, {
    refresh: options.refresh === true,
  }).catch((error) => {
    if (cliVersionOptionsRequestCache.get(cacheKey)?.request === request) {
      cliVersionOptionsRequestCache.delete(cacheKey);
    }
    throw error;
  });
  cliVersionOptionsRequestCache.set(cacheKey, {
    expiresAt: now + CLI_VERSION_OPTIONS_CACHE_TTL_MS,
    request,
  });
  return request;
}

function invalidateCliVersionOptions(packageName) {
  cliVersionOptionsRequestCache.delete(String(packageName || '').trim());
}

function getNpmVersionLoadErrorText(t, code) {
  if (code === NPM_VERSION_AUTH_ERROR_CODE) {
    return t('会话已过期，请重新登录后再加载 npm 版本');
  }
  if (code === NPM_VERSION_FORBIDDEN_CODE) {
    return t('需要管理员权限才能加载 npm 版本');
  }
  if (code === NPM_VERSION_RATE_LIMITED_CODE) {
    return t('npm 版本请求过于频繁，请稍后重试');
  }
  if (code === NPM_VERSION_EMPTY_ERROR_CODE) {
    return t('后台暂无 npm 版本记录，已使用内置版本列表');
  }
  if (code === NPM_VERSION_NOT_RECORDED_CODE) {
    return t('后台暂无 npm 版本记录，已使用内置版本列表');
  }
  return t('npm 版本加载失败，已使用内置版本列表');
}

function normalizeKnownNpmVersionErrorCode(code) {
  const normalized = String(code || '').trim();
  return NPM_VERSION_KNOWN_ERROR_CODES.has(normalized)
    ? normalized
    : NPM_VERSION_LOAD_ERROR_CODE;
}

function getNpmVersionSourceText(t, source) {
  switch (source) {
    case 'recorded':
      return t('后台缓存');
    case 'npm':
      return t('npm 刷新');
    case 'retained':
      return t('保留选择');
    case 'fallback':
      return t('内置兜底');
    case 'missing':
      return t('未记录');
    default:
      return t('未知来源');
  }
}

function formatNpmVersionRefreshedAt(value) {
  if (!value) {
    return '';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return '';
  }
  return date.toLocaleString();
}

function getLastErrorScopeText(t, scope) {
  if (scope === 'process') return t('当前进程');
  if (scope === 'recorded') return t('持久化记录');
  return '';
}

function formatNpmVersionCacheAge(value) {
  const numericValue = Number(value);
  if (!Number.isFinite(numericValue) || numericValue < 0) {
    return '';
  }
  if (numericValue < 1000) return `${Math.round(numericValue)} ms`;
  const seconds = Math.round(numericValue / 1000);
  if (seconds < 60) return `${seconds} s`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes} min`;
  return `${Math.round(minutes / 60)} h`;
}

function formatNpmVersionDiagnosticLastError(t, item) {
  const lastError = item?.last_error;
  if (!lastError) {
    return '';
  }
  return [
    lastError.code,
    lastError.source,
    getLastErrorScopeText(t, item.last_error_scope),
    formatNpmVersionRefreshedAt(lastError.updated_at),
    lastError.message,
  ]
    .map((part) => String(part || '').trim())
    .filter(Boolean)
    .join(' / ');
}

function normalizeNpmVersionDiagnostics(payload) {
  return Array.isArray(payload?.packages)
    ? payload.packages.filter(
        (item) => item && typeof item === 'object' && !Array.isArray(item),
      )
    : [];
}

function buildGroupItems(profiles) {
  return profiles.reduce(
    (groups, profile) => {
      if (profile.scope === 'builtin') {
        groups.builtin.push(profile);
      } else {
        groups.user.push(profile);
      }
      return groups;
    },
    { builtin: [], user: [] },
  );
}

function getProfileUsageHint(t, profile) {
  if (
    profile.id === 'codex-cli' ||
    profile.id === 'codex-desktop' ||
    profile.id === 'claude-code' ||
    profile.id === 'gemini-cli' ||
    profile.id === 'qwen-code' ||
    profile.id === 'droid' ||
    profile.id === 'agy'
  ) {
    return t(
      '固定客户端标识；默认不自动补透传，严格复刻上游链路时再按需补 pass_headers',
    );
  }
  if (profile.category === 'browser') {
    return t('适合要求浏览器访问特征的上游');
  }
  if (profile.category === 'ai_coding_cli') {
    return t('适合 AI 编程客户端标识');
  }
  if (profile.category === 'api_sdk') {
    return t('适合 API 调试工具或 SDK 请求');
  }
  return t('适合保存你自己的完整请求头组合');
}

const HeaderProfileLibrary = ({
  profiles = [],
  selectedProfiles = [],
  selectedProfileIds = [],
  strategyMode = 'fixed',
  loading = false,
  deletingProfileId = '',
  onToggleSelect,
  onCreate,
  onEdit,
  onDelete,
}) => {
  const { t } = useTranslation();
  const versionRequestSeqRef = useRef({});
  const diagnosticsRequestSeqRef = useRef(0);
  const mountedRef = useRef(true);
  const [previewProfileId, setPreviewProfileId] = useState('');
  const [cliVersionState, setCliVersionState] = useState({});
  const [diagnosticsOpen, setDiagnosticsOpen] = useState(false);
  const [diagnosticsLoading, setDiagnosticsLoading] = useState(false);
  const [diagnosticsError, setDiagnosticsError] = useState('');
  const [diagnostics, setDiagnostics] = useState([]);
  const multiTemplateMode =
    strategyMode === 'round_robin' || strategyMode === 'random';
  const groups = useMemo(() => buildGroupItems(profiles), [profiles]);
  const selectedProfileIdSet = useMemo(
    () => new Set(selectedProfileIds),
    [selectedProfileIds],
  );
  const selectedVersionEntries = useMemo(() => {
    const entries = [];
    selectedProfiles.forEach((profile) => {
      const versionMeta = profile?.versionMeta || profile?.version_meta;
      const baseProfileId =
        versionMeta?.baseProfileId || versionMeta?.base_profile_id;
      const version = versionMeta?.version;
      const platform = normalizeAiCodingCliPlatform(versionMeta?.platform);
      if (baseProfileId && version) {
        entries.push([
          baseProfileId,
          {
            profileId: profile.id,
            version,
            platform,
          },
        ]);
      }
    });
    return entries;
  }, [selectedProfiles]);
  const selectedVersionByBaseId = useMemo(
    () => new Map(selectedVersionEntries),
    [selectedVersionEntries],
  );
  const selectedVersionDescriptorKey = useMemo(
    () =>
      selectedVersionEntries
        .map(
          ([profileId, versionInfo]) =>
            `${profileId}:${versionInfo?.profileId || ''}:${
              versionInfo?.version || ''
            }:${versionInfo?.platform || AI_CODING_CLI_DEFAULT_PLATFORM}`,
        )
        .join('|'),
    [selectedVersionEntries],
  );
  const versionedProfileDescriptors = useMemo(
    () =>
      profiles
        .filter((profile) => getAiCodingCliVersionSource(profile))
        .map((profile) => ({
          id: profile.id,
          versionSource: getAiCodingCliVersionSource(profile),
        })),
    [profiles],
  );
  const versionedProfileDescriptorKey = useMemo(
    () =>
      versionedProfileDescriptors
        .map(
          (profile) =>
            `${profile.id}:${profile.versionSource?.packageName || ''}:${
              profile.versionSource?.fallbackVersion || ''
            }`,
        )
        .join('|'),
    [versionedProfileDescriptors],
  );
  const startVersionRequest = (profileId) => {
    const nextSeq = (versionRequestSeqRef.current[profileId] || 0) + 1;
    versionRequestSeqRef.current[profileId] = nextSeq;
    return nextSeq;
  };
  const isLatestVersionRequest = (profileId, requestSeq) =>
    versionRequestSeqRef.current[profileId] === requestSeq;

  const loadNpmVersionDiagnostics = () => {
    const requestSeq = diagnosticsRequestSeqRef.current + 1;
    diagnosticsRequestSeqRef.current = requestSeq;
    setDiagnosticsOpen(true);
    setDiagnosticsLoading(true);
    setDiagnosticsError('');
    API.get('/api/channel/npm_version_options/diagnostics', {
      timeout: CLI_VERSION_REFRESH_TIMEOUT_MS,
      skipErrorHandler: true,
      disableDuplicate: true,
    })
      .then((response) => {
        const payload = response?.data || {};
        const status = Number(response?.status);
        const errorCode = normalizeNpmCliVersionPayloadErrorCode(
          payload,
          status,
        );
        if (payload.success !== true) {
          const error = new Error(
            payload.message || 'failed to load npm version diagnostics',
          );
          error.code = errorCode;
          throw error;
        }
        if (
          !mountedRef.current ||
          diagnosticsRequestSeqRef.current !== requestSeq
        ) {
          return;
        }
        setDiagnostics(normalizeNpmVersionDiagnostics(payload.data));
      })
      .catch((error) => {
        if (
          !mountedRef.current ||
          diagnosticsRequestSeqRef.current !== requestSeq
        ) {
          return;
        }
        const responseErrorCode = normalizeNpmCliVersionPayloadErrorCode(
          error?.response?.data || {},
          Number(error?.response?.status ?? error?.status),
        );
        setDiagnosticsError(
          responseErrorCode === NPM_VERSION_LOAD_ERROR_CODE && error?.code
            ? normalizeKnownNpmVersionErrorCode(error.code)
            : responseErrorCode,
        );
      })
      .finally(() => {
        if (
          !mountedRef.current ||
          diagnosticsRequestSeqRef.current !== requestSeq
        ) {
          return;
        }
        setDiagnosticsLoading(false);
      });
  };

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);
  const previewProfile = useMemo(() => {
    return (
      profiles.find((profile) => profile.id === previewProfileId) ||
      profiles.find((profile) => selectedProfileIdSet.has(profile.id)) ||
      null
    );
  }, [previewProfileId, profiles, selectedProfileIdSet]);

  useEffect(() => {
    if (selectedVersionByBaseId.size === 0) {
      return;
    }
    setCliVersionState((current) => {
      let changed = false;
      const nextState = { ...current };
      selectedVersionByBaseId.forEach((versionInfo, profileId) => {
        const selectedVersion = versionInfo?.version || '';
        const selectedPlatform = normalizeAiCodingCliPlatform(
          versionInfo?.platform,
        );
        if (
          selectedVersion &&
          (nextState[profileId]?.selectedVersion !== selectedVersion ||
            nextState[profileId]?.selectedPlatform !== selectedPlatform)
        ) {
          const profile = profiles.find((item) => item.id === profileId);
          const versionSource = profile
            ? getAiCodingCliVersionSource(profile)
            : null;
          const currentState = nextState[profileId] || {};
          const currentOptions =
            versionSource?.packageName &&
            currentState.packageName === versionSource.packageName
              ? currentState.options || []
              : [];
          const baseOptions =
            currentOptions.length > 0
              ? currentOptions
              : versionSource
                ? buildNpmCliFallbackVersionOptions(
                    versionSource.fallbackVersion,
                  )
                : currentOptions;
          nextState[profileId] = {
            ...(nextState[profileId] || {}),
            options: ensureSelectedVersionOption(baseOptions, selectedVersion),
            selectedVersion,
            selectedPlatform,
          };
          changed = true;
        }
      });
      return changed ? nextState : current;
    });
  }, [profiles, selectedVersionByBaseId, selectedVersionDescriptorKey]);

  useEffect(() => {
    if (versionedProfileDescriptors.length === 0) {
      return undefined;
    }
    let active = true;

    versionedProfileDescriptors.forEach((profile) => {
      const versionSource = profile.versionSource;
      if (!versionSource?.packageName) {
        return;
      }

      setCliVersionState((current) => {
        const currentState = current[profile.id] || {};
        const fallbackVersion = versionSource.fallbackVersion;
        const selectedVersionInfo = selectedVersionByBaseId.get(profile.id);
        const selectedVersion =
          selectedVersionInfo?.version ||
          currentState.selectedVersion ||
          NPM_VERSION_LATEST_ALIAS;
        const canReuseCurrentOptions =
          currentState.packageName === versionSource.packageName &&
          Array.isArray(currentState.options) &&
          currentState.options.length > 1;
        const fallbackOptions = canReuseCurrentOptions
          ? currentState.options
          : ensureSelectedVersionOption(
              buildNpmCliFallbackVersionOptions(fallbackVersion),
              selectedVersion,
            );
        return {
          ...current,
          [profile.id]: {
            ...currentState,
            loading: true,
            error: '',
            options: fallbackOptions,
            packageName: versionSource.packageName,
            selectedVersion,
            selectedPlatform:
              selectedVersionInfo?.platform ||
              currentState.selectedPlatform ||
              AI_CODING_CLI_DEFAULT_PLATFORM,
            source: canReuseCurrentOptions ? currentState.source : 'fallback',
            refreshedAt: canReuseCurrentOptions
              ? currentState.refreshedAt
              : undefined,
            latestVersion: canReuseCurrentOptions
              ? currentState.latestVersion
              : fallbackVersion,
          },
        };
      });

      const requestSeq = startVersionRequest(profile.id);
      loadCliVersionOptions(versionSource.packageName)
        .then((result) => {
          if (!active || !isLatestVersionRequest(profile.id, requestSeq)) {
            return;
          }
          setCliVersionState((current) => {
            const currentState = current[profile.id] || {};
            const options = result.options;
            const selectedVersionInfo = selectedVersionByBaseId.get(profile.id);
            const selectedVersion =
              selectedVersionInfo?.version ||
              currentState.selectedVersion ||
              options[0]?.value ||
              NPM_VERSION_LATEST_ALIAS;
            const fallbackOptions =
              options.length > 0
                ? ensureSelectedVersionOption(options, selectedVersion)
                : ensureSelectedVersionOption(
                    buildNpmCliFallbackVersionOptions(
                      versionSource.fallbackVersion,
                    ),
                    selectedVersion,
                  );
            return {
              ...current,
              [profile.id]: {
                ...currentState,
                loading: false,
                error: '',
                options: fallbackOptions,
                packageName: versionSource.packageName,
                selectedVersion,
                selectedPlatform:
                  selectedVersionInfo?.platform ||
                  currentState.selectedPlatform ||
                  AI_CODING_CLI_DEFAULT_PLATFORM,
                source: result.source,
                refreshedAt: result.refreshedAt,
                latestVersion: result.latestVersion,
              },
            };
          });
        })
        .catch((error) => {
          if (!active || !isLatestVersionRequest(profile.id, requestSeq)) {
            return;
          }
          setCliVersionState((current) => {
            const currentState = current[profile.id] || {};
            const fallbackVersion = versionSource.fallbackVersion;
            const selectedVersionInfo = selectedVersionByBaseId.get(profile.id);
            const selectedVersion =
              selectedVersionInfo?.version ||
              currentState.selectedVersion ||
              NPM_VERSION_LATEST_ALIAS;
            const canReuseCurrentOptions =
              currentState.packageName === versionSource.packageName &&
              Array.isArray(currentState.options) &&
              currentState.options.length > 1;
            const fallbackOptions = canReuseCurrentOptions
              ? currentState.options
              : buildNpmCliFallbackVersionOptions(fallbackVersion);
            return {
              ...current,
              [profile.id]: {
                ...currentState,
                loading: false,
                error: error?.code || NPM_VERSION_LOAD_ERROR_CODE,
                options: ensureSelectedVersionOption(
                  fallbackOptions,
                  selectedVersion,
                ),
                packageName: versionSource.packageName,
                selectedVersion,
                selectedPlatform:
                  selectedVersionInfo?.platform ||
                  currentState.selectedPlatform ||
                  AI_CODING_CLI_DEFAULT_PLATFORM,
                source: canReuseCurrentOptions
                  ? currentState.source
                  : 'fallback',
                refreshedAt: canReuseCurrentOptions
                  ? currentState.refreshedAt
                  : undefined,
                latestVersion: canReuseCurrentOptions
                  ? currentState.latestVersion
                  : fallbackVersion,
              },
            };
          });
        });
    });

    return () => {
      active = false;
    };
  }, [
    selectedVersionByBaseId,
    selectedVersionDescriptorKey,
    versionedProfileDescriptorKey,
  ]);

  useEffect(() => {
    if (selectedVersionByBaseId.size === 0) {
      return;
    }
    const replacements = [];
    for (const [profileId, versionInfo] of selectedVersionByBaseId.entries()) {
      const profile = profiles.find((item) => item.id === profileId);
      const versionSource = profile
        ? getAiCodingCliVersionSource(profile)
        : null;
      const versionState = cliVersionState[profileId];
      if (
        !profile ||
        !versionSource?.packageName ||
        versionState?.packageName !== versionSource.packageName ||
        versionState.loading ||
        versionState.error ||
        !Array.isArray(versionState.options) ||
        versionState.options.length === 0
      ) {
        continue;
      }
      const selectedProfile =
        selectedProfiles.find((item) => item.id === versionInfo.profileId) ||
        selectedProfiles.find((item) => {
          const meta = item?.versionMeta || item?.version_meta;
          return (meta?.baseProfileId || meta?.base_profile_id) === profileId;
        });
      const refreshedProfile = buildRefreshedAiCodingCliProfileSnapshot({
        profile,
        selectedProfile,
        selectedVersion: versionInfo.version || versionState.selectedVersion,
        selectedPlatform: versionInfo.platform || versionState.selectedPlatform,
        options: versionState.options,
      });
      if (
        !refreshedProfile ||
        areAiCodingCliProfileSnapshotsEqual(selectedProfile, refreshedProfile)
      ) {
        continue;
      }
      replacements.push(refreshedProfile);
    }
    if (replacements.length === 1) {
      onToggleSelect(replacements[0].id, replacements[0], { replace: true });
      return;
    }
    if (replacements.length > 1) {
      onToggleSelect('', null, { replaceProfiles: replacements });
    }
  }, [
    cliVersionState,
    onToggleSelect,
    profiles,
    selectedProfiles,
    selectedVersionByBaseId,
  ]);

  const getSelectedVersionForProfile = (profile) => {
    const versionSource = getAiCodingCliVersionSource(profile);
    const versionState = cliVersionState[profile.id] || {};
    return (
      versionState.selectedVersion ||
      selectedVersionByBaseId.get(profile.id)?.version ||
      versionState.options?.[0]?.value ||
      (versionSource ? NPM_VERSION_LATEST_ALIAS : '') ||
      ''
    );
  };

  const getResolvedVersionForProfile = (profile, version, optionsOverride) => {
    const versionSource = getAiCodingCliVersionSource(profile);
    const versionState = cliVersionState[profile.id] || {};
    const versionOptions = Array.isArray(optionsOverride)
      ? optionsOverride
      : versionState.options || [];
    const selectedOption = versionOptions.find(
      (option) => option.value === version,
    );
    return (
      selectedOption?.resolvedVersion ||
      (version === NPM_VERSION_LATEST_ALIAS
        ? versionSource?.fallbackVersion || ''
        : version)
    );
  };

  const getSelectedPlatformForProfile = (profile) => {
    const versionState = cliVersionState[profile.id] || {};
    return normalizeAiCodingCliPlatform(
      versionState.selectedPlatform ||
        selectedVersionByBaseId.get(profile.id)?.platform ||
        AI_CODING_CLI_DEFAULT_PLATFORM,
    );
  };

  const getVersionMetaSourceForProfile = (
    profile,
    version,
    optionsOverride,
  ) => {
    const versionState = cliVersionState[profile.id];
    const versionOptions = Array.isArray(optionsOverride)
      ? optionsOverride
      : versionState?.options || [];
    const selectedOption = versionOptions.find(
      (option) => option.value === version,
    );
    const selectedOptionSource = selectedOption
      ? getNpmCliVersionOptionSource(selectedOption, 'npm')
      : '';
    if (selectedOptionSource === 'retained') {
      return selectedOptionSource;
    }
    return versionState && !versionState.loading && !versionState.error
      ? selectedOptionSource || 'fallback'
      : 'fallback';
  };

  const reloadProfileVersions = (profile) => {
    const versionSource = getAiCodingCliVersionSource(profile);
    if (!versionSource?.packageName) {
      return;
    }
    if (!mountedRef.current) {
      return;
    }
    invalidateCliVersionOptions(versionSource.packageName);
    const requestSeq = startVersionRequest(profile.id);
    const fallbackOptions = buildNpmCliFallbackVersionOptions(
      versionSource.fallbackVersion,
    );
    setCliVersionState((current) => {
      const currentState = current[profile.id] || {};
      const selectedVersion =
        currentState.selectedVersion ||
        selectedVersionByBaseId.get(profile.id)?.version ||
        NPM_VERSION_LATEST_ALIAS;
      const canReuseCurrentOptions =
        currentState.packageName === versionSource.packageName &&
        Array.isArray(currentState.options) &&
        currentState.options.length > 1;
      return {
        ...current,
        [profile.id]: {
          ...currentState,
          loading: true,
          error: '',
          options: ensureSelectedVersionOption(
            canReuseCurrentOptions ? currentState.options : fallbackOptions,
            selectedVersion,
          ),
          packageName: versionSource.packageName,
          selectedVersion,
          selectedPlatform:
            currentState.selectedPlatform ||
            selectedVersionByBaseId.get(profile.id)?.platform ||
            AI_CODING_CLI_DEFAULT_PLATFORM,
          source: canReuseCurrentOptions ? currentState.source : 'fallback',
          refreshedAt: canReuseCurrentOptions
            ? currentState.refreshedAt
            : undefined,
          latestVersion: canReuseCurrentOptions
            ? currentState.latestVersion
            : versionSource.fallbackVersion,
        },
      };
    });
    loadCliVersionOptions(versionSource.packageName, {
      force: true,
      refresh: true,
    })
      .then((result) => {
        if (
          !mountedRef.current ||
          !isLatestVersionRequest(profile.id, requestSeq)
        ) {
          return;
        }
        setCliVersionState((current) => {
          const currentState = current[profile.id] || {};
          const options = result.options;
          const selectedVersion =
            currentState.selectedVersion ||
            selectedVersionByBaseId.get(profile.id)?.version ||
            options[0]?.value ||
            NPM_VERSION_LATEST_ALIAS;
          return {
            ...current,
            [profile.id]: {
              ...currentState,
              loading: false,
              error: '',
              options: ensureSelectedVersionOption(options, selectedVersion),
              packageName: versionSource.packageName,
              selectedVersion,
              selectedPlatform:
                currentState.selectedPlatform ||
                selectedVersionByBaseId.get(profile.id)?.platform ||
                AI_CODING_CLI_DEFAULT_PLATFORM,
              source: result.source,
              refreshedAt: result.refreshedAt,
              latestVersion: result.latestVersion,
            },
          };
        });
      })
      .catch((error) => {
        if (
          !mountedRef.current ||
          !isLatestVersionRequest(profile.id, requestSeq)
        ) {
          return;
        }
        setCliVersionState((current) => {
          const currentState = current[profile.id] || {};
          const selectedVersion =
            currentState.selectedVersion ||
            selectedVersionByBaseId.get(profile.id)?.version ||
            NPM_VERSION_LATEST_ALIAS;
          const canReuseCurrentOptions =
            currentState.packageName === versionSource.packageName &&
            Array.isArray(currentState.options) &&
            currentState.options.length > 1;
          return {
            ...current,
            [profile.id]: {
              ...currentState,
              loading: false,
              error: error?.code || NPM_VERSION_LOAD_ERROR_CODE,
              options: ensureSelectedVersionOption(
                canReuseCurrentOptions ? currentState.options : fallbackOptions,
                selectedVersion,
              ),
              packageName: versionSource.packageName,
              selectedVersion,
              selectedPlatform:
                currentState.selectedPlatform ||
                selectedVersionByBaseId.get(profile.id)?.platform ||
                AI_CODING_CLI_DEFAULT_PLATFORM,
              source: canReuseCurrentOptions ? currentState.source : 'fallback',
              refreshedAt: canReuseCurrentOptions
                ? currentState.refreshedAt
                : undefined,
              latestVersion: canReuseCurrentOptions
                ? currentState.latestVersion
                : versionSource.fallbackVersion,
            },
          };
        });
      });
  };

  const buildProfileForSelection = (profile) => {
    const versionSource = getAiCodingCliVersionSource(profile);
    if (!versionSource) {
      return profile;
    }
    const selectedVersion = getSelectedVersionForProfile(profile);
    return buildVersionedAiCodingCliProfile(
      profile,
      selectedVersion,
      getResolvedVersionForProfile(profile, selectedVersion),
      getSelectedPlatformForProfile(profile),
      getVersionMetaSourceForProfile(profile, selectedVersion),
    );
  };

  const renderCard = (profile) => {
    const selectedVersionInfo = selectedVersionByBaseId.get(profile.id);
    const selected =
      selectedProfileIdSet.has(profile.id) || !!selectedVersionInfo;
    const previewed = previewProfileId === profile.id;
    const versionSource = getAiCodingCliVersionSource(profile);
    const versionState = cliVersionState[profile.id] || {};
    const versionOptions = versionState.options || EMPTY_VERSION_OPTIONS;
    const selectedVersion = getSelectedVersionForProfile(profile);
    const selectedOption = versionOptions.find(
      (option) => option.value === selectedVersion,
    );
    const sourceForDisplay =
      selectedOption?.source || versionState.source || 'fallback';
    const refreshedAtText = formatNpmVersionRefreshedAt(
      versionState.refreshedAt,
    );
    const selectedPlatform = getSelectedPlatformForProfile(profile);
    const activateProfile = () => {
      setPreviewProfileId(profile.id);
      const nextProfile = buildProfileForSelection(profile);
      onToggleSelect(nextProfile.id || profile.id, nextProfile);
    };
    const updateProfileVersion = (version) => {
      const currentVersionOptions = versionOptions;
      setCliVersionState((current) => ({
        ...current,
        [profile.id]: {
          ...(current[profile.id] || {}),
          selectedVersion: version,
        },
      }));
      if (!selected || selectedVersionInfo?.version === version) {
        return;
      }
      const nextProfile = buildVersionedAiCodingCliProfile(
        profile,
        version,
        getResolvedVersionForProfile(profile, version, currentVersionOptions),
        getSelectedPlatformForProfile(profile),
        getVersionMetaSourceForProfile(profile, version, currentVersionOptions),
      );
      onToggleSelect(nextProfile.id || profile.id, nextProfile, {
        replace: true,
      });
    };
    const updateProfilePlatform = (platform) => {
      const nextPlatform = normalizeAiCodingCliPlatform(platform);
      setCliVersionState((current) => ({
        ...current,
        [profile.id]: {
          ...(current[profile.id] || {}),
          selectedPlatform: nextPlatform,
        },
      }));
      if (!selected || selectedVersionInfo?.platform === nextPlatform) {
        return;
      }
      const nextProfile = buildVersionedAiCodingCliProfile(
        profile,
        selectedVersion,
        getResolvedVersionForProfile(profile, selectedVersion, versionOptions),
        nextPlatform,
        getVersionMetaSourceForProfile(
          profile,
          selectedVersion,
          versionOptions,
        ),
      );
      onToggleSelect(nextProfile.id || profile.id, nextProfile, {
        replace: true,
      });
    };

    return (
      <div
        key={profile.id}
        className='group flex items-stretch rounded-md transition-colors duration-150'
        style={{
          border: selected
            ? '1px solid var(--semi-color-primary)'
            : previewed
              ? '1px solid var(--semi-color-primary-light-active)'
              : '1px solid var(--semi-color-fill-2)',
          backgroundColor: selected
            ? 'var(--semi-color-primary-light-default)'
            : previewed
              ? 'var(--semi-color-fill-0)'
              : 'var(--semi-color-bg-1)',
          boxShadow: selected
            ? '0 0 0 1px var(--semi-color-primary-light-active)'
            : 'none',
        }}
        onMouseEnter={() => setPreviewProfileId(profile.id)}
      >
        <button
          type='button'
          className='min-w-0 flex-1 cursor-pointer rounded-md bg-transparent px-2.5 py-1.5 text-left transition-opacity duration-150 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 group-hover:opacity-90'
          style={{
            border: 0,
            color: 'inherit',
            WebkitTapHighlightColor: 'transparent',
          }}
          onFocus={() => setPreviewProfileId(profile.id)}
          onClick={activateProfile}
          aria-pressed={selected}
          aria-label={t('选择请求头模板 {{name}}', { name: profile.name })}
        >
          <span className='block min-w-0'>
            <span className='flex items-center gap-1.5 flex-wrap'>
              <Text
                strong
                size='small'
                ellipsis={{ showTooltip: true }}
                style={{ maxWidth: 220 }}
              >
                {profile.name}
              </Text>
              {profile.passthroughRequired && (
                <Tag size='small' color='orange'>
                  {t('需透传')}
                </Tag>
              )}
              {selected && (
                <Tag size='small' color='blue'>
                  {selectedVersionInfo?.version
                    ? t('当前使用 {{version}} / {{platform}}', {
                        version: selectedVersionInfo.version,
                        platform:
                          AI_CODING_CLI_PLATFORM_OPTIONS.find(
                            (option) =>
                              option.value === selectedVersionInfo.platform,
                          )?.label || AI_CODING_CLI_PLATFORM_OPTIONS[0].label,
                      })
                    : t('当前使用')}
                </Tag>
              )}
            </span>
            <Text type='tertiary' size='small' className='block mt-0.5'>
              {getHeaderProfileCategoryLabel(t, profile.category)}
              {selected ? ` - ${t('再次点击可取消选择')}` : ''}
            </Text>
            <Text
              type='tertiary'
              size='small'
              className='block mt-0.5'
              style={{
                lineHeight: '18px',
                wordBreak: 'break-word',
              }}
            >
              {getProfileUsageHint(t, profile)}
            </Text>
          </span>
        </button>
        <div className='flex items-start justify-between gap-2'>
          {versionSource && (
            <div className='w-40 py-1.5 pr-1.5'>
              <div className='flex items-center gap-1.5'>
                <Select
                  size='small'
                  value={selectedVersion}
                  loading={versionState.loading === true}
                  optionList={versionOptions}
                  placeholder={t('选择版本')}
                  onMouseDown={(event) => event.stopPropagation()}
                  onClick={(event) => event.stopPropagation()}
                  onChange={updateProfileVersion}
                  style={{ width: '100%' }}
                />
                <Button
                  size='small'
                  type='tertiary'
                  theme='borderless'
                  icon={<IconRefresh spin={versionState.loading === true} />}
                  disabled={versionState.loading === true}
                  aria-label={t('重新加载 npm 版本')}
                  onMouseDown={(event) => event.stopPropagation()}
                  onClick={(event) => {
                    event.stopPropagation();
                    reloadProfileVersions(profile);
                  }}
                />
              </div>
              <Select
                size='small'
                value={selectedPlatform}
                optionList={AI_CODING_CLI_PLATFORM_OPTIONS.map((option) => ({
                  value: option.value,
                  label: option.label,
                }))}
                placeholder={t('选择系统')}
                onMouseDown={(event) => event.stopPropagation()}
                onClick={(event) => event.stopPropagation()}
                onChange={updateProfilePlatform}
                style={{ width: '100%', marginTop: 6 }}
              />
              {versionState.error && (
                <Text type='warning' size='small' className='block mt-1'>
                  {getNpmVersionLoadErrorText(t, versionState.error)}
                </Text>
              )}
              <Text type='tertiary' size='small' className='block mt-1'>
                {refreshedAtText
                  ? t('{{source}}，刷新于 {{time}}', {
                      source: getNpmVersionSourceText(t, sourceForDisplay),
                      time: refreshedAtText,
                    })
                  : getNpmVersionSourceText(t, sourceForDisplay)}
              </Text>
            </div>
          )}
          {!profile.readonly && (
            <Space spacing={4} className='py-1.5 pr-1.5'>
              <Button
                size='small'
                type='tertiary'
                icon={<IconEdit />}
                aria-label={t('编辑请求头模板')}
                onClick={(event) => {
                  event.stopPropagation();
                  onEdit(profile);
                }}
              />
              <Button
                size='small'
                type='danger'
                theme='borderless'
                loading={deletingProfileId === profile.id}
                icon={<IconDelete />}
                aria-label={t('删除请求头模板')}
                onClick={(event) => {
                  event.stopPropagation();
                  onDelete(profile);
                }}
              />
            </Space>
          )}
        </div>
      </div>
    );
  };

  const renderPreview = () => {
    const displayPreviewProfile =
      previewProfile && getAiCodingCliVersionSource(previewProfile)
        ? buildProfileForSelection(previewProfile)
        : previewProfile;
    const descriptionText = displayPreviewProfile?.description
      ? t(displayPreviewProfile.description)
      : t('悬停模板后在这里预览完整请求头');
    const previewText =
      displayPreviewProfile?.headers &&
      Object.keys(displayPreviewProfile.headers).length > 0
        ? Object.entries(displayPreviewProfile.headers)
            .map(([key, value]) => `${key}: ${value}`)
            .join('\n')
        : t('暂无可预览内容');

    return (
      <div
        className='flex flex-col rounded-lg px-3 py-2'
        style={{
          backgroundColor: 'var(--semi-color-fill-0)',
          border: '1px solid var(--semi-color-fill-2)',
          boxSizing: 'border-box',
          height: 176,
        }}
      >
        <div className='mb-1 flex items-center justify-between gap-2'>
          <Text strong size='small'>
            {displayPreviewProfile
              ? displayPreviewProfile.name
              : t('请求头预览')}
          </Text>
          {displayPreviewProfile && (
            <Tag size='small'>
              {getHeaderProfileCategoryLabel(t, displayPreviewProfile.category)}
            </Tag>
          )}
        </div>
        <Text
          type='tertiary'
          size='small'
          className='mb-1 block'
          style={{
            display: '-webkit-box',
            lineHeight: '18px',
            minHeight: 36,
            overflow: 'hidden',
            WebkitBoxOrient: 'vertical',
            WebkitLineClamp: 2,
          }}
        >
          {descriptionText}
        </Text>
        <pre className='m-0 min-h-0 flex-1 overflow-auto whitespace-pre-wrap break-all text-xs leading-5'>
          {previewText}
        </pre>
      </div>
    );
  };

  return (
    <div className='flex flex-col gap-3 min-w-0'>
      <div className='flex items-center justify-between gap-2'>
        <div>
          <Text strong size='small'>
            {multiTemplateMode
              ? t('点击模板可追加或取消选择')
              : t('点击模板即可替换当前选择')}
          </Text>
          <div>
            <Text type='tertiary' size='small'>
              {multiTemplateMode
                ? t('轮询和随机模式支持多选，已选模板再次点击会取消')
                : t('悬停可预览完整请求头；固定模式会直接替换当前模板')}
            </Text>
          </div>
        </div>
        <Button size='small' icon={<IconPlus />} onClick={onCreate}>
          {t('新建自定义模板')}
        </Button>
      </div>

      <div
        className='rounded-lg px-3 py-2'
        style={{
          backgroundColor: 'var(--semi-color-fill-0)',
          border: '1px solid var(--semi-color-fill-2)',
        }}
      >
        <Text strong size='small'>
          {t('不知道选哪个？')}
        </Text>
        <Space wrap spacing={6} className='mt-1'>
          <Tag size='small'>{t('网页上游选 Chrome macOS')}</Tag>
          <Tag size='small'>{t('AI CLI 选对应工具')}</Tag>
          <Tag size='small'>{t('调试工具选 Postman')}</Tag>
        </Space>
        <Text type='tertiary' size='small' className='block mt-1'>
          {t(
            '保持不选就是不修改请求。只有上游识别客户端身份时才需要选择模板。',
          )}
        </Text>
      </div>

      <div
        className='rounded-lg px-3 py-2'
        style={{
          backgroundColor: 'var(--semi-color-fill-0)',
          border: '1px solid var(--semi-color-fill-2)',
        }}
      >
        <div className='flex items-center justify-between gap-2'>
          <div className='min-w-0'>
            <Text strong size='small'>
              {t('npm 版本诊断')}
            </Text>
            <Text type='tertiary' size='small' className='block'>
              {t('只读查看后台缓存状态，失败原因包含当前进程与持久化记录')}
            </Text>
          </div>
          <Button
            size='small'
            type='tertiary'
            loading={diagnosticsLoading}
            onClick={loadNpmVersionDiagnostics}
          >
            {t('诊断')}
          </Button>
        </div>
        {diagnosticsOpen && (
          <div className='mt-2 max-h-32 overflow-auto'>
            {diagnosticsError ? (
              <Text type='warning' size='small'>
                {getNpmVersionLoadErrorText(t, diagnosticsError)}
              </Text>
            ) : diagnostics.length === 0 ? (
              <Text type='tertiary' size='small'>
                {diagnosticsLoading ? t('加载中') : t('暂无诊断信息')}
              </Text>
            ) : (
              <div className='flex flex-col gap-1'>
                {diagnostics.map((item) => {
                  const refreshedAt = formatNpmVersionRefreshedAt(
                    item.refreshed_at,
                  );
                  const cacheAge = formatNpmVersionCacheAge(item.cache_age_ms);
                  const lastErrorText = formatNpmVersionDiagnosticLastError(
                    t,
                    item,
                  );
                  return (
                    <div
                      key={item.package}
                      className='grid gap-1'
                      style={{
                        gridTemplateColumns:
                          'minmax(0, 1.2fr) minmax(0, 1fr) minmax(0, 1fr)',
                      }}
                    >
                      <Text size='small' ellipsis={{ showTooltip: true }}>
                        {item.package}
                      </Text>
                      <Text
                        type='tertiary'
                        size='small'
                        ellipsis={{ showTooltip: true }}
                      >
                        {getNpmVersionSourceText(t, item.source)} /{' '}
                        {item.latest_version || '-'} / {item.option_count || 0}
                        {cacheAge ? ` / ${cacheAge}` : ''}
                      </Text>
                      <Text
                        type={item.last_error ? 'warning' : 'tertiary'}
                        size='small'
                        title={lastErrorText || refreshedAt || undefined}
                        ellipsis={{ showTooltip: true }}
                      >
                        {item.last_error
                          ? lastErrorText || '-'
                          : refreshedAt || '-'}
                      </Text>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        )}
      </div>

      <div className='flex flex-col gap-2.5 min-w-0'>
        {renderPreview()}

        <div>
          <div className='mb-1.5'>
            <Text strong size='small'>
              {t('预置模板')}
            </Text>
          </div>
          {groups.builtin.length === 0 ? (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              title={t('暂无预置模板')}
              description={t('当前没有可用的预置项')}
            />
          ) : (
            <div className='grid grid-cols-1 md:grid-cols-2 gap-2'>
              {groups.builtin.map(renderCard)}
            </div>
          )}
        </div>

        <div>
          <div className='mb-1.5 flex items-center justify-between gap-2'>
            <Text strong size='small'>
              {t('我的模板')}
            </Text>
            {loading && (
              <Text type='tertiary' size='small'>
                {t('加载中')}
              </Text>
            )}
          </div>
          {groups.user.length === 0 ? (
            <div
              className='rounded-lg px-3 py-2'
              style={{
                backgroundColor: 'var(--semi-color-fill-0)',
                border: '1px solid var(--semi-color-fill-2)',
              }}
            >
              <Text type='tertiary'>{t('还没有自定义请求头模板')}</Text>
            </div>
          ) : (
            <div className='grid grid-cols-1 md:grid-cols-2 gap-2'>
              {groups.user.map(renderCard)}
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

export default HeaderProfileLibrary;
