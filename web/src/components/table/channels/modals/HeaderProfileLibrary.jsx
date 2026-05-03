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

import React, { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Empty, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { IconDelete, IconEdit, IconPlus } from '@douyinfe/semi-icons';

import { getHeaderProfileCategoryLabel } from './headerProfile.helpers.js';

const { Text } = Typography;

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
    profile.id === 'gemini-cli'
  ) {
    return t('固定客户端标识；选择后会自动写入真实请求头透传规则');
  }
  if (profile.id === 'opencode') {
    return t('固定客户端标识；默认不自动补透传，严格复刻上游链路时再按需补 pass_headers');
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
  const groups = useMemo(() => buildGroupItems(profiles), [profiles]);
  const selectedProfileIdSet = useMemo(
    () => new Set(selectedProfileIds),
    [selectedProfileIds],
  );
  const previewProfile = useMemo(() => {
    return (
      profiles.find((profile) => profile.id === previewProfileId) ||
      profiles.find((profile) => selectedProfileIdSet.has(profile.id)) ||
      null
    );
  }, [previewProfileId, profiles, selectedProfileIdSet]);

  const renderCard = (profile) => {
    const selected = selectedProfileIdSet.has(profile.id);
    const previewed = previewProfileId === profile.id;
    const activateProfile = () => {
      setPreviewProfileId(profile.id);
      onToggleSelect(profile.id);
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
                  {t('当前使用')}
                </Tag>
              )}
            </span>
            <Text type='tertiary' size='small' className='block mt-0.5'>
              {getHeaderProfileCategoryLabel(t, profile.category)}
              {strategyMode === 'fixed' && selected
                ? ` - ${t('点击其他模板会替换当前选择')}`
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
    const descriptionText = previewProfile?.description
      ? t(previewProfile.description)
      : t('悬停模板后在这里预览完整请求头');
    const previewText = previewProfile?.previewText || t('暂无可预览内容');

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
            {previewProfile ? previewProfile.name : t('请求头预览')}
          </Text>
          {previewProfile && (
            <Tag size='small'>
              {getHeaderProfileCategoryLabel(t, previewProfile.category)}
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
