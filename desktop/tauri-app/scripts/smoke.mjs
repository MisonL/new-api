import { execFileSync } from 'node:child_process';
import { tauriAppDir } from './lib/project.mjs';

function run(command, args, cwd = tauriAppDir) {
  execFileSync(command, args, {
    cwd,
    stdio: 'inherit',
    env: process.env,
  });
}

function main() {
  run('bun', ['run', 'prepare:version']);
  run('bun', ['run', 'check:version']);
  run('bun', ['run', 'prepare:web']);
  run('bun', ['run', 'prepare:sidecar']);
  run('bun', ['run', 'test:rust']);
  run('bun', ['run', 'check:rust']);
}

main();
