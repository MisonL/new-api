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

import react from '@vitejs/plugin-react';
import { defineConfig, transformWithEsbuild } from 'vite';
import pkg from '@douyinfe/vite-plugin-semi';
import path from 'path';
import { codeInspectorPlugin } from 'code-inspector-plugin';
const { vitePluginSemi } = pkg;

// Silence the known Browserslist stale-data notice when upstream has no newer dataset.
process.env.BROWSERSLIST_IGNORE_OLD_DATA = '1';

// https://vitejs.dev/config/
export default defineConfig(({ command }) => {
  const plugins = [
    {
      name: 'treat-js-files-as-jsx',
      async transform(code, id) {
        if (!/src\/.*\.js$/.test(id)) {
          return null;
        }

        // Use the exposed transform from vite, instead of directly
        // transforming with esbuild
        return transformWithEsbuild(code, id, {
          loader: 'jsx',
          jsx: 'automatic',
        });
      },
    },
    react(),
    vitePluginSemi({
      cssLayer: true,
    }),
  ];

  if (command === 'serve') {
    plugins.unshift(
      codeInspectorPlugin({
        bundler: 'vite',
      }),
    );
  }

  return {
    resolve: {
      alias: {
        '@': path.resolve(__dirname, './src'),
      },
    },
    plugins,
    optimizeDeps: {
      force: true,
      esbuildOptions: {
        loader: {
          '.js': 'jsx',
          '.json': 'json',
        },
      },
    },
    build: {
      chunkSizeWarningLimit: 2000,
      rollupOptions: {
        onwarn(warning, defaultHandler) {
          const warningId =
            typeof warning.id === 'string'
              ? warning.id.replaceAll('\\', '/')
              : '';
          const isKnownLottieEvalWarning =
            warning.code === 'EVAL' &&
            warningId.includes(
              '/node_modules/lottie-web/build/player/lottie.js',
            );
          if (isKnownLottieEvalWarning) {
            return;
          }
          defaultHandler(warning);
        },
        output: {
          manualChunks: {
            'react-core': ['react', 'react-dom', 'react-router-dom'],
            'semi-ui': ['@douyinfe/semi-icons', '@douyinfe/semi-ui'],
            tools: ['axios', 'history', 'marked'],
            'lucide-icons': ['lucide-react'],
            'react-icons': ['react-icons'],
            'markdown-core': [
              'react-markdown',
              'remark-breaks',
              'remark-gfm',
              'remark-math',
              'rehype-highlight',
              'rehype-katex',
              'katex',
              'clsx',
              'use-debounce',
            ],
            mermaid: ['mermaid'],
            visactor: [
              '@visactor/react-vchart',
              '@visactor/vchart',
              '@visactor/vchart-semi-theme',
            ],
            cytoscape: ['cytoscape'],
            'react-components': [
              'react-dropzone',
              'react-telegram-login',
              'react-toastify',
              'react-turnstile',
            ],
            i18n: [
              'i18next',
              'react-i18next',
              'i18next-browser-languagedetector',
            ],
          },
        },
      },
    },
    server: {
      host: '0.0.0.0',
      proxy: {
        '/api': {
          target: 'http://localhost:3000',
          changeOrigin: true,
        },
        '/mj': {
          target: 'http://localhost:3000',
          changeOrigin: true,
        },
        '/pg': {
          target: 'http://localhost:3000',
          changeOrigin: true,
        },
      },
    },
  };
});
