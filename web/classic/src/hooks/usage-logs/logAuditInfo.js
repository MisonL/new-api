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

export function getTopupAuditEntryDescriptors({
  isAdminUser,
  logType,
  adminInfo,
  t,
}) {
  if (!isAdminUser || logType !== 1) {
    return [];
  }

  if (!adminInfo) {
    return [
      {
        key: t('审计信息'),
        value: t(
          '该记录由旧版本实例写入，缺少审计信息，建议将实例升级至最新版本以便记录服务器IP、回调IP、支付方式与系统版本等审计字段。',
        ),
        warning: true,
      },
    ];
  }

  const candidates = [
    ['payment_method', '订单支付方式'],
    ['callback_payment_method', '回调支付方式'],
    ['caller_ip', '回调调用者IP'],
    ['server_ip', '服务器IP'],
    ['node_name', '节点名称'],
    ['version', '系统版本'],
  ];

  return candidates
    .filter(([field]) => {
      const value = adminInfo[field];
      return value !== undefined && value !== null && value !== '';
    })
    .map(([field, label]) => ({
      key: t(label),
      value: adminInfo[field],
      warning: false,
    }));
}

export function getManageOperatorEntryDescriptor({
  isAdminUser,
  logType,
  adminInfo,
  t,
}) {
  if (!isAdminUser || logType !== 3 || !adminInfo) {
    return null;
  }

  const hasUsername =
    adminInfo.admin_username !== undefined &&
    adminInfo.admin_username !== null &&
    adminInfo.admin_username !== '';
  const hasId =
    adminInfo.admin_id !== undefined &&
    adminInfo.admin_id !== null &&
    adminInfo.admin_id !== '';

  if (!hasUsername && !hasId) {
    return null;
  }

  let operatorValue = '';
  if (hasUsername && hasId) {
    operatorValue = `${adminInfo.admin_username} (ID: ${adminInfo.admin_id})`;
  } else if (hasUsername) {
    operatorValue = String(adminInfo.admin_username);
  } else {
    operatorValue = `ID: ${adminInfo.admin_id}`;
  }

  return {
    key: t('操作管理员'),
    value: operatorValue,
  };
}

export function shouldShowLogIp({ isAdminUser, recordType, ip }) {
  if (!ip) {
    return false;
  }
  return (
    recordType === 2 || recordType === 5 || (isAdminUser && recordType === 1)
  );
}
