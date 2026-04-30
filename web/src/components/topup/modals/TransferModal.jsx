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

import React from 'react';
import {
  Modal,
  Typography,
  Input,
  InputNumber,
  Button,
} from '@douyinfe/semi-ui';
import { CreditCard } from 'lucide-react';

const TransferModal = ({
  t,
  openTransfer,
  transfer,
  handleTransferCancel,
  userState,
  renderQuota,
  getQuotaPerUnit,
  transferAmount,
  setTransferAmount,
}) => {
  const availableAffQuota = Number(userState?.user?.aff_quota || 0);
  const quotaPerUnit = Number(getQuotaPerUnit());
  const normalizedTransferAmount = Number(transferAmount || 0);
  const minTransferQuota =
    Number.isFinite(quotaPerUnit) && quotaPerUnit > 0 ? quotaPerUnit : 0;
  const hasValidTransferRule = minTransferQuota > 0;
  const canTransferAll =
    hasValidTransferRule && availableAffQuota >= minTransferQuota;
  const inputMin = canTransferAll ? minTransferQuota : 0;
  const inputMax = canTransferAll ? availableAffQuota : 0;
  const canSubmitTransfer =
    canTransferAll &&
    normalizedTransferAmount >= minTransferQuota &&
    normalizedTransferAmount <= availableAffQuota;

  return (
    <Modal
      title={
        <div className='flex items-center'>
          <CreditCard className='mr-2' size={18} />
          {t('划转邀请额度')}
        </div>
      }
      visible={openTransfer}
      onOk={transfer}
      onCancel={handleTransferCancel}
      okText={t('确认')}
      cancelText={t('取消')}
      okButtonProps={{ disabled: !canSubmitTransfer }}
      maskClosable={false}
      centered
    >
      <div className='space-y-4'>
        <div>
          <Typography.Text strong className='block mb-2'>
            {t('可用邀请额度')}
          </Typography.Text>
          <Input
            value={renderQuota(availableAffQuota)}
            disabled
            className='!rounded-lg'
            name='components-topup-modals-transfermodal-input-1'
          />
        </div>
        <div>
          <div className='flex items-center justify-between gap-2 mb-2'>
            <Typography.Text strong>
              {t('划转额度')} · {t('最低') + renderQuota(minTransferQuota)}
            </Typography.Text>
            <Button
              size='small'
              type='tertiary'
              disabled={!canTransferAll}
              onClick={() => setTransferAmount(availableAffQuota)}
            >
              {t('全部划转')}
            </Button>
          </div>
          <InputNumber
            min={inputMin}
            max={inputMax}
            disabled={!canTransferAll}
            value={transferAmount}
            onChange={(value) => setTransferAmount(value)}
            className='w-full !rounded-lg'
            name='components-topup-modals-transfermodal-inputnumber-1'
          />
          {!canTransferAll && (
            <Typography.Text
              type='tertiary'
              size='small'
              className='block mt-2'
            >
              {hasValidTransferRule
                ? t('邀请额度不足，需至少达到最低划转额度')
                : t('划转规则未配置')}
            </Typography.Text>
          )}
        </div>
      </div>
    </Modal>
  );
};

export default TransferModal;
