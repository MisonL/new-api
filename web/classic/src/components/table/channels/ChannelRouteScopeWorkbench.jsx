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

import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import {
  Banner,
  Button,
  Card,
  Input,
  InputNumber,
  Select,
  Space,
  Spin,
  Switch,
  Table,
  Tag,
  Toast,
  Typography,
} from '@douyinfe/semi-ui';
import { IconRefresh, IconSave } from '@douyinfe/semi-icons';
import { API } from '../../../helpers';
import {
  adjustRouteWeights,
  formatRouteWeightPercent,
  parseRouteWeightPercent,
  ROUTE_WEIGHT_TOTAL_BPS,
} from '../../../helpers/channelRouteWeight';

const { Text } = Typography;

const ChannelRouteScopeWorkbench = ({ t }) => {
  const [scopes, setScopes] = useState([]);
  const [selectedId, setSelectedId] = useState('');
  const [groupFilter, setGroupFilter] = useState('');
  const [modelFilter, setModelFilter] = useState('');
  const [weightInputs, setWeightInputs] = useState({});
  const [priority, setPriority] = useState(0);
  const [locked, setLocked] = useState({});
  const [redistributionMode, setRedistributionMode] = useState('equal_adjust');
  const [metrics, setMetrics] = useState(null);
  const [metricsError, setMetricsError] = useState(false);
  const [routingConfig, setRoutingConfig] = useState(null);
  const [routingConfigLoaded, setRoutingConfigLoaded] = useState(false);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const scopesRequestRef = useRef(0);

  const selectedScope = useMemo(
    () => scopes.find((scope) => scope.id === selectedId) || scopes[0],
    [scopes, selectedId],
  );
  const v2Enabled = routingConfig?.v2_enabled === true;

  const loadRoutingConfig = useCallback(async () => {
    try {
      const response = await API.get('/api/channel-route-scopes/config');
      if (!response?.data?.success || !response.data.data) {
        throw new Error(response?.data?.message || t('路由 Scope 加载失败'));
      }
      setRoutingConfig(response.data.data);
    } catch (error) {
      setRoutingConfig({
        v2_enabled: false,
        shadow_enabled: false,
        strict_writes: false,
      });
      Toast.error(error?.message || t('路由 Scope 加载失败'));
    } finally {
      setRoutingConfigLoaded(true);
    }
  }, [t]);

  const loadScopes = useCallback(
    async (filters = {}) => {
      const requestId = ++scopesRequestRef.current;
      setLoading(true);
      try {
        const response = await API.get('/api/channel-route-scopes', {
          params: {
            group: filters.group?.trim() || undefined,
            model: filters.model?.trim() || undefined,
          },
        });
        if (!response?.data?.success) {
          throw new Error(response?.data?.message || t('路由 Scope 加载失败'));
        }
        const nextScopes = Array.isArray(response.data.data)
          ? response.data.data
          : [];
        if (requestId !== scopesRequestRef.current) return;
        setScopes(nextScopes);
        setSelectedId((current) =>
          nextScopes.some((scope) => scope.id === current)
            ? current
            : nextScopes[0]?.id || '',
        );
      } catch (error) {
        if (requestId !== scopesRequestRef.current) return;
        Toast.error(error?.message || t('路由 Scope 加载失败'));
      } finally {
        if (requestId === scopesRequestRef.current) setLoading(false);
      }
    },
    [t],
  );

  useEffect(() => {
    void loadRoutingConfig();
    void loadScopes();
  }, [loadRoutingConfig, loadScopes]);

  useEffect(() => {
    if (!selectedScope) {
      setWeightInputs({});
      setPriority(0);
      setMetrics(null);
      setMetricsError(false);
      return;
    }
    setWeightInputs(
      Object.fromEntries(
        (selectedScope.items || []).map((item) => [
          item.channel_id,
          formatRouteWeightPercent(item.route_weight_bps),
        ]),
      ),
    );
    setPriority(selectedScope.route_priority || 0);
    setLocked({});
    setMetrics(null);
    setMetricsError(false);
    let active = true;
    API.get(`/api/channel-route-scopes/${selectedScope.id}/metrics`)
      .then((response) => {
        if (!active) return;
        if (response?.data?.success) {
          setMetrics(response.data.data || null);
          setMetricsError(false);
        } else {
          setMetricsError(true);
        }
      })
      .catch(() => {
        if (active) setMetricsError(true);
      });
    return () => {
      active = false;
    };
  }, [selectedScope]);

  const metricsByChannel = useMemo(
    () =>
      new Map((metrics?.items || []).map((item) => [item.channel_id, item])),
    [metrics],
  );

  const changeWeight = (channelId, value) => {
    const raw = value === null || value === undefined ? '' : String(value);
    const target = parseRouteWeightPercent(raw);
    if (target === null) {
      setWeightInputs((current) => ({ ...current, [channelId]: raw }));
      return;
    }
    const items = selectedScope?.items || [];
    const currentValues = items.map(
      (item) =>
        parseRouteWeightPercent(weightInputs[item.channel_id] || '') ?? 0,
    );
    const targetIndex = items.findIndex(
      (item) => item.channel_id === channelId,
    );
    if (targetIndex < 0) return;
    if (locked[channelId]) {
      Toast.error(t('调整前请先解锁该渠道'));
      return;
    }
    const next = adjustRouteWeights(
      currentValues,
      targetIndex,
      target,
      redistributionMode,
      items.map((item) => Boolean(locked[item.channel_id])),
    );
    if (!next) {
      Toast.error(t('请先解锁其他渠道再调整该权重'));
      return;
    }
    setWeightInputs(
      Object.fromEntries(
        items.map((item, index) => [
          item.channel_id,
          formatRouteWeightPercent(next[index]),
        ]),
      ),
    );
  };

  const weightTotal = (selectedScope?.items || []).reduce((total, item) => {
    const value = parseRouteWeightPercent(weightInputs[item.channel_id] || '');
    return value === null ? Number.NaN : total + value;
  }, 0);
  const allWeightsZero =
    Boolean(selectedScope?.items?.length) &&
    selectedScope.items.every(
      (item) =>
        parseRouteWeightPercent(weightInputs[item.channel_id] || '') === 0,
    );

  const saveWeights = async () => {
    if (!selectedScope) return;
    if (!v2Enabled) {
      Toast.error(t('V2 路由未启用，Scope 只能查看'));
      return;
    }
    if (
      (selectedScope.items || []).some(
        (item) =>
          parseRouteWeightPercent(weightInputs[item.channel_id] || '') === null,
      ) ||
      weightTotal !== ROUTE_WEIGHT_TOTAL_BPS
    ) {
      Toast.error(t('路由权重总和必须为 100%'));
      return;
    }
    setSaving(true);
    try {
      const response = await API.put(
        `/api/channel-route-scopes/${selectedScope.id}/weights`,
        {
          version: selectedScope.version,
          mode: redistributionMode,
          items: selectedScope.items.map((item) => ({
            channel_id: item.channel_id,
            route_weight_bps: parseRouteWeightPercent(
              weightInputs[item.channel_id] || '',
            ),
          })),
        },
      );
      if (!response?.data?.success) {
        throw new Error(response?.data?.message || t('路由 Scope 保存失败'));
      }
      Toast.success(t('路由权重已保存'));
      await loadScopes({ group: groupFilter, model: modelFilter });
    } catch (error) {
      Toast.error(error?.message || t('路由 Scope 保存失败'));
      await loadScopes({ group: groupFilter, model: modelFilter });
    } finally {
      setSaving(false);
    }
  };

  const savePriority = async () => {
    if (
      !selectedScope ||
      !Number.isInteger(Number(priority)) ||
      Number(priority) < 0
    ) {
      Toast.error(t('路由优先级必须是非负整数'));
      return;
    }
    if (!v2Enabled) {
      Toast.error(t('V2 路由未启用，Scope 只能查看'));
      return;
    }
    setSaving(true);
    try {
      const response = await API.put(
        `/api/channel-route-scopes/${selectedScope.id}/priority`,
        { version: selectedScope.version, route_priority: Number(priority) },
      );
      if (!response?.data?.success) {
        throw new Error(response?.data?.message || t('路由 Scope 保存失败'));
      }
      Toast.success(t('路由优先级已保存'));
      await loadScopes({ group: groupFilter, model: modelFilter });
    } catch (error) {
      Toast.error(error?.message || t('路由 Scope 保存失败'));
      await loadScopes({ group: groupFilter, model: modelFilter });
    } finally {
      setSaving(false);
    }
  };

  const resetDraft = () => {
    if (!selectedScope) return;
    setWeightInputs(
      Object.fromEntries(
        selectedScope.items.map((item) => [
          item.channel_id,
          formatRouteWeightPercent(item.route_weight_bps),
        ]),
      ),
    );
    setPriority(selectedScope.route_priority || 0);
    setLocked({});
  };

  const columns = [
    {
      title: t('渠道'),
      dataIndex: 'channel_name',
      render: (_value, item) => (
        <div>
          <Text strong>{`#${item.channel_id} ${item.channel_name}`}</Text>
          <div style={{ fontSize: 12, color: 'var(--semi-color-text-2)' }}>
            {item.ability_enabled ? t('能力已启用') : t('能力未启用')}
          </div>
        </div>
      ),
    },
    {
      title: t('权重 (%)'),
      dataIndex: 'route_weight_bps',
      render: (_value, item) => (
        <Input
          min={0}
          max={100}
          step='0.01'
          type='number'
          value={weightInputs[item.channel_id] ?? ''}
          onChange={(value) => changeWeight(item.channel_id, value)}
          disabled={saving || !v2Enabled || Boolean(locked[item.channel_id])}
          style={{ width: 120 }}
          aria-label={t('Route weight for {{channel}}', {
            channel: item.channel_name,
          })}
        />
      ),
    },
    {
      title: t('最大并发'),
      dataIndex: 'max_concurrency',
      render: (value) => value || t('不限'),
    },
    {
      title: t('容量'),
      render: (_value, item) => {
        const snapshot = metricsByChannel.get(item.channel_id);
        if (!snapshot || item.max_concurrency <= 0) return '-';
        return `${snapshot.current_in_flight}/${item.max_concurrency} (${snapshot.confirmed ? snapshot.source : t('未确认')})`;
      },
    },
    {
      title: t('锁定'),
      render: (_value, item) => (
        <Switch
          checked={Boolean(locked[item.channel_id])}
          onChange={(checked) =>
            setLocked((current) => ({ ...current, [item.channel_id]: checked }))
          }
          disabled={saving || !v2Enabled}
        />
      ),
    },
    {
      title: t('状态'),
      dataIndex: 'channel_status',
      render: (value) => (
        <Tag color={value === 1 ? 'green' : 'grey'}>
          {value === 1 ? t('启用') : t('停用')}
        </Tag>
      ),
    },
  ];

  return (
    <Card
      title={t('路由 Scope 工作台')}
      headerExtraContent={
        <Button
          theme='borderless'
          icon={<IconRefresh />}
          onClick={() =>
            void loadScopes({ group: groupFilter, model: modelFilter })
          }
          loading={loading}
        >
          {t('刷新')}
        </Button>
      }
      style={{ marginBottom: 12 }}
    >
      {!routingConfigLoaded ? (
        <Spin size='small' />
      ) : (
        <>
          {!v2Enabled && (
            <Banner
              type='warning'
              description={
                routingConfig.shadow_enabled
                  ? t('路由影子模式已启用，当前流量仍由旧选择器处理')
                  : t('V2 路由未启用，Scope 编辑为只读')
              }
              style={{ marginBottom: 12 }}
            />
          )}
          <Space wrap style={{ marginBottom: 12 }}>
            <Input
              value={groupFilter}
              onChange={setGroupFilter}
              placeholder={t('全部分组')}
              prefix={t('分组')}
              style={{ width: 180 }}
            />
            <Input
              value={modelFilter}
              onChange={setModelFilter}
              placeholder={t('全部模型')}
              prefix={t('模型')}
              style={{ width: 220 }}
            />
            <Button
              onClick={() =>
                void loadScopes({ group: groupFilter, model: modelFilter })
              }
              disabled={saving}
            >
              {t('应用筛选')}
            </Button>
            <Button
              theme='borderless'
              onClick={() => {
                setGroupFilter('');
                setModelFilter('');
                void loadScopes({ group: '', model: '' });
              }}
              disabled={saving}
            >
              {t('重置筛选')}
            </Button>
          </Space>
          {scopes.length === 0 ? (
            loading ? (
              <Spin size='small' />
            ) : (
              <Banner type='info' description={t('暂无路由 Scope')} />
            )
          ) : (
            <>
              <Select
                value={selectedScope?.id}
                onChange={setSelectedId}
                optionList={scopes.map((scope) => ({
                  value: scope.id,
                  label: `${scope.group} / ${scope.effective_route_model} / P${scope.route_priority}`,
                }))}
                style={{ width: '100%', marginBottom: 12 }}
              />
              {selectedScope && (
                <>
                  <Space wrap style={{ marginBottom: 12 }}>
                    <InputNumber
                      value={priority}
                      min={0}
                      precision={0}
                      prefix={t('优先级')}
                      onChange={setPriority}
                      disabled={saving || !v2Enabled}
                    />
                    <Button
                      icon={<IconSave />}
                      onClick={() => void savePriority()}
                      disabled={saving || !v2Enabled}
                    >
                      {t('保存优先级')}
                    </Button>
                    <Select
                      value={redistributionMode}
                      onChange={setRedistributionMode}
                      optionList={[
                        { value: 'equal_adjust', label: t('平均调整') },
                        {
                          value: 'proportional_adjust',
                          label: t('按比例调整'),
                        },
                      ]}
                      disabled={
                        saving || !v2Enabled || selectedScope.items.length <= 1
                      }
                      style={{ width: 160 }}
                    />
                    <Text
                      type={
                        weightTotal === ROUTE_WEIGHT_TOTAL_BPS
                          ? 'tertiary'
                          : 'danger'
                      }
                    >
                      {t('权重总和显示', {
                        current: Number.isFinite(weightTotal)
                          ? (weightTotal / 100).toFixed(2)
                          : '-',
                        total: '100.00',
                      })}
                    </Text>
                  </Space>
                  <Table
                    columns={columns}
                    dataSource={selectedScope.items}
                    rowKey='channel_id'
                    pagination={false}
                    loading={loading}
                  />
                  {metricsError ? (
                    <Banner
                      type='warning'
                      description={t('容量指标加载失败，路由配置仍可编辑')}
                      style={{ marginTop: 12 }}
                    />
                  ) : null}
                  {allWeightsZero ? (
                    <Banner
                      type='warning'
                      description={t(
                        '所有路由权重都是 0%，该 Scope 不会接收流量',
                      )}
                      style={{ marginTop: 12 }}
                    />
                  ) : null}
                  <Space style={{ marginTop: 12 }}>
                    <Button
                      theme='light'
                      onClick={resetDraft}
                      disabled={saving}
                    >
                      {t('重置草稿')}
                    </Button>
                    <Button
                      icon={<IconSave />}
                      onClick={() => void saveWeights()}
                      disabled={saving || !v2Enabled}
                    >
                      {t('保存权重')}
                    </Button>
                  </Space>
                </>
              )}
            </>
          )}
        </>
      )}
    </Card>
  );
};

export default ChannelRouteScopeWorkbench;
