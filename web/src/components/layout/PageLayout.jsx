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

import HeaderBar from './headerbar';
import { Layout } from '@douyinfe/semi-ui';
import SiderBar from './SiderBar';
import App from '../../App';
import FooterBar from './Footer';
import Loading from '../common/ui/Loading';
import { ToastContainer } from 'react-toastify';
import ErrorBoundary from '../common/ErrorBoundary';
import React, { useContext, useEffect, useMemo, useState } from 'react';
import useFormFieldA11yPatch from '../../hooks/common/useFormFieldA11yPatch';
import { useIsMobile } from '../../hooks/common/useIsMobile';
import { useSidebarCollapsed } from '../../hooks/common/useSidebarCollapsed';
import { useSidebarWidth } from '../../hooks/common/useSidebarWidth';
import { useTranslation } from 'react-i18next';
import { API } from '../../helpers/apiCore';
import { setStatusData } from '../../helpers/data';
import { getLogo, getSystemName, showError } from '../../helpers/utils';
import { handleAuthExpired } from '../../helpers/authRedirect';
import { UserContext } from '../../context/User';
import { StatusContext } from '../../context/Status';
import { useLocation } from 'react-router-dom';
import { normalizeLanguage } from '../../i18n/language';
import {
  IS_READONLY_FRONTEND,
  READONLY_FRONTEND_MESSAGE,
} from '../../constants/runtime.constants';
const { Sider, Content, Header } = Layout;
const SESSION_RESUME_CHECK_INTERVAL_MS = 30 * 1000;

