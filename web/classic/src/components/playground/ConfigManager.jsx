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

import React, { useRef } from 'react';
import { Button, Typography, Toast, Modal, Tag } from '@douyinfe/semi-ui';
import { Download, Upload, RotateCcw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  exportConfig,
  importConfig,
  clearConfig,
  hasStoredConfig,
  getConfigTimestamp,
} from './configStorage';

const ConfigManager = ({
  currentConfig,
  onConfigImport,
  onConfigReset,
  messages,
}) => {
  const { t } = useTranslation();
  const fileInputRef = useRef(null);

  const handleExport = () => {
    try {
      // 在导出前先保存当前配置，确保导出的是最新内容
      const configWithTimestamp = {
        ...currentConfig,
        timestamp: new Date().toISOString(),
      };
      localStorage.setItem(
        'playground_config',
        JSON.stringify(configWithTimestamp),
      );

      exportConfig(currentConfig, messages);
      Toast.success({
        content: t('配置已导出到下载文件夹'),
        duration: 3,
      });
    } catch (error) {
      Toast.error({
        content: t('导出配置失败: ') + error.message,
        duration: 3,
      });
    }
  };

  const handleImportClick = () => {
    fileInputRef.current?.click();
  };

  const handleFileChange = async (event) => {
    const file = event.target.files[0];
    if (!file) return;

    try {
      const importedConfig = await importConfig(file);

      Modal.confirm({
        title: t('确认导入配置'),
        content: t('导入的配置将覆盖当前设置，是否继续？'),
        okText: t('确定导入'),
        cancelText: t('取消'),
        onOk: () => {
          onConfigImport(importedConfig);
          Toast.success({
            content: t('配置导入成功'),
            duration: 3,
          });
        },
      });
    } catch (error) {
      Toast.error({
        content: t('导入配置失败: ') + error.message,
        duration: 3,
      });
    } finally {
      // 重置文件输入，允许重复选择同一文件
      event.target.value = '';
    }
  };

  const handleReset = () => {
    Modal.confirm({
      title: t('重置配置'),
      content: t(
        '将清除所有保存的配置并恢复默认设置，此操作不可撤销。是否继续？',
      ),
      okText: t('确定重置'),
      cancelText: t('取消'),
      okButtonProps: {
        type: 'danger',
      },
      onOk: () => {
        // 询问是否同时重置消息
        Modal.confirm({
          title: t('重置选项'),
          content: t(
            '是否同时重置对话消息？选择"是"将清空所有对话记录并恢复默认示例；选择"否"将保留当前对话记录。',
          ),
          okText: t('同时重置消息'),
          cancelText: t('仅重置配置'),
          okButtonProps: {
            type: 'danger',
          },
          onOk: () => {
            clearConfig();
            onConfigReset({ resetMessages: true });
            Toast.success({
              content: t('配置和消息已全部重置'),
              duration: 3,
            });
          },
          onCancel: () => {
            clearConfig();
            onConfigReset({ resetMessages: false });
            Toast.success({
              content: t('配置已重置，对话消息已保留'),
              duration: 3,
            });
          },
        });
      },
    });
  };

  const getConfigStatus = () => {
    if (hasStoredConfig()) {
      const timestamp = getConfigTimestamp();
      if (timestamp) {
        const date = new Date(timestamp);
        return t('上次保存: ') + date.toLocaleString();
      }
      return t('已有保存的配置');
    }
    return t('暂无保存的配置');
  };

  return (
    <div className='playground-config-manager space-y-3'>
      <div className='playground-config-status-row'>
        <div className='playground-config-status-copy'>
          <div className='playground-config-status-head'>
            <Typography.Text strong className='text-sm'>
              {t('配置草稿')}
            </Typography.Text>
            <Tag size='small' color='blue' className='!rounded-full'>
              {t('本地')}
            </Tag>
          </div>
          <Typography.Text
            className='playground-config-status-text'
            ellipsis={{ showTooltip: true }}
          >
            {getConfigStatus()}
          </Typography.Text>
        </div>
      </div>

      <div className='playground-config-actions-grid'>
        <Button
          icon={<RotateCcw size={13} />}
          size='small'
          theme='light'
          type='danger'
          onClick={handleReset}
          className='playground-config-action-button'
        >
          {t('恢复默认')}
        </Button>
        <Button
          icon={<Download size={12} />}
          size='small'
          theme='solid'
          type='primary'
          onClick={handleExport}
          className='playground-config-action-button'
        >
          {t('导出')}
        </Button>

        <Button
          icon={<Upload size={12} />}
          size='small'
          theme='outline'
          type='primary'
          onClick={handleImportClick}
          className='playground-config-action-button'
        >
          {t('导入')}
        </Button>
      </div>

      <input
        ref={fileInputRef}
        type='file'
        name='playground-config-import'
        accept='.json'
        onChange={handleFileChange}
        style={{ display: 'none' }}
      />
    </div>
  );
};

export default ConfigManager;
