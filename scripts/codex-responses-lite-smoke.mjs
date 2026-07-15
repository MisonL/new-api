#!/usr/bin/env node

const options = parseArgs(process.argv.slice(2));
const baseUrl = requiredOption(options, 'base-url').replace(/\/$/, '');
const token = requiredOption(options, 'token');
const channel = requiredOption(options, 'channel');
const model = requiredOption(options, 'model');
const scenario = options.scenario ?? 'all';
const timeoutMs = Number(options['timeout-ms'] ?? 24000);

const supportedScenarios = new Set(['all', 'basic', 'tools', 'ids', 'compact']);
if (!supportedScenarios.has(scenario)) {
  fail(`unsupported scenario: ${scenario}`);
}
if (!Number.isInteger(timeoutMs) || timeoutMs < 1000 || timeoutMs > 28000) {
  fail('timeout-ms must be an integer from 1000 to 28000');
}
if (!/^\d+$/.test(channel)) {
  fail('channel must be a positive integer');
}

const authorization = token.endsWith(`-${channel}`) ? token : `${token}-${channel}`;
const results = [];

if (scenario === 'all' || scenario === 'basic') {
  results.push(await runBasic());
}
if (scenario === 'all' || scenario === 'tools') {
  results.push(await runTools());
}
if (scenario === 'all' || scenario === 'ids') {
  results.push(await runItemIds());
}
if (scenario === 'all' || scenario === 'compact') {
  results.push(await runCompactAndContinue());
}

for (const result of results) {
  process.stdout.write(`${JSON.stringify(result)}\n`);
}

async function runBasic() {
  const body = {
    model,
    input: [message('Return exactly CODEX_LITE_BASIC_OK.')],
    reasoning: {
      effort: 'low',
      summary: 'auto',
      mode: 'adaptive',
      context: 'all_turns',
    },
    stream: false,
  };
  const response = await postJson('/v1/responses', body);
  assertCompletedResponse('basic', response);
  return summarize('basic', response);
}

async function runTools() {
  const body = {
    model,
    input: [
      {
        type: 'additional_tools',
        role: 'developer',
        tools: [
          {
            type: 'function',
            name: 'extra_lookup',
            description: 'Return a requested smoke-test value.',
            parameters: {
              type: 'object',
              properties: { key: { type: 'string' } },
              required: ['key'],
              additionalProperties: false,
            },
            strict: true,
          },
        ],
      },
      message('Call extra_lookup with key CODEX_LITE_TOOL_OK. Do not answer directly.'),
    ],
    tools: [
      {
        type: 'function',
        name: 'top_lookup',
        description: 'A second tool used to verify merged tool definitions.',
        parameters: {
          type: 'object',
          properties: { key: { type: 'string' } },
          required: ['key'],
          additionalProperties: false,
        },
        strict: true,
      },
    ],
    tool_choice: { type: 'function', name: 'extra_lookup' },
    reasoning: { effort: 'low', summary: 'auto', context: 'all_turns' },
    stream: true,
  };
  const response = await postJson('/v1/responses', body);
  assertCompletedResponse('tools', response);
  const calls = response.output.filter((item) => isToolCall(item));
  if (!calls.some((item) => item.name === 'extra_lookup')) {
    fail('tools: upstream completed without the forced additional tool call');
  }
  return summarize('tools', response, {
    tool_names: calls.map((item) => item.name).filter(Boolean),
  });
}

async function runItemIds() {
  const callId = 'call_codex_lite_id_smoke';
  const body = {
    model,
    input: [
      {
        type: 'message',
        id: 'item_invalid_message',
        role: 'user',
        content: [{ type: 'input_text', text: 'Use the supplied tool history and return CODEX_LITE_IDS_OK.' }],
      },
      {
        type: 'function_call',
        id: 'item_invalid_function_call',
        call_id: callId,
        name: 'id_probe',
        arguments: '{"value":"CODEX_LITE_IDS_OK"}',
        status: 'completed',
      },
      {
        type: 'function_call_output',
        id: 'item_invalid_function_output',
        call_id: callId,
        output: '{"ok":true}',
        status: 'completed',
      },
      {
        type: 'reasoning',
        id: 'item_invalid_reasoning',
        summary: [],
      },
    ],
    tools: [
      {
        type: 'function',
        name: 'id_probe',
        parameters: {
          type: 'object',
          properties: { value: { type: 'string' } },
          required: ['value'],
          additionalProperties: false,
        },
        strict: true,
      },
    ],
    reasoning: { effort: 'low', summary: 'auto', context: 'all_turns' },
    stream: false,
  };
  const response = await postJson('/v1/responses', body);
  assertCompletedResponse('ids', response);
  return summarize('ids', response);
}

