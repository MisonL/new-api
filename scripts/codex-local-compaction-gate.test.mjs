import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { createServer } from 'node:http';
import test from 'node:test';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const gateScript = resolve(repoRoot, 'scripts/codex-local-compaction-gate.mjs');

test('local compaction gate accepts a bounded SSE completion', async () => {
  const requests = [];
  const server = createServer(async (request, response) => {
    const body = await readRequestBody(request);
    requests.push({
      authorization: request.headers.authorization,
      turnMetadata: request.headers['x-codex-turn-metadata'],
      body: JSON.parse(body),
    });
    response.writeHead(200, { 'content-type': 'text/event-stream' });
    response.end([
      'event: response.completed',
      'data: {"type":"response.completed","response":{"status":"completed","output":[]}}',
      '',
    ].join('\n'));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');

  const address = server.address();
  assert.ok(address && typeof address !== 'string');
  const child = spawn(
    process.execPath,
    [
      gateScript,
      '--base-url',
      `http://127.0.0.1:${address.port}`,
      '--token-env',
      'NEWAPI_GATE_TEST_TOKEN',
      '--channel',
      '155',
      '--model',
      'gpt-5.6-sol',
      '--tool-bytes',
      '1048576',
      '--timeout-ms',
      '5000',
    ],
    {
      cwd: repoRoot,
      env: {
        ...process.env,
        NEWAPI_GATE_TEST_TOKEN: 'test-gate-token',
      },
    },
  );
  const stdout = [];
  const stderr = [];
  child.stdout.on('data', (chunk) => stdout.push(chunk));
  child.stderr.on('data', (chunk) => stderr.push(chunk));
  const [exitCode] = await once(child, 'close');
  await new Promise((resolveClose) => server.close(resolveClose));

  assert.equal(exitCode, 0);
  assert.equal(Buffer.concat(stderr).toString('utf8'), '');
  const result = JSON.parse(Buffer.concat(stdout).toString('utf8'));
  assert.equal(result.status, 'passed');
  assert.equal(result.completed, true);
  assert.equal(result.response_format, 'sse');
  assert.equal(result.request_body_bytes > 1048576, true);
  assert.equal(requests.length, 1);
  assert.equal(requests[0].authorization, 'Bearer test-gate-token-155');
  assert.equal(
    requests[0].turnMetadata,
    '{"request_kind":"compaction","compaction":{"implementation":"responses"}}',
  );
  assert.equal(requests[0].body.input[0].type, 'additional_tools');
  assert.equal(requests[0].body.input[0].tools[0].description.length, 1048576);
});

test('local compaction gate builds the current reduced-request shape without replaying tools', async () => {
  const requests = [];
  const server = createServer(async (request, response) => {
    const body = await readRequestBody(request);
    requests.push(JSON.parse(body));
    response.writeHead(200, { 'content-type': 'text/event-stream' });
    response.end([
      'event: response.completed',
      'data: {"type":"response.completed","response":{"status":"completed","output":[]}}',
      '',
    ].join('\n'));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');

  const address = server.address();
  assert.ok(address && typeof address !== 'string');
  const child = spawn(
    process.execPath,
    [
      gateScript,
      '--base-url',
      `http://127.0.0.1:${address.port}`,
      '--token-env',
      'NEWAPI_GATE_TEST_TOKEN',
      '--channel',
      '155',
      '--model',
      'gpt-5.6-sol',
      '--tool-bytes',
      '1024',
      '--request-shape',
      'reduced_current',
    ],
    {
      cwd: repoRoot,
      env: {
        ...process.env,
        NEWAPI_GATE_TEST_TOKEN: 'test-gate-token',
      },
    },
  );
  const [exitCode] = await once(child, 'close');
  await new Promise((resolveClose) => server.close(resolveClose));

  assert.equal(exitCode, 0);
  assert.equal(requests.length, 1);
  assert.equal(requests[0].input.length, 1);
  assert.equal(requests[0].input[0].type, 'message');
  assert.equal(requests[0].input[0].role, 'user');
  assert.equal('tools' in requests[0], false);
  assert.equal('instructions' in requests[0], true);
  assert.equal(requests[0].reasoning.context, 'all_turns');
  assert.equal(requests[0].stream_options.reasoning_summary_delivery, 'sequential_cutoff');
});

test('local compaction gate reports a non-413 upstream failure without claiming success', async () => {
  const server = createServer((request, response) => {
    request.resume();
    response.writeHead(502, { 'content-type': 'application/json' });
    response.end('{"error":{"type":"upstream_error","code":"bad_gateway"}}');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');

  const address = server.address();
  assert.ok(address && typeof address !== 'string');
  const child = spawn(
    process.execPath,
    [
      gateScript,
      '--base-url',
      `http://127.0.0.1:${address.port}`,
      '--token-env',
      'NEWAPI_GATE_TEST_TOKEN',
      '--channel',
      '155',
      '--model',
      'gpt-5.6-sol',
      '--tool-bytes',
      '1024',
    ],
    {
      cwd: repoRoot,
      env: {
        ...process.env,
        NEWAPI_GATE_TEST_TOKEN: 'test-gate-token',
      },
    },
  );
  const stdout = [];
  const stderr = [];
  child.stdout.on('data', (chunk) => stdout.push(chunk));
  child.stderr.on('data', (chunk) => stderr.push(chunk));
  const [exitCode] = await once(child, 'close');
  await new Promise((resolveClose) => server.close(resolveClose));

  assert.notEqual(exitCode, 0);
  const result = JSON.parse(Buffer.concat(stdout).toString('utf8'));
  assert.equal(result.http_status, 502);
  assert.equal(result.completed, false);
  assert.equal(result.error_code, 'bad_gateway');
  assert.equal(result.failure_class, 'http_502');
  assert.match(Buffer.concat(stderr).toString('utf8'), /COMPACTION_GATE_FAILED HTTP 502 bad_gateway/);
});

test('local compaction gate reports a transport timeout without claiming success', async () => {
  const server = createServer((request) => {
    request.resume();
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');

  const address = server.address();
  assert.ok(address && typeof address !== 'string');
  const child = spawn(
    process.execPath,
    [
      gateScript,
      '--base-url',
      `http://127.0.0.1:${address.port}`,
      '--token-env',
      'NEWAPI_GATE_TEST_TOKEN',
      '--channel',
      '155',
      '--model',
      'gpt-5.6-sol',
      '--tool-bytes',
      '1024',
      '--timeout-ms',
      '1000',
    ],
    {
      cwd: repoRoot,
      env: {
        ...process.env,
        NEWAPI_GATE_TEST_TOKEN: 'test-gate-token',
      },
    },
  );
  const stdout = [];
  const stderr = [];
  child.stdout.on('data', (chunk) => stdout.push(chunk));
  child.stderr.on('data', (chunk) => stderr.push(chunk));
  const [exitCode] = await once(child, 'close');
  server.closeAllConnections?.();
  await new Promise((resolveClose) => server.close(resolveClose));

  assert.notEqual(exitCode, 0);
  const result = JSON.parse(Buffer.concat(stdout).toString('utf8'));
  assert.equal(result.http_status, null);
  assert.equal(result.completed, false);
  assert.equal(result.failure_class, 'timeout');
  assert.equal(result.transport_error, 'TimeoutError');
  assert.equal(result.transport_error_code, null);
  assert.match(Buffer.concat(stderr).toString('utf8'), /COMPACTION_GATE_FAILED request failed: TimeoutError/);
});

test('local compaction gate exposes a safe connection cause without exposing endpoint details', async () => {
  const server = createServer((request) => request.destroy());
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');

  const address = server.address();
  assert.ok(address && typeof address !== 'string');
  const child = spawn(
    process.execPath,
    [
      gateScript,
      '--base-url',
      `http://127.0.0.1:${address.port}`,
      '--token-env',
      'NEWAPI_GATE_TEST_TOKEN',
      '--channel',
      '155',
      '--model',
      'gpt-5.6-sol',
      '--tool-bytes',
      '1024',
      '--timeout-ms',
      '5000',
    ],
    {
      cwd: repoRoot,
      env: {
        ...process.env,
        NEWAPI_GATE_TEST_TOKEN: 'test-gate-token',
      },
    },
  );
  const stdout = [];
  const stderr = [];
  child.stdout.on('data', (chunk) => stdout.push(chunk));
  child.stderr.on('data', (chunk) => stderr.push(chunk));
  const [exitCode] = await once(child, 'close');
  server.closeAllConnections?.();
  await new Promise((resolveClose) => server.close(resolveClose));

  assert.notEqual(exitCode, 0);
  const result = JSON.parse(Buffer.concat(stdout).toString('utf8'));
  assert.equal(result.http_status, null);
  assert.equal(result.failure_class, 'transport_error');
  assert.equal(result.transport_error_code, 'UND_ERR_SOCKET');
  assert.doesNotMatch(Buffer.concat(stderr).toString('utf8'), /127\.0\.0\.1/);
});

function readRequestBody(request) {
  return new Promise((resolveBody, rejectBody) => {
    const chunks = [];
    request.on('data', (chunk) => chunks.push(chunk));
    request.on('end', () => resolveBody(Buffer.concat(chunks).toString('utf8')));
    request.on('error', rejectBody);
  });
}
