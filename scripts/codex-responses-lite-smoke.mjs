#!/usr/bin/env node

const options = parseArgs(process.argv.slice(2));
const baseUrl = requiredOption(options, 'base-url').replace(/\/$/, '');
const token = requiredOption(options, 'token');
const channel = requiredOption(options, 'channel');
const model = requiredOption(options, 'model');
const scenario = options.scenario ?? 'all';
const timeoutMs = Number(options['timeout-ms'] ?? 24000);

const supportedScenarios = new Set([
  'all',
  'core',
  'basic',
  'tools',
  'namespace',
  'tool-search',
  'grammar',
  'controls',
  'continuation',
  'web-search',
  'ids',
  'compact',
]);
const coreScenarios = new Set([
  'basic',
  'tools',
  'namespace',
  'grammar',
  'controls',
  'continuation',
  'web-search',
  'ids',
  'compact',
]);
if (!supportedScenarios.has(scenario)) {
  fail(`unsupported scenario: ${scenario}`);
}
if (!Number.isInteger(timeoutMs) || timeoutMs < 1000 || timeoutMs > 180000) {
  fail('timeout-ms must be an integer from 1000 to 180000');
}
if (channel !== 'auto' && !/^\d+$/.test(channel)) {
  fail('channel must be "auto" or a positive integer');
}

const authorization =
  channel === 'auto' || token.endsWith(`-${channel}`) ? token : `${token}-${channel}`;
const results = [];

if (shouldRun('basic')) {
  results.push(await runBasic());
}
if (shouldRun('tools')) {
  results.push(await runTools());
}
if (shouldRun('namespace')) {
  results.push(await runNamespaceTool());
}
if (shouldRun('tool-search')) {
  results.push(await runToolSearch());
}
if (shouldRun('grammar')) {
  results.push(await runGrammarTool());
}
if (shouldRun('controls')) {
  results.push(await runRequestControls());
}
if (shouldRun('continuation')) {
  results.push(await runToolContinuation());
}
if (shouldRun('web-search')) {
  results.push(await runWebSearchDeclaration());
}
if (shouldRun('ids')) {
  results.push(await runItemIds());
}
if (shouldRun('compact')) {
  results.push(await runCompactAndContinue());
}

for (const result of results) {
  process.stdout.write(`${JSON.stringify(result)}\n`);
}

function shouldRun(name) {
  return scenario === 'all' || scenario === name || (scenario === 'core' && coreScenarios.has(name));
}

