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

import React from 'react';
import {
  Button,
  Input,
  InputNumber,
  Switch,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconInfoCircle } from '@douyinfe/semi-icons';
import {
  getModelTestRuntimeSnapshot,
  MODEL_TEST_EDIT_TARGETS,
  normalizeModelTestRuntimeConfig,
} from '../modelTestRuntimeConfig';

const RuntimeSwitchItem = ({
  label,
  description,
  checked,
  disabled,
  configured,
  value,
  onChange,
  onEdit,
  t,
}) => {
  const effectiveChecked = checked && configured;
  const itemDisabled = disabled || !configured;
  const statusText = !configured
    ? t('未配置')
    : effectiveChecked
      ? t('会参与')
      : t('已关闭');

  return (
    <div
      className='flex cursor-pointer items-center justify-between gap-3 rounded-md border border-[var(--semi-color-border)] bg-[var(--semi-color-bg-0)] px-3 py-2 transition-colors hover:bg-[var(--semi-color-fill-0)]'
      role='button'
      tabIndex={0}
      title={t('点击打开渠道编辑')}
      onClick={onEdit}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          onEdit?.();
        }
      }}
    >
      <div className='min-w-0'>
        <div className='flex items-center gap-2'>
          <Typography.Text strong size='small'>
            {label}
          </Typography.Text>
          <Tag color={effectiveChecked ? 'green' : 'grey'} size='small'>
            {statusText}
          </Tag>
        </div>
        <Typography.Text
          type='tertiary'
          size='small'
          className='block truncate'
          title={value || description}
        >
          {value || description}
        </Typography.Text>
      </div>
      <div onClick={(event) => event.stopPropagation()}>
        <Switch
          checked={effectiveChecked}
          disabled={itemDisabled}
          onChange={onChange}
          size='small'
          aria-label={label}
        />
      </div>
    </div>
  );
};

const RuntimeSummaryTag = ({
  label,
  configured,
  enabled,
  value,
  onEdit,
  t,
}) => {
  const text = configured ? value || t('已配置') : t('未配置');
  return (
    <button
      type='button'
      className='m-0 inline-flex cursor-pointer border-0 bg-transparent p-0 text-left transition-opacity hover:opacity-80'
      title={`${label}: ${text}。${t('点击打开渠道编辑')}`}
      onClick={onEdit}
    >
      <Tag color={configured && enabled ? 'green' : 'grey'} size='small'>
        <span className='inline-flex max-w-[260px] items-center gap-1 truncate align-bottom'>
          <span className='shrink-0'>{label}:</span>
          <span className='truncate'>{text}</span>
        </span>
      </Tag>
    </button>
  );
};

