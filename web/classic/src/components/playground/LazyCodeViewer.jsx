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

import React, { lazy, Suspense } from 'react';

const CodeViewer = lazy(() => import('./CodeViewer'));

const stringifyPreviewContent = (content) => {
  if (content === undefined || content === null) {
    return '';
  }
  if (typeof content === 'string') {
    return content;
  }
  try {
    return JSON.stringify(content, null, 2);
  } catch (error) {
    return String(content);
  }
};

const LazyCodeViewer = (props) => (
  <Suspense
    fallback={
      <pre
        style={{
          margin: 0,
          padding: 12,
          maxHeight: '100%',
          overflow: 'auto',
          borderRadius: 8,
          background: 'var(--semi-color-fill-0)',
          color: 'var(--semi-color-text-1)',
          fontSize: 12,
          whiteSpace: 'pre-wrap',
        }}
      >
        {stringifyPreviewContent(props.content)}
      </pre>
    }
  >
    <CodeViewer {...props} />
  </Suspense>
);

export default LazyCodeViewer;
