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

import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  Banner,
  Button,
  Col,
  Descriptions,
  Form,
  Popconfirm,
  Row,
  Spin,
  Typography,
} from '@douyinfe/semi-ui';
import {
  API,
  compareObjects,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

const { Text } = Typography;

const NUMBER_FIELDS = [
  ['log_retention_setting.run_interval_hours', '执行间隔（小时）', 1, 720],
  ['log_retention_setting.batch_size', '批大小', 1, 5000],
  ['log_retention_setting.max_batches', '单次最大批次数', 1, 1000],
  ['log_retention_setting.consume_retention_days', '消费日志保留天数', 0, 3650],
  ['log_retention_setting.error_retention_days', '错误日志保留天数', 0, 3650],
  ['log_retention_setting.system_retention_days', '系统日志保留天数', 0, 3650],
  ['log_retention_setting.manage_retention_days', '管理日志保留天数', 0, 3650],
  ['log_retention_setting.topup_retention_days', '充值日志保留天数', 0, 3650],
  ['log_retention_setting.refund_retention_days', '退款日志保留天数', 0, 3650],
  ['log_retention_setting.unknown_retention_days', '未知日志保留天数', 0, 3650],
  [
    'log_retention_setting.request_payload_retention_days',
    '请求内容保留天数',
    0,
    3650,
  ],
  [
    'log_retention_setting.response_payload_retention_days',
    '响应内容保留天数',
    0,
    3650,
  ],
  [
    'log_retention_setting.server_log_keep_files',
    '服务器日志保留文件数',
    0,
    10000,
  ],
  ['log_retention_setting.server_log_keep_days', '服务器日志保留天数', 0, 3650],
  [
    'log_retention_setting.server_log_max_total_size_mb',
    '服务器日志最大总大小（MB）',
    0,
    1048576,
  ],
];

const DATABASE_FIELDS = NUMBER_FIELDS.slice(3, 10);
const PAYLOAD_FIELDS = NUMBER_FIELDS.slice(10, 12);
const SERVER_FIELDS = NUMBER_FIELDS.slice(12);

const DEFAULT_VALUES = {
  'log_retention_setting.enabled': false,
  'log_retention_setting.run_interval_hours': 24,
  'log_retention_setting.batch_size': 500,
  'log_retention_setting.max_batches': 200,
  'log_retention_setting.consume_retention_days': 0,
  'log_retention_setting.error_retention_days': 0,
  'log_retention_setting.system_retention_days': 0,
  'log_retention_setting.manage_retention_days': 0,
  'log_retention_setting.topup_retention_days': 0,
  'log_retention_setting.refund_retention_days': 0,
  'log_retention_setting.unknown_retention_days': 0,
  'log_retention_setting.request_payload_retention_days': 0,
  'log_retention_setting.response_payload_retention_days': 0,
  'log_retention_setting.server_log_cleanup_enabled': false,
  'log_retention_setting.server_log_keep_files': 30,
  'log_retention_setting.server_log_keep_days': 30,
  'log_retention_setting.server_log_max_total_size_mb': 0,
};

const BOOLEAN_KEYS = new Set([
  'log_retention_setting.enabled',
  'log_retention_setting.server_log_cleanup_enabled',
]);

function normalizeOptions(options) {
  return Object.keys(DEFAULT_VALUES).reduce((result, key) => {
    const value = options?.[key] ?? DEFAULT_VALUES[key];
    result[key] = BOOLEAN_KEYS.has(key)
      ? value === true || value === 'true' || value === '1'
      : Number(value);
    return result;
  }, {});
}

function formatBytes(bytes) {
  if (!bytes) return '0 Bytes';
  const units = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
  const index = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    units.length - 1,
  );
  return `${Number((bytes / 1024 ** index).toFixed(2))} ${units[index]}`;
}

function Field({ name, label, min, max, disabled = false, onChange, t }) {
  return (
    <Form.InputNumber
      field={name}
      label={t(label)}
      min={min}
      max={max}
      disabled={disabled}
      extraText={
        name.includes('retention_days') || name.includes('keep_days')
          ? t('0 表示不自动清理')
          : undefined
      }
      onChange={onChange}
    />
  );
}

function ResultSummary({ result, t }) {
  if (!result) return null;
  const database = result.database_logs || [];
  const payloads = result.payloads || [];
  const server = result.server_log_files || {};
  const matches =
    database.reduce((sum, item) => sum + (item.matched || 0), 0) +
    payloads.reduce((sum, item) => sum + (item.matched || 0), 0);
  const changed =
    database.reduce((sum, item) => sum + (item.deleted || 0), 0) +
    payloads.reduce((sum, item) => sum + (item.updated || 0), 0);
  return (
    <div style={{ marginTop: 16 }}>
      <Descriptions
        data={[
          { key: t('数据库和内容匹配数'), value: matches },
          {
            key: result.preview
              ? t('服务器日志待清理数')
              : t('服务器日志已清理数'),
            value: result.preview
              ? server.matched_files || 0
              : server.deleted_files || 0,
          },
          {
            key: result.preview ? t('预计变更数') : t('已删除或清空数'),
            value: result.preview ? matches : changed,
          },
          ...(!result.preview
            ? [
                {
                  key: t('释放空间'),
                  value: formatBytes(server.freed_bytes || 0),
                },
              ]
            : []),
        ]}
      />
      {server.failed_files?.length ? (
        <Banner
          type='warning'
          description={t('{{count}} 个服务器日志文件清理失败', {
            count: server.failed_files.length,
          })}
        />
      ) : null}
    </div>
  );
}

