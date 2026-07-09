#!/usr/bin/env node

import { spawnSync } from 'node:child_process';

const passthroughIndex = process.argv.indexOf('--');

if (passthroughIndex === -1 || passthroughIndex === process.argv.length - 1) {
  console.error(
    'Usage: node run-with-node-options.mjs [--verbose] [--optional-node-option=<flag>] [--env=<key=value>] -- <command> [args...]',
  );
  process.exit(2);
}

const runnerArgs = process.argv.slice(2, passthroughIndex);
const [command, ...commandArgs] = process.argv.slice(passthroughIndex + 1);
const nodeOptions = [];
const skippedNodeOptions = [];
const envOverrides = {};
let verbose = false;

const trustedWindowsShellCommands = new Set(['rsbuild', 'vite']);
const unsafeWindowsShellPattern = /[&|<>^%!"]/;

function shouldUseWindowsShell(commandName, args) {
  if (process.platform !== 'win32') {
    return false;
  }
  if (!trustedWindowsShellCommands.has(commandName)) {
    return false;
  }
  if ([commandName, ...args].some((part) => unsafeWindowsShellPattern.test(part))) {
    console.error('Refusing to run command with unsafe Windows shell metacharacters.');
    process.exit(2);
  }
  return true;
}

for (const arg of runnerArgs) {
  if (arg === '--verbose') {
    verbose = true;
    continue;
  }

  if (arg.startsWith('--optional-node-option=')) {
    const option = arg.slice('--optional-node-option='.length);
    if (process.allowedNodeEnvironmentFlags.has(option)) {
      nodeOptions.push(option);
    } else {
      skippedNodeOptions.push(option);
    }
    continue;
  }

  if (arg.startsWith('--env=')) {
    const assignment = arg.slice('--env='.length);
    const separatorIndex = assignment.indexOf('=');
    if (separatorIndex <= 0) {
      console.error(`Invalid --env assignment: ${assignment}`);
      process.exit(2);
    }
    envOverrides[assignment.slice(0, separatorIndex)] = assignment.slice(separatorIndex + 1);
    continue;
  }

  console.error(`Unknown runner argument: ${arg}`);
  process.exit(2);
}

const env = {
  ...process.env,
  ...envOverrides,
};
const existingNodeOptions = env.NODE_OPTIONS?.trim();

if (nodeOptions.length > 0) {
  env.NODE_OPTIONS = [existingNodeOptions, ...nodeOptions].filter(Boolean).join(' ');
}

if (verbose) {
  for (const option of skippedNodeOptions) {
    console.warn(`Skipping unsupported NODE_OPTIONS flag: ${option}`);
  }
}

const result = spawnSync(command, commandArgs, {
  env,
  shell: shouldUseWindowsShell(command, commandArgs),
  stdio: 'inherit',
});

if (result.error) {
  console.error(result.error.message);
  process.exit(1);
}

if (result.signal) {
  console.error(`Child process terminated by signal: ${result.signal}`);
}

process.exit(result.status ?? 1);
