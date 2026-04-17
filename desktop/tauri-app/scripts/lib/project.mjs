import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

export const tauriAppDir = resolve(__dirname, '..', '..');
export const repoRoot = resolve(tauriAppDir, '..', '..');
export const webDir = resolve(repoRoot, 'web');
export const versionFile = resolve(repoRoot, 'VERSION');
export const tauriPackageJsonFile = resolve(tauriAppDir, 'package.json');
export const tauriConfigFile = resolve(tauriAppDir, 'src-tauri', 'tauri.conf.json');
export const tauriCargoTomlFile = resolve(tauriAppDir, 'src-tauri', 'Cargo.toml');

export function readVersion() {
  return readFileSync(versionFile, 'utf8').trim();
}

export function normalizeSemver(version) {
  const trimmed = version.trim().replace(/^v/, '');
  const match = trimmed.match(/^(\d+)\.(\d+)\.(\d+)(.*)$/);

  if (!match) {
    throw new Error(`unsupported VERSION format: ${version}`);
  }

  const [, major, minor, patch, suffix] = match;
  return `${major}.${minor}.${patch}${suffix || ''}`;
}

export function syncDesktopVersionFiles() {
  const rawVersion = readVersion();
  const semver = normalizeSemver(rawVersion);

  updateJsonVersion(tauriPackageJsonFile, semver);
  updateJsonVersion(tauriConfigFile, semver);
  updateCargoPackageVersion(tauriCargoTomlFile, semver);

  return {
    rawVersion,
    semver,
  };
}

function updateJsonVersion(filePath, version) {
  const payload = JSON.parse(readFileSync(filePath, 'utf8'));
  if (payload.version === version) {
    return;
  }

  payload.version = version;
  writeFileSync(filePath, `${JSON.stringify(payload, null, 2)}\n`, 'utf8');
}

function updateCargoPackageVersion(filePath, version) {
  const original = readFileSync(filePath, 'utf8');
  const next = original.replace(
    /(^\[package\][\s\S]*?^version = )"[^"]+"/m,
    `$1"${version}"`,
  );

  if (next === original) {
    return;
  }

  writeFileSync(filePath, next, 'utf8');
}
