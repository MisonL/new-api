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

import React, { useEffect, useState, useRef, useMemo } from 'react';
import {
  Banner,
  Button,
  Col,
  Form,
  Row,
  Spin,
  Modal,
  Select,
  InputGroup,
  Input,
} from '@douyinfe/semi-ui';
import {
  compareObjects,
  API,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

export default function GeneralSettings(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [showQuotaWarning, setShowQuotaWarning] = useState(false);
  const [inputs, setInputs] = useState({
    TopUpLink: '',
    'general_setting.docs_link': '',
    'general_setting.responses_stream_bootstrap_recovery_enabled': false,
    'general_setting.responses_stream_bootstrap_grace_period_seconds': 180,
    'general_setting.responses_stream_bootstrap_probe_interval_milliseconds': 1000,
    'general_setting.responses_stream_bootstrap_ping_interval_seconds': 10,
    'general_setting.responses_stream_bootstrap_retryable_status_codes':
      '[401,403,408,429,500,502,503,504]',
    'general_setting.quota_display_type': 'USD',
    'general_setting.custom_currency_symbol': '¤',
    'general_setting.custom_currency_exchange_rate': '',
    QuotaPerUnit: '',
    RetryTimes: '',
    USDExchangeRate: '',
    DisplayTokenStatEnabled: false,
    DefaultCollapseSidebar: false,
    DemoSiteEnabled: false,
    SelfUseModeEnabled: false,
    'token_setting.max_user_tokens': 1000,
  });
  const refForm = useRef();
  const [inputsRow, setInputsRow] = useState(inputs);

  function handleFieldChange(fieldName) {
    return (value) => {
      setInputs((inputs) => ({ ...inputs, [fieldName]: value }));
    };
  }

  function onSubmit() {
    const updateArray = compareObjects(inputs, inputsRow);
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    const requestQueue = updateArray.map((item) => {
      let value = '';
      if (typeof inputs[item.key] === 'boolean') {
        value = String(inputs[item.key]);
      } else {
        value = inputs[item.key];
      }
      return API.put('/api/option/', {
        key: item.key,
        value,
      });
    });
    setLoading(true);
    Promise.all(requestQueue)
      .then((res) => {
        if (requestQueue.length === 1) {
          if (res.includes(undefined)) return;
        } else if (requestQueue.length > 1) {
          if (res.includes(undefined))
            return showError(t('部分保存失败，请重试'));
        }
        showSuccess(t('保存成功'));
        props.refresh();
      })
      .catch(() => {
        showError(t('保存失败，请重试'));
      })
      .finally(() => {
        setLoading(false);
      });
  }

  // 计算展示在输入框中的“1 USD = X <currency>”中的 X
  const combinedRate = useMemo(() => {
    const type = inputs['general_setting.quota_display_type'];
    if (type === 'USD') return '1';
    if (type === 'CNY') return String(inputs['USDExchangeRate'] || '');
    if (type === 'TOKENS') return String(inputs['QuotaPerUnit'] || '');
    if (type === 'CUSTOM')
      return String(
        inputs['general_setting.custom_currency_exchange_rate'] || '',
      );
    return '';
  }, [inputs]);

  const onCombinedRateChange = (val) => {
    const type = inputs['general_setting.quota_display_type'];
    if (type === 'CNY') {
      handleFieldChange('USDExchangeRate')(val);
    } else if (type === 'TOKENS') {
      handleFieldChange('QuotaPerUnit')(val);
    } else if (type === 'CUSTOM') {
      handleFieldChange('general_setting.custom_currency_exchange_rate')(val);
    }
  };

  useEffect(() => {
    const currentInputs = {};
    for (let key in props.options) {
      if (Object.keys(inputs).includes(key)) {
        currentInputs[key] = props.options[key];
      }
    }
    // 若旧字段存在且新字段缺失，则做一次兜底映射
    if (
      currentInputs['general_setting.quota_display_type'] === undefined &&
      props.options?.DisplayInCurrencyEnabled !== undefined
    ) {
      currentInputs['general_setting.quota_display_type'] = props.options
        .DisplayInCurrencyEnabled
        ? 'USD'
        : 'TOKENS';
    }
    // 回填自定义货币相关字段（如果后端已存在）
    if (props.options['general_setting.custom_currency_symbol'] !== undefined) {
      currentInputs['general_setting.custom_currency_symbol'] =
        props.options['general_setting.custom_currency_symbol'];
    }
    if (
      props.options['general_setting.custom_currency_exchange_rate'] !==
      undefined
    ) {
      currentInputs['general_setting.custom_currency_exchange_rate'] =
        props.options['general_setting.custom_currency_exchange_rate'];
    }
    setInputs(currentInputs);
    setInputsRow(structuredClone(currentInputs));
    refForm.current.setValues(currentInputs);
  }, [props.options]);

  return (
    <>
      <Spin spinning={loading}>
        <Form
          values={inputs}
          getFormApi={(formAPI) => (refForm.current = formAPI)}
          style={{ marginBottom: 15 }}
        >
          <Form.Section text={t('通用设置')}>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field={'TopUpLink'}
                  label={t('充值链接')}
                  initValue={''}
                  placeholder={t('例如发卡网站的购买链接')}
                  onChange={handleFieldChange('TopUpLink')}
                  showClear
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field={'general_setting.docs_link'}
                  label={t('文档地址')}
                  initValue={''}
                  placeholder={t('例如 https://docs.newapi.pro')}
                  onChange={handleFieldChange('general_setting.docs_link')}
                  showClear
                />
              </Col>
              {/* 单位美元额度已合入汇率组合控件（TOKENS 模式下编辑），不再单独展示 */}
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field={'RetryTimes'}
                  label={t('失败重试次数')}
                  initValue={''}
                  placeholder={t('失败重试次数')}
                  onChange={handleFieldChange('RetryTimes')}
                  showClear
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <div className='semi-form-field'>
                  <label
                    htmlFor='quota-display-rate'
                    className='semi-form-field-label semi-form-field-label-left'
                  >
                    <div className='semi-form-field-label-text'>
                      {t('站点额度展示类型及汇率')}
                    </div>
                  </label>
                  <InputGroup style={{ width: '100%' }}>
                    <Input
                      id='quota-display-rate'
                      prefix={'1 USD = '}
                      style={{ width: '50%' }}
                      value={combinedRate}
                      onChange={onCombinedRateChange}
                      disabled={
                        inputs['general_setting.quota_display_type'] === 'USD'
                      }
                      name='pages-setting-operation-settingsgeneral-input-1'
                    />
                    <Select
                      style={{ width: '50%' }}
                      value={inputs['general_setting.quota_display_type']}
                      onChange={handleFieldChange(
                        'general_setting.quota_display_type',
                      )}
                    >
                      <Select.Option value='USD'>USD ($)</Select.Option>
                      <Select.Option value='CNY'>CNY (¥)</Select.Option>
                      <Select.Option value='TOKENS'>Tokens</Select.Option>
                      <Select.Option value='CUSTOM'>
                        {t('自定义货币')}
                      </Select.Option>
                    </Select>
                  </InputGroup>
                </div>
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field={'general_setting.custom_currency_symbol'}
                  label={t('自定义货币符号')}
                  placeholder={t('例如 €, £, Rp, ₩, ₹...')}
                  onChange={handleFieldChange(
                    'general_setting.custom_currency_symbol',
                  )}
                  showClear
                  disabled={
                    inputs['general_setting.quota_display_type'] !== 'CUSTOM'
                  }
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'DisplayTokenStatEnabled'}
                  label={t('额度查询接口返回令牌额度而非用户额度')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={handleFieldChange('DisplayTokenStatEnabled')}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'DefaultCollapseSidebar'}
                  label={t('默认折叠侧边栏')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={handleFieldChange('DefaultCollapseSidebar')}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'DemoSiteEnabled'}
                  label={t('演示站点模式')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={handleFieldChange('DemoSiteEnabled')}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'SelfUseModeEnabled'}
                  label={t('自用模式')}
                  extraText={t('开启后不限制：必须设置模型倍率')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={handleFieldChange('SelfUseModeEnabled')}
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('用户最大令牌数量')}
                  field={'token_setting.max_user_tokens'}
                  step={1}
                  min={1}
                  extraText={t(
                    '每个用户最多可创建的令牌数量，默认 1000，设置过大可能会影响性能',
                  )}
                  placeholder={'1000'}
                  onChange={handleFieldChange('token_setting.max_user_tokens')}
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24}>
                <Banner
                  type='info'
                  description={t(
                    'Responses 流启动恢复仅作用于 /v1/responses 的流式请求首包前阶段。它会在短时渠道故障时通过 SSE ping 保持连接，并在恢复后继续返回真实内容；首包发出后不会跨渠道续传。',
                  )}
                  bordered
                  fullMode={false}
                  closeIcon={null}
                />
              </Col>
            </Row>
            <Row gutter={16} style={{ marginTop: 16 }}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={
                    'general_setting.responses_stream_bootstrap_recovery_enabled'
                  }
                  label={t('启用 Responses 流启动恢复')}
                  extraText={t(
                    '仅在 /v1/responses 流式请求首包前生效，用于短时故障恢复。',
                  )}
                  size='default'
                  onChange={handleFieldChange(
                    'general_setting.responses_stream_bootstrap_recovery_enabled',
                  )}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  field={
                    'general_setting.responses_stream_bootstrap_grace_period_seconds'
                  }
                  label={t('启动恢复等待窗口（秒）')}
                  min={1}
                  step={1}
                  placeholder={'180'}
                  extraText={t(
                    '在该时间窗口内持续探测可用渠道，超时后返回真实错误。',
                  )}
                  onChange={handleFieldChange(
                    'general_setting.responses_stream_bootstrap_grace_period_seconds',
                  )}
                  disabled={
                    !inputs[
                      'general_setting.responses_stream_bootstrap_recovery_enabled'
                    ]
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  field={
                    'general_setting.responses_stream_bootstrap_probe_interval_milliseconds'
                  }
                  label={t('渠道探测间隔（毫秒）')}
                  min={1}
                  step={50}
                  placeholder={'1000'}
                  extraText={t('每次重新探测可用渠道之间的等待时间。')}
                  onChange={handleFieldChange(
                    'general_setting.responses_stream_bootstrap_probe_interval_milliseconds',
                  )}
                  disabled={
                    !inputs[
                      'general_setting.responses_stream_bootstrap_recovery_enabled'
                    ]
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  field={
                    'general_setting.responses_stream_bootstrap_ping_interval_seconds'
                  }
                  label={t('SSE 保活间隔（秒）')}
                  min={1}
                  step={1}
                  placeholder={'10'}
                  extraText={t(
                    '在等待恢复期间发送 : PING，帮助客户端保持连接。',
                  )}
                  onChange={handleFieldChange(
                    'general_setting.responses_stream_bootstrap_ping_interval_seconds',
                  )}
                  disabled={
                    !inputs[
                      'general_setting.responses_stream_bootstrap_recovery_enabled'
                    ]
                  }
                />
              </Col>
              <Col xs={24} sm={24} md={16} lg={16} xl={16}>
                <Form.Input
                  field={
                    'general_setting.responses_stream_bootstrap_retryable_status_codes'
                  }
                  label={t('可触发启动恢复的状态码')}
                  placeholder={'[401,403,408,429,500,502,503,504]'}
                  extraText={t(
                    '填写 JSON 数组，例如 [401,403,429,500,502,503,504]。',
                  )}
                  onChange={handleFieldChange(
                    'general_setting.responses_stream_bootstrap_retryable_status_codes',
                  )}
                  disabled={
                    !inputs[
                      'general_setting.responses_stream_bootstrap_recovery_enabled'
                    ]
                  }
                  showClear
                />
              </Col>
            </Row>
            <Row>
              <Button size='default' onClick={onSubmit}>
                {t('保存通用设置')}
              </Button>
            </Row>
          </Form.Section>
        </Form>
      </Spin>

      <Modal
        title={t('警告')}
        visible={showQuotaWarning}
        onOk={() => setShowQuotaWarning(false)}
        onCancel={() => setShowQuotaWarning(false)}
        closeOnEsc={true}
        width={500}
      >
        <Banner
          type='warning'
          description={t(
            '此设置用于系统内部计算，默认值500000是为了精确到6位小数点设计，不推荐修改。',
          )}
          bordered
          fullMode={false}
          closeIcon={null}
        />
      </Modal>
    </>
  );
}
