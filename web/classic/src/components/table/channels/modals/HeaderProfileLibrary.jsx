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

import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Empty,
  Select,
  Space,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconDelete, IconEdit, IconPlus } from '@douyinfe/semi-icons';

import { API } from '../../../../helpers';
import { getHeaderProfileCategoryLabel } from './headerProfile.helpers.js';
import {
  buildVersionedAiCodingCliProfile,
  fetchNpmCliVersionOptions,
  getAiCodingCliVersionSource,
} from './headerProfile.constants.js';

const { Text } = Typography;
const EMPTY_VERSION_OPTIONS = [];
const CLI_VERSION_OPTIONS_CACHE_TTL_MS = 10 * 60 * 1000;
const NPM_VERSION_LOAD_ERROR_CODE = 'npm_version_load_failed';
const cliVersionOptionsRequestCache = new Map();

function loadCliVersionOptions(packageName) {
  const cached = cliVersionOptionsRequestCache.get(packageName);
  const now = Date.now();
  if (cached && cached.expiresAt > now) {
    return cached.request;
  }
  if (cached) {
    cliVersionOptionsRequestCache.delete(packageName);
  }
  const request = fetchNpmCliVersionOptions(
    packageName,
    API.get.bind(API),
  ).catch((error) => {
    cliVersionOptionsRequestCache.delete(packageName);
    throw error;
  });
  cliVersionOptionsRequestCache.set(packageName, {
    expiresAt: now + CLI_VERSION_OPTIONS_CACHE_TTL_MS,
    request,
  });
  return request;
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
    profile.id === 'claude-code' ||
    profile.id === 'gemini-cli' ||
    profile.id === 'qwen-code' ||
    profile.id === 'droid'
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
  const [previewProfileId, setPreviewProfileId] = useState('');
  const [cliVersionState, setCliVersionState] = useState({});
  const groups = useMemo(() => buildGroupItems(profiles), [profiles]);
  const selectedProfileIdSet = useMemo(
    () => new Set(selectedProfileIds),
    [selectedProfileIds],
  );
  const selectedVersionByBaseId = useMemo(() => {
    const versionMap = new Map();
    selectedProfiles.forEach((profile) => {
      const versionMeta = profile?.versionMeta || profile?.version_meta;
      const baseProfileId =
        versionMeta?.baseProfileId || versionMeta?.base_profile_id;
      const version = versionMeta?.version;
      if (baseProfileId && version) {
        versionMap.set(baseProfileId, {
          profileId: profile.id,
          version,
        });
      }
    });
    return versionMap;
  }, [selectedProfiles]);
  const versionedProfiles = useMemo(
    () => profiles.filter((profile) => getAiCodingCliVersionSource(profile)),
    [profiles],
  );
  const versionedProfileDescriptors = useMemo(
    () =>
      versionedProfiles.map((profile) => ({
        id: profile.id,
        versionSource: getAiCodingCliVersionSource(profile),
      })),
    [versionedProfiles],
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
        if (
          selectedVersion &&
          nextState[profileId]?.selectedVersion !== selectedVersion
        ) {
          nextState[profileId] = {
            ...(nextState[profileId] || {}),
            selectedVersion,
          };
          changed = true;
        }
      });
      return changed ? nextState : current;
    });
  }, [selectedVersionByBaseId]);

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
        const fallbackVersion =
          currentState.selectedVersion || versionSource.fallbackVersion;
        const fallbackOptions =
          Array.isArray(currentState.options) && currentState.options.length > 0
            ? currentState.options
            : [
                {
                  value: fallbackVersion,
                  label: fallbackVersion,
                  isLatest: true,
                },
              ];
        return {
          ...current,
          [profile.id]: {
            ...currentState,
            loading: true,
            error: '',
            options: fallbackOptions,
            packageName: versionSource.packageName,
            selectedVersion: fallbackVersion,
          },
        };
      });

      loadCliVersionOptions(versionSource.packageName)
        .then((options) => {
          if (!active) {
            return;
          }
          const fallbackOptions =
            options.length > 0
              ? options
              : [
                  {
                    value: versionSource.fallbackVersion,
                    label: versionSource.fallbackVersion,
                    isLatest: true,
                  },
                ];
          setCliVersionState((current) => {
            const currentState = current[profile.id] || {};
            const selectedVersion =
              currentState.selectedVersion ||
              fallbackOptions[0]?.value ||
              versionSource.fallbackVersion;
            return {
              ...current,
              [profile.id]: {
                ...currentState,
                loading: false,
                error: '',
                options: fallbackOptions,
                packageName: versionSource.packageName,
                selectedVersion,
              },
            };
          });
        })
        .catch(() => {
          if (!active) {
            return;
          }
          setCliVersionState((current) => {
            const currentState = current[profile.id] || {};
            const fallbackVersion =
              currentState.selectedVersion || versionSource.fallbackVersion;
            return {
              ...current,
              [profile.id]: {
                ...currentState,
                loading: false,
                error: NPM_VERSION_LOAD_ERROR_CODE,
                options: [
                  {
                    value: fallbackVersion,
                    label: fallbackVersion,
                    isLatest: true,
                  },
                ],
                packageName: versionSource.packageName,
                selectedVersion: fallbackVersion,
              },
            };
          });
        });
    });

    return () => {
      active = false;
    };
  }, [versionedProfileDescriptorKey]);

  const getSelectedVersionForProfile = (profile) => {
    const versionSource = getAiCodingCliVersionSource(profile);
    const versionState = cliVersionState[profile.id] || {};
    return (
      versionState.selectedVersion ||
      selectedVersionByBaseId.get(profile.id)?.version ||
      versionState.options?.[0]?.value ||
      versionSource?.fallbackVersion ||
      ''
    );
  };

  const buildProfileForSelection = (profile) => {
    const versionSource = getAiCodingCliVersionSource(profile);
    if (!versionSource) {
      return profile;
    }
    return buildVersionedAiCodingCliProfile(
      profile,
      getSelectedVersionForProfile(profile),
      cliVersionState[profile.id]?.error ? 'fallback' : 'npm',
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
    const activateProfile = () => {
      setPreviewProfileId(profile.id);
      const nextProfile = buildProfileForSelection(profile);
      onToggleSelect(nextProfile.id || profile.id, nextProfile);
    };
    const updateProfileVersion = (version) => {
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
        versionState.error ? 'fallback' : 'npm',
      );
      onToggleSelect(nextProfile.id || profile.id, nextProfile);
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
                    ? t('当前使用 {{version}}', {
                        version: selectedVersionInfo.version,
                      })
                    : t('当前使用')}
                </Tag>
              )}
            </span>
            <Text type='tertiary' size='small' className='block mt-0.5'>
              {getHeaderProfileCategoryLabel(t, profile.category)}
              {strategyMode === 'fixed' && selected
                ? ` - ${t('再次点击可取消选择')}`
                : ''}
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
            <div className='w-36 py-1.5 pr-1.5'>
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
              {versionState.error && (
                <Text type='warning' size='small' className='block mt-1'>
                  {t('npm 版本加载失败，已使用内置版本')}
                </Text>
              )}
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
            {t('点击模板即可使用')}
          </Text>
          <div>
            <Text type='tertiary' size='small'>
              {t('悬停可预览完整请求头；固定模式会直接替换当前模板')}
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
