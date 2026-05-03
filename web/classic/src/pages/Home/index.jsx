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

import React, { useContext, useEffect, useState } from 'react';
import { Button, Typography, Input } from '@douyinfe/semi-ui';
import {
  API,
  showError,
  copy,
  showSuccess,
  getEffectiveServerAddress,
} from '../../helpers';
import { useIsMobile } from '../../hooks/common/useIsMobile';
import { API_ENDPOINTS } from '../../constants/common.constant';
import { StatusContext } from '../../context/Status';
import { useActualTheme } from '../../context/Theme';
import { marked } from 'marked';
import { useTranslation } from 'react-i18next';
import {
  IconGithubLogo,
  IconPlay,
  IconFile,
  IconCopy,
} from '@douyinfe/semi-icons';
import { Link } from 'react-router-dom';
import NoticeModal from '../../components/layout/NoticeModal';
import {
  Moonshot,
  OpenAI,
  XAI,
  Zhipu,
  Volcengine,
  Cohere,
  Claude,
  Gemini,
  Suno,
  Minimax,
  Wenxin,
  Spark,
  Qingyan,
  DeepSeek,
  Qwen,
  Midjourney,
  Grok,
  AzureAI,
  Hunyuan,
  Xinference,
} from '@lobehub/icons';

const { Text } = Typography;

const getViewportHeight = () =>
  typeof window === 'undefined' ? 900 : window.innerHeight;
const ENDPOINT_ROTATE_INTERVAL_MS = 5000;
const ENDPOINT_ITEM_HEIGHT = 32;
const ENDPOINT_ANIMATION_DURATION_MS = 320;