async function runBasic() {
  const body = {
    model,
    input: [message('Return exactly CODEX_LITE_BASIC_OK.')],
    reasoning: {
      effort: 'low',
      summary: 'auto',
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

async function runNamespaceTool() {
  const body = {
    model,
    input: [
      additionalTools([
        {
          type: 'namespace',
          name: 'smoke_namespace',
          description: 'Codex namespace compatibility probes.',
          tools: [
            {
              type: 'function',
              name: 'namespace_lookup',
              description: 'Return a requested namespace smoke-test value.',
              parameters: strictStringParameter('value'),
              strict: true,
            },
          ],
        },
      ]),
      message('Call namespace_lookup with value CODEX_LITE_NAMESPACE_OK. Do not answer directly.'),
    ],
    tool_choice: { type: 'namespace', name: 'smoke_namespace' },
    reasoning: { effort: 'low', summary: 'auto', context: 'all_turns' },
    stream: true,
  };
  const response = await postJson('/v1/responses', body);
  assertCompletedResponse('namespace', response);
  const calls = response.output.filter((item) => isToolCall(item));
  if (!calls.some((item) => item.name === 'namespace_lookup')) {
    fail('namespace: upstream completed without the namespace_lookup call');
  }
  return summarize('namespace', response, {
    tool_names: calls.map((item) => item.name).filter(Boolean),
    namespaces: calls.map((item) => item.namespace).filter(Boolean),
  });
}

async function runToolSearch() {
  const searchTool = {
    type: 'tool_search',
    execution: 'client',
    description: 'Search deferred smoke-test tools.',
    parameters: {
      type: 'object',
      properties: {
        query: { type: 'string', description: 'Search query for deferred tools.' },
        limit: { type: 'number', description: 'Maximum number of tools to return.' },
      },
      required: ['query'],
      additionalProperties: false,
    },
  };
  const initialInput = [
    additionalTools([searchTool]),
    message('You must call tool_search with query deferred_lookup. Do not answer directly.'),
  ];
  const discovered = await postJson('/v1/responses', {
    model,
    input: initialInput,
    tool_choice: 'auto',
    reasoning: { effort: 'low', summary: 'auto', context: 'all_turns' },
    stream: true,
  });
  assertCompletedResponse('tool-search-discovery', discovered);
  const searchCall = discovered.output.find((item) => item?.type === 'tool_search_call');
  if (!searchCall?.call_id) {
    const observed = (discovered.output ?? []).map((item) => ({
      type: item?.type,
      name: item?.name,
      call_id_present: Boolean(item?.call_id),
    }));
    fail(`tool-search: upstream completed without a tool_search_call call_id; observed=${JSON.stringify(observed)}`);
  }

  const loaded = await postJson('/v1/responses', {
    model,
    input: [
      ...initialInput,
      searchCall,
      {
        type: 'tool_search_output',
        call_id: searchCall.call_id,
        status: 'completed',
        execution: 'client',
        tools: [
          {
            type: 'function',
            name: 'deferred_lookup',
            description: 'Return a requested deferred smoke-test value.',
            defer_loading: true,
            strict: true,
            parameters: strictStringParameter('value'),
          },
        ],
      },
    ],
    tool_choice: { type: 'function', name: 'deferred_lookup' },
    reasoning: { effort: 'low', summary: 'auto', context: 'all_turns' },
    stream: true,
  });
  assertCompletedResponse('tool-search-loaded', loaded);
  const loadedCalls = loaded.output.filter((item) => isToolCall(item));
  if (!loadedCalls.some((item) => item.name === 'deferred_lookup')) {
    const observed = (loaded.output ?? []).map((item) => ({
      type: item?.type,
      name: item?.name,
      namespace: item?.namespace,
      call_id_present: Boolean(item?.call_id),
    }));
    fail(`tool-search: deferred_lookup was not callable after tool_search_output; observed=${JSON.stringify(observed)}`);
  }
  return summarize('tool-search', loaded, {
    discovery_output_types: uniqueTypes(discovered.output),
    tool_names: loadedCalls.map((item) => item.name).filter(Boolean),
  });
}

async function runGrammarTool() {
  const applyPatchTool = {
    type: 'custom',
    name: 'apply_patch',
    description: 'A grammar-constrained Codex compatibility probe.',
    format: {
      type: 'grammar',
      syntax: 'lark',
      definition: 'start: SOURCE\nSOURCE: /[\\s\\S]+/',
    },
  };
  const body = {
    model,
    input: [
      {
        type: 'additional_tools',
        role: 'developer',
        tools: [applyPatchTool],
      },
      message('Call apply_patch with the exact input CODEX_LITE_GRAMMAR_OK.'),
    ],
    tool_choice: { type: 'custom', name: 'apply_patch' },
    reasoning: { effort: 'low', summary: 'auto', context: 'all_turns' },
    stream: true,
  };
  const response = await postJson('/v1/responses', body);
  assertCompletedResponse('grammar', response);
  const calls = response.output.filter((item) => isToolCall(item) && item?.name === 'apply_patch');
  if (calls.length === 0) {
    const observed = response.output.map((item) => ({
      type: item?.type,
      name: item?.name,
      call_id_present: Boolean(item?.call_id),
    }));
    fail(`grammar: upstream completed without the forced apply_patch tool call; observed=${JSON.stringify(observed)}`);
  }
  return summarize('grammar', response, {
    tool_names: calls.map((item) => item.name).filter(Boolean),
    tool_types: calls.map((item) => item.type).filter(Boolean),
  });
}

async function runRequestControls() {
  const body = {
    model,
    input: [message('Return a JSON object with status set to CODEX_LITE_CONTROLS_OK.')],
    tool_choice: 'auto',
    parallel_tool_calls: true,
    reasoning: { effort: 'low', summary: 'auto', context: 'all_turns' },
    store: false,
    stream: true,
    stream_options: { reasoning_summary_delivery: 'sequential_cutoff' },
    include: ['reasoning.encrypted_content'],
    prompt_cache_key: 'codex-lite-controls-cache-key',
    text: {
      verbosity: 'low',
      format: {
        type: 'json_schema',
        strict: true,
        name: 'codex_lite_controls',
        schema: {
          type: 'object',
          properties: { status: { type: 'string' } },
          required: ['status'],
          additionalProperties: false,
        },
      },
    },
    client_metadata: {
      session_id: 'codex-lite-controls-session',
      thread_id: 'codex-lite-controls-thread',
      turn_id: 'codex-lite-controls-turn',
    },
  };
  const response = await postJson('/v1/responses', body);
  assertCompletedResponse('controls', response);
  const parsed = parseJson(responseOutputText(response), 'controls output text');
  if (parsed?.status !== 'CODEX_LITE_CONTROLS_OK') {
    fail('controls: JSON schema response did not contain the expected status');
  }
  return summarize('controls', response, { parsed_status: parsed.status });
}

async function runToolContinuation() {
  const tool = {
    type: 'function',
    name: 'continuation_lookup',
    description: 'Return a value used to validate tool output continuation.',
    parameters: strictStringParameter('value'),
    strict: true,
  };
  const initialInput = [
    additionalTools([tool]),
    message('Call continuation_lookup with value CODEX_LITE_CONTINUATION_INPUT.'),
  ];
  const called = await postJson('/v1/responses', {
    model,
    input: initialInput,
    tool_choice: { type: 'function', name: 'continuation_lookup' },
    reasoning: { effort: 'low', summary: 'auto', context: 'all_turns' },
    stream: true,
  });
  assertCompletedResponse('continuation-call', called);
  const call = called.output.find(
    (item) => isToolCall(item) && item?.name === 'continuation_lookup' && item?.call_id,
  );
  if (!call) fail('continuation: upstream completed without continuation_lookup');

  const continued = await postJson('/v1/responses', {
    model,
    input: [
      ...initialInput,
      call,
      {
        type: 'function_call_output',
        call_id: call.call_id,
        output: 'CODEX_LITE_CONTINUATION_OK',
      },
      message('Return exactly the tool output and do not call another tool.'),
    ],
    tool_choice: 'none',
    reasoning: { effort: 'low', summary: 'auto', context: 'all_turns' },
    stream: false,
  });
  assertCompletedResponse('continuation-result', continued);
  if (!responseOutputText(continued).includes('CODEX_LITE_CONTINUATION_OK')) {
    fail('continuation: final response did not preserve the function_call_output value');
  }
  return summarize('continuation', continued, {
    call_type: call.type,
    call_id_prefix: String(call.call_id).split('_')[0],
  });
}

async function runWebSearchDeclaration() {
  const body = {
    model,
    input: [
      additionalTools([
        {
          type: 'web_search',
          external_web_access: true,
          indexed_web_access: true,
          filters: { allowed_domains: ['example.com'] },
          user_location: {
            type: 'approximate',
            country: 'US',
            region: 'California',
            city: 'San Francisco',
            timezone: 'America/Los_Angeles',
          },
          search_context_size: 'low',
          search_content_types: ['text'],
        },
      ]),
      message('Return exactly CODEX_LITE_WEB_SEARCH_DECLARATION_OK without using a tool.'),
    ],
    tool_choice: 'none',
    reasoning: { effort: 'low', summary: 'auto', context: 'all_turns' },
    stream: false,
  };
  const response = await postJson('/v1/responses', body);
  assertCompletedResponse('web-search', response);
  if (!responseOutputText(response).includes('CODEX_LITE_WEB_SEARCH_DECLARATION_OK')) {
    fail('web-search: response did not preserve the declaration probe result');
  }
  return summarize('web-search', response);
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
    input: [
      additionalTools([
        {
          type: 'function',
          name: 'compact_lookup',
          description: 'A compact request tool declaration.',
          parameters: strictStringParameter('value'),
          strict: true,
        },
      ]),
      message('Compact this state: CODEX_LITE_COMPACT_OK.'),
    ],
    parallel_tool_calls: false,
    reasoning: { effort: 'low', summary: 'auto', context: 'all_turns' },
    prompt_cache_key: 'codex-lite-compact-cache-key',
    text: { verbosity: 'low' },
  });
  assertCompactResponse('compact', compact);
  if (!compact.output.some((item) => item?.type === 'compaction')) {
    fail('compact: completed response did not contain a compaction item');
  }

  const continued = await postJson('/v1/responses', {
    model,
    input: [...compact.output, message('Continue and return exactly CODEX_LITE_CONTINUE_OK.')],
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
        'User-Agent': 'codex-tui/0.145.0-alpha.12 (compatibility-smoke)',
        'X-OpenAI-Internal-Codex-Responses-Lite': 'true',
      },
      body: JSON.stringify(body),
      signal: AbortSignal.timeout(timeoutMs),
    });
  } catch (error) {
    fail(`${path}: request failed: ${safeErrorName(error)}`);
  }

  const contentType = response.headers.get('content-type') ?? '';
  const raw = await response.text();
  const trimmed = raw.trimStart();
  const isJsonBody = trimmed.startsWith('{') || trimmed.startsWith('[');
  const isSseBody =
    trimmed.startsWith('data:') || trimmed.startsWith('event:') || trimmed.startsWith(':');
  let payload;
  if (isJsonBody) {
    payload = parseJson(raw, path);
  } else if (isSseBody || contentType.includes('text/event-stream')) {
    payload = parseSse(raw);
  } else {
    payload = parseJson(raw, path);
  }
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
  if (!terminal) {
    const observed = events.map((event) => ({
      type: event?.type,
      has_response: Boolean(event?.response),
    }));
    fail(`SSE response contained no terminal event; observed=${JSON.stringify(observed)}`);
  }
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

function assertCompactResponse(name, response) {
  if (response.http_status !== 200) fail(`${name}: expected HTTP 200`);
  if (response.status !== undefined && response.status !== 'completed') {
    fail(`${name}: expected completed response when status is present, got ${String(response.status)}`);
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

function additionalTools(tools) {
  return { type: 'additional_tools', role: 'developer', tools };
}

function strictStringParameter(name) {
  return {
    type: 'object',
    properties: { [name]: { type: 'string' } },
    required: [name],
    additionalProperties: false,
  };
}

function responseOutputText(response) {
  return (response.output ?? [])
    .flatMap((item) => (Array.isArray(item?.content) ? item.content : []))
    .filter((item) => item?.type === 'output_text' && typeof item.text === 'string')
    .map((item) => item.text)
    .join('');
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
