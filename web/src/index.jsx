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
import ReactDOM from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import '@douyinfe/semi-ui/dist/css/semi.css';
import { UserProvider } from './context/User';
import 'react-toastify/dist/ReactToastify.css';
import { StatusProvider } from './context/Status';
import { ThemeProvider } from './context/Theme';
import PageLayout from './components/layout/PageLayout';
import './i18n/i18n';
import './index.css';
import { LocaleProvider } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import zh_CN from '@douyinfe/semi-ui/lib/es/locale/source/zh_CN';
import en_GB from '@douyinfe/semi-ui/lib/es/locale/source/en_GB';

// 欢迎信息（二次开发者未经允许不准将此移除）
// Welcome message (Do not remove this without permission from the original developer)
if (typeof window !== 'undefined') {
  console.log(
    '%cWE ❤ NEWAPI%c Github: https://github.com/QuantumNous/new-api',
    'color: #10b981; font-weight: bold; font-size: 24px;',
    'color: inherit; font-size: 14px;',
  );

  const startGlobalFieldIdentityPatch = () => {
    if (window.__newApiFieldIdentityPatchStarted) {
      return;
    }

    window.__newApiFieldIdentityPatchStarted = true;
    window.__newApiFieldIdentityPatchVersion = '2026-04-12-1';

    let generatedCounter = 0;
    const toStableToken = (value, fallback) => {
      const token = String(value || '')
        .trim()
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, '-')
        .replace(/^-+|-+$/g, '');
      return token || fallback;
    };

    const patchFields = () => {
      if (!document.body) {
        return;
      }

      document
        .querySelectorAll('input:not([type="hidden"]), textarea, select')
        .forEach((element) => {
          if (!element.id) {
            generatedCounter += 1;
            const source =
              element.getAttribute('data-insp-path') ||
              element.getAttribute('aria-label') ||
              element.getAttribute('placeholder') ||
              element.getAttribute('type') ||
              element.tagName.toLowerCase();
            element.id = `global-field-${toStableToken(source, 'field')}-${generatedCounter}`;
          }

          if (!element.getAttribute('name')) {
            element.setAttribute('name', element.id);
          }
        });
    };

    const schedulePatch = () => {
      window.requestAnimationFrame(() => {
        patchFields();
      });
    };

    patchFields();
    window.setTimeout(schedulePatch, 0);
    window.setTimeout(schedulePatch, 300);
    window.setTimeout(schedulePatch, 1000);
    window.setTimeout(schedulePatch, 2000);
    window.setTimeout(schedulePatch, 4000);
    window.setInterval(schedulePatch, 800);

    const observer = new MutationObserver(() => {
      schedulePatch();
    });

    const startObserve = () => {
      if (!document.body) {
        return;
      }
      observer.observe(document.body, {
        childList: true,
        subtree: true,
        attributes: true,
        attributeFilter: ['class', 'value', 'checked', 'placeholder'],
      });
    };

    if (document.body) {
      startObserve();
    } else {
      window.addEventListener('DOMContentLoaded', startObserve, { once: true });
    }
  };

  startGlobalFieldIdentityPatch();
}

function SemiLocaleWrapper({ children }) {
  const { i18n } = useTranslation();
  const semiLocale = React.useMemo(
    () => ({ zh: zh_CN, en: en_GB })[i18n.language] || zh_CN,
    [i18n.language],
  );
  return <LocaleProvider locale={semiLocale}>{children}</LocaleProvider>;
}

// initialization

const RootWrapper = import.meta.env.DEV ? React.Fragment : React.StrictMode;

const root = ReactDOM.createRoot(document.getElementById('root'));
root.render(
  <RootWrapper>
    <StatusProvider>
      <UserProvider>
        <BrowserRouter
          future={{
            v7_startTransition: true,
            v7_relativeSplatPath: true,
          }}
        >
          <ThemeProvider>
            <SemiLocaleWrapper>
              <PageLayout />
            </SemiLocaleWrapper>
          </ThemeProvider>
        </BrowserRouter>
      </UserProvider>
    </StatusProvider>
  </RootWrapper>,
);
