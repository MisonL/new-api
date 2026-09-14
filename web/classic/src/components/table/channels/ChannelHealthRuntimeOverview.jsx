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
import { Button, Table, Tag, Typography } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { showError } from '../../../helpers';
import {
  getChannelHealthRuntimeEntrySearchCriteria,
  getChannelHealthRuntimeEntryKey,
  getChannelHealthRuntimeRouteSummary,
  getChannelHealthRuntimeStateDisplay,
  getChannelHealthRuntimeStateBehavior,
  summarizeChannelHealthRuntime,
} from '../../../helpers/channelHealthRuntime';
import ChannelHealthRuntimeEvents from './ChannelHealthRuntimeEvents';

const { Text } = Typography;

const formatRemainingSeconds = (seconds) => {
  const normalized = Number(seconds);
  return Number.isFinite(normalized) && normalized > 0 ? `${normalized}s` : '-';
};

export default function ChannelHealthRuntimeOverview({
  runtimeState,
  canConfigure,
  onLocateChannel,
}) {
  const { t } = useTranslation();
  const {
    runtime,
    runtimeUnavailable,
    runtimeUpdatedAt,
    runtimeLoading,
    refresh,
  } = runtimeState || {};

  if (!runtimeState) return null;

  const summary = runtime ? summarizeChannelHealthRuntime(runtime) : null;
  const runtimeTag = (() => {
    if (runtimeUnavailable)
      return { label: t('无法确认运行状态'), color: 'red' };
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
      render: (value, entry) => {
        const searchCriteria =
          getChannelHealthRuntimeEntrySearchCriteria(entry);
        if (!searchCriteria || !onLocateChannel) {
          return <Text code>#{value}</Text>;
        }
        return (
          <Button
            theme='borderless'
            type='tertiary'
            size='small'
            aria-label={t('渠道 #{{channel}}', { channel: value })}
            style={{ height: 24, padding: 0 }}
            onClick={(event) => {
              event.stopPropagation();
              onLocateChannel(entry);
            }}
          >
            <Text code>#{value}</Text>
          </Button>
        );
      },
    },
    { title: t('分组'), dataIndex: 'group' },
    {
      title: t('模型'),
      dataIndex: 'model_name',
      render: (value) => <Text code>{value}</Text>,
    },
    {
      title: t('渠道健康路由'),
      render: (_, entry) => (
        <Text code>{getChannelHealthRuntimeRouteSummary(entry)}</Text>
      ),
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
  const handleRefresh = async () => {
    try {
      await refresh();
    } catch {
      showError(t('无法获取渠道健康运行状态'));
    }
  };

  return (
    <div
      className='rounded-xl border p-3 sm:p-4'
      style={{ borderColor: 'var(--semi-color-border)', marginBottom: 12 }}
    >
      <div className='flex flex-col gap-3'>
        <div className='flex flex-col gap-2 md:flex-row md:items-start md:justify-between'>
          <div>
            <Text strong>{t('渠道健康路由')}</Text>
            <Text type='tertiary' size='small' style={{ display: 'block' }}>
              {t('运行态按渠道、分组和模型隔离。')}
            </Text>
          </div>
          <Tag color={runtimeTag.color}>{runtimeTag.label}</Tag>
        </div>

        {runtimeUnavailable ? (
          <Text type='danger'>
            {t('当前健康路由状态无法确认，不展示过期运行数据。')}
          </Text>
        ) : null}

        {summary ? (
          <div className='flex flex-wrap gap-x-5 gap-y-1 text-sm'>
            <span className='text-amber-700 dark:text-amber-400'>
              {t('冷却渠道')}: {summary.cooling}
            </span>
            <span className='text-sky-700 dark:text-sky-400'>
              {t('进行中的探测')}: {summary.probing}
            </span>
            <span className='text-teal-700 dark:text-teal-400'>
              {t('等待探测')}: {summary.awaitingProbe}
            </span>
          </div>
        ) : null}

        {runtime?.enabled &&
        runtime.redis_available &&
        runtime.entries?.length === 0 ? (
          <Text type='tertiary'>{t('当前没有生效中的渠道临时绕过。')}</Text>
        ) : null}

        {runtime?.entries?.length > 0 ? (
          <Table
            columns={columns}
            dataSource={runtime.entries}
            pagination={false}
            size='small'
            rowKey={(entry) => getChannelHealthRuntimeEntryKey(entry)}
            scroll={{ x: 'max-content' }}
          />
        ) : null}

        <ChannelHealthRuntimeEvents runtime={runtime} />

        <div className='flex flex-wrap gap-2'>
          <Button
            size='small'
            theme='light'
            loading={runtimeLoading}
            onClick={() => void handleRefresh()}
          >
            {t('刷新运行状态')}
          </Button>
          {canConfigure ? (
            <Button
              size='small'
              theme='light'
              onClick={() => window.open('/console/setting', '_self')}
            >
              {t('配置渠道健康路由')}
            </Button>
          ) : null}
          <Button
            size='small'
            theme='light'
            onClick={() => window.open('/console/log', '_self')}
          >
            {t('查看请求路由日志')}
          </Button>
        </div>

        {runtime && runtimeUpdatedAt ? (
          <Text type='tertiary' size='small'>
            {t('最近刷新：{{time}}', {
              time: runtimeUpdatedAt.toLocaleTimeString(),
            })}
          </Text>
        ) : null}
      </div>
    </div>
  );
}
