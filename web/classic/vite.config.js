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
        // semi-ui 2.104 起在 package.json 增加了 exports 字段，但没有导出
        // ./dist/*，于是官方的整包样式入口 `@douyinfe/semi-ui/dist/css/semi.css`
        // 会被 exports 解析拒绝（文件本身仍在包里）。这里直接指向绝对路径，
        // 绕开 exports 校验；升级 semi 后若它补上导出，这条别名可以删掉。
        '@douyinfe/semi-ui/dist/css/semi.css': path.resolve(
          __dirname,
          './node_modules/@douyinfe/semi-ui/dist/css/semi.css',
        ),
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
          // Vite 8 底层换成 rolldown，manualChunks 只接受函数形式
          // （对象映射是 rollup 的旧写法，会报 "manualChunks is not a function"）。
          // 分块意图与原先的对象映射保持一致，按包名前缀归类。
          manualChunks(id) {
            if (!id.includes('/node_modules/')) return undefined;
            if (/\/node_modules\/(react|react-dom|react-router-dom)\//.test(id))
              return 'react-core';
            if (/\/node_modules\/@douyinfe\/semi-(ui|icons)\//.test(id))
              return 'semi-ui';
            if (/\/node_modules\/(axios|history|marked)\//.test(id))
              return 'tools';
            if (/\/node_modules\/lucide-react\//.test(id))
              return 'lucide-icons';
            if (/\/node_modules\/react-icons\//.test(id)) return 'react-icons';
            if (
              /\/node_modules\/(react-markdown|remark-breaks|remark-gfm|remark-math|rehype-highlight|rehype-katex|katex)\//.test(
                id,
              )
            )
              return 'markdown-core';
            if (/\/node_modules\/use-debounce\//.test(id)) return 'input-tools';
            if (/\/node_modules\/@visactor\//.test(id)) return 'visactor';
            if (/\/node_modules\/react-toastify\//.test(id)) return 'toast';
            if (
              /\/node_modules\/(react-telegram-login|react-turnstile)\//.test(
                id,
              )
            )
              return 'auth-widgets';
            if (/\/node_modules\/react-dropzone\//.test(id))
              return 'file-upload';
            return undefined;
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
