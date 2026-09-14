import http from 'node:http';
import { createHash } from 'node:crypto';
import { URL } from 'node:url';

// The isolated new-api container reaches the host through host.docker.internal.
// Bind all interfaces by default so the fixture is reachable from that container.
const host = process.env.COMPACT_E2E_FAKE_UPSTREAM_HOST || '0.0.0.0';
const port = Number.parseInt(process.env.COMPACT_E2E_FAKE_UPSTREAM_PORT || '18081', 10);
const remoteCompactionBodyLimitBytes = Number.parseInt(process.env.COMPACT_E2E_REMOTE_COMPACTION_BODY_LIMIT_BYTES || '32768', 10);
const localCompactionBodyLimitBytes = Number.parseInt(process.env.COMPACT_E2E_LOCAL_COMPACTION_BODY_LIMIT_BYTES || '32768', 10);
const acceptAllStreamRequests = process.env.COMPACT_E2E_FAKE_UPSTREAM_ACCEPT_ALL_STREAMS === '1';
const captures = [];
const knownResponsesInputTypes = new Set([
  'additional_tools',
  'compaction',
  'compaction_trigger',
  'computer_call',
  'computer_call_output',
  'custom_tool_call',
  'custom_tool_call_output',
  'file_search_call',
  'function_call',
  'function_call_output',
  'image_generation_call',
  'message',
  'reasoning',
  'tool_search_call',
  'tool_search_output',
  'web_search_call',
]);
const knownResponsesRootKeys = new Set([
  'additional_tools',
  'background',
  'conversation',
  'include',
  'input',
  'instructions',
  'max_output_tokens',
  'max_tool_calls',
  'metadata',
  'model',
  'parallel_tool_calls',
  'previous_response_id',
  'prompt',
  'prompt_cache_key',
  'reasoning',
  'safety_identifier',
  'service_tier',
  'store',
  'stream',
  'stream_options',
  'temperature',
  'text',
  'tool_choice',
  'tools',
  'top_logprobs',
  'top_p',
  'truncation',
  'user',
]);
const knownResponsesInputItemKeys = new Set([
  'arguments',
  'call_id',
  'content',
  'encrypted_content',
  'id',
  'name',
  'namespace',
  'output',
  'role',
  'status',
  'summary',
  'text',
  'tools',
  'type',
]);

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
  const reasoning = parsed?.reasoning;
  const topLevelTools = Array.isArray(parsed?.tools) ? parsed.tools : [];
  const topLevelAdditionalTools = Array.isArray(parsed?.additional_tools) ? parsed.additional_tools : [];
  const inputAdditionalTools = input
    .filter((item) => item?.type === 'additional_tools' && Array.isArray(item?.tools))
    .reduce((count, item) => count + item.tools.length, 0);
  const bodyBytes = Buffer.byteLength(body);
  const declaredContentLength = parseContentLength(req.headers['content-length']);
  return {
    rootShape: summarizeObjectShape(parsed, knownResponsesRootKeys),
    request: {
      method: req.method,
      path: url.pathname,
      model: describeJSONValue(parsed?.model),
      stream: describeJSONValue(parsed?.stream),
      previousResponseID: describeJSONValue(parsed?.previous_response_id),
      instructions: describeJSONValue(parsed?.instructions),
    },
    inputItemCount: input.length,
    inputTypes: input.map(summarizeInputType),
    inputShapes: input.map(summarizeInputShape),
    hasCompaction: input.some((item) => item?.type === 'compaction'),
    hasCompactionTrigger: input.some((item) => item?.type === 'compaction_trigger'),
    hasLocalCompaction413Marker: hasInputMarker(parsed, 'LOCAL_COMPACTION_413_E2E'),
    hasLocalCompactionPreemptiveMarker: hasInputMarker(parsed, 'LOCAL_COMPACTION_PREEMPTIVE_E2E'),
    customToolOutputCount: input.filter((item) => item?.type === 'custom_tool_call_output').length,
    hasEmbeddedMediaDataURI: hasEmbeddedMediaDataURI(input),
    reasoning: {
      value: describeJSONValue(reasoning),
      context: describeJSONValue(reasoning?.context),
      mode: describeJSONValue(reasoning?.mode),
      hasExpectedContextAllTurns: reasoning?.context === 'all_turns',
      hasExpectedModeSummary: reasoning?.mode === 'summary',
    },
    streamOptions: {
      value: describeJSONValue(streamOptions),
      includeUsage: describeJSONValue(streamOptions?.include_usage),
      includeObfuscation: describeJSONValue(streamOptions?.include_obfuscation),
      reasoningSummaryDelivery: describeJSONValue(streamOptions?.reasoning_summary_delivery),
    },
    hasStreamOptionsIncludeUsage: streamOptions?.include_usage === true,
    hasStreamOptionsIncludeObfuscation: streamOptions?.include_obfuscation === true,
    hasReasoningSummaryDelivery: streamOptions?.reasoning_summary_delivery === 'detailed',
    tools: {
      topLevel: describeJSONValue(parsed?.tools),
      topLevelCount: topLevelTools.length,
      topLevelAdditional: describeJSONValue(parsed?.additional_tools),
      topLevelAdditionalCount: topLevelAdditionalTools.length,
      inputAdditionalCount: inputAdditionalTools,
    },
    hasTopLevelTools: topLevelTools.length > 0,
    hasTopLevelAdditionalTools: topLevelAdditionalTools.length > 0,
    bodyBytes,
    bodySHA256: createHash('sha256').update(body).digest('hex'),
    declaredContentLength,
    contentLengthMatchesBody: declaredContentLength === bodyBytes,
    headers: summarizeHeaderPresence(req),
  };
}

