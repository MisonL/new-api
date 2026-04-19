import { readFileSync } from 'node:fs';
import {
  normalizeSemver,
  readVersion,
  tauriCargoTomlFile,
  tauriConfigFile,
  tauriPackageJsonFile,
} from './lib/project.mjs';

function readJsonVersion(filePath) {
  const payload = JSON.parse(readFileSync(filePath, 'utf8'));
  if (!payload.version) {
    throw new Error(`missing version field in ${filePath}`);
  }
  return payload.version;
}

function readCargoVersion(filePath) {
  const source = readFileSync(filePath, 'utf8');
  const match = source.match(/^\[package\][\s\S]*?^version = "([^"]+)"/m);
  if (!match) {
    throw new Error(`unable to read package version from ${filePath}`);
  }
  return match[1];
}

function main() {
  const expectedVersion = normalizeSemver(readVersion());
  const actualVersions = [
    ['desktop/tauri-app/package.json', readJsonVersion(tauriPackageJsonFile)],
    ['desktop/tauri-app/src-tauri/tauri.conf.json', readJsonVersion(tauriConfigFile)],
    ['desktop/tauri-app/src-tauri/Cargo.toml', readCargoVersion(tauriCargoTomlFile)],
  ];

  const mismatches = actualVersions.filter(([, actual]) => actual !== expectedVersion);
  if (mismatches.length > 0) {
    const detail = mismatches
      .map(([filePath, actual]) => `${filePath}: expected ${expectedVersion}, got ${actual}`)
      .join('\n');
    throw new Error(`desktop version files are out of sync with VERSION\n${detail}`);
  }

  console.log(`desktop version files are in sync: ${expectedVersion}`);
}

main();
