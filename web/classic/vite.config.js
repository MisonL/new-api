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
import fs from 'fs';
import { createRequire } from 'module';
import { codeInspectorPlugin } from 'code-inspector-plugin';
import {
  rewriteJavaScriptFilesInDirectory,
  rewriteSafariAmbiguousDecimals,
} from '../scripts/safari-compatibility.mjs';
const { vitePluginSemi } = pkg;
const require = createRequire(import.meta.url);

// Silence the known Browserslist stale-data notice when upstream has no newer dataset.
process.env.BROWSERSLIST_IGNORE_OLD_DATA = '1';

const copyDir = (sourceDir, targetDir) => {
  fs.mkdirSync(targetDir, { recursive: true });
  for (const entry of fs.readdirSync(sourceDir, { withFileTypes: true })) {
    const sourcePath = path.join(sourceDir, entry.name);
    const targetPath = path.join(targetDir, entry.name);
    if (entry.isDirectory()) {
      copyDir(sourcePath, targetPath);
    } else if (entry.isFile() && entry.name.endsWith('.mjs')) {
      fs.copyFileSync(sourcePath, targetPath);
    }
  }
};

const resolveMermaidVendorPaths = () => {
  const mermaidDistDir = path.dirname(
    require.resolve('mermaid/dist/mermaid.esm.min.mjs'),
  );
  return {
    mermaidDistDir,
    mermaidEntry: path.join(mermaidDistDir, 'mermaid.esm.min.mjs'),
    mermaidChunksDir: path.join(mermaidDistDir, 'chunks', 'mermaid.esm.min'),
  };
};

const serveMermaidVendor = (server, mermaidDistDir) => {
  server.middlewares.use((req, res, next) => {
    if (!req.url?.startsWith('/vendor/mermaid/')) {
      next();
      return;
    }
    const relativePath = decodeURIComponent(
      req.url.replace('/vendor/mermaid/', '').split('?')[0],
    );
    const filePath = path.join(mermaidDistDir, relativePath);
    const boundary = path.relative(mermaidDistDir, filePath);
    if (
      boundary.startsWith('..') ||
      path.isAbsolute(boundary) ||
      !fs.existsSync(filePath)
    ) {
      next();
      return;
    }
    res.setHeader('Content-Type', 'text/javascript; charset=utf-8');
    fs.createReadStream(filePath).pipe(res);
  });
};

const copyMermaidVendor = (outputDir, mermaidEntry, mermaidChunksDir) => {
  const targetDir = path.join(outputDir, 'vendor', 'mermaid');
  fs.mkdirSync(path.join(targetDir, 'chunks', 'mermaid.esm.min'), {
    recursive: true,
  });
  fs.copyFileSync(mermaidEntry, path.join(targetDir, 'mermaid.esm.min.mjs'));
  copyDir(mermaidChunksDir, path.join(targetDir, 'chunks', 'mermaid.esm.min'));
};

const mermaidVendorPlugin = () => {
  const { mermaidDistDir, mermaidEntry, mermaidChunksDir } =
    resolveMermaidVendorPaths();

  return {
    name: 'vendor-mermaid-prebuilt',
    configureServer(server) {
      serveMermaidVendor(server, mermaidDistDir);
    },
    writeBundle(options) {
      const outputDir =
        typeof options.dir === 'string'
          ? options.dir
          : path.resolve(__dirname, 'dist');
      copyMermaidVendor(outputDir, mermaidEntry, mermaidChunksDir);
    },
  };
};

const safariDecimalCompatibilityPlugin = () => ({
  name: 'safari-decimal-compatibility',
  renderChunk(code) {
    const rewritten = rewriteSafariAmbiguousDecimals(code);
    if (rewritten === code) {
      return null;
    }
    return {
      code: rewritten,
      map: null,
    };
  },
  writeBundle(options) {
    const outputDir =
      typeof options.dir === 'string'
        ? options.dir
        : path.resolve(__dirname, 'dist');
    rewriteJavaScriptFilesInDirectory(outputDir);
  },
});

// https://vitejs.dev/config/
export default defineConfig(({ command }) => {
  const devProxyTarget =
    process.env.VITE_DEV_PROXY_TARGET || 'http://localhost:3000';
  const fastBuild = process.env.VITE_FAST_BUILD === 'true';
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
    mermaidVendorPlugin(),
    safariDecimalCompatibilityPlugin(),
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
      minify: fastBuild ? false : 'esbuild',
      reportCompressedSize: !fastBuild,
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
            ],
            'input-tools': ['use-debounce'],
            visactor: [
              '@visactor/react-vchart',
              '@visactor/vchart',
              '@visactor/vchart-semi-theme',
            ],
            cytoscape: ['cytoscape'],
            toast: ['react-toastify'],
            'auth-widgets': ['react-telegram-login', 'react-turnstile'],
            'file-upload': ['react-dropzone'],
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
          target: devProxyTarget,
          changeOrigin: true,
        },
        '/mj': {
          target: devProxyTarget,
          changeOrigin: true,
        },
        '/pg': {
          target: devProxyTarget,
          changeOrigin: true,
        },
      },
    },
  };
});