function summarizeObjectShape(value, knownKeys) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return {
      present: value !== undefined,
      keyCount: 0,
      knownKeys: [],
      unknownKeyCount: 0,
    };
  }
  const keys = Object.keys(value);
  return {
    present: true,
    keyCount: keys.length,
    knownKeys: keys.filter((key) => knownKeys.has(key)).sort(),
    unknownKeyCount: keys.filter((key) => !knownKeys.has(key)).length,
  };
}

function describeJSONValue(value) {
  if (value === undefined) {
    return { present: false, type: 'missing' };
  }
  if (value === null) {
    return { present: true, type: 'null' };
  }
  if (Array.isArray(value)) {
    return { present: true, type: 'array' };
  }
  return { present: true, type: typeof value };
}

function summarizeInputType(item) {
  if (!item || typeof item !== 'object' || Array.isArray(item)) {
    return 'invalid';
  }
  return knownResponsesInputTypes.has(item.type) ? item.type : 'unknown';
}

function summarizeInputShape(item) {
  return {
    type: summarizeInputType(item),
    ...summarizeObjectShape(item, knownResponsesInputItemKeys),
  };
}

function parseContentLength(value) {
  const raw = Array.isArray(value) ? value[0] : value;
  if (typeof raw !== 'string' || !/^\d+$/.test(raw)) {
    return null;
  }
  const parsed = Number(raw);
  return Number.isSafeInteger(parsed) ? parsed : null;
}

function summarizeHeaderPresence(req) {
  return {
    accept: hasHeader(req, 'accept'),
    contentType: hasHeader(req, 'content-type'),
    originator: hasHeader(req, 'originator'),
    userAgent: hasHeader(req, 'user-agent'),
    codexTurnMetadata: hasHeader(req, 'x-codex-turn-metadata'),
    responsesLite: hasHeader(req, 'x-openai-internal-codex-responses-lite'),
  };
}

function hasHeader(req, name) {
  const value = req.headers[name];
  if (Array.isArray(value)) {
    return value.some((entry) => typeof entry === 'string' && entry.length > 0);
  }
  return typeof value === 'string' && value.length > 0;
}

