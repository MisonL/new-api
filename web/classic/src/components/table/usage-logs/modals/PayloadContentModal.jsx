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

import React, { useCallback, useMemo } from 'react';
import { Modal, Descriptions, Typography, Button } from '@douyinfe/semi-ui';
import { IconDownload } from '@douyinfe/semi-icons';
import CodeViewer from '../../../playground/LazyCodeViewer';
import { showError, showSuccess } from '../../../../helpers/utils';

const { Text } = Typography;

const sanitizeFileNamePart = (value) => {
  return String(value || '')
    .trim()
    .replace(/[^\w.-]+/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-|-$/g, '');
};

const buildPayloadExportFileName = (target) => {
  const requestId = sanitizeFileNamePart(target?.requestId) || 'payload';
  const title = sanitizeFileNamePart(target?.title) || 'content';
  return `${title.toLowerCase()}-${requestId}.json`;
};

const formatPayloadForExport = (target) => {
  const rawContent = target?.content;

  if (rawContent && typeof rawContent === 'object') {
    return JSON.stringify(rawContent, null, 2);
  }

  const textContent = String(rawContent || '');
  try {
    return JSON.stringify(JSON.parse(textContent), null, 2);
  } catch (error) {
    return JSON.stringify(
      {
        title: target?.title || '',
        request_id: target?.requestId || '',
        request_path: target?.requestPath || '',
        content_type: target?.contentType || '',
        content: textContent,
      },
      null,
      2,
    );
  }
};

const PayloadContentModal = ({
  t,
  showPayloadContentModal,
  closePayloadContentModal,
  payloadContentTarget,
}) => {
  const hasContent =
    payloadContentTarget &&
    payloadContentTarget.content !== undefined &&
    payloadContentTarget.content !== null;

  const metadata = useMemo(() => {
    const target = payloadContentTarget || {};
    const rows = [];

    if (target.modelName) {
      rows.push({
        key: t('模型'),
        value: target.modelName,
      });
    }
    if (target.requestId) {
      rows.push({
        key: t('Request ID'),
        value: target.requestId,
      });
    }
    if (target.requestPath) {
      rows.push({
        key: t('请求路径'),
        value: target.requestPath,
      });
    }
    if (target.metaText) {
      rows.push({
        key: t('内容信息'),
        value: target.metaText,
      });
    }

    return rows;
  }, [payloadContentTarget, t]);

  const language = String(payloadContentTarget?.contentType || '').includes(
    'json',
  )
    ? 'json'
    : 'text';

  const handleExportJson = useCallback(() => {
    if (!hasContent) {
      showError(t('暂无内容'));
      return;
    }

    try {
      const jsonText = formatPayloadForExport(payloadContentTarget);
      const blob = new Blob([jsonText], {
        type: 'application/json;charset=utf-8',
      });
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = buildPayloadExportFileName(payloadContentTarget);
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);
      showSuccess(t('日志已下载'));
    } catch (error) {
      console.error('导出内容日志失败:', error);
      showError(t('导出日志失败'));
    }
  }, [hasContent, payloadContentTarget, t]);

  return (
    <Modal
      title={payloadContentTarget?.title || t('内容详情')}
      visible={showPayloadContentModal}
      onCancel={closePayloadContentModal}
      footer={null}
      centered
      closable
      maskClosable
      width={960}
      bodyStyle={{
        maxHeight: '80vh',
        overflow: 'auto',
        padding: 20,
      }}
    >
      <div className='usage-log-payload-modal'>
        {metadata.length > 0 ? (
          <Descriptions
            className='usage-log-payload-modal-meta'
            data={metadata}
          />
        ) : null}
        <div className='usage-log-payload-modal-actions'>
          <Button
            theme='light'
            type='primary'
            icon={<IconDownload />}
            onClick={handleExportJson}
            disabled={!hasContent}
            className='usage-log-payload-export'
          >
            {t('导出')} JSON
          </Button>
        </div>
        {hasContent ? (
          <div className='usage-log-payload-modal-code'>
            <CodeViewer
              content={payloadContentTarget.content}
              language={language}
            />
          </div>
        ) : (
          <Text type='tertiary'>{t('暂无内容')}</Text>
        )}
      </div>
    </Modal>
  );
};

export default PayloadContentModal;