export default function SettingsLogRetention({ options, refresh }) {
  const { t } = useTranslation();
  const refForm = useRef(null);
  const initialValues = normalizeOptions(options);
  const [inputs, setInputs] = useState(initialValues);
  const [baseline, setBaseline] = useState(initialValues);
  const [loading, setLoading] = useState(false);
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState(null);

  useEffect(() => {
    const next = normalizeOptions(options);
    setInputs(next);
    setBaseline(next);
    // Semi's Form does not read a `values` prop; only initValues or setValues
    // reach the fields. Without this the form rendered empty and every save
    // computed an empty change set, so the page could not be edited at all.
    refForm.current?.setValues(next);
  }, [options]);

  const changed = useMemo(
    () => compareObjects(inputs, baseline),
    [inputs, baseline],
  );
  const setField = (key, value) =>
    setInputs((current) => ({ ...current, [key]: value }));
  const save = async () => {
    if (!changed.length) return showWarning(t('你似乎并没有修改什么'));
    setLoading(true);
    try {
      await Promise.all(
        changed.map((item) =>
          API.put('/api/option/', {
            key: item.key,
            value: String(inputs[item.key]),
          }),
        ),
      );
      setBaseline(inputs);
      setResult(null);
      showSuccess(t('保存成功'));
      refresh();
    } catch (error) {
      showError(error?.message || t('保存失败，请重试'));
    } finally {
      setLoading(false);
    }
  };
  const run = async (preview) => {
    if (changed.length) return showError(t('请先保存日志保留策略'));
    setRunning(true);
    try {
      const response = await API.post('/api/log/retention/execute', {
        preview,
      });
      if (!response.data?.success)
        throw new Error(response.data?.message || t('日志保留执行失败'));
      setResult(response.data.data || null);
      showSuccess(t(preview ? '日志保留预览完成' : '日志保留清理完成'));
    } catch (error) {
      setResult(null);
      showError(error?.message || t('日志保留执行失败'));
    } finally {
      setRunning(false);
    }
  };

  return (
    <Spin spinning={loading}>
      <Form
        values={inputs}
        getFormApi={(formAPI) => (refForm.current = formAPI)}
        style={{ marginBottom: 15 }}
      >
        <Form.Section text={t('自动日志保留')}>
          <Banner
            type='info'
            description={t(
              '配置数据库日志、请求响应内容和服务器日志文件的自动清理策略。',
            )}
            style={{ marginBottom: 16 }}
          />
          <Row gutter={16}>
            <Col xs={24} sm={12} md={8}>
              <Form.Switch
                field='log_retention_setting.enabled'
                label={t('启用自动清理调度')}
                extraText={t('仅主节点执行自动清理')}
                checkedText={t('开')}
                uncheckedText={t('关')}
                onChange={(value) =>
                  setField('log_retention_setting.enabled', value)
                }
              />
            </Col>
            {NUMBER_FIELDS.slice(0, 3).map(([name, label, min, max]) => (
              <Col key={name} xs={24} sm={12} md={8}>
                <Field
                  name={name}
                  label={label}
                  min={min}
                  max={max}
                  onChange={(value) => setField(name, value)}
                  t={t}
                />
              </Col>
            ))}
          </Row>
          <Text strong>{t('数据库日志保留')}</Text>
          <Row gutter={16}>
            {DATABASE_FIELDS.map(([name, label, min, max]) => (
              <Col key={name} xs={24} sm={12} md={8} lg={6}>
                <Field
                  name={name}
                  label={label}
                  min={min}
                  max={max}
                  onChange={(value) => setField(name, value)}
                  t={t}
                />
              </Col>
            ))}
          </Row>
          <Text strong>{t('请求响应内容保留')}</Text>
          <Row gutter={16}>
            {PAYLOAD_FIELDS.map(([name, label, min, max]) => (
              <Col key={name} xs={24} sm={12} md={8}>
                <Field
                  name={name}
                  label={label}
                  min={min}
                  max={max}
                  onChange={(value) => setField(name, value)}
                  t={t}
                />
              </Col>
            ))}
          </Row>
          <Form.Switch
            field='log_retention_setting.server_log_cleanup_enabled'
            label={t('启用服务器日志文件清理')}
            extraText={t('当前正在写入的日志文件会自动跳过')}
            checkedText={t('开')}
            uncheckedText={t('关')}
            onChange={(value) =>
              setField(
                'log_retention_setting.server_log_cleanup_enabled',
                value,
              )
            }
          />
          <Row gutter={16}>
            {SERVER_FIELDS.map(([name, label, min, max]) => (
              <Col key={name} xs={24} sm={12} md={8}>
                <Field
                  name={name}
                  label={label}
                  min={min}
                  max={max}
                  disabled={
                    !inputs['log_retention_setting.server_log_cleanup_enabled']
                  }
                  onChange={(value) => setField(name, value)}
                  t={t}
                />
              </Col>
            ))}
          </Row>
          <Row gutter={8}>
            <Col>
              <Button onClick={save} disabled={loading}>
                {t('保存日志保留策略')}
              </Button>
            </Col>
            <Col>
              <Button
                onClick={() => run(true)}
                loading={running}
                disabled={loading}
              >
                {t('预览清理')}
              </Button>
            </Col>
            <Col>
              <Popconfirm
                title={t('确认执行日志清理？')}
                content={t('将按已保存策略永久删除日志或清空请求响应内容。')}
                onConfirm={() => run(false)}
              >
                <Button type='danger' loading={running} disabled={loading}>
                  {t('立即清理')}
                </Button>
              </Popconfirm>
            </Col>
          </Row>
          <ResultSummary result={result} t={t} />
        </Form.Section>
      </Form>
    </Spin>
  );
}
