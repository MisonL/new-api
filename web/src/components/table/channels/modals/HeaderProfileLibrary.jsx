import React, { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Empty,
  Space,
  Tag,
  Tooltip,
  Typography,
} from '@douyinfe/semi-ui';
import { IconDelete, IconEdit, IconPlus } from '@douyinfe/semi-icons';

const { Text } = Typography;

function getCategoryLabel(t, category) {
  switch (category) {
    case 'browser':
      return t('浏览器');
    case 'ai_coding_cli':
      return t('AI Coding CLI');
    case 'api_sdk':
      return t('API SDK / 调试');
    default:
      return t('自定义');
  }
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
  const groups = useMemo(() => buildGroupItems(profiles), [profiles]);
  const selectedProfileIdSet = useMemo(
    () => new Set(selectedProfileIds),
    [selectedProfileIds],
  );

  const renderCard = (profile) => {
    const selected = selectedProfileIdSet.has(profile.id);
    const previewText = profile.previewText || t('暂无可预览内容');

    return (
      <Tooltip
        key={profile.id}
        position='top'
        content={
          <pre className='mb-0 text-xs leading-5 whitespace-pre-wrap break-all max-w-[420px] max-h-64 overflow-auto'>
            {previewText}
          </pre>
        }
      >
        <div
          className='cursor-pointer rounded-lg px-3 py-2.5 transition-colors duration-150'
          style={{
            border: selected
              ? '1px solid var(--semi-color-primary)'
              : '1px solid var(--semi-color-fill-2)',
            backgroundColor: selected
              ? 'var(--semi-color-primary-light-default)'
              : 'var(--semi-color-bg-1)',
          }}
          onClick={() => onToggleSelect(profile.id)}
        >
          <div className='flex items-start justify-between gap-2'>
            <div className='min-w-0'>
              <div className='flex items-center gap-1.5 flex-wrap'>
                <Text strong size='small'>
                  {profile.name}
                </Text>
                {selected && <Tag size='small' color='blue'>{t('已选中')}</Tag>}
              </div>
              <div className='mt-1 flex items-center gap-1.5 flex-wrap'>
                <Tag size='small'>{getCategoryLabel(t, profile.category)}</Tag>
                {strategyMode === 'fixed' && selected && (
                  <Text type='tertiary' size='small'>
                    {t('固定模式会直接替换为当前选择')}
                  </Text>
                )}
              </div>
            </div>

            {!profile.readonly && (
              <Space spacing={4}>
                <Button
                  size='small'
                  type='tertiary'
                  icon={<IconEdit />}
                  onClick={(event) => {
                    event.stopPropagation();
                    onEdit(profile);
                  }}
                />
                <Button
                  size='small'
                  type='danger'
                  loading={deletingProfileId === profile.id}
                  icon={<IconDelete />}
                  onClick={(event) => {
                    event.stopPropagation();
                    onDelete(profile);
                  }}
                />
              </Space>
            )}
          </div>
        </div>
      </Tooltip>
    );
  };

  return (
    <div className='flex flex-col gap-3'>
      <div className='flex items-center justify-between gap-2'>
        <div>
          <Text strong size='small'>{t('Header Profile 资源库')}</Text>
          <div>
            <Text type='tertiary' size='small'>
              {t('点击条目即可加入或移出当前策略')}
            </Text>
          </div>
        </div>
        <Button size='small' icon={<IconPlus />} onClick={onCreate}>
          {t('新建 Profile')}
        </Button>
      </div>

      <div className='flex flex-col gap-3'>
        <div>
          <div className='mb-1.5'>
            <Text strong size='small'>{t('预置 Profile')}</Text>
          </div>
          {groups.builtin.length === 0 ? (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              title={t('暂无预置 Profile')}
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
            <Text strong size='small'>{t('我的 Profile')}</Text>
            {loading && (
              <Text type='tertiary' size='small'>
                {t('加载中...')}
              </Text>
            )}
          </div>
          {groups.user.length === 0 ? (
            <div
              className='rounded-lg p-3'
              style={{
                backgroundColor: 'var(--semi-color-fill-0)',
                border: '1px solid var(--semi-color-fill-2)',
              }}
            >
              <Text type='tertiary'>{t('还没有自定义 Header Profile')}</Text>
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