function hasEmbeddedMediaDataURI(input) {
  const pending = [...input];
  while (pending.length > 0) {
    const value = pending.pop();
    if (typeof value === 'string') {
      if (value.startsWith('data:image/') && value.includes(';base64,')) {
        return true;
      }
      continue;
    }
    if (Array.isArray(value)) {
      pending.push(...value);
      continue;
    }
    if (value && typeof value === 'object') {
      for (const nested of Object.values(value)) {
        pending.push(nested);
      }
    }
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

function fakeOpaqueResponsesBody(parsed) {
  return {
    id: 'resp_opaque_passthrough_nonstream',
    object: 'response',
    status: 'completed',
    created_at: Math.floor(Date.now() / 1000),
    model: parsed?.model || 'gpt-5.6-sol',
    output: [
      {
        id: 'rs_opaque_passthrough_nonstream',
        type: 'reasoning',
        status: 'completed',
        summary: [],
        encrypted_content: 'OPAQUE_RESPONSE_NONSTREAM_E2E',
        future_state: { version: 5 },
      },
      {
        type: 'message',
        role: 'assistant',
        content: [{ type: 'output_text', text: 'opaque non-stream response preserved' }],
      },
    ],
    usage: { input_tokens: 128, output_tokens: 48, total_tokens: 176 },
    future_response_state: { preserved: true },
  };
}

function fakeCompactBody(parsed) {
  const opaquePassthrough = hasInputMarker(parsed, 'OPAQUE_PASSTHROUGH_COMPACT_E2E');
  const encryptedContent = opaquePassthrough ? 'OPAQUE_RESPONSE_COMPACT_E2E' : 'fake-native-opaque';
  const compactionOutput = {
    type: 'compaction',
    encrypted_content: encryptedContent,
  };
  if (opaquePassthrough) {
    compactionOutput.future_state = { version: 7 };
  }
  const response = {
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
      compactionOutput,
    ],
    usage: { input_tokens: 3000, output_tokens: 120, total_tokens: 3120 },
    compaction: { encrypted_content: encryptedContent },
  };
  if (opaquePassthrough) {
    response.future_response_state = { preserved: true };
  }
  return response;
}

function hasInputMarker(parsed, marker) {
  const pending = [parsed?.input ?? []];
  while (pending.length > 0) {
    const value = pending.pop();
    if (typeof value === 'string') {
      if (value.includes(marker)) {
        return true;
      }
      continue;
    }
    if (Array.isArray(value)) {
      pending.push(...value);
      continue;
    }
    if (value && typeof value === 'object') {
      for (const nested of Object.values(value)) {
        pending.push(nested);
      }
    }
  }
  return false;
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

function localCompaction413SuccessEvents(parsed) {
  const response = {
    id: 'resp_local_compaction_413_ok',
    object: 'response',
    status: 'completed',
    model: parsed?.model || 'gpt-5.5',
    output: [
      {
        type: 'message',
        role: 'assistant',
        content: [{ type: 'output_text', text: 'local_compaction_413_ok: continue from the latest task context' }],
      },
    ],
    usage: { input_tokens: 24, output_tokens: 12, total_tokens: 36 },
  };
  return [
    { type: 'response.created', response: { id: response.id, object: 'response' } },
    { type: 'response.in_progress', response: { id: response.id, object: 'response' } },
    { type: 'response.completed', response },
  ];
}

function opaquePassthroughStreamEvents(parsed) {
  const reasoning = {
    id: 'rs_opaque_passthrough_stream',
    type: 'reasoning',
    status: 'completed',
    summary: [],
    encrypted_content: 'OPAQUE_RESPONSE_STREAM_E2E',
    future_state: { version: 6 },
  };
  const response = {
    id: 'resp_opaque_passthrough_stream',
    object: 'response',
    status: 'completed',
    model: parsed?.model || 'gpt-5.6-sol',
    output: [reasoning],
    usage: { input_tokens: 128, output_tokens: 48, total_tokens: 176 },
    future_response_state: { preserved: true },
  };
  return [
    { type: 'response.created', response: { id: response.id, object: 'response' } },
    {
      type: 'response.output_item.done',
      output_index: 0,
      item: reasoning,
      future_event_state: { preserved: true },
    },
    { type: 'response.completed', response, future_event_state: { preserved: true } },
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
    return sendJson(res, 200, captures);
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
    if (
      hasInputMarker(parsed, 'LOCAL_COMPACTION_413_E2E') &&
      captures.filter((item) => item.hasLocalCompaction413Marker).length === 1 &&
      Buffer.byteLength(body) > localCompactionBodyLimitBytes
    ) {
      return sendJson(res, 413, {
        error: {
          message: 'upstream request too large',
          type: 'invalid_request_error',
          code: 'upstream_request_too_large',
        },
      });
    }
    if (
      parsed?.stream === true &&
      (hasInputMarker(parsed, 'LOCAL_COMPACTION_413_E2E') || hasInputMarker(parsed, 'LOCAL_COMPACTION_PREEMPTIVE_E2E'))
    ) {
      return sendSse(res, localCompaction413SuccessEvents(parsed));
    }
    if (hasInputMarker(parsed, 'REMOTE_COMPACTION_413_E2E') && captures.at(-1)?.hasCompactionTrigger) {
      return sendJson(res, 413, {
        error: {
          message: 'upstream request too large',
          type: 'invalid_request_error',
          code: 'upstream_request_too_large',
        },
      });
    }
    if (hasInputMarker(parsed, 'REMOTE_COMPACTION_MEDIA_413_E2E') && Buffer.byteLength(body) > remoteCompactionBodyLimitBytes) {
      return sendJson(res, 413, {
        error: {
          message: 'upstream request body exceeds compact threshold',
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
    if (parsed?.stream === true && hasInputMarker(parsed, 'OPAQUE_PASSTHROUGH_STREAM_E2E')) {
      return sendSse(res, opaquePassthroughStreamEvents(parsed));
    }
    if (parsed?.stream === true && acceptAllStreamRequests) {
      return sendSse(res, localCompaction413SuccessEvents(parsed));
    }
    if (hasInputMarker(parsed, 'OPAQUE_PASSTHROUGH_NONSTREAM_E2E')) {
      return sendJson(res, 200, fakeOpaqueResponsesBody(parsed));
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
