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
import { Modal, Button, Checkbox, RadioGroup, Radio } from '@douyinfe/semi-ui';
import { IconChevronDown, IconChevronUp } from '@douyinfe/semi-icons';
import { getLogsColumns } from '../UsageLogsColumnDefs';
import {
  applyColumnOrder,
  getMovableColumnKeys,
} from '../../../../hooks/usage-logs/columnPreferences';

const ColumnSelectorModal = ({
  showColumnSelector,
  setShowColumnSelector,
  visibleColumns,
  columnOrder,
  handleColumnVisibilityChange,
  handleColumnOrderChange,
  handleSelectAll,
  initDefaultColumns,
  billingDisplayMode,
  setBillingDisplayMode,
  COLUMN_KEYS,
  isAdminUser,
  copyText,
  openEditUserPanel,
  t,
}) => {
  const handleBillingDisplayModeChange = (eventOrValue) => {
    setBillingDisplayMode(eventOrValue?.target?.value ?? eventOrValue);
  };

  const isTokensDisplay =
    typeof localStorage !== 'undefined' &&
    localStorage.getItem('quota_display_type') === 'TOKENS';

  const allColumns = React.useMemo(
    () =>
      getLogsColumns({
        t,
        COLUMN_KEYS,
        copyText,
        openEditUserPanel,
        isAdminUser,
        billingDisplayMode,
      }),
    [
      t,
      COLUMN_KEYS,
      copyText,
      openEditUserPanel,
      isAdminUser,
      billingDisplayMode,
    ],
  );
  const orderedColumns = React.useMemo(
    () => applyColumnOrder(allColumns, null, columnOrder),
    [allColumns, columnOrder],
  );
  const selectableColumns = React.useMemo(
    () =>
      orderedColumns.filter((column) => {
        if (!isAdminUser) {
          return ![
            COLUMN_KEYS.CHANNEL,
            COLUMN_KEYS.USERNAME,
            COLUMN_KEYS.REQUEST_UA,
            COLUMN_KEYS.REQUEST_HEADERS,
            COLUMN_KEYS.RETRY,
          ].includes(column.key);
        }
        return true;
      }),
    [
      orderedColumns,
      isAdminUser,
      COLUMN_KEYS.CHANNEL,
      COLUMN_KEYS.USERNAME,
      COLUMN_KEYS.REQUEST_UA,
      COLUMN_KEYS.REQUEST_HEADERS,
      COLUMN_KEYS.RETRY,
    ],
  );
  const selectedStates = React.useMemo(
    () => selectableColumns.map((column) => !!visibleColumns[column.key]),
    [selectableColumns, visibleColumns],
  );
  const allSelected =
    selectedStates.length > 0 && selectedStates.every((checked) => checked);
  const partiallySelected =
    selectedStates.some((checked) => checked) && !allSelected;
  const movableColumnKeys = React.useMemo(
    () => getMovableColumnKeys(selectableColumns),
    [selectableColumns],
  );
  const movableColumnKeySet = React.useMemo(
    () => new Set(movableColumnKeys),
    [movableColumnKeys],
  );

  return (
    <Modal
      title={t('列设置')}
      visible={showColumnSelector}
      onCancel={() => setShowColumnSelector(false)}
      width='min(448px, calc(100vw - 24px))'
      footer={
        <div className='flex justify-end'>
          <Button onClick={() => initDefaultColumns()}>{t('重置')}</Button>
          <Button onClick={() => setShowColumnSelector(false)}>
            {t('取消')}
          </Button>
          <Button onClick={() => setShowColumnSelector(false)}>
            {t('确定')}
          </Button>
        </div>
      }
    >
      <div style={{ marginBottom: 20 }}>
        <div style={{ marginBottom: 16 }}>
          <div style={{ marginBottom: 8, fontWeight: 600 }}>
            {t('计费显示模式')}
          </div>
          <RadioGroup
            type='button'
            value={billingDisplayMode}
            onChange={handleBillingDisplayModeChange}
            name='components-table-usage-logs-modals-columnselectormodal-radiogroup-1'
          >
            <Radio value='price'>
              {isTokensDisplay ? t('价格模式') : t('价格模式（默认）')}
            </Radio>
            <Radio value='ratio'>
              {isTokensDisplay ? t('倍率模式（默认）') : t('倍率模式')}
            </Radio>
          </RadioGroup>
        </div>
        <Checkbox
          checked={allSelected}
          indeterminate={partiallySelected}
          onChange={(e) => handleSelectAll(e.target.checked)}
          name='components-table-usage-logs-modals-columnselectormodal-checkbox-1'
        >
          {t('全选')}
        </Checkbox>
      </div>
      <div
        className='max-h-96 overflow-y-auto rounded-lg p-2'
        style={{ border: '1px solid var(--semi-color-border)' }}
      >
        {selectableColumns.map((column, index) => {
          const movableIndex = movableColumnKeys.indexOf(column.key);
          const canMoveUp = movableIndex > 0;
          const canMoveDown =
            movableIndex >= 0 && movableIndex < movableColumnKeys.length - 1;
          const isMovable = movableColumnKeySet.has(column.key);

          return (
            <div
              key={column.key}
              className='flex items-center justify-between gap-2 px-2 py-2'
              style={{
                borderBottom:
                  index === selectableColumns.length - 1
                    ? 'none'
                    : '1px solid var(--semi-color-border)',
              }}
            >
              <Checkbox
                className='min-w-0 flex-1'
                checked={!!visibleColumns[column.key]}
                onChange={(e) =>
                  handleColumnVisibilityChange(column.key, e.target.checked)
                }
                name='components-table-usage-logs-modals-columnselectormodal-checkbox-2'
              >
                {column.title}
              </Checkbox>
              <div className='flex shrink-0 gap-1'>
                <Button
                  aria-label={t('上移')}
                  title={t('上移')}
                  size='small'
                  theme='borderless'
                  type='tertiary'
                  icon={<IconChevronUp />}
                  disabled={!isMovable || !canMoveUp}
                  onClick={() =>
                    handleColumnOrderChange(column.key, 'up', movableColumnKeys)
                  }
                />
                <Button
                  aria-label={t('下移')}
                  title={t('下移')}
                  size='small'
                  theme='borderless'
                  type='tertiary'
                  icon={<IconChevronDown />}
                  disabled={!isMovable || !canMoveDown}
                  onClick={() =>
                    handleColumnOrderChange(
                      column.key,
                      'down',
                      movableColumnKeys,
                    )
                  }
                />
              </div>
            </div>
          );
        })}
      </div>
    </Modal>
  );
};

export default ColumnSelectorModal;
