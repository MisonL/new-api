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
import {
  API,
  showError,
  showInfo,
  showSuccess,
} from '../../../../helpers';
import {
  Banner,
  Button,
  Empty,
  Input,
  Popconfirm,
  Space,
  Spin,
  Tag,
  Typography,
  Tooltip,
} from '@douyinfe/semi-ui';
import {
  IconDelete,
  IconRefresh,
  IconSave,
  IconUpload,
} from '@douyinfe/semi-icons';
import { normalizeHeaderTemplateContent } from '../../../../helpers/headerOverrideUserAgent';

const { Text } = Typography;

const SECTION_SURFACE_STYLE = {
  backgroundColor: 'var(--semi-color-bg-0)',
  border: '1px solid var(--semi-color-border)',
  boxShadow: 'none',
};

const ITEM_SURFACE_STYLE = {
  backgroundColor: 'var(--semi-color-fill-0)',
  border: '1px solid var(--semi-color-border)',
  boxShadow: 'none',
};

function formatTemplateTime(timestamp, t) {
  const value = Number(timestamp || 0);
  if (!value) {
    return t('暂无');
  }
  return new Date(value * 1000).toLocaleString();
}

function buildTemplatePreview(content) {
  const raw = String(content || '').trim();
  if (!raw) {
    return '';
  }
  try {
    const parsed = JSON.parse(raw);
    const entries = Object.entries(parsed || {}).slice(0, 2);
    const preview = entries
      .map(([key, value]) => `${key}: ${String(value)}`)
      .join(' | ');
    if (Object.keys(parsed || {}).length > 2) {
      return `${preview} ...`;
    }
    return preview;
  } catch {
    return raw.replace(/\s+/g, ' ').slice(0, 96);
  }
}

