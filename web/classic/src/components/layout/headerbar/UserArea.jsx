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
import { Link } from 'react-router-dom';
import { Button, Typography } from '@douyinfe/semi-ui';
import { ChevronDown } from 'lucide-react';
import {
  IconExit,
  IconUserSetting,
  IconCreditCard,
  IconKey,
} from '@douyinfe/semi-icons';
import { stringToColor } from '../../../helpers/color';
import { useActualTheme } from '../../../context/Theme';
import SkeletonWrapper from '../components/SkeletonWrapper';
import HeaderPopupMenu from './HeaderPopupMenu';

const adjustHexChannel = (hex, delta) => {
  const value = parseInt(hex, 16);
  const nextValue = Math.max(0, Math.min(255, value + delta));
  return nextValue.toString(16).padStart(2, '0');
};

const adjustHexColor = (color, delta) => {
  if (
    typeof color !== 'string' ||
    !color.startsWith('#') ||
    color.length !== 7
  ) {
    return color;
  }
  return `#${adjustHexChannel(color.slice(1, 3), delta)}${adjustHexChannel(
    color.slice(3, 5),
    delta,
  )}${adjustHexChannel(color.slice(5, 7), delta)}`;
};

const UserArea = ({
  userState,
  isLoading,
  isMobile,
  isSelfUseMode,
  logout,
  navigate,
  t,
}) => {
  const actualTheme = useActualTheme();

  if (isLoading) {
    return (
      <SkeletonWrapper
        loading={true}
        type='userArea'
        width={50}
        isMobile={isMobile}
      />
    );
  }

  if (userState.user) {
    const username = userState.user.username;
    const userInitial = username[0].toUpperCase();
    const userBadgeColor = stringToColor(username);
    const userBadgeStyle = {
      backgroundColor:
        actualTheme === 'dark'
          ? adjustHexColor(userBadgeColor, 12)
          : adjustHexColor(userBadgeColor, -8),
      color: '#ffffff',
      WebkitTextFillColor: '#ffffff',
      boxShadow:
        actualTheme === 'dark'
          ? 'inset 0 0 0 1px rgba(255,255,255,0.08)'
          : 'inset 0 0 0 1px rgba(15,23,42,0.06)',
    };

    const menuItems = [
      {
        key: 'personal',
        label: t('个人设置'),
        icon: (
          <IconUserSetting
            size='small'
            className='text-gray-500 dark:text-gray-400'
          />
        ),
        onClick: () => navigate('/console/personal'),
      },
      {
        key: 'token',
        label: t('令牌管理'),
        icon: (
          <IconKey size='small' className='text-gray-500 dark:text-gray-400' />
        ),
        onClick: () => navigate('/console/token'),
      },
      {
        key: 'wallet',
        label: t('钱包管理'),
        icon: (
          <IconCreditCard
            size='small'
            className='text-gray-500 dark:text-gray-400'
          />
        ),
        onClick: () => navigate('/console/topup'),
      },
      {
        key: 'logout',
        label: t('退出'),
        icon: (
          <IconExit size='small' className='text-gray-500 dark:text-gray-400' />
        ),
        onClick: logout,
        danger: true,
      },
    ];

    return (
      <HeaderPopupMenu
        menuLabel={username}
        renderTrigger={({ open, toggle }) => (
          <Button
            theme='borderless'
            type='tertiary'
            aria-label={username}
            aria-haspopup='menu'
            aria-expanded={open}
            onClick={toggle}
            className='flex items-center gap-1.5 !p-1 !rounded-full !bg-zinc-800 hover:!bg-zinc-700 dark:!bg-semi-color-fill-1 dark:hover:!bg-semi-color-fill-2'
          >
            <span
              aria-hidden='true'
              style={userBadgeStyle}
              className='mr-1 inline-flex h-6 w-6 items-center justify-center rounded-full text-[11px] font-semibold'
            >
              {userInitial}
            </span>
            <span className='hidden md:inline'>
              <Typography.Text className='!text-xs !font-medium !text-white mr-1'>
                {username}
              </Typography.Text>
            </span>
            <ChevronDown size={14} className='text-xs text-white/80' />
          </Button>
        )}
        renderContent={({ closeMenu }) => (
          <>
            {menuItems.map((item) => (
              <button
                key={item.key}
                type='button'
                role='menuitem'
                onClick={() => {
                  item.onClick();
                  closeMenu();
                }}
                className={`flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm ${
                  item.danger
                    ? '!text-semi-color-text-0 hover:!bg-semi-color-fill-1 dark:!text-gray-200 dark:hover:!bg-red-500 dark:hover:!text-white'
                    : '!text-semi-color-text-0 hover:!bg-semi-color-fill-1 dark:!text-gray-200 dark:hover:!bg-blue-500 dark:hover:!text-white'
                }`}
              >
                {item.icon}
                <span>{item.label}</span>
              </button>
            ))}
          </>
        )}
      />
    );
  } else {
    const showRegisterButton = !isSelfUseMode;

    const commonSizingAndLayoutClass =
      'flex items-center justify-center !py-[10px] !px-1.5';

    const loginButtonSpecificStyling =
      '!bg-semi-color-fill-0 dark:!bg-semi-color-fill-1 hover:!bg-semi-color-fill-1 dark:hover:!bg-gray-700 transition-colors';
    let loginButtonClasses = `${commonSizingAndLayoutClass} ${loginButtonSpecificStyling}`;

    let registerButtonClasses = `${commonSizingAndLayoutClass}`;

    const loginButtonTextSpanClass =
      '!text-xs !text-semi-color-text-1 dark:!text-gray-300 !p-1.5';
    const registerButtonTextSpanClass = '!text-xs !text-white !p-1.5';

    if (showRegisterButton) {
      if (isMobile) {
        loginButtonClasses += ' !rounded-full';
      } else {
        loginButtonClasses += ' !rounded-l-full !rounded-r-none';
      }
      registerButtonClasses += ' !rounded-r-full !rounded-l-none';
    } else {
      loginButtonClasses += ' !rounded-full';
    }

    return (
      <div className='flex items-center'>
        <Link to='/login' className='flex'>
          <Button
            theme='borderless'
            type='tertiary'
            className={loginButtonClasses}
          >
            <span className={loginButtonTextSpanClass}>{t('登录')}</span>
          </Button>
        </Link>
        {showRegisterButton && (
          <div className='hidden md:block'>
            <Link to='/register' className='flex -ml-px'>
              <Button
                theme='solid'
                type='primary'
                className={registerButtonClasses}
              >
                <span className={registerButtonTextSpanClass}>{t('注册')}</span>
              </Button>
            </Link>
          </div>
        )}
      </div>
    );
  }
};

export default UserArea;
