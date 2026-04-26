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
import { useTranslation } from 'react-i18next';
import {
  Button,
  Input,
  Modal,
  Select,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';

import { HEADER_PROFILE_GROUPS } from './headerProfile.constants.js';
import {
  buildHeaderProfilePreviewText,
  validateHeaderProfileDraft,
} from './headerProfile.helpers.js';

const { Text } = Typography;

function buildDraft(profile) {
  if (!profile) {
    return {
      id: '',
      name: '',
      category: 'custom',
      headersText: '',
      description: '',
      source: '',
    };
  }

  return {
    id: profile.id || '',
    name: profile.name || '',
    category: profile.category || 'custom',
    headersText: JSON.stringify(profile.headers || {}, null, 2),
    description: profile.description || '',
    source: profile.source || '',
  };
}

const HeaderProfileEditorModal = ({
  visible,
  saving = false,
  profile = null,
  profiles = [],
  onCancel,
  onSave,
}) => {
  const { t } = useTranslation();
  const [draft, setDraft] = useState(() => buildDraft(profile));
  const [touched, setTouched] = useState({});

  useEffect(() => {
    if (visible) {
      setDraft(buildDraft(profile));
      setTouched({});
    }
  }, [profile, visible]);

  const validation = useMemo(
    () =>
      validateHeaderProfileDraft(draft, {
        profiles,
        currentProfileId: draft.id,
      }),
    [draft, profiles],
  );
  const previewText = validation.parsedHeaders
    ? buildHeaderProfilePreviewText(validation.parsedHeaders)
    : '';
  const categoryOptions = [
    ...HEADER_PROFILE_GROUPS.map((group) => ({
      label:
        group.key === 'browser'
          ? t('浏览器')
          : group.key === 'ai_coding_cli'
            ? t('AI Coding CLI')
            : t('API SDK / 调试'),
      value: group.key,
    })),
    {
      label: t('自定义'),
      value: 'custom',
    },
  ];
  const title = profile?.id
    ? t('编辑请求头模板')
    : profile?.source === 'legacy_header_override'
      ? t('导入旧请求头覆盖')
      : t('新建请求头模板');

  return (
    <Modal
      title={title}
      visible={visible}
      width='min(760px, calc(100vw - 24px))'
      onCancel={onCancel}
      footer={
        <div className='flex justify-end gap-2'>
          <Button onClick={onCancel}>{t('取消')}</Button>
          <Button
            type='primary'
            loading={saving}
            disabled={!validation.isValid}
            onClick={() =>
              onSave({
                id: draft.id || undefined,
                name: draft.name.trim(),
                category: draft.category || 'custom',
                headers: validation.parsedHeaders || {},
                description: draft.description || '',
              })
            }
          >
            {profile?.id ? t('保存') : t('创建')}
          </Button>
        </div>
      }
    >
      <div className='flex flex-col gap-4'>
        {profile?.source === 'legacy_header_override' && (
          <div
            className='rounded-xl p-3'
            style={{
              backgroundColor: 'var(--semi-color-warning-light-default)',
              border: '1px solid var(--semi-color-warning)',
            }}
          >
            <Text>
              {t('这是从旧请求头覆盖导入的草稿，保存后会写入我的请求头模板。')}
            </Text>
          </div>
        )}

        <div className='grid grid-cols-1 md:grid-cols-2 gap-4'>
          <div className='flex flex-col gap-1'>
            <Text strong>{t('名称')}</Text>
            <Input
              value={draft.name}
              onChange={(value) => {
                setTouched((prev) => ({ ...prev, name: true }));
                setDraft((prev) => ({ ...prev, name: value }));
              }}
              placeholder={t('请输入模板名称')}
            />
            {touched.name && validation.errors.name && (
              <Text type='danger' size='small'>
                {t(validation.errors.name)}
              </Text>
            )}
          </div>

          <div className='flex flex-col gap-1'>
            <Text strong>{t('分类')}</Text>
            <Select
              value={draft.category || 'custom'}
              optionList={categoryOptions}
              onChange={(value) =>
                setDraft((prev) => ({ ...prev, category: value || 'custom' }))
              }
            />
          </div>
        </div>

        <div className='flex flex-col gap-1'>
          <Text strong>{t('请求头 JSON')}</Text>
          <TextArea
            rows={12}
            value={draft.headersText}
            onChange={(value) => {
              setTouched((prev) => ({ ...prev, headersText: true }));
              setDraft((prev) => ({
                ...prev,
                headersText: value,
              }));
            }}
            placeholder={t('请输入合法的 JSON 对象，所有值必须是字符串')}
          />
          {touched.headersText && validation.errors.headersText && (
            <Text type='danger' size='small'>
              {t(validation.errors.headersText)}
            </Text>
          )}
        </div>

        <div
          className='rounded-xl p-3'
          style={{
            backgroundColor: 'var(--semi-color-fill-0)',
            border: '1px solid var(--semi-color-fill-2)',
          }}
        >
          <div className='mb-2'>
            <Text strong>{t('预览')}</Text>
          </div>
          <pre className='mb-0 text-xs leading-5 whitespace-pre-wrap break-all max-h-56 overflow-auto'>
            {previewText || t('暂无可预览内容')}
          </pre>
        </div>
      </div>
    </Modal>
  );
};

export default HeaderProfileEditorModal;
