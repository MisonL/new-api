import { copyFileSync, existsSync, mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { execFileSync } from 'node:child_process';
import { readVersion, repoRoot, tauriAppDir } from './lib/project.mjs';

const binariesDir = join(tauriAppDir, 'src-tauri', 'binaries');
const goCacheDir = join(tauriAppDir, '.cache', 'go-build');
const goModCacheDir = join(tauriAppDir, '.cache', 'gomod');

function resolveTargetTriple() {
  return (
    process.env.TAURI_ENV_TARGET_TRIPLE ||
    process.env.CARGO_BUILD_TARGET ||
    execFileSync('rustc', ['-vV'], {
      cwd: repoRoot,
      encoding: 'utf8',
    })
      .split('\n')
      .find((line) => line.startsWith('host: '))
      ?.replace('host: ', '')
      .trim()
  );
}

function resolveGoTarget(targetTriple) {
  if (!targetTriple) {
    throw new Error('unable to resolve Rust target triple');
  }

  if (targetTriple.startsWith('x86_64-pc-windows')) {
    return { goos: 'windows', goarch: 'amd64', executable: true };
  }
  if (targetTriple.startsWith('aarch64-pc-windows')) {
    return { goos: 'windows', goarch: 'arm64', executable: true };
  }
  if (targetTriple.startsWith('x86_64-apple-darwin')) {
    return { goos: 'darwin', goarch: 'amd64', executable: false };
  }
  if (targetTriple.startsWith('aarch64-apple-darwin')) {
    return { goos: 'darwin', goarch: 'arm64', executable: false };
  }
  if (targetTriple.startsWith('x86_64-unknown-linux')) {
    return { goos: 'linux', goarch: 'amd64', executable: false };
  }
  if (targetTriple.startsWith('aarch64-unknown-linux')) {
    return { goos: 'linux', goarch: 'arm64', executable: false };
  }

  throw new Error(`unsupported target triple: ${targetTriple}`);
}

function buildSidecar() {
  const targetTriple = resolveTargetTriple();
  const goTarget = resolveGoTarget(targetTriple);
  const version = readVersion();
  const outputName = `new-api-${targetTriple}${goTarget.executable ? '.exe' : ''}`;
  const outputPath = join(binariesDir, outputName);

  mkdirSync(binariesDir, { recursive: true });
  mkdirSync(goCacheDir, { recursive: true });
  mkdirSync(goModCacheDir, { recursive: true });

  execFileSync(
    'go',
    [
      'build',
      '-ldflags',
      `-s -w -X github.com/QuantumNous/new-api/common.Version=${version}`,
      '-o',
      outputPath,
      '.',
    ],
    {
      cwd: repoRoot,
      env: {
        ...process.env,
        GOOS: goTarget.goos,
        GOARCH: goTarget.goarch,
        GOCACHE: process.env.GOCACHE || goCacheDir,
        GOMODCACHE: process.env.GOMODCACHE || goModCacheDir,
      },
      stdio: 'inherit',
    },
  );

  syncDirectRunSidecar(outputPath, goTarget);
  console.log(`prepared sidecar: ${outputPath}`);
}

function syncDirectRunSidecar(outputPath, goTarget) {
  const executableName = `new-api-tauri-desktop${goTarget.executable ? '.exe' : ''}`;
  const sidecarName = `new-api${goTarget.executable ? '.exe' : ''}`;
  const directRunTargets = ['debug', 'release']
    .map((profile) => ({
      profile,
      appPath: join(tauriAppDir, 'src-tauri', 'target', profile, executableName),
      sidecarPath: join(tauriAppDir, 'src-tauri', 'target', profile, sidecarName),
    }))
    .filter(({ appPath }) => existsSync(appPath));

  for (const target of directRunTargets) {
    mkdirSync(join(tauriAppDir, 'src-tauri', 'target', target.profile), {
      recursive: true,
    });
    copyFileSync(outputPath, target.sidecarPath);
    console.log(`synced direct-run sidecar: ${target.sidecarPath}`);
  }
}

buildSidecar();
