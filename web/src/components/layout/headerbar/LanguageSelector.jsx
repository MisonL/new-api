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

import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Button } from '@douyinfe/semi-ui';
import { Languages } from 'lucide-react';

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
  const [open, setOpen] = useState(false);
  const containerRef = useRef(null);
  const changeLanguageLabel = t('common.changeLanguage', {
    defaultValue: 'Change Language',
  });
  const currentLanguage = useMemo(
    () =>
      LANGUAGE_OPTIONS.find((option) => option.key === currentLang)?.label ||
      changeLanguageLabel,
    [changeLanguageLabel, currentLang],
  );

  useEffect(() => {
    const handlePointerDown = (event) => {
      if (!containerRef.current?.contains(event.target)) {
        setOpen(false);
      }
    };

    const handleEscape = (event) => {
      if (event.key === 'Escape') {
        setOpen(false);
      }
    };

    document.addEventListener('mousedown', handlePointerDown);
    document.addEventListener('keydown', handleEscape);
    return () => {
      document.removeEventListener('mousedown', handlePointerDown);
      document.removeEventListener('keydown', handleEscape);
    };
  }, []);

  return (
    <div ref={containerRef} className='relative'>
      <Button
        icon={<Languages size={18} />}
        aria-label={changeLanguageLabel}
        aria-haspopup='menu'
        aria-expanded={open}
        theme='borderless'
        type='tertiary'
        onClick={() => setOpen((prev) => !prev)}
        className='!p-1.5 !text-current focus:!bg-semi-color-fill-1 dark:focus:!bg-gray-700 !rounded-full !bg-semi-color-fill-0 dark:!bg-semi-color-fill-1 hover:!bg-semi-color-fill-1 dark:hover:!bg-semi-color-fill-2'
      />
      {open && (
        <div
          role='menu'
          aria-label={currentLanguage}
          className='absolute right-0 top-full z-[120] mt-2 min-w-[144px] overflow-hidden rounded-lg border border-semi-color-border bg-semi-color-bg-overlay p-1 shadow-lg dark:border-gray-600 dark:bg-gray-700'
        >
          {LANGUAGE_OPTIONS.map((option) => (
            <button
              key={option.key}
              type='button'
              role='menuitemradio'
              aria-checked={currentLang === option.key}
              onClick={() => {
                onLanguageChange(option.key);
                setOpen(false);
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
        </div>
      )}
    </div>
  );
};

export default LanguageSelector;
