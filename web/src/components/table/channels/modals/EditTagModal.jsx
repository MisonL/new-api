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

import React, { useState, useEffect, useRef, useMemo } from 'react';
import {
  API,
  showError,
  showInfo,
  showSuccess,
  showWarning,
  verifyJSON,
  selectFilter,
} from '../../../../helpers';
import {
  SideSheet,
  Space,
  Button,
  Typography,
  Spin,
  Banner,
  Card,
  Tag,
  Avatar,
  Form,
} from '@douyinfe/semi-ui';
import {
  IconSave,
  IconClose,
  IconBookmark,
  IconUser,
  IconCode,
  IconSetting,
} from '@douyinfe/semi-icons';
import { getChannelModels } from '../../../../helpers';
import {
  applyUserAgentPresetToHeaderOverride,
  buildUserAgentStrategyPayload,
  normalizeHeaderTemplateContent,
  normalizeUserAgentStrategy,
  normalizeUserAgentValues,
} from '../../../../helpers/headerOverrideUserAgent';
import { useTranslation } from 'react-i18next';
import HeaderOverrideUserAgentPresets from './HeaderOverrideUserAgentPresets';
import UserHeaderTemplateManager from './UserHeaderTemplateManager';

const { Text, Title } = Typography;

const MODEL_MAPPING_EXAMPLE = {
  'gpt-3.5-turbo': 'gpt-3.5-turbo-0125',
};

const HEADER_POLICY_MODE_OPTIONS = [
  { label: '跟随系统默认', value: 'system_default' },
  { label: '渠道优先', value: 'prefer_channel' },
  { label: '标签优先', value: 'prefer_tag' },
  { label: '合并', value: 'merge' },
];

const USER_AGENT_STRATEGY_MODE_OPTIONS = [
  { label: '轮询', value: 'round_robin' },
  { label: '随机', value: 'random' },
];

