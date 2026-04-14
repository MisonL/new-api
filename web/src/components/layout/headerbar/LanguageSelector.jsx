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
import { Languages } from 'lucide-react';
import HeaderPopupMenu from './HeaderPopupMenu';

const LANGUAGE_OPTIONS = [
  { key: 'zh-CN', label: '简体中文' },
  { key: 'zh-TW', label: '繁體中文' },
  { key: 'en', label: 'English' },
  { key: 'fr', label: 'Français' },
  { key: 'ja', label: '日本語' },
  { key: 'ru', label: 'Русский' },
  { key: 'vi', label: 'Tiếng Việt' },
];

const LanguageSelector = ({ currentLang, onLanguageChange, t }) => {
  const changeLanguageLabel = t('common.changeLanguage', {
    defaultValue: 'Change Language',
  });
  const currentLanguage = useMemo(() => {
    return (
      LANGUAGE_OPTIONS.find((option) => option.key === currentLang)?.label ||
      changeLanguageLabel
    );
  }, [changeLanguageLabel, currentLang]);

  return (
    <HeaderPopupMenu
      menuLabel={currentLanguage}
      renderTrigger={({ open, toggle }) => (
        <Button
          icon={<Languages size={18} />}
          aria-label={changeLanguageLabel}
          aria-haspopup='menu'
          aria-expanded={open}
          theme='borderless'
          type='tertiary'
          onClick={toggle}
          className='!p-1.5 !text-current focus:!bg-semi-color-fill-1 dark:focus:!bg-gray-700 !rounded-full !bg-semi-color-fill-0 dark:!bg-semi-color-fill-1 hover:!bg-semi-color-fill-1 dark:hover:!bg-semi-color-fill-2'
        />
      )}
      renderContent={({ closeMenu }) => (
        <>
          {LANGUAGE_OPTIONS.map((option) => (
            <button
              key={option.key}
              type='button'
              role='menuitemradio'
              aria-checked={currentLang === option.key}
              onClick={() => {
                onLanguageChange(option.key);
                closeMenu();
              }}
              className={`flex w-full items-center rounded-md px-3 py-1.5 text-left text-sm !text-semi-color-text-0 dark:!text-gray-200 ${
                currentLang === option.key
                  ? '!bg-semi-color-primary-light-default !font-semibold dark:!bg-blue-600'
                  : 'hover:!bg-semi-color-fill-1 dark:hover:!bg-gray-600'
              }`}
            >
              {option.label}
            </button>
          ))}
        </>
      )}
    />
  );
};

export default LanguageSelector;
