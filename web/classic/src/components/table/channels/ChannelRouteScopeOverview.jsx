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

import React, { useCallback, useEffect, useState } from 'react';
import { Banner, Button, Card, Spin, Tag, Toast } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { API } from '../../../helpers';

const ChannelRouteScopeOverview = ({ t }) => {
  const [scopes, setScopes] = useState([]);
  const [loading, setLoading] = useState(false);

  const loadScopes = useCallback(async () => {
    setLoading(true);
    try {
      const response = await API.get('/api/channel-route-scopes');
      const data = response?.data?.data;
      if (!response?.data?.success || !Array.isArray(data)) {
        throw new Error(response?.data?.message || t('路由 Scope 加载失败'));
      }
      setScopes(data);
    } catch (error) {
      Toast.error(error?.message || t('路由 Scope 加载失败'));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void loadScopes();
  }, [loadScopes]);

  return (
    <Card
      title={t('路由 Scope')}
      headerExtraContent={
        <Button
          theme='borderless'
          icon={<IconRefresh />}
          onClick={() => void loadScopes()}
          loading={loading}
        >
          {t('刷新')}
        </Button>
      }
      style={{ marginBottom: 12 }}
    >
      {loading && scopes.length === 0 ? (
        <Spin size='small' />
      ) : scopes.length === 0 ? (
        <Banner type='info' description={t('暂无路由 Scope')} />
      ) : (
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
          {scopes.slice(0, 24).map((scope) => (
            <Tag key={scope.id} color='blue' size='large'>
              {scope.group} / {scope.effective_route_model} / P
              {scope.route_priority} ({scope.total_weight_bps} {t('bps')})
            </Tag>
          ))}
          {scopes.length > 24 ? (
            <Tag color='grey'>
              {t('还有 {{count}} 个 Scope', { count: scopes.length - 24 })}
            </Tag>
          ) : null}
        </div>
      )}
    </Card>
  );
};

export default ChannelRouteScopeOverview;
