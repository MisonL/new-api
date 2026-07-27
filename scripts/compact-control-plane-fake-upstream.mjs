import http from 'node:http';
import crypto from 'node:crypto';
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

function sendSse(res, events) {
  const body = `${events.map((event) => `data: ${JSON.stringify(event)}\n\n`).join('')}data: [DONE]\n\n`;
  res.writeHead(200, {
    'content-type': 'text/event-stream',
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
  const streamOptions = parsed?.stream_options;
  return {
    method: req.method,
    url: url.pathname,
    model: parsed?.model || null,
    previous_response_id: parsed?.previous_response_id || '',
    inputTypes: input.map((item) => item?.type).filter(Boolean),
    hasCompaction: input.some((item) => item?.type === 'compaction'),
    hasCompactionTrigger: input.some((item) => item?.type === 'compaction_trigger'),
    hasStreamOptionsIncludeUsage: streamOptions?.include_usage === true,
    hasStreamOptionsIncludeObfuscation: streamOptions?.include_obfuscation === true,
    hasEncryptedContent: body.includes('encrypted_content'),
    bodySha256: crypto.createHash('sha256').update(body).digest('hex'),
    bodyBytes: Buffer.byteLength(body),
  };
}

function containsValue(value, expected) {
  if (value === expected) return true;
  if (Array.isArray(value)) return value.some((item) => containsValue(item, expected));
  if (value && typeof value === 'object') {
    return Object.values(value).some((item) => containsValue(item, expected));
  }
  return false;
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

function hasInputMarker(parsed, marker) {
  return JSON.stringify(parsed?.input ?? []).includes(marker);
}

function streamOptionsCompatErrorEvents() {
  return [
    { type: 'response.created', response: { id: 'resp_stream_options_compat_error', object: 'response' } },
    { type: 'response.in_progress', response: { id: 'resp_stream_options_compat_error', object: 'response' } },
    {
      type: 'response.error',
      error: {
        message: "Unknown parameter: 'stream_options.include_usage'.",
        type: 'invalid_request_error',
        param: 'stream_options.include_usage',
        code: 'unknown_parameter',
      },
    },
  ];
}

function streamOptionsCompatSuccessEvents(parsed) {
  const response = {
    id: 'resp_stream_options_compat_ok',
    object: 'response',
    status: 'completed',
    model: parsed?.model || 'gpt-5.5',
    output: [
      {
        type: 'message',
        role: 'assistant',
        content: [{ type: 'output_text', text: 'stream options compatibility recovered' }],
      },
    ],
    usage: { input_tokens: 12, output_tokens: 4, total_tokens: 16 },
  };
  return [
    { type: 'response.created', response: { id: response.id, object: 'response' } },
    { type: 'response.completed', response },
  ];
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
        hasCompactionTrigger: item.hasCompactionTrigger,
        hasStreamOptionsIncludeUsage: item.hasStreamOptionsIncludeUsage,
        hasStreamOptionsIncludeObfuscation: item.hasStreamOptionsIncludeObfuscation,
        hasEncryptedContent: item.hasEncryptedContent,
        bodySha256: item.bodySha256,
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

  if (containsValue(parsed, 'opaque-native-retry-guard')) {
    return sendJson(res, 400, {
      error: {
        type: 'invalid_request_error',
        code: 'invalid_encrypted_content',
        param: 'input[0].encrypted_content',
        message: 'invalid_encrypted_content',
      },
    });
  }

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
    if (hasInputMarker(parsed, 'REMOTE_COMPACTION_413_E2E') && captures.at(-1)?.hasCompactionTrigger) {
      return sendJson(res, 413, {
        error: {
          message: 'upstream request too large',
          type: 'invalid_request_error',
          code: 'upstream_request_too_large',
        },
      });
    }
    if (parsed?.stream === true && hasInputMarker(parsed, 'STREAM_OPTIONS_INCLUDE_USAGE_E2E')) {
      if (parsed?.stream_options?.include_usage === true) {
        return sendSse(res, streamOptionsCompatErrorEvents());
      }
      return sendSse(res, streamOptionsCompatSuccessEvents(parsed));
    }
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
