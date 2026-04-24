import React, { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Modal,
  Select,
  Switch,
  Tag,
  Tooltip,
  Typography,
} from '@douyinfe/semi-ui';
import { IconDelete, IconMenu } from '@douyinfe/semi-icons';

import HeaderProfileLibrary from './HeaderProfileLibrary.jsx';

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

const HeaderProfileStrategySection = ({
  loading = false,
  strategy,
  selectedItems = [],
  profiles = [],
  deletingProfileId = '',
  showLegacyBanner = false,
  passthroughWarning = '',
  onEnabledChange,
  onModeChange,
  onToggleSelect,
  onRemoveSelected,
  onReorderSelected,
  onCreateProfile,
  onEditProfile,
  onDeleteProfile,
  onImportLegacy,
}) => {
  const { t } = useTranslation();
  const [libraryVisible, setLibraryVisible] = useState(false);
  const [draggedProfileId, setDraggedProfileId] = useState('');
  const [dragOverProfileId, setDragOverProfileId] = useState('');
  const [dragOverPosition, setDragOverPosition] = useState('before');

  const selectedCount = selectedItems.length;
  const modeOptions = useMemo(
    () => [
      { label: t('固定'), value: 'fixed' },
      { label: t('轮询'), value: 'round_robin' },
      { label: t('随机'), value: 'random' },
    ],
    [t],
  );
  const selectionHint = !strategy.enabled
    ? t('关闭后不会应用任何 Header Profile')
    : strategy.mode === 'fixed'
      ? t('固定模式只保留一个 Profile，重新点击会直接替换')
      : strategy.mode === 'round_robin'
        ? t('轮询模式支持多选并按顺序依次使用')
        : t('随机模式支持多选并随机挑选使用');
  const selectionError =
    strategy.enabled && selectedCount === 0
      ? t('已启用 Header Profile，但还没有选择任何 Profile')
      : '';
  const selectedSummaryText =
    selectedCount === 0
      ? t('未选择任何 Profile')
      : strategy.mode === 'round_robin'
        ? t('已选择 {{count}} 个，按顺序轮询', { count: selectedCount })
        : strategy.mode === 'random'
          ? t('已选择 {{count}} 个，随机取用', { count: selectedCount })
          : t('固定使用 {{name}}', {
              name: selectedItems[0]?.name || t('当前选择'),
            });

  const resetDragState = useCallback(() => {
    setDraggedProfileId('');
    setDragOverProfileId('');
    setDragOverPosition('before');
  }, []);

  const handleDragStart = useCallback((event, profileId) => {
    setDraggedProfileId(profileId);
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData('text/plain', profileId);
  }, []);

  const handleDragOver = useCallback(
    (event, profileId) => {
      event.preventDefault();
      if (!draggedProfileId || draggedProfileId === profileId) {
        return;
      }
      const rect = event.currentTarget.getBoundingClientRect();
      const position =
        event.clientY - rect.top > rect.height / 2 ? 'after' : 'before';
      setDragOverProfileId(profileId);
      setDragOverPosition(position);
      event.dataTransfer.dropEffect = 'move';
    },
    [draggedProfileId],
  );

  const handleDrop = useCallback(
    (event, profileId) => {
      event.preventDefault();
      const sourceId =
        draggedProfileId || event.dataTransfer.getData('text/plain');
      const position =
        dragOverProfileId === profileId ? dragOverPosition : 'before';
      onReorderSelected(sourceId, profileId, position);
      resetDragState();
    },
    [
      dragOverPosition,
      dragOverProfileId,
      draggedProfileId,
      onReorderSelected,
      resetDragState,
    ],
  );

  return (
    <div className='flex flex-col gap-3'>
      <div className='flex items-start justify-between gap-3 flex-wrap'>
        <div>
          <Text strong size='small'>{t('Header Profile')}</Text>
          <div>
            <Text type='tertiary' size='small'>
              {t('优先管理完整请求头模板；常规渠道只需要在这里完成选择即可')}
            </Text>
          </div>
        </div>
        <div className='flex items-center gap-2 flex-wrap'>
          <div className='flex items-center gap-1.5'>
            <Text size='small'>{t('启用')}</Text>
            <Switch checked={strategy.enabled} onChange={onEnabledChange} />
          </div>
          <Select
            size='small'
            style={{ minWidth: 132 }}
            value={strategy.mode}
            disabled={!strategy.enabled}
            optionList={modeOptions}
            onChange={(value) => onModeChange(value || 'fixed')}
          />
        </div>
      </div>

      <Text type='tertiary' size='small'>
        {selectionHint}
      </Text>

      {passthroughWarning && (
        <div
          className='rounded-lg px-3 py-2'
          style={{
            backgroundColor: 'var(--semi-color-warning-light-default)',
            border: '1px solid var(--semi-color-warning-light-active)',
          }}
        >
          <div className='flex items-center gap-2 flex-wrap min-w-0'>
            <Tag color='orange' size='small'>
              {t('需透传')}
            </Tag>
            <Text type='tertiary' size='small'>
              {passthroughWarning}
            </Text>
          </div>
        </div>
      )}

      {showLegacyBanner && (
        <div
          className='rounded-lg px-3 py-2'
          style={{
            backgroundColor: 'var(--semi-color-warning-light-default)',
            border: '1px solid var(--semi-color-warning-light-active)',
          }}
        >
          <div className='flex items-center justify-between gap-2 flex-wrap'>
            <div className='flex items-center gap-2 flex-wrap min-w-0'>
              <Tag color='orange' size='small'>
                {t('旧覆盖待导入')}
              </Tag>
              <Text type='tertiary' size='small'>
                {t('当前仍保留旧的 header_override，建议导入为 Header Profile 后统一管理')}
              </Text>
            </div>
            <Button size='small' type='warning' theme='solid' onClick={onImportLegacy}>
              {t('导入')}
            </Button>
          </div>
        </div>
      )}

      <div
        className='rounded-lg p-2.5'
        style={{
          backgroundColor: 'var(--semi-color-fill-0)',
          border: '1px solid var(--semi-color-fill-2)',
        }}
      >
        <div className='flex items-start justify-between gap-3 flex-wrap'>
          <div className='min-w-0 flex-1'>
            <div className='flex items-center gap-2 flex-wrap'>
              <Text strong size='small'>{t('已选 Profile')}</Text>
              <Tag size='small' color={strategy.enabled ? 'blue' : 'grey'}>
                {strategy.enabled ? t('已启用') : t('未启用')}
              </Tag>
              <Tag size='small'>
                {modeOptions.find((item) => item.value === strategy.mode)?.label}
              </Tag>
            </div>
            <div className='mt-1'>
              <Text type='tertiary' size='small'>
                {selectedSummaryText}
              </Text>
            </div>
          </div>
          <Button
            size='small'
            type='tertiary'
            onClick={() => setLibraryVisible(true)}
          >
            {selectedCount === 0 ? t('选择 Profile') : t('管理 Profile')}
          </Button>
        </div>
        {selectionError && (
          <Text type='danger' size='small' className='mt-2 block'>
            {selectionError}
          </Text>
        )}
        {selectedCount === 0 ? (
          <div className='mt-2'>
            <Text type='tertiary' size='small'>
              {t('主表单不再直接展开资源库；点击右侧按钮进入二级窗口选择')}
            </Text>
          </div>
        ) : (
          <div className='mt-2 flex flex-col gap-1.5'>
            {selectedItems.map((profile, index) => {
              const showOrder = strategy.mode === 'round_robin';
              const isDragging = draggedProfileId === profile.id;
              const isDragTarget = dragOverProfileId === profile.id;

              return (
                <Tooltip
                  key={profile.id}
                  position='topLeft'
                  content={
                    <pre className='mb-0 text-xs leading-5 whitespace-pre-wrap break-all max-w-[420px] max-h-64 overflow-auto'>
                      {profile.previewText || t('暂无可预览内容')}
                    </pre>
                  }
                >
                  <div
                    className='flex items-center justify-between gap-2 rounded-lg px-2.5 py-2'
                    style={{
                      backgroundColor: 'var(--semi-color-bg-1)',
                      border: isDragTarget
                        ? '1px solid var(--semi-color-primary)'
                        : '1px solid var(--semi-color-fill-2)',
                      opacity: isDragging ? 0.6 : 1,
                      borderTopWidth:
                        isDragTarget && dragOverPosition === 'before' ? 2 : 1,
                      borderBottomWidth:
                        isDragTarget && dragOverPosition === 'after' ? 2 : 1,
                    }}
                    draggable={showOrder && selectedCount > 1}
                    onDragStart={(event) => handleDragStart(event, profile.id)}
                    onDragOver={(event) => handleDragOver(event, profile.id)}
                    onDrop={(event) => handleDrop(event, profile.id)}
                    onDragEnd={resetDragState}
                  >
                    <div className='flex items-center gap-2 min-w-0'>
                      {showOrder && <IconMenu style={{ color: 'var(--semi-color-text-2)' }} />}
                      <div className='min-w-0'>
                        <div className='flex items-center gap-1.5 flex-wrap'>
                          <Text strong size='small'>{profile.name}</Text>
                          <Tag size='small'>{getCategoryLabel(t, profile.category)}</Tag>
                          {profile.missing && (
                            <Tag color='red' size='small'>
                              {t('已不存在')}
                            </Tag>
                          )}
                        </div>
                        <div>
                          <Text type='tertiary' size='small'>
                            {showOrder
                              ? t('顺序 {{index}}', { index: index + 1 })
                              : strategy.mode === 'random'
                                ? t('随机候选')
                                : t('固定选择')}
                          </Text>
                        </div>
                      </div>
                    </div>
                    <Button
                      size='small'
                      type='tertiary'
                      icon={<IconDelete />}
                      onClick={() => onRemoveSelected(profile.id)}
                    />
                  </div>
                </Tooltip>
              );
            })}
          </div>
        )}
      </div>

      <Modal
        title={t('管理 Header Profile')}
        visible={libraryVisible}
        width={920}
        onCancel={() => setLibraryVisible(false)}
        footer={
          <Button onClick={() => setLibraryVisible(false)}>
            {t('完成')}
          </Button>
        }
      >
        <HeaderProfileLibrary
          profiles={profiles}
          selectedProfileIds={strategy.selectedProfileIds}
          strategyMode={strategy.mode}
          loading={loading}
          deletingProfileId={deletingProfileId}
          onToggleSelect={onToggleSelect}
          onCreate={onCreateProfile}
          onEdit={onEditProfile}
          onDelete={onDeleteProfile}
        />
      </Modal>
    </div>
  );
};

export default HeaderProfileStrategySection;