export default function UserHeaderTemplateManager({
  t,
  value,
  visible,
  onApply,
}) {
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [templateName, setTemplateName] = useState('');
  const [templates, setTemplates] = useState([]);
  const rawTemplateDraft = typeof value === 'string' ? value.trim() : '';

  const sortedTemplates = useMemo(
    () =>
      [...templates].sort(
        (left, right) => Number(right.updated_at || 0) - Number(left.updated_at || 0),
      ),
    [templates],
  );
  const normalizedTemplateDraft = useMemo(
    () => normalizeHeaderTemplateContent(value, { allowEmpty: false }),
    [value],
  );
  const canSaveTemplate =
    templateName.trim().length > 0 && normalizedTemplateDraft.ok;
  const showTemplateValidation =
    !normalizedTemplateDraft.ok &&
    (templateName.trim().length > 0 || rawTemplateDraft.length > 0);

  const loadTemplates = async () => {
    setLoading(true);
    try {
      const res = await API.get('/api/user/header-templates');
      setTemplates(Array.isArray(res?.data?.data) ? res.data.data : []);
    } catch (error) {
      showError(error.message || t('加载模板失败'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (visible) {
      loadTemplates();
    }
  }, [visible]);

  const getNormalizedContent = () => {
    const normalized = normalizedTemplateDraft;
    if (!normalized.ok) {
      showInfo(t(normalized.message));
      return null;
    }
    return normalized.value;
  };

  const handleCreateTemplate = async () => {
    const name = templateName.trim();
    if (!name) {
      showInfo(t('模板名称不能为空'));
      return;
    }

    const content = getNormalizedContent();
    if (!content) {
      return;
    }

    setSaving(true);
    try {
      const res = await API.post('/api/user/header-templates', {
        name,
        content,
      });
      const record = res?.data?.data;
      if (!record) {
        showError(t('模板保存失败'));
        return;
      }
      setTemplateName('');
      setTemplates((prev) => [record, ...prev.filter((item) => item.id !== record.id)]);
      showSuccess(t('模板保存成功'));
    } catch (error) {
      showError(error.message || t('模板保存失败'));
    } finally {
      setSaving(false);
    }
  };

  const handleOverwriteTemplate = async (template) => {
    const content = getNormalizedContent();
    if (!content) {
      return;
    }

    setSaving(true);
    try {
      const res = await API.put(`/api/user/header-templates/${template.id}`, {
        name: template.name,
        content,
      });
      const record = res?.data?.data;
      if (!record) {
        showError(t('模板更新失败'));
        return;
      }
      setTemplates((prev) =>
        prev.map((item) => (item.id === record.id ? record : item)),
      );
      showSuccess(t('模板已覆盖'));
    } catch (error) {
      showError(error.message || t('模板更新失败'));
    } finally {
      setSaving(false);
    }
  };

  const handleDeleteTemplate = async (template) => {
    setSaving(true);
    try {
      await API.delete(`/api/user/header-templates/${template.id}`);
      setTemplates((prev) => prev.filter((item) => item.id !== template.id));
      showSuccess(t('模板已删除'));
    } catch (error) {
      showError(error.message || t('模板删除失败'));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div
      className='mt-3 rounded-lg p-3'
      style={SECTION_SURFACE_STYLE}
    >
      <div className='flex items-center justify-between gap-3 mb-3'>
        <div className='min-w-0 flex flex-col'>
          <Text strong size='small'>
            {t('请求头模板')}
          </Text>
          <Text type='tertiary' size='small'>
            {t('仅保存当前用户自己的合法 JSON 请求头模板')}
          </Text>
        </div>
        <Button
          type='tertiary'
          theme='light'
          size='small'
          icon={<IconRefresh />}
          onClick={loadTemplates}
          loading={loading}
        >
          {t('刷新')}
        </Button>
      </div>

      <div className='mb-4 flex flex-wrap items-end gap-2'>
        <Input
          value={templateName}
          placeholder={t('输入模板名称')}
          onChange={setTemplateName}
          maxLength={128}
          style={{ width: 220 }}
        />
        <Button
          type='primary'
          theme='light'
          size='small'
          icon={<IconSave />}
          onClick={handleCreateTemplate}
          loading={saving}
          disabled={!canSaveTemplate}
        >
          {t('保存为模板')}
        </Button>
      </div>
      {showTemplateValidation && (
        <Banner
          className='mb-3'
          type='warning'
          closeIcon={null}
          description={t(normalizedTemplateDraft.message)}
        />
      )}

      {loading ? (
        <div className='py-4 flex justify-center'>
          <Spin />
        </div>
      ) : sortedTemplates.length === 0 ? (
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          title={t('暂无模板')}
          description={t('保存一次当前请求头后，这里会显示可复用模板')}
        />
      ) : (
        <div className='flex flex-col gap-2'>
          {sortedTemplates.map((template) => (
            <div
              key={template.id}
              className='rounded-lg px-3 py-3'
              style={ITEM_SURFACE_STYLE}
            >
              <div className='flex flex-wrap items-start justify-between gap-3'>
                <div className='min-w-0 flex-1'>
                  <Tag color='grey' size='small'>
                    {template.name}
                  </Tag>
                  <div className='mt-2'>
                    <Tooltip content={template.content} position='topLeft'>
                      <div
                        className='rounded px-2 py-1 font-mono text-xs'
                        style={{
                          backgroundColor: 'var(--semi-color-bg-0)',
                          border: '1px solid var(--semi-color-border)',
                          color: 'var(--semi-color-text-1)',
                        }}
                      >
                        {buildTemplatePreview(template.content)}
                      </div>
                    </Tooltip>
                  </div>
                  <div className='mt-2'>
                    <Text type='tertiary' size='small'>
                      {t('更新于')} {formatTemplateTime(template.updated_at, t)}
                    </Text>
                  </div>
                </div>
                <Space spacing={6} wrap>
                  <Button
                    type='tertiary'
                    theme='light'
                    size='small'
                    icon={<IconUpload />}
                    onClick={() => onApply(template.content)}
                  >
                    {t('应用')}
                  </Button>
                  <Button
                    type='tertiary'
                    theme='light'
                    size='small'
                    icon={<IconSave />}
                    onClick={() => handleOverwriteTemplate(template)}
                    loading={saving}
                    disabled={!normalizedTemplateDraft.ok}
                  >
                    {t('覆盖')}
                  </Button>
                  <Popconfirm
                    title={t('确认删除这个模板吗？')}
                    content={t('删除后不可恢复')}
                    onConfirm={() => handleDeleteTemplate(template)}
                  >
                    <Button
                      type='danger'
                      theme='borderless'
                      size='small'
                      icon={<IconDelete />}
                    >
                      {t('删除')}
                    </Button>
                  </Popconfirm>
                </Space>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
