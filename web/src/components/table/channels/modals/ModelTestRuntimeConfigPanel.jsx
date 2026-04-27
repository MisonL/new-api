import React from 'react';
import { Button, Switch, Tag, Typography } from '@douyinfe/semi-ui';
import { IconInfoCircle } from '@douyinfe/semi-icons';
import {
  getModelTestRuntimeSnapshot,
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
    <div className='flex items-center justify-between gap-3 rounded-md border border-[var(--semi-color-border)] bg-[var(--semi-color-bg-0)] px-3 py-2 transition-colors hover:bg-[var(--semi-color-fill-0)]'>
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
      <Switch
        checked={effectiveChecked}
        disabled={itemDisabled}
        onChange={onChange}
        size='small'
        aria-label={label}
      />
    </div>
  );
};

const RuntimeSummaryTag = ({ label, configured, enabled, value, t }) => {
  const text = configured ? value || t('已配置') : t('未配置');
  return (
    <Tag color={configured && enabled ? 'green' : 'grey'} size='small'>
      <span className='inline-flex max-w-[260px] items-center gap-1 truncate align-bottom'>
        <span className='shrink-0'>{label}:</span>
        <span className='truncate' title={text}>
          {text}
        </span>
      </span>
    </Tag>
  );
};

const ModelTestRuntimeConfigPanel = ({
  channel,
  runtimeConfig,
  setRuntimeConfig,
  isBatchTesting,
  globalPassThroughEnabled,
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
  const runtimeSummaryItems = [
    {
      key: 'headers',
      label: t('请求头'),
      configured: runtimeSnapshot.headerConfigured,
      enabled: runtimeEnabled && normalizedRuntimeConfig.headerConfig,
      value: runtimeSnapshot.headerValue,
    },
    {
      key: 'paramOverride',
      label: t('参数覆盖'),
      configured: runtimeSnapshot.paramConfigured,
      enabled: runtimeEnabled && normalizedRuntimeConfig.paramOverride,
      value: runtimeSnapshot.paramValue,
    },
    {
      key: 'proxy',
      label: t('代理'),
      configured: runtimeSnapshot.proxyConfigured,
      enabled: runtimeEnabled && normalizedRuntimeConfig.proxy,
      value: runtimeSnapshot.proxyValue,
    },
    {
      key: 'modelMapping',
      label: t('模型映射'),
      configured: runtimeSnapshot.modelMappingConfigured,
      enabled: runtimeEnabled && normalizedRuntimeConfig.modelMapping,
      value: runtimeSnapshot.modelMappingValue,
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
          <Switch
            checked={normalizedRuntimeConfig.enabled}
            onChange={(checked) => updateRuntimeConfig('enabled', checked)}
            disabled={isBatchTesting}
            size='small'
            aria-label={t('按真实运行配置测试')}
          />
        </div>
        <div className='flex flex-wrap gap-1.5'>
          {runtimeSummaryItems.map((item) => (
            <RuntimeSummaryTag key={item.key} {...item} t={t} />
          ))}
        </div>
        <div className='flex items-center justify-between gap-2'>
          <Typography.Text type='tertiary' size='small'>
            {t(
              '普通测试保持开启即可；需要定位原始连通性或单项配置时再展开诊断。',
            )}
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
