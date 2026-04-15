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

import React, { useState, useMemo, useCallback } from 'react';
import { Button, Tooltip, Toast } from '@douyinfe/semi-ui';
import { Copy, ChevronDown, ChevronUp } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { copy } from '../../helpers';
import { useActualTheme } from '../../context/Theme';

const PERFORMANCE_CONFIG = {
  MAX_DISPLAY_LENGTH: 50000, // 最大显示字符数
  PREVIEW_LENGTH: 5000, // 预览长度
  VERY_LARGE_MULTIPLIER: 2, // 超大内容倍数
};

const codeThemePalettes = {
  light: {
    background: '#f8fafc',
    foreground: '#1f2937',
    border: '#d7e0ea',
    shadow: '0 8px 24px rgba(15, 23, 42, 0.08)',
    buttonBg: 'rgba(255, 255, 255, 0.92)',
    buttonBorder: 'rgba(148, 163, 184, 0.35)',
    buttonText: '#334155',
    buttonHoverBg: 'rgba(255, 255, 255, 0.98)',
    buttonHoverBorder: 'rgba(100, 116, 139, 0.45)',
    mutedText: '#64748b',
    warningBg: 'rgba(245, 158, 11, 0.12)',
    warningBorder: 'rgba(245, 158, 11, 0.28)',
    warningText: '#b45309',
    spinnerTrack: '#cbd5e1',
    spinnerHead: '#64748b',
    jsonKey: '#0f766e',
    jsonString: '#b45309',
    jsonKeyword: '#1d4ed8',
  },
  dark: {
    background: '#111827',
    foreground: '#e5eefb',
    border: '#334155',
    shadow: '0 10px 28px rgba(0, 0, 0, 0.35)',
    buttonBg: 'rgba(15, 23, 42, 0.92)',
    buttonBorder: 'rgba(148, 163, 184, 0.18)',
    buttonText: '#dbe7f5',
    buttonHoverBg: 'rgba(30, 41, 59, 0.98)',
    buttonHoverBorder: 'rgba(148, 163, 184, 0.28)',
    mutedText: '#94a3b8',
    warningBg: 'rgba(245, 158, 11, 0.18)',
    warningBorder: 'rgba(245, 158, 11, 0.32)',
    warningText: '#fbbf24',
    spinnerTrack: '#475569',
    spinnerHead: '#cbd5e1',
    jsonKey: '#7dd3fc',
    jsonString: '#fdba74',
    jsonKeyword: '#93c5fd',
  },
};

const escapeHtml = (str) => {
  return str
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;');
};

