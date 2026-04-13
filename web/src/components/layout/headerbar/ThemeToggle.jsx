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

import React, { useMemo } from 'react';
import { Button } from '@douyinfe/semi-ui';
import { Sun, Moon, Monitor } from 'lucide-react';
import { useActualTheme } from '../../../context/Theme';
import HeaderPopupMenu from './HeaderPopupMenu';

const ThemeToggle = ({ theme, onThemeToggle, t }) => {
  const actualTheme = useActualTheme();

  const themeOptions = useMemo(
    () => [
      {
        key: 'light',
        icon: <Sun size={18} />,
        buttonIcon: <Sun size={18} />,
        label: t('浅色模式'),
        description: t('始终使用浅色主题'),
      },
      {
        key: 'dark',
        icon: <Moon size={18} />,
        buttonIcon: <Moon size={18} />,
        label: t('深色模式'),
        description: t('始终使用深色主题'),
      },
      {
        key: 'auto',
        icon: <Monitor size={18} />,
        buttonIcon: <Monitor size={18} />,
        label: t('自动模式'),
        description: t('跟随系统主题设置'),
      },
    ],
    [t],
  );

  const getItemClassName = (isSelected) =>
    isSelected
      ? '!bg-semi-color-primary-light-default !font-semibold'
      : 'hover:!bg-semi-color-fill-1';

  const currentButtonIcon = useMemo(() => {
    const currentOption = themeOptions.find((option) => option.key === theme);
    return currentOption?.buttonIcon || themeOptions[2].buttonIcon;
  }, [theme, themeOptions]);

  return (
    <HeaderPopupMenu
      menuLabel={t('切换主题')}
      renderTrigger={({ open, toggle }) => (
        <Button
          icon={currentButtonIcon}
          aria-label={t('切换主题')}
          aria-haspopup='menu'
          aria-expanded={open}
          theme='borderless'
          type='tertiary'
          onClick={toggle}
          className='!p-1.5 !text-current focus:!bg-semi-color-fill-1 !rounded-full !bg-semi-color-fill-0 hover:!bg-semi-color-fill-1'
        />
      )}
      renderContent={({ closeMenu }) => (
        <>
          {themeOptions.map((option) => (
            <button
              key={option.key}
              type='button'
              role='menuitemradio'
              aria-checked={theme === option.key}
              onClick={() => {
                onThemeToggle(option.key);
                closeMenu();
              }}
              className={`flex w-full items-start gap-2 rounded-md px-3 py-2 text-left text-sm !text-semi-color-text-0 dark:!text-gray-200 ${getItemClassName(theme === option.key)}`}
            >
              <span className='mt-0.5'>{option.icon}</span>
              <span className='flex flex-col'>
                <span>{option.label}</span>
                <span className='text-xs text-semi-color-text-2 dark:text-gray-400'>
                  {option.description}
                </span>
              </span>
            </button>
          ))}
          {theme === 'auto' && (
            <div className='mt-1 border-t border-semi-color-border px-3 py-2 text-xs text-semi-color-text-2 dark:border-gray-600 dark:text-gray-400'>
              {t('当前跟随系统')}：
              {actualTheme === 'dark' ? t('深色') : t('浅色')}
            </div>
          )}
        </>
      )}
    />
  );
};

export default ThemeToggle;
