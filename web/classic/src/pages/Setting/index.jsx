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

import React, { useEffect, useMemo, useState } from 'react';
import { Layout } from '@douyinfe/semi-ui';
import { useNavigate, useLocation } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  Settings,
  Calculator,
  Gauge,
  Shapes,
  Cog,
  MoreHorizontal,
  LayoutDashboard,
  MessageSquare,
  Palette,
  CreditCard,
  Server,
  Activity,
} from 'lucide-react';

import SystemSetting from '../../components/settings/SystemSetting';
import { isRoot } from '../../helpers';
import OtherSetting from '../../components/settings/OtherSetting';
import OperationSetting from '../../components/settings/OperationSetting';
import RateLimitSetting from '../../components/settings/RateLimitSetting';
import ModelSetting from '../../components/settings/ModelSetting';
import DashboardSetting from '../../components/settings/DashboardSetting';
import RatioSetting from '../../components/settings/RatioSetting';
import ChatsSetting from '../../components/settings/ChatsSetting';
import DrawingSetting from '../../components/settings/DrawingSetting';
import PaymentSetting from '../../components/settings/PaymentSetting';
import ModelDeploymentSetting from '../../components/settings/ModelDeploymentSetting';
import PerformanceSetting from '../../components/settings/PerformanceSetting';
import SelectableButtonGroup from '../../components/common/ui/SelectableButtonGroup';

const Setting = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const [tabActiveKey, setTabActiveKey] = useState(() => {
    const currentTab = new URLSearchParams(location.search).get('tab');
    return currentTab || 'operation';
  });
  const panes = useMemo(() => {
    const nextPanes = [];

    if (isRoot()) {
      nextPanes.push({
        label: t('运营设置'),
        icon: <Settings size={18} />,
        content: <OperationSetting />,
        itemKey: 'operation',
      });
      nextPanes.push({
        label: t('仪表盘设置'),
        icon: <LayoutDashboard size={18} />,
        content: <DashboardSetting />,
        itemKey: 'dashboard',
      });
      nextPanes.push({
        label: t('聊天设置'),
        icon: <MessageSquare size={18} />,
        content: <ChatsSetting />,
        itemKey: 'chats',
      });
      nextPanes.push({
        label: t('绘图设置'),
        icon: <Palette size={18} />,
        content: <DrawingSetting />,
        itemKey: 'drawing',
      });
      nextPanes.push({
        label: t('支付设置'),
        icon: <CreditCard size={18} />,
        content: <PaymentSetting />,
        itemKey: 'payment',
      });
      nextPanes.push({
        label: t('分组与模型定价设置'),
        icon: <Calculator size={18} />,
        content: <RatioSetting />,
        itemKey: 'ratio',
      });
      nextPanes.push({
        label: t('速率限制设置'),
        icon: <Gauge size={18} />,
        content: <RateLimitSetting />,
        itemKey: 'ratelimit',
      });
      nextPanes.push({
        label: t('模型相关设置'),
        icon: <Shapes size={18} />,
        content: <ModelSetting />,
        itemKey: 'models',
      });
      nextPanes.push({
        label: t('模型部署设置'),
        icon: <Server size={18} />,
        content: <ModelDeploymentSetting />,
        itemKey: 'model-deployment',
      });
      nextPanes.push({
        label: t('性能设置'),
        icon: <Activity size={18} />,
        content: <PerformanceSetting />,
        itemKey: 'performance',
      });
      nextPanes.push({
        label: t('系统设置'),
        icon: <Cog size={18} />,
        content: <SystemSetting />,
        itemKey: 'system',
      });
      nextPanes.push({
        label: t('其他设置'),
        icon: <MoreHorizontal size={18} />,
        content: <OtherSetting />,
        itemKey: 'other',
      });
    }

    return nextPanes;
  }, [t]);

  const onChangeTab = (key) => {
    setTabActiveKey(key);
    navigate(`?tab=${key}`);
  };

  useEffect(() => {
    const searchParams = new URLSearchParams(window.location.search);
    const tab = searchParams.get('tab');
    const fallbackKey = panes[0]?.itemKey || 'operation';
    if (tab && panes.some((pane) => pane.itemKey === tab)) {
      setTabActiveKey(tab);
    } else {
      onChangeTab(fallbackKey);
    }
  }, [location.search, panes]);

  const activePane =
    panes.find((pane) => pane.itemKey === tabActiveKey) || panes[0] || null;
  const paneItems = panes.map((pane) => ({
    value: pane.itemKey,
    label: pane.label,
    icon: pane.icon,
  }));

  return (
    <div className='mt-[60px] px-2'>
      <Layout>
        <Layout.Content>
          <SelectableButtonGroup
            activeValue={tabActiveKey}
            collapsible={false}
            compact
            items={paneItems}
            layout='scroll'
            onChange={onChangeTab}
            t={t}
            variant='teal'
          />
          {activePane ? (
            <div role='region' aria-label={activePane.label}>
              {activePane.content}
            </div>
          ) : null}
        </Layout.Content>
      </Layout>
    </div>
  );
};

export default Setting;
