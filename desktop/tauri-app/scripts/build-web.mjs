import { execFileSync } from 'node:child_process';
import { readVersion, webDir } from './lib/project.mjs';

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
  console.log(`prepared web dist for desktop: ${version}`);
}

buildWeb();