async function runCompactAndContinue() {
  const compactModel = model.endsWith('-openai-compact') ? model : `${model}-openai-compact`;
  const compact = await postJson('/v1/responses/compact', {
    model: compactModel,
    input: [message('Compact this state: CODEX_LITE_COMPACT_OK.')],
    reasoning: { effort: 'low', summary: 'auto', context: 'all_turns' },
    stream: false,
  });
  assertCompletedResponse('compact', compact);
  if (!compact.output.some((item) => item?.type === 'compaction')) {
    fail('compact: completed response did not contain a compaction item');
  }

  const continued = await postJson('/v1/responses', {
    model,
    previous_response_id: compact.id,
    input: [message('Continue and return exactly CODEX_LITE_CONTINUE_OK.')],
    reasoning: { effort: 'low', summary: 'auto', context: 'all_turns' },
    stream: false,
  });
  assertCompletedResponse('compact_continue', continued);
  return summarize('compact', compact, {
    continue_status: continued.status,
    continue_output_types: uniqueTypes(continued.output),
  });
}

async function postJson(path, body) {
  const startedAt = Date.now();
  let response;
  try {
    response = await fetch(`${baseUrl}${path}`, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${authorization}`,
        'Content-Type': 'application/json',
        Accept: 'text/event-stream, application/json',
        Originator: 'codex-tui',
        'User-Agent': 'codex-tui/0.144.4 (compatibility-smoke)',
        'X-Codex-Beta-Features': 'responses_lite',
      },
      body: JSON.stringify(body),
      signal: AbortSignal.timeout(timeoutMs),
    });
  } catch (error) {
    fail(`${path}: request failed: ${safeErrorName(error)}`);
  }

  const contentType = response.headers.get('content-type') ?? '';
  const raw = await response.text();
  const payload = contentType.includes('text/event-stream') || raw.trimStart().startsWith('data:')
    ? parseSse(raw)
    : parseJson(raw, path);
  const normalized = normalizeResponsePayload(payload);
  normalized.http_status = response.status;
  normalized.duration_ms = Date.now() - startedAt;

  if (!response.ok) {
    const error = normalized.error ?? normalized.response?.error ?? {};
    fail(`${path}: HTTP ${response.status} ${safeErrorSummary(error)}`);
  }
  return normalized;
}

function parseSse(raw) {
  const events = [];
  for (const line of raw.split(/\r?\n/)) {
    if (!line.startsWith('data:')) continue;
    const data = line.slice(5).trim();
    if (!data || data === '[DONE]') continue;
    events.push(parseJson(data, 'SSE event'));
  }
  if (events.length === 0) fail('SSE response contained no data events');
  const terminal = [...events].reverse().find((event) =>
    ['response.completed', 'response.failed', 'response.incomplete', 'response.error'].includes(event?.type),
  );
  if (!terminal) fail('SSE response contained no terminal event');
  return terminal.response ?? terminal;
}

function normalizeResponsePayload(payload) {
  if (payload?.type === 'response.completed' && payload.response) return payload.response;
  return payload ?? {};
}

function assertCompletedResponse(name, response) {
  if (response.http_status !== 200) fail(`${name}: expected HTTP 200`);
  if (response.status !== 'completed') {
    fail(`${name}: expected completed response, got ${String(response.status ?? 'missing')}`);
  }
  if (!Array.isArray(response.output)) fail(`${name}: response.output is not an array`);
}

function summarize(name, response, extra = {}) {
  return {
    scenario: name,
    http_status: response.http_status,
    status: response.status,
    duration_ms: response.duration_ms,
    output_types: uniqueTypes(response.output),
    ...extra,
  };
}

function uniqueTypes(items) {
  return [...new Set((items ?? []).map((item) => item?.type).filter(Boolean))];
}

function isToolCall(item) {
  return ['function_call', 'custom_tool_call', 'tool_search_call'].includes(item?.type);
}

function message(text) {
  return {
    type: 'message',
    role: 'user',
    content: [{ type: 'input_text', text }],
  };
}

function parseJson(raw, label) {
  try {
    return JSON.parse(raw);
  } catch {
    fail(`${label}: response was not valid JSON`);
  }
}

function safeErrorSummary(error) {
  const parts = [error?.type, error?.code, error?.param].filter((value) => typeof value === 'string' && value);
  return parts.length > 0 ? parts.join('/') : 'upstream_error_without_structured_code';
}

function safeErrorName(error) {
  return typeof error?.name === 'string' && error.name ? error.name : 'request_error';
}

function parseArgs(args) {
  const parsed = {};
  for (let index = 0; index < args.length; index += 2) {
    const key = args[index];
    const value = args[index + 1];
    if (!key?.startsWith('--') || value === undefined) fail('arguments must use --name value pairs');
    parsed[key.slice(2)] = value;
  }
  return parsed;
}

function requiredOption(parsed, name) {
  const value = parsed[name];
  if (!value) fail(`missing required option --${name}`);
  return value;
}

function fail(message) {
  process.stderr.write(`SMOKE_FAILED ${message}\n`);
  process.exit(1);
}
