import http from 'node:http';
import { URL } from 'node:url';

const host = process.env.COMPACT_E2E_FAKE_UPSTREAM_HOST || '127.0.0.1';
const port = Number.parseInt(process.env.COMPACT_E2E_FAKE_UPSTREAM_PORT || '18081', 10);
const captures = [];

function readBody(req) {
  return new Promise((resolve, reject) => {
    let body = '';
    req.setEncoding('utf8');
    req.on('data', (chunk) => {
      body += chunk;
    });
    req.on('end', () => resolve(body));
    req.on('error', reject);
  });
}

function sendJson(res, status, obj) {
  const body = JSON.stringify(obj);
  res.writeHead(status, {
    'content-type': 'application/json',
    'content-length': Buffer.byteLength(body),
  });
  res.end(body);
}

function summarizeCapture(req, url, body) {
  let parsed = null;
  try {
    parsed = body ? JSON.parse(body) : null;
  } catch {
    parsed = null;
  }
  const input = Array.isArray(parsed?.input) ? parsed.input : [];
  return {
    method: req.method,
    url: url.pathname,
    model: parsed?.model || null,
    previous_response_id: parsed?.previous_response_id || '',
    inputTypes: input.map((item) => item?.type).filter(Boolean),
    hasCompaction: input.some((item) => item?.type === 'compaction'),
    bodyBytes: Buffer.byteLength(body),
  };
}

function fakeResponsesBody(parsed) {
  return {
    id: 'resp_fake_response',
    object: 'response',
    created_at: Math.floor(Date.now() / 1000),
    model: parsed?.model || 'gpt-5.5',
    output: [
      {
        type: 'message',
        role: 'assistant',
        content: [
          {
            type: 'output_text',
            text: 'Continue the task from the latest compact marker and keep the current goal active.',
          },
        ],
      },
    ],
    usage: { input_tokens: 128, output_tokens: 48, total_tokens: 176 },
  };
}

function fakeCompactBody(parsed) {
  return {
    id: 'resp_fake_compact',
    object: 'response',
    created_at: Math.floor(Date.now() / 1000),
    model: parsed?.model || 'gpt-5.5-openai-compact',
    output: [
      {
        type: 'message',
        role: 'assistant',
        content: [
          {
            type: 'output_text',
            text: 'Compact summary for Codex continuation: keep task state, preserve verification status, and continue from the latest objective.',
          },
        ],
      },
      { type: 'compaction', encrypted_content: 'fake-native-opaque' },
    ],
    usage: { input_tokens: 3000, output_tokens: 120, total_tokens: 3120 },
    compaction: { encrypted_content: 'fake-native-opaque' },
  };
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, `http://${host}:${port}`);
  if (url.pathname === '/_reset') {
    captures.length = 0;
    return sendJson(res, 200, { ok: true });
  }
  if (url.pathname === '/_captures') {
    return sendJson(res, 200, captures);
  }
  if (url.pathname === '/_captures/summary') {
    return sendJson(
      res,
      200,
      captures.map((item) => ({
        url: item.url,
        model: item.model,
        previous_response_id: item.previous_response_id,
        inputTypes: item.inputTypes,
        hasCompaction: item.hasCompaction,
        bodyBytes: item.bodyBytes,
      })),
    );
  }

  let body = '';
  try {
    body = await readBody(req);
  } catch (err) {
    return sendJson(res, 400, { error: 'read request body failed', message: err.message });
  }
  let parsed = null;
  try {
    parsed = body ? JSON.parse(body) : null;
  } catch {
    parsed = null;
  }
  captures.push(summarizeCapture(req, url, body));

  if (url.pathname === '/v1/models') {
    return sendJson(res, 200, {
      data: [
        { id: 'gpt-5.5' },
        { id: 'gpt-5.5-openai-compact' },
        { id: 'gpt-5.4' },
        { id: 'gpt-5.4-openai-compact' },
      ],
    });
  }
  if (url.pathname === '/v1/responses/compact') {
    return sendJson(res, 200, fakeCompactBody(parsed));
  }
  if (url.pathname === '/v1/responses') {
    return sendJson(res, 200, fakeResponsesBody(parsed));
  }
  if (url.pathname === '/v1/chat/completions') {
    return sendJson(res, 200, {
      id: 'chatcmpl_fake',
      object: 'chat.completion',
      created: Math.floor(Date.now() / 1000),
      model: parsed?.model || 'gpt-5.5',
      choices: [
        {
          index: 0,
          message: { role: 'assistant', content: 'chat ok' },
          finish_reason: 'stop',
        },
      ],
      usage: { prompt_tokens: 12, completion_tokens: 8, total_tokens: 20 },
    });
  }
  return sendJson(res, 404, { error: 'not found', path: url.pathname });
});

server.listen(port, host, () => {
  console.log(`compact fake upstream listening on ${host}:${port}`);
});
