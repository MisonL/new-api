#!/usr/bin/env node
import { runCli } from '../src/cli.js';

runCli(process.argv.slice(2), {
  env: process.env,
  stdin: process.stdin,
  stdout: process.stdout,
  stderr: process.stderr,
}).catch((error) => {
  const message = error && error.message ? error.message : String(error);
  process.stderr.write(`Error: ${message}\n`);
  process.exitCode = 1;
});
