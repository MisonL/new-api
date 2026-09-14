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
import { Space, Tag, Tooltip } from '@douyinfe/semi-ui';
import {
  getChannelHealthRoutingEventDisplay,
  getChannelHealthRoutingEventImpact,
  getChannelHealthRoutingEventTone,
  getChannelHealthRoutingEvents,
  getChannelHealthRoutingResult,
  getChannelHealthRoutingResultDisplay,
  getChannelHealthRoutingResultImpact,
} from '../../../helpers/usageLogsChannelHealthRouting';

function HealthOverview({ other, t }) {
  const result = getChannelHealthRoutingResult(other);
  if (!result) {
    return null;
  }

  const display = getChannelHealthRoutingResultDisplay(result, t);
  return (
    <div className='usage-log-health-overview'>
      <div className='usage-log-health-overview-item'>
        <span className='usage-log-health-overview-label'>
          {t('健康路由决策')}
        </span>
        <span className={'usage-log-health-overview-value is-' + display.color}>
          {display.label}
        </span>
      </div>
      <div className='usage-log-health-overview-item usage-log-health-overview-impact'>
        <span className='usage-log-health-overview-label'>{t('路由影响')}</span>
        <span className='usage-log-health-overview-value'>
          {getChannelHealthRoutingResultImpact(result, t)}
        </span>
      </div>
      <div className='usage-log-health-overview-item'>
        <span className='usage-log-health-overview-label'>
          {t('已记录动作')}
        </span>
        <span className='usage-log-health-overview-value is-count'>
          {result.actionCount}
        </span>
      </div>
    </div>
  );
}

export function ChannelHealthRoutingEvents({ other, t }) {
  const events = getChannelHealthRoutingEvents(other);
  if (events.length === 0) {
    return null;
  }

  return (
    <section className='usage-log-health-events'>
      <div className='usage-log-health-events-header'>
        <span className='usage-log-health-events-title'>
          {t('渠道健康事件')}
        </span>
        <span className='usage-log-health-events-note'>
          {t('仅记录本次请求中的动作')}
        </span>
      </div>
      <div className='usage-log-health-events-list'>
        {events.map((event, index) => {
          const display = getChannelHealthRoutingEventDisplay(event, t);
          const tone = getChannelHealthRoutingEventTone(event);
          return (
            <div
              className={'usage-log-health-event is-' + tone}
              key={[
                event.channelId,
                event.action,
                event.statusCode ?? '',
                event.errorCode ?? '',
                event.routeKey ?? '',
                event.effectiveModel ?? '',
                event.protocol ?? '',
                event.upstreamPath ?? '',
                event.multiKeyIndex ?? '',
                index,
              ].join(':')}
            >
              <span className='usage-log-health-event-label'>
                {display.label}
              </span>
              <span className='usage-log-health-event-value'>
                {display.value}
              </span>
              <span className='usage-log-health-event-impact'>
                {t('路由影响')}: {getChannelHealthRoutingEventImpact(event, t)}
              </span>
            </div>
          );
        })}
      </div>
    </section>
  );
}

export function ChannelHealthRoutingTooltip({ other, t }) {
  const result = getChannelHealthRoutingResult(other);
  if (!result) {
    return null;
  }

  return (
    <div className='usage-log-health-tooltip'>
      <div className='usage-log-health-tooltip-header'>
        <span className='usage-log-header-audit-label-tag usage-log-health-tooltip-label-tag'>
          {t('渠道健康')}
        </span>
      </div>
      <HealthOverview other={other} t={t} />
      {result.kind === 'healthy' && (
        <div className='usage-log-health-note'>
          {t('健康门控仅表示选择时的状态，不表示上游请求成功。')}
        </div>
      )}
      <ChannelHealthRoutingEvents other={other} t={t} />
    </div>
  );
}

export function ChannelHealthRoutingResult({ other, t }) {
  const result = getChannelHealthRoutingResult(other);
  if (!result) {
    return null;
  }

  const display = getChannelHealthRoutingResultDisplay(result, t);
  return (
    <Tooltip
      className='usage-log-info-popover-layer'
      content={<ChannelHealthRoutingTooltip other={other} t={t} />}
      position='top'
      showArrow
    >
      <span>
        <Tag color={display.color} shape='circle'>
          <Space spacing='tight'>
            <span>{display.label}</span>
            <span
              className='usage-log-routing-action-count'
              title={t('已记录动作：{{count}}', {
                count: result.actionCount,
              })}
            >
              {result.actionCount}
            </span>
          </Space>
        </Tag>
      </span>
    </Tooltip>
  );
}

export function ChannelHealthRoutingDetails({ other, t }) {
  const result = getChannelHealthRoutingResult(other);
  if (!result) {
    return null;
  }

  return (
    <div className='usage-log-health-details'>
      <HealthOverview other={other} t={t} />
      {result.kind === 'healthy' && (
        <div className='usage-log-health-note'>
          {t('健康门控仅表示选择时的状态，不表示上游请求成功。')}
        </div>
      )}
      <ChannelHealthRoutingEvents other={other} t={t} />
    </div>
  );
}