const EditTagModal = (props) => {
  const { t } = useTranslation();
  const { visible, tag, handleClose, refresh } = props;
  const [loading, setLoading] = useState(false);
  const [originModelOptions, setOriginModelOptions] = useState([]);
  const [modelOptions, setModelOptions] = useState([]);
  const [groupOptions, setGroupOptions] = useState([]);
  const [customModel, setCustomModel] = useState('');
  const [modelSearchValue, setModelSearchValue] = useState('');
  const originInputs = {
    tag: '',
    new_tag: null,
    model_mapping: null,
    groups: [],
    models: [],
    param_override: null,
    header_override: '',
    header_policy_mode: 'system_default',
    override_header_user_agent: false,
    user_agent_strategy_configured: false,
    user_agent_strategy_enabled: false,
    user_agent_strategy_mode: 'round_robin',
    user_agent_strategy_user_agents: [],
  };
  const [inputs, setInputs] = useState(originInputs);
  const [tagPolicyExists, setTagPolicyExists] = useState(false);
  const modelSearchMatchedCount = useMemo(() => {
    const keyword = modelSearchValue.trim();
    if (!keyword) {
      return modelOptions.length;
    }
    return modelOptions.reduce(
      (count, option) => count + (selectFilter(keyword, option) ? 1 : 0),
      0,
    );
  }, [modelOptions, modelSearchValue]);
  const modelSearchHintText = useMemo(() => {
    const keyword = modelSearchValue.trim();
    if (!keyword || modelSearchMatchedCount !== 0) {
      return '';
    }
    return t('未匹配到模型，按回车键可将「{{name}}」作为自定义模型名添加', {
      name: keyword,
    });
  }, [modelSearchMatchedCount, modelSearchValue, t]);
  const formApiRef = useRef(null);
  const getInitValues = () => ({ ...originInputs });

  const handleInputChange = (name, value) => {
    setInputs((inputs) => ({ ...inputs, [name]: value }));
    if (formApiRef.current) {
      formApiRef.current.setValue(name, value);
    }
    if (name === 'type') {
      let localModels = [];
      switch (value) {
        case 2:
          localModels = [
            'mj_imagine',
            'mj_variation',
            'mj_reroll',
            'mj_blend',
            'mj_upscale',
            'mj_describe',
            'mj_uploads',
          ];
          break;
        case 5:
          localModels = [
            'swap_face',
            'mj_imagine',
            'mj_video',
            'mj_edits',
            'mj_variation',
            'mj_reroll',
            'mj_blend',
            'mj_upscale',
            'mj_describe',
            'mj_zoom',
            'mj_shorten',
            'mj_modal',
            'mj_inpaint',
            'mj_custom_zoom',
            'mj_high_variation',
            'mj_low_variation',
            'mj_pan',
            'mj_uploads',
          ];
          break;
        case 36:
          localModels = ['suno_music', 'suno_lyrics'];
          break;
        case 53:
          localModels = [
            'NousResearch/Hermes-4-405B-FP8',
            'Qwen/Qwen3-235B-A22B-Thinking-2507',
            'Qwen/Qwen3-Coder-480B-A35B-Instruct-FP8',
            'Qwen/Qwen3-235B-A22B-Instruct-2507',
            'zai-org/GLM-4.5-FP8',
            'openai/gpt-oss-120b',
            'deepseek-ai/DeepSeek-R1-0528',
            'deepseek-ai/DeepSeek-R1',
            'deepseek-ai/DeepSeek-V3-0324',
            'deepseek-ai/DeepSeek-V3.1',
          ];
          break;
        default:
          localModels = getChannelModels(value);
          break;
      }
      if (inputs.models.length === 0) {
        setInputs((inputs) => ({ ...inputs, models: localModels }));
      }
    }
  };

  const applyHeaderOverrideUserAgentPreset = (preset) => {
    const result = applyUserAgentPresetToHeaderOverride(
      inputs.header_override,
      preset.ua,
    );

    if (!result.ok) {
      showInfo(t(result.message));
      return;
    }

    handleInputChange('header_override', result.value);
  };

  const handleUserAgentStrategyListChange = (value) => {
    const normalized = normalizeUserAgentValues(value);
    handleInputChange('user_agent_strategy_user_agents', normalized);
    if (normalized.length > 0 || inputs.user_agent_strategy_enabled) {
      handleInputChange('user_agent_strategy_configured', true);
    }
  };

  const appendUserAgentStrategyPreset = (preset) => {
    const normalized = normalizeUserAgentValues([
      ...(inputs.user_agent_strategy_user_agents || []),
      preset.ua,
    ]);
    handleInputChange('user_agent_strategy_user_agents', normalized);
    handleInputChange('user_agent_strategy_enabled', true);
    handleInputChange('user_agent_strategy_configured', true);
  };

  const clearTagHeaderPolicyDraft = () => {
    handleInputChange('header_override', '');
    handleInputChange('header_policy_mode', 'system_default');
    handleInputChange('override_header_user_agent', false);
    handleInputChange('user_agent_strategy_configured', false);
    handleInputChange('user_agent_strategy_enabled', false);
    handleInputChange('user_agent_strategy_mode', 'round_robin');
    handleInputChange('user_agent_strategy_user_agents', []);
  };

  const fetchModels = async () => {
    try {
      let res = await API.get(`/api/channel/models`);
      let localModelOptions = res.data.data.map((model) => ({
        label: model.id,
        value: model.id,
      }));
      setOriginModelOptions(localModelOptions);
    } catch (error) {
      showError(error.message);
    }
  };

  const fetchGroups = async () => {
    try {
      let res = await API.get(`/api/group/`);
      if (res === undefined) {
        return;
      }
      setGroupOptions(
        res.data.data.map((group) => ({
          label: group,
          value: group,
        })),
      );
    } catch (error) {
      showError(error.message);
    }
  };

  const fetchTagHeaderPolicy = async () => {
    if (!tag) {
      return;
    }
    const res = await API.get(
      `/api/channel/tag-policy?tag=${encodeURIComponent(tag)}`,
    );
    const policy = res?.data?.data || {};
    const rawUserAgentStrategy =
      policy.ua_strategy &&
      typeof policy.ua_strategy === 'object' &&
      !Array.isArray(policy.ua_strategy)
        ? policy.ua_strategy
        : null;
    const normalizedUserAgentStrategy = normalizeUserAgentStrategy(
      rawUserAgentStrategy,
    );
    setTagPolicyExists(policy.exists === true);
    setInputs((prev) => ({
      ...prev,
      header_override: policy.header_override || '',
      header_policy_mode: policy.header_policy_mode || 'system_default',
      override_header_user_agent: policy.override_header_user_agent === true,
      user_agent_strategy_configured: rawUserAgentStrategy !== null,
      user_agent_strategy_enabled: rawUserAgentStrategy?.enabled === true,
      user_agent_strategy_mode:
        normalizedUserAgentStrategy?.mode ||
        String(rawUserAgentStrategy?.mode || '').trim() ||
        'round_robin',
      user_agent_strategy_user_agents: normalizeUserAgentValues(
        rawUserAgentStrategy?.user_agents || rawUserAgentStrategy?.userAgents || [],
      ),
    }));
  };

  const handleSave = async (values) => {
    setLoading(true);
    const formVals = values || formApiRef.current?.getValues() || {};
    const bulkData = { tag };
    if (formVals.model_mapping) {
      if (!verifyJSON(formVals.model_mapping)) {
        showInfo('模型映射必须是合法的 JSON 格式！');
        setLoading(false);
        return;
      }
      bulkData.model_mapping = formVals.model_mapping;
    }
    if (formVals.groups && formVals.groups.length > 0) {
      bulkData.groups = formVals.groups.join(',');
    }
    if (formVals.models && formVals.models.length > 0) {
      bulkData.models = formVals.models.join(',');
    }
    if (
      formVals.param_override !== undefined &&
      formVals.param_override !== null
    ) {
      if (typeof formVals.param_override !== 'string') {
        showInfo('参数覆盖必须是合法的 JSON 格式！');
        setLoading(false);
        return;
      }
      const trimmedParamOverride = formVals.param_override.trim();
      if (trimmedParamOverride !== '' && !verifyJSON(trimmedParamOverride)) {
        showInfo('参数覆盖必须是合法的 JSON 格式！');
        setLoading(false);
        return;
      }
      bulkData.param_override = trimmedParamOverride;
    }
    const requestedTag = String(formVals.new_tag ?? '').trim();
    const nextTag = requestedTag === '' ? '' : requestedTag || tag;
    if (requestedTag !== tag) {
      bulkData.new_tag = requestedTag;
    }

    const normalizedHeaderOverride = normalizeHeaderTemplateContent(
      formVals.header_override,
      {
        allowEmpty: true,
      },
    );
    if (!normalizedHeaderOverride.ok) {
      showInfo(t(normalizedHeaderOverride.message));
      setLoading(false);
      return;
    }

    const userAgentStrategyPayload = buildUserAgentStrategyPayload({
      configured: formVals.user_agent_strategy_configured,
      enabled: formVals.user_agent_strategy_enabled,
      mode: formVals.user_agent_strategy_mode,
      userAgents: formVals.user_agent_strategy_user_agents,
    });
    if (!userAgentStrategyPayload.ok) {
      showInfo(t(userAgentStrategyPayload.message));
      setLoading(false);
      return;
    }

    const headerPolicyMode = formVals.header_policy_mode || 'system_default';
    const overrideHeaderUserAgent =
      formVals.override_header_user_agent === true;
    const shouldPersistPolicy =
      normalizedHeaderOverride.value !== '' ||
      headerPolicyMode !== 'system_default' ||
      overrideHeaderUserAgent ||
      userAgentStrategyPayload.value !== null;
    if (nextTag === '' && shouldPersistPolicy) {
      showInfo(t('清空标签前请先清空标签级请求头策略'));
      setLoading(false);
      return;
    }
    if (
      bulkData.model_mapping === undefined &&
      bulkData.groups === undefined &&
      bulkData.models === undefined &&
      bulkData.new_tag === undefined &&
      bulkData.param_override === undefined &&
      !shouldPersistPolicy &&
      !tagPolicyExists
    ) {
      showWarning('没有任何修改！');
      setLoading(false);
      return;
    }

    try {
      if (
        bulkData.model_mapping !== undefined ||
        bulkData.groups !== undefined ||
        bulkData.models !== undefined ||
        bulkData.new_tag !== undefined ||
        bulkData.param_override !== undefined
      ) {
        const bulkRes = await API.put('/api/channel/tag', bulkData);
        if (!bulkRes?.data?.success) {
          throw new Error(bulkRes?.data?.message || '标签更新失败');
        }
      }

      if (shouldPersistPolicy) {
        const policyRes = await API.put('/api/channel/tag-policy', {
          tag: nextTag,
          header_override: normalizedHeaderOverride.value,
          header_policy_mode: headerPolicyMode,
          override_header_user_agent: overrideHeaderUserAgent,
          ua_strategy: userAgentStrategyPayload.value,
        });
        if (!policyRes?.data?.success) {
          throw new Error(policyRes?.data?.message || '标签请求头策略保存失败');
        }
      }

      if (tagPolicyExists && (!shouldPersistPolicy || nextTag !== tag)) {
        const deleteRes = await API.delete(
          `/api/channel/tag-policy?tag=${encodeURIComponent(tag)}`,
        );
        if (!deleteRes?.data?.success) {
          throw new Error(deleteRes?.data?.message || '标签请求头策略删除失败');
        }
      }

      showSuccess('标签更新成功！');
      refresh();
      handleClose();
    } catch (error) {
      showError(error.message || error);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    let localModelOptions = [...originModelOptions];
    inputs.models.forEach((model) => {
      if (!localModelOptions.find((option) => option.label === model)) {
        localModelOptions.push({
          label: model,
          value: model,
        });
      }
    });
    setModelOptions(localModelOptions);
  }, [originModelOptions, inputs.models]);

  useEffect(() => {
    const fetchTagData = async () => {
      if (!tag) return;
      setLoading(true);
      try {
        const [res] = await Promise.all([
          API.get(`/api/channel/tag/models?tag=${encodeURIComponent(tag)}`),
          fetchTagHeaderPolicy(),
        ]);
        if (res?.data?.success) {
          const models = res.data.data ? res.data.data.split(',') : [];
          handleInputChange('models', models);
        } else {
          showError(res.data.message);
        }
      } catch (error) {
        showError(error.message);
      } finally {
        setLoading(false);
      }
    };

    fetchModels().then();
    fetchGroups().then();
    fetchTagData().then();
    setModelSearchValue('');
    if (formApiRef.current) {
      formApiRef.current.setValues({
        ...getInitValues(),
        tag: tag,
        new_tag: tag,
      });
    }

    setInputs({
      ...originInputs,
      tag: tag,
      new_tag: tag,
    });
    setTagPolicyExists(false);
  }, [visible, tag]);

  useEffect(() => {
    if (formApiRef.current) {
      formApiRef.current.setValues(inputs);
    }
  }, [inputs]);

  const addCustomModels = () => {
    if (customModel.trim() === '') return;
    const modelArray = customModel.split(',').map((model) => model.trim());

    let localModels = [...inputs.models];
    let localModelOptions = [...modelOptions];
    const addedModels = [];

    modelArray.forEach((model) => {
      if (model && !localModels.includes(model)) {
        localModels.push(model);
        localModelOptions.push({
          key: model,
          text: model,
          value: model,
        });
        addedModels.push(model);
      }
    });

    setModelOptions(localModelOptions);
    setCustomModel('');
    handleInputChange('models', localModels);

    if (addedModels.length > 0) {
      showSuccess(
        t('已新增 {{count}} 个模型：{{list}}', {
          count: addedModels.length,
          list: addedModels.join(', '),
        }),
      );
    } else {
      showInfo(t('未发现新增模型'));
    }
  };

  return (
    <SideSheet
      placement='right'
      title={
        <Space>
          <Tag color='blue' shape='circle'>
            {t('编辑')}
          </Tag>
          <Title heading={4} className='m-0'>
            {t('编辑标签')}
          </Title>
        </Space>
      }
      bodyStyle={{ padding: '0' }}
      visible={visible}
      width={600}
      onCancel={handleClose}
      footer={
        <div className='flex justify-end bg-white'>
          <Space>
            <Button
              theme='solid'
              onClick={() => formApiRef.current?.submitForm()}
              loading={loading}
              icon={<IconSave />}
            >
              {t('保存')}
            </Button>
            <Button
              theme='light'
              type='primary'
              onClick={handleClose}
              icon={<IconClose />}
            >
              {t('取消')}
            </Button>
          </Space>
        </div>
      }
      closeIcon={null}
    >
      <Form
        key={tag || 'edit'}
        initValues={getInitValues()}
        getFormApi={(api) => (formApiRef.current = api)}
        onSubmit={handleSave}
      >
        {() => (
          <Spin spinning={loading}>
            <div className='p-2'>
              <Card className='!rounded-2xl shadow-sm border-0 mb-6'>
                {/* Header: Tag Info */}
                <div className='flex items-center mb-2'>
                  <Avatar size='small' color='blue' className='mr-2 shadow-md'>
                    <IconBookmark size={16} />
                  </Avatar>
                  <div>
                    <Text className='text-lg font-medium'>{t('标签信息')}</Text>
                    <div className='text-xs text-gray-600'>
                      {t('标签的基本配置')}
                    </div>
                  </div>
                </div>

                <Banner
                  type='warning'
                  description={t('所有编辑均为覆盖操作，留空则不更改')}
                  className='!rounded-lg mb-4'
                />

                <div className='space-y-4'>
                  <Form.Input
                    field='new_tag'
                    label={t('标签名称')}
                    placeholder={t('请输入新标签，留空则解散标签')}
                    onChange={(value) => handleInputChange('new_tag', value)}
                  />
                </div>
              </Card>

              <Card className='!rounded-2xl shadow-sm border-0 mb-6'>
                {/* Header: Model Config */}
                <div className='flex items-center mb-2'>
                  <Avatar
                    size='small'
                    color='purple'
                    className='mr-2 shadow-md'
                  >
                    <IconCode size={16} />
                  </Avatar>
                  <div>
                    <Text className='text-lg font-medium'>{t('模型配置')}</Text>
                    <div className='text-xs text-gray-600'>
                      {t('模型选择和映射设置')}
                    </div>
                  </div>
                </div>

                <div className='space-y-4'>
                  <Banner
                    type='info'
                    description={t(
                      '当前模型列表为该标签下所有渠道模型列表最长的一个，并非所有渠道的并集，请注意可能导致某些渠道模型丢失。',
                    )}
                    className='!rounded-lg mb-4'
                  />
                  <Form.Select
                    field='models'
                    label={t('模型')}
                    placeholder={t('请选择该渠道所支持的模型，留空则不更改')}
                    multiple
                    filter={selectFilter}
                    allowCreate
                    autoClearSearchValue={false}
                    searchPosition='dropdown'
                    optionList={modelOptions}
                    onSearch={(value) => setModelSearchValue(value)}
                    innerBottomSlot={
                      modelSearchHintText ? (
                        <Text className='px-3 py-2 block text-xs !text-semi-color-text-2'>
                          {modelSearchHintText}
                        </Text>
                      ) : null
                    }
                    style={{ width: '100%' }}
                    onChange={(value) => handleInputChange('models', value)}
                  />

                  <Form.Input
                    field='custom_model'
                    label={t('自定义模型名称')}
                    placeholder={t('输入自定义模型名称')}
                    onChange={(value) => setCustomModel(value.trim())}
                    suffix={
                      <Button
                        size='small'
                        type='primary'
                        onClick={addCustomModels}
                      >
                        {t('填入')}
                      </Button>
                    }
                  />

                  <Form.TextArea
                    field='model_mapping'
                    label={t('模型重定向')}
                    placeholder={t(
                      '此项可选，用于修改请求体中的模型名称，为一个 JSON 字符串，键为请求中模型名称，值为要替换的模型名称，留空则不更改',
                    )}
                    autosize
                    onChange={(value) =>
                      handleInputChange('model_mapping', value)
                    }
                    extraText={
                      <Space>
                        <Text
                          className='!text-semi-color-primary cursor-pointer'
                          onClick={() =>
                            handleInputChange(
                              'model_mapping',
                              JSON.stringify(MODEL_MAPPING_EXAMPLE, null, 2),
                            )
                          }
                        >
                          {t('填入模板')}
                        </Text>
                        <Text
                          className='!text-semi-color-primary cursor-pointer'
                          onClick={() =>
                            handleInputChange(
                              'model_mapping',
                              JSON.stringify({}, null, 2),
                            )
                          }
                        >
                          {t('清空重定向')}
                        </Text>
                        <Text
                          className='!text-semi-color-primary cursor-pointer'
                          onClick={() => handleInputChange('model_mapping', '')}
                        >
                          {t('不更改')}
                        </Text>
                      </Space>
                    }
                  />
                </div>
              </Card>

              <Card className='!rounded-2xl shadow-sm border-0 mb-6'>
                {/* Header: Advanced Settings */}
                <div className='flex items-center mb-2'>
                  <Avatar
                    size='small'
                    color='orange'
                    className='mr-2 shadow-md'
                  >
                    <IconSetting size={16} />
                  </Avatar>
                  <div>
                    <Text className='text-lg font-medium'>
                      {t('标签级请求头策略')}
                    </Text>
                    <div className='text-xs text-gray-600'>
                      {t('这里配置的是标签运行时请求头策略，不再是批量写入渠道字段')}
                    </div>
                  </div>
                </div>

                <div className='space-y-4'>
                  <Form.TextArea
                    field='param_override'
                    label={t('参数覆盖')}
                    placeholder={
                      t('此项可选，用于覆盖请求参数。不支持覆盖 stream 参数') +
                      '\n' +
                      t('旧格式（直接覆盖）：') +
                      '\n{\n  "temperature": 0,\n  "max_tokens": 1000\n}' +
                      '\n\n' +
                      t('新格式（支持条件判断与json自定义）：') +
                      '\n{\n  "operations": [\n    {\n      "path": "temperature",\n      "mode": "set",\n      "value": 0.7,\n      "conditions": [\n        {\n          "path": "model",\n          "mode": "prefix",\n          "value": "gpt"\n        }\n      ]\n    }\n  ]\n}'
                    }
                    autosize
                    showClear
                    onChange={(value) =>
                      handleInputChange('param_override', value)
                    }
                    extraText={
                      <div className='flex gap-2 flex-wrap'>
                        <Text
                          className='!text-semi-color-primary cursor-pointer'
                          onClick={() =>
                            handleInputChange(
                              'param_override',
                              JSON.stringify({ temperature: 0 }, null, 2),
                            )
                          }
                        >
                          {t('旧格式模板')}
                        </Text>
                        <Text
                          className='!text-semi-color-primary cursor-pointer'
                          onClick={() =>
                            handleInputChange(
                              'param_override',
                              JSON.stringify(
                                {
                                  operations: [
                                    {
                                      path: 'temperature',
                                      mode: 'set',
                                      value: 0.7,
                                      conditions: [
                                        {
                                          path: 'model',
                                          mode: 'prefix',
                                          value: 'gpt',
                                        },
                                      ],
                                      logic: 'AND',
                                    },
                                  ],
                                },
                                null,
                                2,
                              ),
                            )
                          }
                        >
                          {t('新格式模板')}
                        </Text>
                        <Text
                          className='!text-semi-color-primary cursor-pointer'
                          onClick={() =>
                            handleInputChange('param_override', null)
                          }
                        >
                          {t('不更改')}
                        </Text>
                      </div>
                    }
                  />

                  <Form.TextArea
                    field='header_override'
                    label={t('请求头覆盖')}
                    placeholder={
                      t('此项可选，用于覆盖请求头参数') +
                      '\n' +
                      t('格式示例：') +
                      '\n{\n  "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36 Edg/139.0.0.0",\n  "Authorization": "Bearer {api_key}"\n}'
                    }
                    autosize
                    showClear
                    onChange={(value) =>
                      handleInputChange('header_override', value)
                    }
                    extraText={
                      <div className='flex flex-col gap-1'>
                        <div className='flex gap-2 flex-wrap items-center'>
                          <Text
                            className='!text-semi-color-primary cursor-pointer'
                            onClick={() =>
                              handleInputChange(
                                'header_override',
                                JSON.stringify(
                                  {
                                    'User-Agent':
                                      'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36 Edg/139.0.0.0',
                                    Authorization: 'Bearer {api_key}',
                                  },
                                  null,
                                  2,
                                ),
                              )
                            }
                          >
                            {t('填入模板')}
                          </Text>
                          <Text
                            className='!text-semi-color-primary cursor-pointer'
                            onClick={() =>
                              handleInputChange('header_override', '')
                            }
                          >
                            {t('不更改')}
                          </Text>
                          <Text
                            className='!text-semi-color-primary cursor-pointer'
                            onClick={() => handleInputChange('header_override', '')}
                          >
                            {t('清空')}
                          </Text>
                        </div>
                        <HeaderOverrideUserAgentPresets
                          t={t}
                          onSelect={applyHeaderOverrideUserAgentPreset}
                        />
                        <div>
                          <Text type='tertiary' size='small'>
                            {t('支持变量：')}
                          </Text>
                          <div className='text-xs text-tertiary ml-2'>
                            <div>
                              {t('渠道密钥')}: {'{api_key}'}
                            </div>
                          </div>
                        </div>
                      </div>
                    }
                  />
                  <div
                    className='mt-3 rounded-xl p-3'
                    style={{
                      backgroundColor: 'var(--semi-color-fill-0)',
                      border: '1px solid var(--semi-color-fill-2)',
                    }}
                  >
                    <div className='flex items-start justify-between gap-3 mb-3'>
                      <div>
                        <Text strong>{t('User-Agent 策略')}</Text>
                        <div className='mt-1'>
                          <Text type='tertiary' size='small'>
                            {t(
                              '标签策略会在运行时参与与渠道策略的优先级决策，并影响最终发往上游的 User-Agent',
                            )}
                          </Text>
                        </div>
                      </div>
                      <Text
                        className='!text-semi-color-primary cursor-pointer'
                        onClick={clearTagHeaderPolicyDraft}
                      >
                        {t('清空标签策略')}
                      </Text>
                    </div>
                    <div className='grid gap-3 md:grid-cols-2'>
                      <Form.Switch
                        field='user_agent_strategy_enabled'
                        label={t('启用 UA 策略')}
                        checkedText={t('开启')}
                        uncheckedText={t('关闭')}
                        initValue={false}
                        onChange={(checked) => {
                          handleInputChange('user_agent_strategy_enabled', checked);
                          if (
                            checked ||
                            (inputs.user_agent_strategy_user_agents || []).length > 0
                          ) {
                            handleInputChange('user_agent_strategy_configured', true);
                          }
                        }}
                      />
                      <Form.Switch
                        field='override_header_user_agent'
                        label={t('覆盖静态 User-Agent')}
                        checkedText={t('覆盖')}
                        uncheckedText={t('保留静态值')}
                        initValue={false}
                        onChange={(checked) =>
                          handleInputChange('override_header_user_agent', checked)
                        }
                      />
                      <Form.Select
                        field='header_policy_mode'
                        label={t('请求头优先级')}
                        optionList={HEADER_POLICY_MODE_OPTIONS.map((item) => ({
                          ...item,
                          label: t(item.label),
                        }))}
                        initValue='system_default'
                        onChange={(value) =>
                          handleInputChange('header_policy_mode', value)
                        }
                      />
                      <Form.Select
                        field='user_agent_strategy_mode'
                        label={t('UA 策略模式')}
                        optionList={USER_AGENT_STRATEGY_MODE_OPTIONS.map((item) => ({
                          ...item,
                          label: t(item.label),
                        }))}
                        initValue='round_robin'
                        disabled={!inputs.user_agent_strategy_enabled}
                        onChange={(value) => {
                          handleInputChange('user_agent_strategy_mode', value);
                          handleInputChange('user_agent_strategy_configured', true);
                        }}
                      />
                    </div>
                    <div className='mt-3'>
                      <Form.TagInput
                        field='user_agent_strategy_user_agents'
                        label={t('User-Agent 列表')}
                        placeholder={t('输入 UA，按回车或逗号可追加多个')}
                        addOnBlur
                        showClear
                        disabled={!inputs.user_agent_strategy_enabled}
                        onChange={handleUserAgentStrategyListChange}
                        style={{ width: '100%' }}
                      />
                    </div>
                    <div className='mt-3'>
                      <HeaderOverrideUserAgentPresets
                        t={t}
                        onSelect={appendUserAgentStrategyPreset}
                      />
                    </div>
                  </div>
                  <UserHeaderTemplateManager
                    t={t}
                    value={inputs.header_override}
                    visible={visible}
                    onApply={(content) => handleInputChange('header_override', content)}
                  />
                </div>
              </Card>

              <Card className='!rounded-2xl shadow-sm border-0'>
                {/* Header: Group Settings */}
                <div className='flex items-center mb-2'>
                  <Avatar size='small' color='green' className='mr-2 shadow-md'>
                    <IconUser size={16} />
                  </Avatar>
                  <div>
                    <Text className='text-lg font-medium'>{t('分组设置')}</Text>
                    <div className='text-xs text-gray-600'>
                      {t('用户分组配置')}
                    </div>
                  </div>
                </div>

                <div className='space-y-4'>
                  <Form.Select
                    field='groups'
                    label={t('分组')}
                    placeholder={t('请选择可以使用该渠道的分组，留空则不更改')}
                    multiple
                    allowAdditions
                    additionLabel={t(
                      '请在系统设置页面编辑分组倍率以添加新的分组：',
                    )}
                    optionList={groupOptions}
                    style={{ width: '100%' }}
                    onChange={(value) => handleInputChange('groups', value)}
                  />
                </div>
              </Card>
            </div>
          </Spin>
        )}
      </Form>
    </SideSheet>
  );
};

export default EditTagModal;
