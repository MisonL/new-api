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
import { Button, DatePicker, Select } from '@douyinfe/semi-ui';
import { DATE_RANGE_PRESETS } from '../../constants/console.constants';

const ChartsRangeToolbar = ({
  activeRangePreset,
  activeGranularityLabel,
  customRangeDraft,
  customRangeValue,
  hasCompleteCustomRange,
  isCustomRangeOrderValid,
  loading,
  quickRangeOptions,
  timeOptions,
  handleRangePresetChange,
  handleCustomRangeChange,
  handleCustomRangeConfirm,
  t,
}) => (
  <>
    <div className='flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 px-4 py-3'>
      <div className='min-w-0 flex-1 overflow-x-auto pb-1 [-ms-overflow-style:none] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden'>
        <div className='flex min-w-max items-center gap-2'>
          <span className='shrink-0 text-xs font-medium text-gray-500'>
            {t('时间范围')}
          </span>
          {quickRangeOptions.map((option) => {
            const isActive = activeRangePreset === option.value;
            return (
              <Button
                key={option.value}
                size='small'
                type={isActive ? 'primary' : 'tertiary'}
                theme={isActive ? 'solid' : 'borderless'}
                className='shrink-0 whitespace-nowrap'
                disabled={loading}
                onClick={() => handleRangePresetChange(option.value)}
              >
                <span className='inline-flex items-center gap-1'>
                  <span>{option.label}</span>
                  <span className='text-[11px] opacity-75'>
                    {t('按{{granularity}}', {
                      granularity: option.granularityLabel,
                    })}
                  </span>
                </span>
              </Button>
            );
          })}
          <Button
            size='small'
            type={activeRangePreset === 'custom' ? 'primary' : 'tertiary'}
            theme={activeRangePreset === 'custom' ? 'solid' : 'light'}
            className='shrink-0 whitespace-nowrap'
            disabled={loading}
            onClick={() => handleRangePresetChange('custom')}
          >
            {t('自定义范围')}
          </Button>
        </div>
      </div>
      <div className='shrink-0 rounded-full bg-gray-50 px-3 py-1 text-xs font-medium text-gray-600 ring-1 ring-gray-200'>
        {t('当前粒度')}: {activeGranularityLabel}
      </div>
    </div>
    {activeRangePreset === 'custom' && (
      <div className='flex flex-col gap-3 border-b border-gray-100 bg-gray-50/60 px-4 py-3 lg:flex-row lg:items-center lg:justify-between'>
        <DatePicker
          type='dateTimeRange'
          value={customRangeValue}
          onChange={(_, dateStrings) => handleCustomRangeChange(dateStrings)}
          presets={DATE_RANGE_PRESETS.map((preset) => ({
            text: t(preset.text),
            start: preset.start(),
            end: preset.end(),
          }))}
          placeholder={[t('开始时间'), t('结束时间')]}
          showClear
          size='small'
          disabled={loading}
          className='min-w-0 w-full lg:max-w-[420px]'
        />
        <div className='flex w-full flex-col gap-3 sm:flex-row sm:items-center sm:justify-end lg:w-auto'>
          <Select
            value={customRangeDraft.default_time}
            optionList={timeOptions}
            onChange={(value) =>
              handleCustomRangeChange(customRangeValue, value)
            }
            placeholder={t('时间粒度')}
            size='small'
            disabled={loading}
            className='w-full sm:w-36'
          />
          <Button
            type='primary'
            size='small'
            className='w-full sm:w-auto'
            loading={loading}
            disabled={
              loading || !hasCompleteCustomRange || !isCustomRangeOrderValid
            }
            onClick={handleCustomRangeConfirm}
          >
            {t('应用')}
          </Button>
        </div>
      </div>
    )}
  </>
);

export default ChartsRangeToolbar;
