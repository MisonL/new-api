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
import { Modal } from '@douyinfe/semi-ui';

const DeleteUserModal = ({
  visible,
  onCancel,
  onConfirm,
  user,
  users,
  activePage,
  refresh,
  manageUser,
  hardDeleteUser,
  t,
}) => {
  // 已注销用户是软删除的墓碑行，仍占用邮箱 / OAuth 绑定；对它再调
  // manageUser('delete') 只会再软删一次，什么都不会释放。这类目标必须走
  // 硬删除，才能真正释放身份。
  const isDeleted = user?.DeletedAt !== null && user?.DeletedAt !== undefined;

  const handleConfirm = async () => {
    if (isDeleted) {
      await hardDeleteUser(user);
    } else {
      await manageUser(user.id, 'delete', user);
    }
    await refresh();
    setTimeout(() => {
      if (users.length === 0 && activePage > 1) {
        refresh(activePage - 1);
      }
    }, 100);
    onCancel(); // Close modal after success
  };

  return (
    <Modal
      title={t(isDeleted ? '确定彻底删除此用户？' : '确定是否要注销此用户？')}
      visible={visible}
      onCancel={onCancel}
      onOk={handleConfirm}
      type='danger'
    >
      {t(
        isDeleted
          ? '将永久删除该用户记录并释放其邮箱与第三方绑定，此操作不可恢复'
          : '相当于删除用户，此修改将不可逆',
      )}
    </Modal>
  );
};

export default DeleteUserModal;