const PageLayout = () => {
  const [userState, userDispatch] = useContext(UserContext);
  const [, statusDispatch] = useContext(StatusContext);
  const isMobile = useIsMobile();
  const [collapsed, , setCollapsed] = useSidebarCollapsed();
  const [
    sidebarWidth,
    setSidebarWidth,
    resetSidebarWidth,
    defaultSidebarWidth,
  ] = useSidebarWidth();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const { i18n } = useTranslation();
  const location = useLocation();

  const cardProPages = [
    '/console/channel',
    '/console/log',
    '/console/redemption',
    '/console/user',
    '/console/token',
    '/console/midjourney',
    '/console/task',
    '/console/models',
    '/pricing',
  ];

  const shouldHideFooter = cardProPages.includes(location.pathname);

  const shouldInnerPadding =
    location.pathname.includes('/console') &&
    !location.pathname.startsWith('/console/chat') &&
    location.pathname !== '/console/playground';

  const isConsoleRoute = location.pathname.startsWith('/console');
  const isHomeRoute = location.pathname === '/';
  const useHomeViewportLock = !isMobile && isHomeRoute;
  const isViewportLockedConsoleRoute =
    location.pathname === '/console/playground' ||
    location.pathname.startsWith('/console/chat');
  const useContentViewportLock =
    !isMobile && (useHomeViewportLock || isViewportLockedConsoleRoute);
  const showSider = isConsoleRoute && (!isMobile || drawerOpen);
  const preferredLang = useMemo(() => {
    if (userState?.user?.setting) {
      try {
        const settings = JSON.parse(userState.user.setting);
        const normalizedLanguage = normalizeLanguage(settings.language);
        if (normalizedLanguage) {
          return normalizedLanguage;
        }
      } catch (e) {
        // Ignore parse errors
      }
    }

    const savedLang = localStorage.getItem('i18nextLng');
    return normalizeLanguage(savedLang);
  }, [userState?.user?.setting]);
  const currentLanguage = normalizeLanguage(
    i18n.resolvedLanguage || i18n.language,
  );
  const shouldDelayConsoleRender =
    isConsoleRoute &&
    Boolean(preferredLang) &&
    preferredLang !== currentLanguage;

  useEffect(() => {
    if (isMobile && drawerOpen && collapsed) {
      setCollapsed(false);
    }
  }, [isMobile, drawerOpen, collapsed, setCollapsed]);

  const loadUser = () => {
    let user = localStorage.getItem('user');
    if (user) {
      let data = JSON.parse(user);
      userDispatch({ type: 'login', payload: data });
    }
  };

  const loadStatus = async () => {
    try {
      const res = await API.get('/api/status');
      const { success, data } = res.data;
      if (success) {
        statusDispatch({ type: 'set', payload: data });
        setStatusData(data);
      } else {
        showError('Unable to connect to server');
      }
    } catch (error) {
      showError('Failed to load status');
    }
  };

  useEffect(() => {
    loadUser();
    loadStatus().catch(console.error);
    let systemName = getSystemName();
    if (systemName) {
      document.title = systemName;
    }
    let logo = getLogo();
    if (logo) {
      let linkElement = document.querySelector("link[rel~='icon']");
      if (linkElement) {
        linkElement.href = logo;
      }
    }
  }, []);

  useEffect(() => {
    if (!preferredLang) {
      return;
    }

    localStorage.setItem('i18nextLng', preferredLang);
    if (preferredLang !== currentLanguage) {
      i18n.changeLanguage(preferredLang).catch(console.error);
    }
  }, [currentLanguage, i18n, preferredLang]);

  useEffect(() => {
    if (!isConsoleRoute || !localStorage.getItem('user')) {
      return;
    }

    let checking = false;
    let lastCheckedAt = 0;

    const verifySession = async () => {
      if (
        checking ||
        document.visibilityState !== 'visible' ||
        !localStorage.getItem('user')
      ) {
        return;
      }

      const now = Date.now();
      if (now - lastCheckedAt < SESSION_RESUME_CHECK_INTERVAL_MS) {
        return;
      }

      checking = true;
      lastCheckedAt = now;
      try {
        await API.get('/api/user/self', {
          skipErrorHandler: true,
          disableDuplicate: true,
        });
      } catch (error) {
        if (error?.response?.status === 401) {
          handleAuthExpired(window.location);
        }
      } finally {
        checking = false;
      }
    };

    const handleVisibilityChange = () => {
      if (document.visibilityState === 'visible') {
        verifySession();
      }
    };

    window.addEventListener('focus', verifySession);
    document.addEventListener('visibilitychange', handleVisibilityChange);

    return () => {
      window.removeEventListener('focus', verifySession);
      document.removeEventListener('visibilitychange', handleVisibilityChange);
    };
  }, [isConsoleRoute]);

  useFormFieldA11yPatch(`${location.pathname}${location.search}`);

  return (
    <Layout
      className='app-layout'
      style={{
        display: 'flex',
        flexDirection: 'column',
        overflow: isMobile ? 'visible' : 'hidden',
      }}
    >
      <Header
        style={{
          padding: 0,
          height: 'auto',
          lineHeight: 'normal',
          position: 'fixed',
          width: '100%',
          top: 0,
          zIndex: 100,
        }}
      >
        <HeaderBar
          onMobileMenuToggle={() => setDrawerOpen((prev) => !prev)}
          drawerOpen={drawerOpen}
        />
      </Header>
      <Layout
        style={{
          overflow: useHomeViewportLock
            ? 'hidden'
            : isMobile
              ? 'visible'
              : 'auto',
          display: 'flex',
          flexDirection: 'column',
          minHeight: isMobile ? 'auto' : '100vh',
          paddingTop: isMobile ? '0' : '64px',
          boxSizing: 'border-box',
        }}
      >
        {showSider && (
          <Sider
            className='app-sider'
            style={{
              position: 'fixed',
              left: 0,
              top: '64px',
              zIndex: 99,
              border: 'none',
              paddingRight: '0',
              width: 'var(--sidebar-current-width)',
            }}
          >
            <SiderBar
              sidebarWidth={sidebarWidth}
              defaultSidebarWidth={defaultSidebarWidth}
              setSidebarWidth={setSidebarWidth}
              resetSidebarWidth={resetSidebarWidth}
              isMobile={isMobile}
              onNavigate={() => {
                if (isMobile) setDrawerOpen(false);
              }}
            />
          </Sider>
        )}
        <Layout
          style={{
            marginLeft: isMobile
              ? '0'
              : showSider
                ? 'var(--sidebar-current-width)'
                : '0',
            flex: '1 1 auto',
            display: 'flex',
            flexDirection: 'column',
          }}
        >
          <Content
            style={{
              flex: '1 1 auto',
              minHeight: 0,
              overflowY: isMobile
                ? 'visible'
                : useContentViewportLock
                  ? 'hidden'
                  : 'auto',
              WebkitOverflowScrolling: 'touch',
              padding: shouldInnerPadding ? (isMobile ? '5px' : '24px') : '0',
              position: 'relative',
            }}
          >
            {IS_READONLY_FRONTEND && (
              <div
                style={{
                  margin: shouldInnerPadding ? '0 0 16px 0' : '0',
                  padding: '12px 16px',
                  borderBottom: shouldInnerPadding
                    ? '1px solid var(--semi-color-border)'
                    : '1px solid var(--semi-color-warning-light-default)',
                  background:
                    'var(--semi-color-warning-light-default, #fff7e8)',
                  color: 'var(--semi-color-warning, #ad6800)',
                  fontSize: '14px',
                  lineHeight: 1.5,
                }}
              >
                {READONLY_FRONTEND_MESSAGE}
              </div>
            )}
            <ErrorBoundary>
              {shouldDelayConsoleRender ? <Loading /> : <App />}
            </ErrorBoundary>
          </Content>
          {!shouldHideFooter && (
            <Layout.Footer
              style={{
                flex: '0 0 auto',
                width: '100%',
              }}
            >
              <FooterBar />
            </Layout.Footer>
          )}
        </Layout>
      </Layout>
      <ToastContainer />
    </Layout>
  );
};

export default PageLayout;
