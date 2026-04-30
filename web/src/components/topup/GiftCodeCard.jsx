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

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  API,
  copy,
  showError,
  showSuccess,
  getQuotaPerUnit,
} from '../../helpers';
import {
  displayAmountToQuota,
  quotaToDisplayAmount,
} from '../../helpers/quota';
import {
  Avatar,
  Button,
  Card,
  DatePicker,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Tag,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { Gift, Link2, Send, Inbox } from 'lucide-react';

const { Text } = Typography;

const receiverTypeOptions = (t) => [
  { value: '', label: t('不绑定接收人') },
  { value: 'username', label: t('绑定用户名') },
  { value: 'id', label: t('绑定用户 ID') },
  { value: 'email', label: t('绑定邮箱') },
];

const getGiftCodeStatus = (t, giftCode) => {
  const now = Math.floor(Date.now() / 1000);
  if (giftCode.status === 3) return { color: 'green', text: t('已接收') };
  if (giftCode.status === 2) return { color: 'red', text: t('已禁用') };
  if (giftCode.expired_time && giftCode.expired_time < now) {
    return { color: 'red', text: t('已过期') };
  }
  return { color: 'blue', text: t('待接收') };
};

const buildGiftCodeLink = (code) =>
  `${window.location.origin}/console/topup?gift_code=${encodeURIComponent(code)}`;

const toUnixSeconds = (value) => {
  if (!value) return 0;
  if (value instanceof Date) return Math.floor(value.getTime() / 1000);
  const time = Date.parse(value);
  return Number.isNaN(time) ? -1 : Math.floor(time / 1000);
};

const GiftCodeCard = ({
  t,
  renderQuota,
  reloadUserQuota,
  initialGiftCode,
  onGiftCodeHandled,
}) => {
  const [amount, setAmount] = useState(
    Number(quotaToDisplayAmount(getQuotaPerUnit()).toFixed(6)),
  );
  const [receiverType, setReceiverType] = useState('');
  const [receiverValue, setReceiverValue] = useState('');
  const [expiredTime, setExpiredTime] = useState(null);
  const [message, setMessage] = useState('');
  const [giftCodes, setGiftCodes] = useState([]);
  const [loading, setLoading] = useState(false);
  const [receiveVisible, setReceiveVisible] = useState(false);
  const [receiveLoading, setReceiveLoading] = useState(false);
  const [receiveInfo, setReceiveInfo] = useState(null);
  const [thankMessage, setThankMessage] = useState('');
  const [manualCopyLink, setManualCopyLink] = useState('');

  const minAmount = useMemo(
    () => Number(quotaToDisplayAmount(getQuotaPerUnit()).toFixed(6)),
    [],
  );

  const fetchGiftCodes = useCallback(async () => {
    try {
      const res = await API.get('/api/user/gift_codes');
      const { success, message: msg, data } = res.data;
      if (success) {
        setGiftCodes(data || []);
      } else {
        showError(msg);
      }
    } catch (error) {
      showError(t('获取礼品码列表失败'));
    }
  }, [t]);

  const openReceiveGiftCode = useCallback(
    async (code) => {
      if (!code) return false;
      setReceiveLoading(true);
      try {
        const res = await API.get(`/api/user/gift_codes/${code}`);
        const { success, message: msg, data } = res.data;
        if (success) {
          setReceiveInfo(data);
          setThankMessage('');
          setReceiveVisible(true);
          return true;
        } else {
          showError(msg);
        }
      } catch (error) {
        showError(t('获取礼品码失败'));
      } finally {
        setReceiveLoading(false);
      }
      return false;
    },
    [t],
  );

  useEffect(() => {
    void fetchGiftCodes();
  }, [fetchGiftCodes]);

  useEffect(() => {
    if (!initialGiftCode) return;
    void openReceiveGiftCode(initialGiftCode).then((opened) => {
      if (opened) {
        onGiftCodeHandled?.();
      }
    });
  }, [initialGiftCode, onGiftCodeHandled, openReceiveGiftCode]);

  const copyGiftCodeLink = useCallback(async (link) => {
    try {
      return await copy(link);
    } catch (copyError) {
      return false;
    }
  }, []);

  const resetGiftCodeForm = useCallback(() => {
    setReceiverType('');
    setReceiverValue('');
    setExpiredTime(null);
    setMessage('');
    setAmount(minAmount);
    setManualCopyLink('');
  }, [minAmount]);

  const createGiftCode = async () => {
    const quota = displayAmountToQuota(amount);
    if (quota <= 0 || quota < getQuotaPerUnit()) {
      showError(t('礼品码额度不能低于最低划转额度'));
      return;
    }
    if (receiverType && !receiverValue.trim()) {
      showError(t('请填写绑定用户'));
      return;
    }
    const expiredAt = toUnixSeconds(expiredTime);
    if (expiredAt < 0) {
      showError(t('礼品码有效期格式不正确'));
      return;
    }
    setLoading(true);
    try {
      const res = await API.post('/api/user/gift_codes', {
        quota,
        receiver_bind_type: receiverType,
        receiver_bind_value: receiverValue.trim(),
        expired_time: expiredAt,
        message: message.trim(),
      });
      const { success, message: msg, data } = res.data;
      if (success) {
        const link = buildGiftCodeLink(data.code);
        const copied = await copyGiftCodeLink(link);
        if (copied) {
          showSuccess(t('礼品码已生成，专用链接已复制'));
          resetGiftCodeForm();
        } else {
          setManualCopyLink(link);
          showSuccess(t('礼品码已生成'));
          showError(t('复制失败，请手动复制'));
        }
        void fetchGiftCodes();
        reloadUserQuota?.();
      } else {
        showError(msg);
      }
    } catch (error) {
      showError(t('生成礼品码失败'));
    } finally {
      setLoading(false);
    }
  };

  const receiveGiftCode = async () => {
    if (!receiveInfo?.code) return;
    setReceiveLoading(true);
    try {
      const res = await API.post(
        `/api/user/gift_codes/${receiveInfo.code}/receive`,
        { thank_message: thankMessage.trim() },
      );
      const { success, message: msg, data } = res.data;
      if (success) {
        showSuccess(t('礼品码接收成功'));
        setReceiveInfo(data);
        setReceiveVisible(false);
        void fetchGiftCodes();
        reloadUserQuota?.();
      } else {
        showError(msg);
      }
    } catch (error) {
      showError(t('接收礼品码失败'));
    } finally {
      setReceiveLoading(false);
    }
  };

  return (
    <Card className='!rounded-2xl shadow-sm border-0'>
      <div className='flex items-center mb-4'>
        <Avatar size='small' color='amber' className='mr-3 shadow-md'>
          <Gift size={16} />
        </Avatar>
        <div>
          <Typography.Text className='text-lg font-medium'>
            {t('礼品码')}
          </Typography.Text>
          <div className='text-xs'>
            {t('从余额生成专用礼品码，接收后自动到账')}
          </div>
        </div>
      </div>

      <Space vertical align='start' style={{ width: '100%' }}>
        <div className='grid grid-cols-1 sm:grid-cols-2 gap-3 w-full'>
          <div>
            <Text strong className='block mb-2'>
              {t('礼品额度')}
            </Text>
            <InputNumber
              min={minAmount}
              value={amount}
              onChange={(value) => setAmount(value)}
              className='w-full !rounded-lg'
            />
          </div>
          <div>
            <Text strong className='block mb-2'>
              {t('有效期')}
            </Text>
            <DatePicker
              type='dateTime'
              value={expiredTime}
              onChange={(value) => setExpiredTime(value)}
              placeholder={t('留空为永久有效')}
              showClear
              style={{ width: '100%' }}
            />
          </div>
        </div>

        <div className='grid grid-cols-1 sm:grid-cols-2 gap-3 w-full'>
          <Select
            value={receiverType}
            onChange={(value) => {
              setReceiverType(value);
              if (!value) setReceiverValue('');
            }}
            optionList={receiverTypeOptions(t)}
            style={{ width: '100%' }}
          />
          <Input
            value={receiverValue}
            disabled={!receiverType}
            onChange={setReceiverValue}
            placeholder={t('填写用户名、用户 ID 或邮箱')}
            showClear
          />
        </div>

        <TextArea
          value={message}
          onChange={setMessage}
          rows={2}
          maxCount={500}
          placeholder={t('给接收人的留言，可选')}
          style={{ width: '100%' }}
        />

        <div className='flex items-center justify-between gap-3 w-full'>
          <Text type='tertiary' size='small'>
            {t('生成后会立即扣除余额，接收人登录后才能领取')}
          </Text>
          <Button
            type='primary'
            theme='solid'
            icon={<Send size={14} />}
            loading={loading}
            onClick={createGiftCode}
          >
            {t('生成礼品码')}
          </Button>
        </div>

        {manualCopyLink && (
          <div
            className='w-full rounded-lg p-3'
            style={{
              backgroundColor: 'var(--semi-color-warning-light-default)',
            }}
          >
            <Text type='warning' size='small' className='block mb-2'>
              {t('复制失败，请手动复制')}
            </Text>
            <Input
              value={manualCopyLink}
              onChange={() => {}}
              showClear={false}
            />
          </div>
        )}

        {giftCodes.length > 0 && (
          <div
            className='w-full pt-2'
            style={{ borderTop: '1px solid var(--semi-color-border)' }}
          >
            <Text strong className='block mb-2'>
              {t('最近生成')}
            </Text>
            <Space vertical align='start' style={{ width: '100%' }}>
              {giftCodes.slice(0, 5).map((giftCode) => {
                const status = getGiftCodeStatus(t, giftCode);
                return (
                  <div
                    key={giftCode.id}
                    className='w-full flex items-center justify-between gap-2 text-sm'
                  >
                    <Space wrap>
                      <Tag color={status.color}>{status.text}</Tag>
                      <Text>{renderQuota(giftCode.quota)}</Text>
                      {giftCode.received_username && (
                        <Text type='tertiary'>
                          {t('接收人')}：{giftCode.received_username}
                        </Text>
                      )}
                    </Space>
                    <Button
                      size='small'
                      type='tertiary'
                      icon={<Link2 size={13} />}
                      onClick={async () => {
                        const link = buildGiftCodeLink(giftCode.code);
                        const copied = await copyGiftCodeLink(link);
                        if (copied) {
                          setManualCopyLink('');
                          showSuccess(t('已复制到剪贴板'));
                        } else {
                          setManualCopyLink(link);
                          showError(t('复制失败，请手动复制'));
                        }
                      }}
                    >
                      {t('复制链接')}
                    </Button>
                  </div>
                );
              })}
            </Space>
          </div>
        )}
      </Space>

      <Modal
        title={
          <div className='flex items-center'>
            <Inbox className='mr-2' size={18} />
            {t('接收礼品码')}
          </div>
        }
        visible={receiveVisible}
        onOk={receiveGiftCode}
        onCancel={() => setReceiveVisible(false)}
        confirmLoading={receiveLoading}
        maskClosable={false}
        centered
      >
        {receiveInfo && (
          <Space vertical align='start' style={{ width: '100%' }}>
            <Text>
              {t('赠送人')}：{receiveInfo.creator_username || '-'}
            </Text>
            <Text>
              {t('礼品额度')}：{renderQuota(receiveInfo.quota)}
            </Text>
            {receiveInfo.message && (
              <Text>
                {t('留言')}：{receiveInfo.message}
              </Text>
            )}
            <TextArea
              value={thankMessage}
              onChange={setThankMessage}
              rows={3}
              maxCount={500}
              placeholder={t('写一句感谢回信，可选')}
              style={{ width: '100%' }}
            />
          </Space>
        )}
      </Modal>
    </Card>
  );
};

export default GiftCodeCard;
