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
import { Button, TabPane, Tabs } from '@douyinfe/semi-ui';
import { PieChart } from 'lucide-react';

const ChartsPanelHeader = ({
  activeChartTab,
  setActiveChartTab,
  isAdminUser,
  flexCenterGap2,
  onOpenLogs,
  t,
}) => (
  <div className='flex w-full flex-col gap-3 lg:flex-row lg:items-center lg:justify-between'>
    <div className={`${flexCenterGap2} shrink-0`}>
      <PieChart size={16} />
      {t('模型数据分析')}
    </div>
    <div className='flex min-w-0 flex-1 flex-col gap-2 lg:flex-row lg:items-center lg:justify-end'>
      <div className='min-w-0 overflow-x-auto pb-1 [-ms-overflow-style:none] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden'>
        <div className='min-w-max'>
          <Tabs
            type='slash'
            activeKey={activeChartTab}
            onChange={setActiveChartTab}
          >
            <TabPane tab={<span>{t('消耗分布')}</span>} itemKey='1' />
            <TabPane tab={<span>{t('调用趋势')}</span>} itemKey='2' />
            <TabPane tab={<span>{t('调用次数分布')}</span>} itemKey='3' />
            <TabPane tab={<span>{t('调用次数排行')}</span>} itemKey='4' />
            {isAdminUser && (
              <TabPane tab={<span>{t('用户消耗排行')}</span>} itemKey='5' />
            )}
            {isAdminUser && (
              <TabPane tab={<span>{t('用户消耗趋势')}</span>} itemKey='6' />
            )}
          </Tabs>
        </div>
      </div>
      <Button
        size='small'
        type='tertiary'
        theme='light'
        className='shrink-0'
        onClick={onOpenLogs}
      >
        {t('查看日志')}
      </Button>
    </div>
  </div>
);

export default ChartsPanelHeader;