const Home = () => {
  const { t, i18n } = useTranslation();
  const [statusState] = useContext(StatusContext);
  const actualTheme = useActualTheme();
  const [homePageContentLoaded, setHomePageContentLoaded] = useState(false);
  const [homePageContent, setHomePageContent] = useState('');
  const [noticeVisible, setNoticeVisible] = useState(false);
  const isMobile = useIsMobile();
  const isDemoSiteMode = statusState?.status?.demo_site_enabled || false;
  const docsLink = statusState?.status?.docs_link || '';
  const serverAddress = getEffectiveServerAddress(
    statusState?.status?.server_address,
  );
  const endpointItems = API_ENDPOINTS.map((e) => ({ value: e }));
  const endpointTrackItems =
    endpointItems.length > 0 ? [...endpointItems, endpointItems[0]] : [];
  const [endpointIndex, setEndpointIndex] = useState(0);
  const [endpointTransitionEnabled, setEndpointTransitionEnabled] =
    useState(true);
  const endpointActiveIndex =
    endpointItems.length > 0 ? endpointIndex % endpointItems.length : -1;
  const [viewportHeight, setViewportHeight] = useState(getViewportHeight);
  const isChinese = i18n.language.startsWith('zh');
  const isCompactHeight = !isMobile && viewportHeight <= 860;
  const isTightHeight = !isMobile && viewportHeight <= 760;
  const footerEstimatedHeight = isDemoSiteMode
    ? isTightHeight
      ? 160
      : 190
    : 92;
  const desktopAvailableHeight = Math.max(
    500,
    viewportHeight - 64 - footerEstimatedHeight,
  );
  const providerIconSize = isTightHeight ? 30 : isCompactHeight ? 34 : 40;
  const providerBoxSize = isTightHeight ? 34 : isCompactHeight ? 38 : 44;
  const providerLabelFontSize = isTightHeight
    ? '1.25rem'
    : isCompactHeight
      ? '1.5rem'
      : '1.875rem';
  const headingClass = isTightHeight
    ? 'text-4xl md:text-5xl lg:text-5xl xl:text-6xl'
    : isCompactHeight
      ? 'text-4xl md:text-5xl lg:text-6xl xl:text-6xl'
      : 'text-4xl md:text-5xl lg:text-6xl xl:text-7xl';
  const subtitleClass = isTightHeight
    ? 'text-base md:text-base lg:text-lg'
    : 'text-base md:text-lg lg:text-xl';
  const heroPaddingClass = isTightHeight
    ? 'py-4 md:py-5'
    : isCompactHeight
      ? 'py-5 md:py-6'
      : 'py-6 md:py-8';
  const providerSectionSpacingClass = isTightHeight
    ? 'pt-5 md:pt-6'
    : isCompactHeight
      ? 'pt-6 md:pt-8'
      : 'pt-8 md:pt-10';
  const actionTopClass = isTightHeight
    ? 'mt-4'
    : isCompactHeight
      ? 'mt-5 md:mt-6'
      : 'mt-6 md:mt-7';
  const providerGapClass = isTightHeight
    ? 'gap-2 sm:gap-2 md:gap-3 lg:gap-4'
    : isCompactHeight
      ? 'gap-2 sm:gap-3 md:gap-4 lg:gap-5'
      : 'gap-3 sm:gap-4 md:gap-6 lg:gap-8';
  const desktopHeroMinHeight = Math.max(
    isTightHeight ? 520 : isCompactHeight ? 580 : 640,
    desktopAvailableHeight,
  );

  const providerIconRenderers = [
    (size) => <Moonshot size={size} />,
    (size) => <OpenAI size={size} />,
    (size) => <XAI size={size} />,
    (size) => <Zhipu.Color size={size} />,
    (size) => <Volcengine.Color size={size} />,
    (size) => <Cohere.Color size={size} />,
    (size) => <Claude.Color size={size} />,
    (size) => <Gemini.Color size={size} />,
    (size) => <Suno size={size} />,
    (size) => <Minimax.Color size={size} />,
    (size) => <Wenxin.Color size={size} />,
    (size) => <Spark.Color size={size} />,
    (size) => <Qingyan.Color size={size} />,
    (size) => <DeepSeek.Color size={size} />,
    (size) => <Qwen.Color size={size} />,
    (size) => <Midjourney size={size} />,
    (size) => <Grok size={size} />,
    (size) => <AzureAI.Color size={size} />,
    (size) => <Hunyuan.Color size={size} />,
    (size) => <Xinference.Color size={size} />,
  ];

  const displayHomePageContent = async () => {
    setHomePageContent(localStorage.getItem('home_page_content') || '');
    const res = await API.get('/api/home_page_content');
    const { success, message, data } = res.data;
    if (success) {
      let content = data;
      if (!data.startsWith('https://')) {
        content = marked.parse(data);
      }
      setHomePageContent(content);
      localStorage.setItem('home_page_content', content);

      // 如果内容是 URL，则发送主题模式
      if (data.startsWith('https://')) {
        const iframe = document.querySelector('iframe');
        if (iframe) {
          iframe.onload = () => {
            iframe.contentWindow.postMessage({ themeMode: actualTheme }, '*');
            iframe.contentWindow.postMessage({ lang: i18n.language }, '*');
          };
        }
      }
    } else {
      showError(message);
      setHomePageContent('加载首页内容失败...');
    }
    setHomePageContentLoaded(true);
  };

  const handleCopyBaseURL = async () => {
    const ok = await copy(serverAddress);
    if (ok) {
      showSuccess(t('已复制到剪切板'));
    }
  };

  useEffect(() => {
    const checkNoticeAndShow = async () => {
      const lastCloseDate = localStorage.getItem('notice_close_date');
      const today = new Date().toDateString();
      if (lastCloseDate !== today) {
        try {
          const res = await API.get('/api/notice');
          const { success, data } = res.data;
          if (success && data && data.trim() !== '') {
            setNoticeVisible(true);
          }
        } catch (error) {
          console.error('获取公告失败:', error);
        }
      }
    };

    checkNoticeAndShow();
  }, []);

  useEffect(() => {
    displayHomePageContent().then();
  }, []);

  useEffect(() => {
    if (endpointItems.length <= 1) {
      return undefined;
    }
    const timer = setInterval(() => {
      setEndpointTransitionEnabled(true);
      setEndpointIndex((prev) => prev + 1);
    }, ENDPOINT_ROTATE_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [endpointItems.length]);

  useEffect(() => {
    if (endpointItems.length <= 1 || endpointIndex < endpointItems.length) {
      return undefined;
    }
    let cancelled = false;
    let rafId1 = 0;
    let rafId2 = 0;
    const resetTimer = setTimeout(() => {
      if (cancelled) {
        return;
      }
      setEndpointTransitionEnabled(false);
      setEndpointIndex(0);
      rafId1 = requestAnimationFrame(() => {
        rafId2 = requestAnimationFrame(() => {
          if (!cancelled) {
            setEndpointTransitionEnabled(true);
          }
        });
      });
    }, ENDPOINT_ANIMATION_DURATION_MS);
    return () => {
      cancelled = true;
      clearTimeout(resetTimer);
      if (rafId1) {
        cancelAnimationFrame(rafId1);
      }
      if (rafId2) {
        cancelAnimationFrame(rafId2);
      }
    };
  }, [endpointIndex, endpointItems.length]);

  useEffect(() => {
    if (typeof window === 'undefined') {
      return;
    }
    const handleResize = () => setViewportHeight(getViewportHeight());
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, []);

  return (
    <div className='w-full overflow-x-hidden'>
      {noticeVisible ? (
        <NoticeModal
          visible={noticeVisible}
          onClose={() => setNoticeVisible(false)}
          isMobile={isMobile}
        />
      ) : null}
      {homePageContentLoaded && homePageContent === '' ? (
        <div className='w-full overflow-x-hidden'>
          {/* Banner 部分 */}
          <div
            className='w-full border-b border-semi-color-border relative overflow-x-hidden pt-10 md:pt-12'
            style={
              !isMobile
                ? {
                    minHeight: `${desktopHeroMinHeight}px`,
                    height: `${desktopHeroMinHeight}px`,
                  }
                : {}
            }
          >
            {/* 背景模糊晕染球 */}
            <div className='blur-ball blur-ball-indigo' />
            <div className='blur-ball blur-ball-teal' />
            <div className={`flex h-full px-4 ${heroPaddingClass}`}>
              {/* 居中内容区 */}
              <div className='flex flex-col items-center h-full w-full text-center max-w-4xl mx-auto'>
                <div
                  className={`flex flex-col items-center justify-start ${isTightHeight ? 'mb-4 md:mb-5' : 'mb-6 md:mb-8'}`}
                >
                  <h1
                    className={`${headingClass} font-bold text-semi-color-text-0 leading-tight ${isChinese ? 'tracking-wide md:tracking-wider' : ''}`}
                  >
                    <>
                      {t('统一的')}
                      <br />
                      <span className='shine-text'>{t('大模型接口网关')}</span>
                    </>
                  </h1>
                  <p
                    className={`${subtitleClass} text-semi-color-text-1 ${isTightHeight ? 'mt-3 md:mt-4' : 'mt-4 md:mt-6'} max-w-xl`}
                  >
                    {t('更好的价格，更好的稳定性，只需要将模型基址替换为：')}
                  </p>
                  {/* BASE URL 与端点选择 */}
                  <div
                    className={`flex flex-col md:flex-row items-center justify-center gap-4 w-full ${isTightHeight ? 'mt-3 md:mt-4' : 'mt-4 md:mt-6'} max-w-md`}
                  >
                    <Input
                      readonly
                      value={serverAddress}
                      className='flex-1 !rounded-full'
                      size={isMobile ? 'default' : 'large'}
                      suffix={
                        <div className='flex items-center gap-2'>
                          <div
                            role='listbox'
                            aria-label={t('API端点')}
                            style={{
                              height: `${ENDPOINT_ITEM_HEIGHT}px`,
                              overflow: 'hidden',
                              minWidth: '162px',
                            }}
                          >
                            <div
                              style={{
                                transform: `translateY(-${endpointIndex * ENDPOINT_ITEM_HEIGHT}px)`,
                                transition: endpointTransitionEnabled
                                  ? `transform ${ENDPOINT_ANIMATION_DURATION_MS}ms linear`
                                  : 'none',
                              }}
                            >
                              {endpointTrackItems.map((item, idx) => (
                                <div
                                  key={`home-endpoint-${item.value}-${idx}`}
                                  role='option'
                                  aria-selected={
                                    endpointItems.length > 0 &&
                                    idx % endpointItems.length ===
                                      endpointActiveIndex
                                  }
                                  style={{
                                    height: `${ENDPOINT_ITEM_HEIGHT}px`,
                                    lineHeight: `${ENDPOINT_ITEM_HEIGHT}px`,
                                    color: 'var(--semi-color-primary)',
                                    fontWeight: 600,
                                    whiteSpace: 'nowrap',
                                  }}
                                >
                                  {item.value}
                                </div>
                              ))}
                            </div>
                          </div>
                          <Button
                            type='primary'
                            onClick={handleCopyBaseURL}
                            icon={<IconCopy />}
                            className='!rounded-full'
                          />
                        </div>
                      }
                      name='pages-home-index-input-1'
                    />
                  </div>
                  {/* 操作按钮 */}
                  <div
                    className={`flex flex-row gap-4 justify-center items-center ${actionTopClass}`}
                  >
                    <Link to='/console'>
                      <Button
                        theme='solid'
                        type='primary'
                        size={isMobile ? 'default' : 'large'}
                        className='!rounded-3xl px-8 py-2'
                        icon={<IconPlay />}
                      >
                        {t('获取密钥')}
                      </Button>
                    </Link>
                    {isDemoSiteMode && statusState?.status?.version ? (
                      <Button
                        size={isMobile ? 'default' : 'large'}
                        className='flex items-center !rounded-3xl px-6 py-2'
                        icon={<IconGithubLogo />}
                        onClick={() =>
                          window.open(
                            'https://github.com/QuantumNous/new-api',
                            '_blank',
                          )
                        }
                      >
                        {statusState.status.version}
                      </Button>
                    ) : (
                      docsLink && (
                        <Button
                          theme='solid'
                          type='primary'
                          size={isMobile ? 'default' : 'large'}
                          className='flex items-center !rounded-3xl px-6 py-2'
                          icon={<IconFile />}
                          onClick={() => window.open(docsLink, '_blank')}
                        >
                          {t('文档')}
                        </Button>
                      )
                    )}
                  </div>
                </div>

                {/* 框架兼容性图标 */}
                <div
                  className={`${providerSectionSpacingClass} mt-auto w-full`}
                >
                  <div
                    className={`flex items-center justify-center ${isTightHeight ? 'mb-3 md:mb-4' : 'mb-6 md:mb-8'}`}
                  >
                    <Text
                      type='secondary'
                      className={`${isTightHeight ? 'text-base md:text-lg' : 'text-lg md:text-xl lg:text-2xl'} font-medium`}
                    >
                      {t('支持众多的大模型供应商')}
                    </Text>
                  </div>
                  <div
                    className={`flex flex-wrap items-center justify-center ${providerGapClass} max-w-5xl mx-auto px-4`}
                  >
                    {providerIconRenderers.map((renderIcon, index) => (
                      <div
                        key={`home-provider-icon-${index}`}
                        className='flex items-center justify-center'
                        style={{
                          width: `${providerBoxSize}px`,
                          height: `${providerBoxSize}px`,
                        }}
                      >
                        {renderIcon(providerIconSize)}
                      </div>
                    ))}
                    <div
                      className='flex items-center justify-center'
                      style={{
                        width: `${providerBoxSize}px`,
                        height: `${providerBoxSize}px`,
                      }}
                    >
                      <Typography.Text
                        className='font-bold leading-none'
                        style={{ fontSize: providerLabelFontSize }}
                      >
                        30+
                      </Typography.Text>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      ) : (
        <div className='overflow-x-hidden w-full'>
          {homePageContent.startsWith('https://') ? (
            <iframe
              src={homePageContent}
              className='w-full h-screen border-none'
            />
          ) : (
            <div
              className='mt-[60px]'
              dangerouslySetInnerHTML={{ __html: homePageContent }}
            />
          )}
        </div>
      )}
    </div>
  );
};

export default Home;
