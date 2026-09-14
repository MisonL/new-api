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

import React, { useCallback, useEffect, useRef, useState } from 'react';
import {
  Banner,
  Button,
  Col,
  Form,
  Row,
  Spin,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import {
  API,
  compareObjects,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';
import {
  getChannelHealthRuntimeRefreshDelay,
  getChannelHealthRuntimeStateBehavior,
  getChannelHealthRuntimeStateDisplay,
  summarizeChannelHealthRuntime,
} from '../../../helpers/channelHealthRuntime';
import ChannelHealthRuntimeEvents from '../../../components/table/channels/ChannelHealthRuntimeEvents';

const { Text } = Typography;

const KEY_ENABLED = 'general_setting.channel_health_enabled';
const KEY_COOLDOWN = 'general_setting.channel_health_cooldown_seconds';
const KEY_PROBE_LEASE = 'general_setting.channel_health_probe_lease_seconds';

const DEFAULT_INPUTS = {
  [KEY_ENABLED]: false,
  [KEY_COOLDOWN]: 30,
  [KEY_PROBE_LEASE]: 15,
};

const RUNTIME_REQUEST_TIMEOUT_MS = 8000;
const runtimeRequestConfig = {
  timeout: RUNTIME_REQUEST_TIMEOUT_MS,
  skipBusinessError: true,
  skipErrorHandler: true,
};

function normalizePositiveInteger(value, fallback) {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback;
}

function formatRemainingSeconds(seconds) {
  const normalized = Number(seconds);
  return Number.isFinite(normalized) && normalized > 0 ? `${normalized}s` : '-';
}

export default function SettingsChannelHealth(props) {
  const { t } = useTranslation();
  const refForm = useRef();
  const [inputs, setInputs] = useState(() => ({ ...DEFAULT_INPUTS }));
  const [inputsRow, setInputsRow] = useState(() => ({ ...DEFAULT_INPUTS }));
  const [loading, setLoading] = useState(false);
  const [runtimeLoading, setRuntimeLoading] = useState(false);
  const [runtime, setRuntime] = useState(null);
  const [runtimeUnavailable, setRuntimeUnavailable] = useState(false);
  const [runtimeUpdatedAt, setRuntimeUpdatedAt] = useState(null);
  const runtimeRequestRef = useRef(null);
  const runtimeRequestShouldNotifyRef = useRef(false);
  const runtimeSnapshotRef = useRef(null);

  const fetchRuntime = useCallback(
    ({ silent = false } = {}) => {
      if (!silent) {
        runtimeRequestShouldNotifyRef.current = true;
        setRuntimeLoading(true);
      }

      if (runtimeRequestRef.current) return runtimeRequestRef.current;

      const request = API.get(
        '/api/option/channel_health_status',
        runtimeRequestConfig,
      )
        .then((res) => {
          if (res.data?.success && res.data?.data) {
            runtimeSnapshotRef.current = res.data.data;
            setRuntime(res.data.data);
            setRuntimeUnavailable(false);
            setRuntimeUpdatedAt(new Date());
            return;
          }

          runtimeSnapshotRef.current = null;
          setRuntime(null);
          setRuntimeUnavailable(true);
          setRuntimeUpdatedAt(null);
          if (runtimeRequestShouldNotifyRef.current) {
            showError(res.data?.message || t('无法获取渠道健康运行状态'));
          }
        })
        .catch(() => {
          runtimeSnapshotRef.current = null;
          setRuntime(null);
          setRuntimeUnavailable(true);
          setRuntimeUpdatedAt(null);
          if (runtimeRequestShouldNotifyRef.current) {
            showError(t('无法获取渠道健康运行状态'));
          }
        })
        .finally(() => {
          runtimeRequestRef.current = null;
          runtimeRequestShouldNotifyRef.current = false;
          setRuntimeLoading(false);
        });

      runtimeRequestRef.current = request;
      return request;
    },
    [t],
  );

  useEffect(() => {
    const next = { ...DEFAULT_INPUTS };
    for (const key of Object.keys(DEFAULT_INPUTS)) {
      if (props.options?.[key] === undefined) continue;
      if (key === KEY_ENABLED) {
        next[key] =
          props.options[key] === true || props.options[key] === 'true';
      } else {
        next[key] = normalizePositiveInteger(
          props.options[key],
          DEFAULT_INPUTS[key],
        );
      }
    }
    setInputs(next);
    setInputsRow(structuredClone(next));
    refForm.current?.setValues(next);
  }, [props.options]);

  useEffect(() => {
    let disposed = false;
    let timer;

    const clearScheduledRefresh = () => {
      if (timer !== undefined) {
        window.clearTimeout(timer);
        timer = undefined;
      }
    };

    const scheduleRefresh = async () => {
      if (document.visibilityState === 'hidden') return;
      await fetchRuntime({ silent: true });
      if (!disposed) {
        const refreshDelay = getChannelHealthRuntimeRefreshDelay(
          runtimeSnapshotRef.current,
        );
        timer = window.setTimeout(scheduleRefresh, refreshDelay);
      }
    };

    const handleVisibilityChange = () => {
      if (document.visibilityState === 'hidden') {
        clearScheduledRefresh();
        return;
      }
      clearScheduledRefresh();
      void scheduleRefresh();
    };

    document.addEventListener('visibilitychange', handleVisibilityChange);
    void scheduleRefresh();
    return () => {
      disposed = true;
      document.removeEventListener('visibilitychange', handleVisibilityChange);
      clearScheduledRefresh();
    };
  }, [fetchRuntime]);

  const onSubmit = async () => {
    const cooldown = normalizePositiveInteger(inputs[KEY_COOLDOWN], 0);
    const probeLease = normalizePositiveInteger(inputs[KEY_PROBE_LEASE], 0);
    if (cooldown < 1 || cooldown > 300) {
      showError(t('冷却窗口必须在 1 到 300 秒之间'));
      return;
    }
    if (probeLease < 1 || probeLease > 60) {
      showError(t('半开探测租约必须在 1 到 60 秒之间'));
      return;
    }

    const normalizedInputs = {
      ...inputs,
      [KEY_COOLDOWN]: cooldown,
      [KEY_PROBE_LEASE]: probeLease,
    };
    const updates = compareObjects(normalizedInputs, inputsRow);
    if (!updates.length) {
      showWarning(t('你似乎并没有修改什么'));
      return;
    }

    setLoading(true);
    try {
      const enabledUpdate = updates.find((item) => item.key === KEY_ENABLED);
      const remainingUpdates = updates.filter(
        (item) => item.key !== KEY_ENABLED,
      );
      const orderedUpdates = [
        ...(enabledUpdate && normalizedInputs[KEY_ENABLED] === false
          ? [enabledUpdate]
          : []),
        ...remainingUpdates,
        ...(enabledUpdate && normalizedInputs[KEY_ENABLED] === true
          ? [enabledUpdate]
          : []),
      ];
      for (const item of orderedUpdates) {
        await API.put('/api/option/', {
          key: item.key,
          value:
            typeof normalizedInputs[item.key] === 'boolean'
              ? String(normalizedInputs[item.key])
              : normalizedInputs[item.key],
        });
      }
      showSuccess(t('保存成功'));
      props.refresh();
      await fetchRuntime({ silent: true });
    } catch (error) {
      showError(t('保存失败，请重试'));
    } finally {
      setLoading(false);
    }
  };

  const summary = runtime ? summarizeChannelHealthRuntime(runtime) : null;
  const runtimeTag = (() => {
    if (runtimeUnavailable) {
      return { label: t('无法确认运行状态'), color: 'red' };
    }
    if (!summary) return { label: t('正在加载运行状态'), color: 'grey' };
    if (summary.state === 'redis_unavailable') {
      return { label: t('Redis 不可用，路由将放行'), color: 'red' };
    }
    if (summary.state === 'active') {
      return { label: t('健康路由已启用'), color: 'green' };
    }
    return { label: t('健康路由未启用'), color: 'grey' };
  })();

  const columns = [
    {
      title: t('渠道'),
      dataIndex: 'channel_id',
      render: (value) => <Text code>#{value}</Text>,
    },
    { title: t('分组'), dataIndex: 'group' },
    {
      title: t('模型'),
      dataIndex: 'model_name',
      render: (value) => <Text code>{value}</Text>,
    },
    {
      title: t('状态'),
      dataIndex: 'state',
      render: (value) => {
        const display = getChannelHealthRuntimeStateDisplay(value, t);
        return <Tag color={display.color}>{display.label}</Tag>;
      },
    },
    {
      title: t('剩余时间'),
      render: (_, entry) =>
        entry.state === 'cooldown'
          ? formatRemainingSeconds(entry.cooldown_remaining_seconds)
          : formatRemainingSeconds(entry.probe_lease_remaining_seconds),
    },
    {
      title: t('最近一次合格失败'),
      render: (_, entry) =>
        [entry.status_code, entry.error_code].filter(Boolean).join(' / ') ||
        '-',
    },
    {
      title: t('路由影响'),
      render: (_, entry) =>
        getChannelHealthRuntimeStateBehavior(entry.state, t).impact,
    },
    {
      title: t('下一次转换'),
      render: (_, entry) =>
        getChannelHealthRuntimeStateBehavior(entry.state, t).nextStep,
    },
  ];

  return (
    <Spin spinning={loading}>
      <Form
        values={inputs}
        getFormApi={(formAPI) => (refForm.current = formAPI)}
        style={{ marginBottom: 15 }}
      >
        <Form.Section text={t('渠道健康路由')}>
          <Banner
            type='info'
            description={t(
              '渠道健康路由只对上游传输中断、502、503、504 和 524 建立临时冷却。请求参数错误和 413 不会被视为渠道故障；429 会按 Retry-After（如有）或配置的回退时长执行临时冷却；Redis 不可用时会放行路由并记录管理员日志。',
            )}
            bordered
            fullMode={false}
            closeIcon={null}
            style={{ marginBottom: 16 }}
          />
          <Row gutter={16}>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.Switch
                field={KEY_ENABLED}
                label={t('启用渠道健康路由')}
                checkedText={t('开')}
                uncheckedText={t('关')}
                onChange={(value) =>
                  setInputs((current) => ({ ...current, [KEY_ENABLED]: value }))
                }
                extraText={t('对同一分组和模型临时绕过刚发生网关故障的渠道。')}
              />
            </Col>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.InputNumber
                field={KEY_COOLDOWN}
                label={t('冷却窗口（秒）')}
                min={1}
                max={300}
                step={1}
                disabled={!inputs[KEY_ENABLED]}
                onChange={(value) =>
                  setInputs((current) => ({
                    ...current,
                    [KEY_COOLDOWN]: Number(value),
                  }))
                }
                extraText={t(
                  '冷却期间选择器跳过该渠道，范围只限相同分组和模型。',
                )}
              />
            </Col>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.InputNumber
                field={KEY_PROBE_LEASE}
                label={t('半开探测租约（秒）')}
                min={1}
                max={60}
                step={1}
                disabled={!inputs[KEY_ENABLED]}
                onChange={(value) =>
                  setInputs((current) => ({
                    ...current,
                    [KEY_PROBE_LEASE]: Number(value),
                  }))
                }
                extraText={t(
                  '冷却结束后仅允许一个请求探测恢复，其余请求继续绕过。',
                )}
              />
            </Col>
          </Row>
          <Row gutter={16} style={{ marginTop: 8 }}>
            <Col span={24}>
              <Button onClick={onSubmit}>{t('保存渠道健康路由设置')}</Button>
              <Button
                theme='light'
                style={{ marginLeft: 8 }}
                loading={runtimeLoading}
                onClick={() => void fetchRuntime()}
              >
                {t('刷新运行状态')}
              </Button>
            </Col>
          </Row>
          <Row gutter={16} style={{ marginTop: 20 }}>
            <Col span={24}>
              <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                <Text strong>{t('当前运行状态')}</Text>
                <Tag color={runtimeTag.color}>{runtimeTag.label}</Tag>
              </div>
              {runtimeUnavailable && (
                <Text type='danger' style={{ display: 'block', marginTop: 10 }}>
                  {t('当前健康路由状态无法确认，不展示过期运行数据。')}
                </Text>
              )}
              {summary && (
                <div
                  style={{
                    display: 'flex',
                    flexWrap: 'wrap',
                    gap: 16,
                    marginTop: 10,
                    color: 'var(--semi-color-text-2)',
                  }}
                >
                  <span>
                    {t('冷却渠道')}: {summary.cooling}
                  </span>
                  <span>
                    {t('进行中的探测')}: {summary.probing}
                  </span>
                  <span>
                    {t('等待探测')}: {summary.awaitingProbe}
                  </span>
                </div>
              )}
              {runtime && (
                <Text
                  type='tertiary'
                  size='small'
                  style={{ display: 'block', marginTop: 10 }}
                >
                  {t(
                    '实际运行策略：冷却 {{cooldown}} 秒，半开探测租约 {{lease}} 秒。',
                    {
                      cooldown: runtime.cooldown_seconds,
                      lease: runtime.probe_lease_seconds,
                    },
                  )}
                  {runtimeUpdatedAt
                    ? ` ${t('最近刷新：{{time}}', {
                        time: runtimeUpdatedAt.toLocaleTimeString(),
                      })}`
                    : ''}
                </Text>
              )}
              {runtime?.enabled &&
                runtime.redis_available &&
                runtime.entries?.length === 0 && (
                  <Text
                    type='tertiary'
                    style={{ display: 'block', marginTop: 10 }}
                  >
                    {t('当前没有处于冷却或探测中的渠道。')}
                  </Text>
                )}
              {runtime?.entries?.length > 0 && (
                <Table
                  columns={columns}
                  dataSource={runtime.entries}
                  pagination={false}
                  size='small'
                  style={{ marginTop: 12 }}
                  rowKey={(entry) =>
                    `${entry.group}:${entry.model_name}:${entry.channel_id}`
                  }
                />
              )}
              <ChannelHealthRuntimeEvents runtime={runtime} />
              <Text
                type='tertiary'
                size='small'
                style={{ display: 'block', marginTop: 10 }}
              >
                {t(
                  '运行态为短期 Redis 状态；每次请求为何绕过或恢复渠道，请在使用日志的渠道健康字段中查看。',
                )}
              </Text>
            </Col>
          </Row>
        </Form.Section>
      </Form>
    </Spin>
  );
}
