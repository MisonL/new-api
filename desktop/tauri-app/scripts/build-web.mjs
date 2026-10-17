import { execFileSync } from 'node:child_process';
import { cpSync, existsSync, rmSync } from 'node:fs';
import { resolve } from 'node:path';
import { readVersion, repoRoot, tauriAppDir, webDir } from './lib/project.mjs';

function run(command, args, cwd, env = {}) {
  execFileSync(command, args, {
    cwd,
    env: {
      ...process.env,
      ...env,
    },
    stdio: 'inherit',
  });
}

function buildWeb() {
  const version = readVersion();
  run('bun', ['install', '--backend=copyfile', '--frozen-lockfile'], webDir);
  run('bun', ['run', 'build'], webDir, {
    DISABLE_ESLINT_PLUGIN: 'true',
    VITE_REACT_APP_VERSION: version,
  });
  // 桌面产物同样内嵌前端 bundle，必须与 Web/镜像走同一道产物版本门禁：
  // audit 只读 lockfile，证明不了 dist 里打包的是哪个版本。
  run('bun', ['scripts/frontend-bundle-versions.mjs', '--only', 'web/classic'], repoRoot);
  const webDistDir = resolve(webDir, 'dist');
  const tauriDistDir = resolve(tauriAppDir, 'dist');
  if (!existsSync(webDistDir)) {
    throw new Error(`web dist not found: ${webDistDir}`);
  }
  rmSync(tauriDistDir, { recursive: true, force: true });
  cpSync(webDistDir, tauriDistDir, { recursive: true });
  console.log(`prepared web dist for desktop: ${version} -> ${tauriDistDir}`);
}

buildWeb();