const highlightJson = (str, palette) => {
  const tokenRegex =
    /("(\\u[a-zA-Z0-9]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(true|false|null)\b|-?\d+(?:\.\d*)?(?:[eE][+-]?\d+)?)/g;

  let result = '';
  let lastIndex = 0;
  let match;

  while ((match = tokenRegex.exec(str)) !== null) {
    // Escape non-token text (structural chars like {, }, [, ], :, comma, whitespace)
    result += escapeHtml(str.slice(lastIndex, match.index));

    const token = match[0];
    let color = palette.jsonString;
    if (/^"/.test(token)) {
      color = /:$/.test(token) ? palette.jsonKey : palette.jsonString;
    } else if (/true|false|null/.test(token)) {
      color = palette.jsonKeyword;
    }
    // Escape token content before wrapping in span
    result += `<span style="color: ${color}">${escapeHtml(token)}</span>`;
    lastIndex = tokenRegex.lastIndex;
  }

  // Escape remaining text
  result += escapeHtml(str.slice(lastIndex));
  return result;
};

const linkRegex = /(https?:\/\/(?:[^\s<"'\]),;&}]|&amp;)+)/g;

const linkifyHtml = (html) => {
  const parts = html.split(/(<[^>]+>)/g);
  return parts
    .map((part) => {
      if (part.startsWith('<')) return part;
      return part.replace(
        linkRegex,
        (url) => `<a href="${url}" target="_blank" rel="noreferrer">${url}</a>`,
      );
    })
    .join('');
};

const isJsonLike = (content, language) => {
  if (language === 'json') return true;
  const trimmed = content.trim();
  return (
    (trimmed.startsWith('{') && trimmed.endsWith('}')) ||
    (trimmed.startsWith('[') && trimmed.endsWith(']'))
  );
};

const formatContent = (content) => {
  if (!content) return '';

  if (typeof content === 'object') {
    try {
      return JSON.stringify(content, null, 2);
    } catch (e) {
      return String(content);
    }
  }

  if (typeof content === 'string') {
    try {
      const parsed = JSON.parse(content);
      return JSON.stringify(parsed, null, 2);
    } catch (e) {
      return content;
    }
  }

  return String(content);
};

const CodeViewer = ({ content, title, language = 'json' }) => {
  const { t } = useTranslation();
  const actualTheme = useActualTheme();
  const [copied, setCopied] = useState(false);
  const [isHoveringCopy, setIsHoveringCopy] = useState(false);
  const [isExpanded, setIsExpanded] = useState(false);
  const [isProcessing, setIsProcessing] = useState(false);
  const palette =
    actualTheme === 'dark' ? codeThemePalettes.dark : codeThemePalettes.light;

  const formattedContent = useMemo(() => formatContent(content), [content]);

  const contentMetrics = useMemo(() => {
    const length = formattedContent.length;
    const isLarge = length > PERFORMANCE_CONFIG.MAX_DISPLAY_LENGTH;
    const isVeryLarge =
      length >
      PERFORMANCE_CONFIG.MAX_DISPLAY_LENGTH *
        PERFORMANCE_CONFIG.VERY_LARGE_MULTIPLIER;
    return { length, isLarge, isVeryLarge };
  }, [formattedContent.length]);

  const displayContent = useMemo(() => {
    if (!contentMetrics.isLarge || isExpanded) {
      return formattedContent;
    }
    return (
      formattedContent.substring(0, PERFORMANCE_CONFIG.PREVIEW_LENGTH) +
      '\n\n// ... 内容被截断以提升性能 ...'
    );
  }, [formattedContent, contentMetrics.isLarge, isExpanded]);

  const highlightedContent = useMemo(() => {
    if (contentMetrics.isVeryLarge && !isExpanded) {
      return escapeHtml(displayContent);
    }

    if (isJsonLike(displayContent, language)) {
      return highlightJson(displayContent, palette);
    }

    return escapeHtml(displayContent);
  }, [
    displayContent,
    language,
    contentMetrics.isVeryLarge,
    isExpanded,
    palette,
  ]);

  const renderedContent = useMemo(() => {
    return linkifyHtml(highlightedContent);
  }, [highlightedContent]);

  const handleCopy = useCallback(async () => {
    try {
      const textToCopy =
        typeof content === 'object' && content !== null
          ? JSON.stringify(content, null, 2)
          : content;

      const success = await copy(textToCopy);
      setCopied(true);
      Toast.success(t('已复制到剪贴板'));
      setTimeout(() => setCopied(false), 2000);

      if (!success) {
        throw new Error('Copy operation failed');
      }
    } catch (err) {
      Toast.error(t('复制失败'));
      console.error('Copy failed:', err);
    }
  }, [content, t]);

  const handleToggleExpand = useCallback(() => {
    if (contentMetrics.isVeryLarge && !isExpanded) {
      setIsProcessing(true);
      setTimeout(() => {
        setIsExpanded(true);
        setIsProcessing(false);
      }, 100);
    } else {
      setIsExpanded(!isExpanded);
    }
  }, [isExpanded, contentMetrics.isVeryLarge]);

  if (!content) {
    const placeholderText =
      {
        preview: t('正在构造请求体预览...'),
        request: t('暂无请求数据'),
        response: t('暂无响应数据'),
      }[title] || t('暂无数据');

    return (
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          height: '100%',
          color: palette.mutedText,
          fontSize: '14px',
          fontStyle: 'italic',
          backgroundColor: 'var(--semi-color-fill-0)',
          borderRadius: '8px',
          border: `1px solid ${palette.border}`,
        }}
      >
        <span>{placeholderText}</span>
      </div>
    );
  }

  const warningTop = contentMetrics.isLarge ? '52px' : '12px';
  const contentPadding = contentMetrics.isLarge ? '52px' : '16px';

  return (
    <div
      style={{
        backgroundColor: palette.background,
        color: palette.foreground,
        fontFamily: 'Consolas, "Courier New", Monaco, "SF Mono", monospace',
        fontSize: '13px',
        lineHeight: '1.4',
        borderRadius: '8px',
        border: `1px solid ${palette.border}`,
        position: 'relative',
        overflow: 'hidden',
        boxShadow: palette.shadow,
      }}
      className='h-full'
    >
      {/* 性能警告 */}
      {contentMetrics.isLarge && (
        <div
          style={{
            padding: '8px 12px',
            backgroundColor: palette.warningBg,
            border: `1px solid ${palette.warningBorder}`,
            borderRadius: '6px',
            color: palette.warningText,
            fontSize: '12px',
            marginBottom: '8px',
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
          }}
        >
          <span>INFO</span>
          <span>
            {contentMetrics.isVeryLarge
              ? t('内容较大，已启用性能优化模式')
              : t('内容较大，部分功能可能受限')}
          </span>
        </div>
      )}

      {/* 复制按钮 */}
      <div
        style={{
          position: 'absolute',
          zIndex: 10,
          backgroundColor: isHoveringCopy
            ? palette.buttonHoverBg
            : palette.buttonBg,
          border: `1px solid ${
            isHoveringCopy ? palette.buttonHoverBorder : palette.buttonBorder
          }`,
          color: palette.buttonText,
          borderRadius: '6px',
          transition: 'all 0.2s ease',
          transform: isHoveringCopy ? 'scale(1.05)' : 'scale(1)',
          top: warningTop,
          right: '12px',
        }}
        onMouseEnter={() => setIsHoveringCopy(true)}
        onMouseLeave={() => setIsHoveringCopy(false)}
      >
        <Tooltip content={copied ? t('已复制') : t('复制代码')}>
          <Button
            icon={<Copy size={14} />}
            onClick={handleCopy}
            size='small'
            theme='borderless'
            style={{
              backgroundColor: 'transparent',
              border: 'none',
              color: copied ? '#22c55e' : palette.buttonText,
              padding: '6px',
            }}
          />
        </Tooltip>
      </div>

      {/* 代码内容 */}
      <div
        style={{
          height: '100%',
          overflowY: 'auto',
          overflowX: 'auto',
          margin: 0,
          background: palette.background,
          color: palette.foreground,
          paddingTop: contentPadding,
          paddingRight: '16px',
          paddingBottom: '16px',
          paddingLeft: '16px',
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
        }}
        className='model-settings-scroll'
      >
        {isProcessing ? (
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              height: '200px',
              color: palette.mutedText,
            }}
          >
            <div
              style={{
                width: '20px',
                height: '20px',
                border: `2px solid ${palette.spinnerTrack}`,
                borderTop: `2px solid ${palette.spinnerHead}`,
                borderRadius: '50%',
                animation: 'spin 1s linear infinite',
                marginRight: '8px',
              }}
            />
            {t('正在处理大内容...')}
          </div>
        ) : (
          <div dangerouslySetInnerHTML={{ __html: renderedContent }} />
        )}
      </div>

      {/* 展开/收起按钮 */}
      {contentMetrics.isLarge && !isProcessing && (
        <div
          style={{
            position: 'absolute',
            zIndex: 10,
            backgroundColor: palette.buttonBg,
            border: `1px solid ${palette.buttonBorder}`,
            color: palette.buttonText,
            borderRadius: '6px',
            transition: 'all 0.2s ease',
            bottom: '12px',
            left: '50%',
            transform: 'translateX(-50%)',
          }}
        >
          <Tooltip content={isExpanded ? t('收起内容') : t('显示完整内容')}>
            <Button
              icon={
                isExpanded ? <ChevronUp size={14} /> : <ChevronDown size={14} />
              }
              onClick={handleToggleExpand}
              size='small'
              theme='borderless'
              style={{
                backgroundColor: 'transparent',
                border: 'none',
                color: palette.buttonText,
                padding: '6px 12px',
              }}
            >
              {isExpanded ? t('收起') : t('展开')}
              {!isExpanded && (
                <span
                  style={{ fontSize: '11px', opacity: 0.7, marginLeft: '4px' }}
                >
                  (+
                  {Math.round(
                    (contentMetrics.length -
                      PERFORMANCE_CONFIG.PREVIEW_LENGTH) /
                      1000,
                  )}
                  K)
                </span>
              )}
            </Button>
          </Tooltip>
        </div>
      )}
    </div>
  );
};

export default CodeViewer;