const ModelTestRuntimeConfigPanel = ({
  channel,
  runtimeConfig,
  setRuntimeConfig,
  selectedEndpointType,
  isBatchTesting,
  globalPassThroughEnabled,
  onEditChannel,
  t,
}) => {
  const normalizedRuntimeConfig =
    normalizeModelTestRuntimeConfig(runtimeConfig);
  const runtimeSnapshot = getModelTestRuntimeSnapshot(channel, t);
  const [showAdvanced, setShowAdvanced] = React.useState(false);
  const advancedPanelId = React.useId();
  const updateRuntimeConfig = (key, value) => {
    setRuntimeConfig((prev) => ({
      ...normalizeModelTestRuntimeConfig(prev),
      [key]: value,
    }));
  };
  const runtimeEnabled = normalizedRuntimeConfig.enabled;
  const pathHint =
    selectedEndpointType === 'image-generation'
      ? t(
          'GPT 生图测试会走 /v1/images/generations，并复用当前测试内容作为 prompt。',
        )
      : selectedEndpointType === 'openai'
        ? t('当前测试路径为 /v1/chat/completions，适合验证上游普通 Chat 模型。')
        : t(
            '默认使用 /v1/responses；如上游只支持 Chat，可在顶部切换测试路径。',
          );
  const runtimeSummaryItems = [
    {
      key: 'headers',
      label: t('请求头'),
      configured: runtimeSnapshot.headerConfigured,
      enabled: runtimeEnabled && normalizedRuntimeConfig.headerConfig,
      value: runtimeSnapshot.headerValue,
      onEdit: () => onEditChannel?.(MODEL_TEST_EDIT_TARGETS.HEADER_CONFIG),
    },
    {
      key: 'paramOverride',
      label: t('参数覆盖'),
      configured: runtimeSnapshot.paramConfigured,
      enabled: runtimeEnabled && normalizedRuntimeConfig.paramOverride,
      value: runtimeSnapshot.paramValue,
      onEdit: () => onEditChannel?.(MODEL_TEST_EDIT_TARGETS.PARAM_OVERRIDE),
    },
    {
      key: 'proxy',
      label: t('代理'),
      configured: runtimeSnapshot.proxyConfigured,
      enabled: runtimeEnabled && normalizedRuntimeConfig.proxy,
      value: runtimeSnapshot.proxyValue,
      onEdit: () => onEditChannel?.(MODEL_TEST_EDIT_TARGETS.PROXY),
    },
    {
      key: 'modelMapping',
      label: t('模型映射'),
      configured: runtimeSnapshot.modelMappingConfigured,
      enabled: runtimeEnabled && normalizedRuntimeConfig.modelMapping,
      value: runtimeSnapshot.modelMappingValue,
      onEdit: () => onEditChannel?.(MODEL_TEST_EDIT_TARGETS.MODEL_MAPPING),
    },
  ];

  return (
    <div className='mb-2 rounded-lg border border-[var(--semi-color-border)] bg-[var(--semi-color-fill-0)] px-3 py-2'>
      <div className='flex flex-col gap-2'>
        <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
          <div className='min-w-0'>
            <div className='flex items-center gap-2'>
              <Typography.Text strong>
                {t('按真实运行配置测试')}
              </Typography.Text>
              <Tag
                color={normalizedRuntimeConfig.enabled ? 'green' : 'grey'}
                size='small'
              >
                {normalizedRuntimeConfig.enabled ? t('已开启') : t('已关闭')}
              </Tag>
              {globalPassThroughEnabled ? (
                <Tag color='orange' size='small'>
                  {t('全局透传开启')}
                </Tag>
              ) : null}
            </div>
            <Typography.Text type='tertiary' size='small'>
              {t(
                '默认按真实请求链路执行。关闭后只用于排查原始连通性，不代表用户实际调用效果。',
              )}
            </Typography.Text>
          </div>
          <div className='flex items-center gap-2'>
            <Button
              theme='borderless'
              type='tertiary'
              size='small'
              onClick={() => onEditChannel?.(MODEL_TEST_EDIT_TARGETS.CORE)}
            >
              {t('编辑渠道配置')}
            </Button>
            <Switch
              checked={normalizedRuntimeConfig.enabled}
              onChange={(checked) => updateRuntimeConfig('enabled', checked)}
              disabled={isBatchTesting}
              size='small'
              aria-label={t('按真实运行配置测试')}
            />
          </div>
        </div>
        <div className='flex gap-1.5 overflow-x-auto whitespace-nowrap pb-1'>
          {runtimeSummaryItems.map((item) => (
            <RuntimeSummaryTag key={item.key} {...item} t={t} />
          ))}
        </div>
        <div className='grid grid-cols-1 gap-2 md:grid-cols-[minmax(0,1fr)_220px]'>
          <div className='min-w-0'>
            <Typography.Text strong size='small' className='mb-1 block'>
              {t('测试内容')}
            </Typography.Text>
            <Input
              value={normalizedRuntimeConfig.testPrompt}
              onChange={(value) => updateRuntimeConfig('testPrompt', value)}
              disabled={isBatchTesting}
              size='small'
              showClear
              placeholder={t('输入模型测试内容')}
              name='components-table-channels-modals-modeltestruntimeconfigpanel-input-1'
            />
          </div>
          <div className='min-w-0'>
            <Typography.Text strong size='small' className='mb-1 block'>
              {t('最大输出 Tokens')}
            </Typography.Text>
            <InputNumber
              value={normalizedRuntimeConfig.maxTokens}
              onChange={(value) => updateRuntimeConfig('maxTokens', value)}
              disabled={isBatchTesting}
              min={1}
              max={8192}
              size='small'
              style={{ width: '100%' }}
              name='components-table-channels-modals-modeltestruntimeconfigpanel-inputnumber-1'
            />
          </div>
        </div>
        <div className='flex items-center justify-between gap-2'>
          <Typography.Text type='tertiary' size='small'>
            {pathHint}
          </Typography.Text>
          <Button
            theme='borderless'
            type='tertiary'
            size='small'
            onClick={() => setShowAdvanced((prev) => !prev)}
            aria-expanded={showAdvanced}
            aria-controls={advancedPanelId}
          >
            {showAdvanced ? t('收起诊断') : t('高级诊断')}
          </Button>
        </div>
        {showAdvanced ? (
          <div id={advancedPanelId} className='flex flex-col gap-2'>
            <div className='grid grid-cols-1 gap-2 md:grid-cols-2'>
              <RuntimeSwitchItem
                label={t('请求头配置')}
                description={t('Header Profile、header_override、UA 策略')}
                checked={
                  normalizedRuntimeConfig.enabled &&
                  normalizedRuntimeConfig.headerConfig
                }
                disabled={!normalizedRuntimeConfig.enabled || isBatchTesting}
                configured={runtimeSnapshot.headerConfigured}
                value={runtimeSnapshot.headerValue}
                onChange={(checked) =>
                  updateRuntimeConfig('headerConfig', checked)
                }
                onEdit={() =>
                  onEditChannel?.(MODEL_TEST_EDIT_TARGETS.HEADER_CONFIG)
                }
                t={t}
              />
              <RuntimeSwitchItem
                label={t('参数覆盖')}
                description={t('请求体改写和 pass_headers')}
                checked={
                  normalizedRuntimeConfig.enabled &&
                  normalizedRuntimeConfig.paramOverride
                }
                disabled={!normalizedRuntimeConfig.enabled || isBatchTesting}
                configured={runtimeSnapshot.paramConfigured}
                value={runtimeSnapshot.paramValue}
                onChange={(checked) =>
                  updateRuntimeConfig('paramOverride', checked)
                }
                onEdit={() =>
                  onEditChannel?.(MODEL_TEST_EDIT_TARGETS.PARAM_OVERRIDE)
                }
                t={t}
              />
              <RuntimeSwitchItem
                label={t('代理')}
                description={t('渠道代理设置')}
                checked={
                  normalizedRuntimeConfig.enabled &&
                  normalizedRuntimeConfig.proxy
                }
                disabled={!normalizedRuntimeConfig.enabled || isBatchTesting}
                configured={runtimeSnapshot.proxyConfigured}
                value={runtimeSnapshot.proxyValue}
                onChange={(checked) => updateRuntimeConfig('proxy', checked)}
                onEdit={() => onEditChannel?.(MODEL_TEST_EDIT_TARGETS.PROXY)}
                t={t}
              />
              <RuntimeSwitchItem
                label={t('模型映射')}
                description={t('渠道模型重定向配置')}
                checked={
                  normalizedRuntimeConfig.enabled &&
                  normalizedRuntimeConfig.modelMapping
                }
                disabled={!normalizedRuntimeConfig.enabled || isBatchTesting}
                configured={runtimeSnapshot.modelMappingConfigured}
                value={runtimeSnapshot.modelMappingValue}
                onChange={(checked) =>
                  updateRuntimeConfig('modelMapping', checked)
                }
                onEdit={() =>
                  onEditChannel?.(MODEL_TEST_EDIT_TARGETS.MODEL_MAPPING)
                }
                t={t}
              />
            </div>
            <div className='flex items-start gap-2'>
              <IconInfoCircle
                size='small'
                aria-hidden={true}
                className='mt-[2px] text-[var(--semi-color-text-2)]'
              />
              <Typography.Text type='tertiary' size='small'>
                {t(
                  '如果上游站点仍看到 Go 默认客户端，请确认请求头配置和参数覆盖开关都处于开启状态。',
                )}
              </Typography.Text>
            </div>
          </div>
        ) : null}
      </div>
    </div>
  );
};

export default ModelTestRuntimeConfigPanel;
