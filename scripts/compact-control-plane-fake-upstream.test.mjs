import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { createHash } from 'node:crypto';
import { once } from 'node:events';
import { createServer } from 'node:http';
import test from 'node:test';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const fakeUpstreamScript = resolve(repoRoot, 'scripts/compact-control-plane-fake-upstream.mjs');

test('fake upstream retains only sanitized outbound request metadata', async () => {
  const port = await reservePort();
  const child = spawn(process.execPath, [fakeUpstreamScript], {
    cwd: repoRoot,
    env: {
      ...process.env,
      COMPACT_E2E_FAKE_UPSTREAM_HOST: '127.0.0.1',
      COMPACT_E2E_FAKE_UPSTREAM_PORT: String(port),
      COMPACT_E2E_FAKE_UPSTREAM_ACCEPT_ALL_STREAMS: '1',
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  const stderr = [];
  child.stderr.on('data', (chunk) => stderr.push(chunk));

  try {
    await waitForReady(`http://127.0.0.1:${port}`);
    const bodySecret = 'BODY_SECRET_MUST_NOT_BE_CAPTURED';
    const authSecret = 'AUTH_SECRET_MUST_NOT_BE_CAPTURED';
    const schemaSecret = 'SCHEMA_SECRET_MUST_NOT_BE_CAPTURED';
    const unknownRootKeySecret = 'UNKNOWN_ROOT_KEY_SECRET_MUST_NOT_BE_CAPTURED';
    const requestBody = JSON.stringify({
      model: 'gpt-5.6-sol',
      stream: true,
      previous_response_id: 'resp_previous_secret',
      instructions: bodySecret,
      reasoning: { context: 'all_turns', mode: 'summary' },
      stream_options: { reasoning_summary_delivery: 'sequential_cutoff' },
      tools: [{ type: 'function', name: 'top_level', description: schemaSecret }],
      [unknownRootKeySecret]: bodySecret,
      input: [
        {
          type: 'additional_tools',
          role: 'developer',
          tools: [{ type: 'function', name: 'nested', description: schemaSecret }],
        },
        {
          type: 'message',
          role: 'user',
          content: [{ type: 'input_text', text: bodySecret }],
        },
      ],
    });
    const response = await fetch(`http://127.0.0.1:${port}/v1/responses`, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${authSecret}`,
        'Content-Type': 'application/json',
        Accept: 'text/event-stream',
        Originator: 'codex-tui',
        'User-Agent': 'codex-tui/test',
        'X-Codex-Turn-Metadata': '{"request_kind":"compaction"}',
        'X-OpenAI-Internal-Codex-Responses-Lite': 'true',
      },
      body: requestBody,
    });
    assert.equal(response.status, 200);
    assert.match(response.headers.get('content-type') ?? '', /^text\/event-stream/);

    const captures = await getJSON(`http://127.0.0.1:${port}/_captures/summary`);
    assert.equal(captures.length, 1);
    const [capture] = captures;
    assert.equal(capture.bodyBytes > 0, true);
    assert.equal(capture.bodySHA256, createHash('sha256').update(requestBody).digest('hex'));
    assert.equal(capture.declaredContentLength, capture.bodyBytes);
    assert.equal(capture.contentLengthMatchesBody, true);
    assert.equal(capture.request.previousResponseID.present, true);
    assert.equal(capture.request.previousResponseID.type, 'string');
    assert.equal(capture.request.instructions.present, true);
    assert.equal(capture.request.instructions.type, 'string');
    assert.deepEqual(capture.rootShape.knownKeys, [
      'input',
      'instructions',
      'model',
      'previous_response_id',
      'reasoning',
      'stream',
      'stream_options',
      'tools',
    ]);
    assert.equal(capture.rootShape.unknownKeyCount, 1);
    assert.deepEqual(capture.inputShapes, [
      {
        type: 'additional_tools',
        present: true,
        keyCount: 3,
        knownKeys: ['role', 'tools', 'type'],
        unknownKeyCount: 0,
      },
      {
        type: 'message',
        present: true,
        keyCount: 3,
        knownKeys: ['content', 'role', 'type'],
        unknownKeyCount: 0,
      },
    ]);
    assert.equal(capture.reasoning.context.present, true);
    assert.equal(capture.reasoning.context.type, 'string');
    assert.equal(capture.tools.topLevelCount, 1);
    assert.equal(capture.tools.inputAdditionalCount, 1);
    assert.equal(capture.headers.authorization, undefined);
    assert.equal(capture.headers.contentType, true);
    assert.equal(capture.headers.originator, true);
    assert.equal(capture.headers.userAgent, true);
    assert.equal(capture.headers.codexTurnMetadata, true);
    assert.equal(capture.headers.responsesLite, true);

    const serialized = JSON.stringify(captures);
    for (const secret of [bodySecret, authSecret, schemaSecret, unknownRootKeySecret, 'resp_previous_secret']) {
      assert.equal(serialized.includes(secret), false, `capture leaked ${secret}`);
    }

    const rawCaptures = await getJSON(`http://127.0.0.1:${port}/_captures`);
    assert.deepEqual(rawCaptures, captures);
  } finally {
    child.kill('SIGTERM');
    await once(child, 'close');
  }

  assert.equal(Buffer.concat(stderr).toString('utf8'), '');
});

async function reservePort() {
  const server = await new Promise((resolveServer, rejectServer) => {
    const candidate = createServer();
    candidate.once('error', rejectServer);
    candidate.listen(0, '127.0.0.1', () => resolveServer(candidate));
  });
  const address = server.address();
  assert.ok(address && typeof address !== 'string');
  await new Promise((resolveClose) => server.close(resolveClose));
  return address.port;
}

async function waitForReady(baseURL) {
  let lastError = null;
  for (let attempt = 0; attempt < 40; attempt += 1) {
    try {
      const response = await fetch(`${baseURL}/_captures/summary`);
      if (response.ok) return;
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolveDelay) => setTimeout(resolveDelay, 50));
  }
  throw lastError ?? new Error('fake upstream did not become ready');
}

async function getJSON(url) {
  const response = await fetch(url);
  assert.equal(response.ok, true);
  return response.json();
}
