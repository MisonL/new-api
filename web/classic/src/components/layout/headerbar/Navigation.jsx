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
import { Button, Dropdown } from '@douyinfe/semi-ui';
import { IconMenu } from '@douyinfe/semi-icons';
import SkeletonWrapper from '../components/SkeletonWrapper';

const Navigation = ({
  mainNavLinks,
  isMobile,
  isLoading,
  userState,
  pricingRequireAuth,
  t,
}) => {
  // getNavTarget keeps unauthenticated redirects consistent for userState.user and pricingRequireAuth.
  const getNavTarget = (link) => {
    if (link.itemKey === 'console' && !userState.user) {
      return '/login';
    }
    if (link.itemKey === 'pricing' && pricingRequireAuth && !userState.user) {
      return '/login';
    }
    return link.to;
  };

  const renderNavLinks = () => {
    const baseClasses =
      'flex-shrink-0 flex items-center gap-1 font-semibold rounded-md transition-all duration-200 ease-in-out';
    const hoverClasses = 'hover:text-semi-color-primary';
    const spacingClasses = isMobile ? 'p-1' : 'p-2';

    const commonLinkClasses = `${baseClasses} ${spacingClasses} ${hoverClasses}`;

    return mainNavLinks.map((link) => {
      const linkContent = <span>{link.text}</span>;

      if (link.isExternal) {
        return (
          <a
            key={link.itemKey}
            href={link.externalLink}
            target='_blank'
            rel='noopener noreferrer'
            className={commonLinkClasses}
          >
            {linkContent}
          </a>
        );
      }

      const targetPath = getNavTarget(link);

      return (
        <Link key={link.itemKey} to={targetPath} className={commonLinkClasses}>
          {linkContent}
        </Link>
      );
    });
  };

  if (isMobile) {
    return (
      <nav
        aria-label={t('主导航')}
        className='flex flex-1 items-center justify-end mx-1'
      >
        <SkeletonWrapper
          loading={isLoading}
          type='navigation'
          count={1}
          width={32}
          height={32}
          isMobile={isMobile}
        >
          <Dropdown
            trigger='click'
            position='bottomRight'
            render={
              <Dropdown.Menu>
                {mainNavLinks.map((link) => {
                  const targetPath = getNavTarget(link);

                  return (
                    <Dropdown.Item key={link.itemKey}>
                      {link.isExternal ? (
                        <a
                          href={link.externalLink}
                          target='_blank'
                          rel='noopener noreferrer'
                          className='block min-w-28 px-2 py-1 text-semi-color-text-0'
                        >
                          {link.text}
                        </a>
                      ) : (
                        <Link
                          to={targetPath}
                          className='block min-w-28 px-2 py-1 text-semi-color-text-0'
                        >
                          {link.text}
                        </Link>
                      )}
                    </Dropdown.Item>
                  );
                })}
              </Dropdown.Menu>
            }
          >
            <Button
              theme='borderless'
              type='tertiary'
              icon={<IconMenu />}
              aria-label={t('打开导航菜单')}
              className='!p-2 !text-current'
            />
          </Dropdown>
        </SkeletonWrapper>
      </nav>
    );
  }

  return (
    <nav className='flex flex-1 items-center gap-1 lg:gap-2 mx-2 md:mx-4 overflow-x-auto whitespace-nowrap scrollbar-hide'>
      <SkeletonWrapper
        loading={isLoading}
        type='navigation'
        count={4}
        width={60}
        height={16}
        isMobile={isMobile}
      >
        {renderNavLinks()}
      </SkeletonWrapper>
    </nav>
  );
};

export default Navigation;
