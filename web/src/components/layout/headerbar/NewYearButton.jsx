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
import { Button } from '@douyinfe/semi-ui';
import fireworks from 'react-fireworks';
import HeaderPopupMenu from './HeaderPopupMenu';

const NewYearButton = ({ isNewYear }) => {
  if (!isNewYear) {
    return null;
  }

  const handleNewYearClick = () => {
    fireworks.init('root', {});
    fireworks.start();
    setTimeout(() => {
      fireworks.stop();
    }, 3000);
  };

  return (
    <HeaderPopupMenu
      menuLabel='New Year'
      renderTrigger={({ open, toggle }) => (
        <Button
          theme='borderless'
          type='tertiary'
          icon={<span className='text-xl'>🎉</span>}
          aria-label='New Year'
          aria-haspopup='menu'
          aria-expanded={open}
          onClick={toggle}
          className='!p-1.5 !text-current focus:!bg-semi-color-fill-1 dark:focus:!bg-gray-700 rounded-full'
        />
      )}
      renderContent={({ closeMenu }) => (
        <button
          type='button'
          role='menuitem'
          onClick={() => {
            handleNewYearClick();
            closeMenu();
          }}
          className='flex w-full items-center rounded-md px-3 py-2 text-left text-sm !text-semi-color-text-0 hover:!bg-semi-color-fill-1 dark:!text-gray-200 dark:hover:!bg-gray-600'
        >
          Happy New Year!!! 🎉
        </button>
      )}
    />
  );
};

export default NewYearButton;
