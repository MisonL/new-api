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
import { Button, Popover, Tag, Typography } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import {
  getChannelHealthRuntimeDominantEntry,
  getChannelHealthRuntimeEntryKey,
  getChannelHealthRuntimeChannelStateDisplay,
  getChannelHealthRuntimeEntriesForChannel,
  getChannelHealthRuntimeRouteSummary,
  getChannelHealthRuntimeStateBehavior,
  getChannelHealthRuntimeStateDisplay,
} from '../../../helpers/channelHealthRuntime';

const { Text } = Typography;

const formatRemainingSeconds = (entry) => {
  const seconds =
    entry.state === 'cooldown'
      ? entry.cooldown_remaining_seconds
      : entry.probe_lease_remaining_seconds;
  return Number.isFinite(seconds) && seconds > 0 ? `${seconds}s` : '-';
};

const formatLastQualifyingFailure = (entry) =>
  [entry.status_code, entry.error_code].filter(Boolean).join(' / ') || '-';

export default function ChannelHealthRuntimeChannelState({
  channelId,
  runtime,
}) {
  const { t } = useTranslation();
  const entries = getChannelHealthRuntimeEntriesForChannel(runtime, channelId);
  const display = getChannelHealthRuntimeChannelStateDisplay(entries, t);

  if (!display) return null;

  const dominantEntry = getChannelHealthRuntimeDominantEntry(entries);
  const behavior = dominantEntry
    ? getChannelHealthRuntimeStateBehavior(dominantEntry.state, t)
    : null;
  const content = (
    <div className='w-[min(360px,calc(100vw-32px))] space-y-3 p-1'>
      <div className='space-y-1'>
        <Text strong>{t('渠道健康路由')}</Text>
        <Text type='tertiary' size='small' style={{ display: 'block' }}>
          {t('运行态按渠道、分组和模型隔离。')}
        </Text>
        {behavior ? (
          <div className='space-y-1 text-xs'>
            <Text type='tertiary' size='small' style={{ display: 'block' }}>
              {t('路由影响')}: {behavior.impact}
            </Text>
            <Text type='tertiary' size='small' style={{ display: 'block' }}>
              {t('下一次转换')}: {behavior.nextStep}
            </Text>
          </div>
        ) : null}
      </div>
      <div className='max-h-80 space-y-2 overflow-y-auto pr-1'>
        {entries.map((entry) => {
          const entryDisplay = getChannelHealthRuntimeStateDisplay(
            entry.state,
            t,
          );
          const entryBehavior = getChannelHealthRuntimeStateBehavior(
            entry.state,
            t,
          );
          return (
            <div
              key={getChannelHealthRuntimeEntryKey(entry)}
              className='space-y-2 rounded-md border border-[var(--semi-color-border)] bg-[var(--semi-color-fill-0)] p-2.5'
            >
              <Tag color={entryDisplay.color} type='light' size='small'>
                {entryDisplay.label}
              </Tag>
              <div className='grid grid-cols-2 gap-x-3 gap-y-2 text-xs'>
                <div className='min-w-0'>
                  <Text type='tertiary' size='small'>
                    {t('分组')}
                  </Text>
                  <Text size='small' className='mt-0.5 block break-words'>
                    {entry.group}
                  </Text>
                </div>
                <div className='min-w-0'>
                  <Text type='tertiary' size='small'>
                    {t('剩余时间')}
                  </Text>
                  <Text code size='small' className='mt-0.5 block'>
                    {formatRemainingSeconds(entry)}
                  </Text>
                </div>
                <div className='col-span-2 min-w-0'>
                  <Text type='tertiary' size='small'>
                    {t('模型')}
                  </Text>
                  <Text code size='small' className='mt-0.5 block break-words'>
                    {entry.model_name}
                  </Text>
                </div>
                <div className='col-span-2 min-w-0'>
                  <Text type='tertiary' size='small'>
                    {t('渠道健康路由')}
                  </Text>
                  <Text code size='small' className='mt-0.5 block break-words'>
                    {getChannelHealthRuntimeRouteSummary(entry)}
                  </Text>
                </div>
                <div className='col-span-2 min-w-0'>
                  <Text type='tertiary' size='small'>
                    {t('路由影响')}
                  </Text>
                  <Text size='small' className='mt-0.5 block break-words'>
                    {entryBehavior.impact}
                  </Text>
                </div>
                <div className='col-span-2 min-w-0'>
                  <Text type='tertiary' size='small'>
                    {t('下一次转换')}
                  </Text>
                  <Text size='small' className='mt-0.5 block break-words'>
                    {entryBehavior.nextStep}
                  </Text>
                </div>
                <div className='col-span-2 min-w-0'>
                  <Text type='tertiary' size='small'>
                    {t('最近一次合格失败')}
                  </Text>
                  <Text code size='small' className='mt-0.5 block break-words'>
                    {formatLastQualifyingFailure(entry)}
                  </Text>
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );

  return (
    <Popover content={content} trigger='click' position='top' showArrow>
      <Button
        theme='borderless'
        size='small'
        aria-label={`${t('渠道健康路由')}: ${display.label}`}
        style={{ height: 24, padding: '0 4px' }}
        onClick={(event) => event.stopPropagation()}
      >
        <Tag color={display.color} type='light' size='small' shape='circle'>
          {display.label}
        </Tag>
      </Button>
    </Popover>
  );
}
