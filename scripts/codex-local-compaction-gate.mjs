#!/usr/bin/env node

const supportedRequestShapes = new Set([
  'baseline',
  'reduced_current',
  'reduced_no_instructions',
  'reduced_empty_additional_tools',
  'reduced_minimal_additional_tool',
  'reduced_empty_top_level_tools',
  'reduced_minimal_top_level_tool',
  'reduced_no_reasoning_context',
  'reduced_no_stream_options',
]);

const currentRecoveryInstructions = 'You are performing a CONTEXT CHECKPOINT COMPACTION. Create a self-contained handoff summary for another LLM that will resume the same task.\nThe summary will be injected as developer context in a future request, so it must preserve working memory rather than answer the user.\nReturn only the compact summary text in this exact Markdown outline:\nCurrent task:\n- ...\nProgress and decisions:\n- ...\nImportant context and constraints:\n- ...\nRemaining work:\n- ...\nFiles, commands, and evidence:\n- ...\nDo not output an acknowledgement. Do not say there is no task unless the visible conversation and previous summary truly contain no active or recently completed task. If repository instructions, AGENTS.md, skill notices, or setup messages appear after a compact marker, treat them as background and preserve the latest user task from the conversation or previous summary. Do not invent facts.';

const options = parseArgs(process.argv.slice(2));
const baseUrl = requiredOption(options, 'base-url').replace(/\/$/, '');
const token = resolveToken(options);
const channel = requiredNumericOption(options, 'channel');
const model = requiredOption(options, 'model');
const toolBytes = requiredPositiveInteger(options, 'tool-bytes');
const timeoutMs = positiveInteger(options['timeout-ms'] ?? '180000', 'timeout-ms');
const requestShape = resolveRequestShape(options['request-shape']);

if (timeoutMs < 1000 || timeoutMs > 300000) {
  fail('timeout-ms must be an integer from 1000 to 300000');
}

const authorization = token.endsWith(`-${channel}`) ? token : `${token}-${channel}`;
const request = buildRequest(model, toolBytes, requestShape);
const requestBody = JSON.stringify(request);

let response;
try {
  response = await fetch(`${baseUrl}/v1/responses`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${authorization}`,
      'Content-Type': 'application/json',
      Accept: 'text/event-stream, application/json',
      Originator: 'codex-tui',
      'User-Agent': 'codex-tui/0.145.0-alpha.18 (local-compaction-gate)',
      'X-Codex-Turn-Metadata': '{"request_kind":"compaction","compaction":{"implementation":"responses"}}',
      'X-OpenAI-Internal-Codex-Responses-Lite': 'true',
    },
    body: requestBody,
    signal: AbortSignal.timeout(timeoutMs),
  });
} catch (error) {
  const outcome = summarizeTransportError(error);
  const summary = {
    channel,
    model,
    request_shape: requestShape,
    request_body_bytes: Buffer.byteLength(requestBody),
    tool_definition_bytes: toolBytes,
    ...outcome,
  };
  process.stdout.write(`${JSON.stringify(summary)}\n`);
  fail(`request failed: ${outcome.transport_error}`);
}

const contentType = response.headers.get('content-type') ?? '';
const raw = await response.text();
const outcome = summarizeResponse(response.status, contentType, raw);
const summary = {
  channel,
  model,
  request_shape: requestShape,
  request_body_bytes: Buffer.byteLength(requestBody),
  tool_definition_bytes: toolBytes,
  ...outcome,
};

if (!response.ok) {
  process.stdout.write(`${JSON.stringify(summary)}\n`);
  fail(`HTTP ${response.status} ${outcome.error_code ?? 'upstream_error_without_structured_code'}`);
}
if (!outcome.completed) {
  process.stdout.write(`${JSON.stringify(summary)}\n`);
  fail(`missing successful Responses terminal state: ${outcome.terminal_type ?? 'missing'}`);
}

process.stdout.write(`${JSON.stringify({ status: 'passed', ...summary })}\n`);

function buildRequest(modelName, descriptionBytes, shape) {
  const message = {
    type: 'message',
    role: 'user',
    content: [
      {
        type: 'input_text',
        text: 'Local Codex compaction gate. Preserve this visible task context while reducing oversized tool definitions.',
      },
    ],
  };
  const reducedMessage = {
    ...message,
    content: [{
      type: 'input_text',
      text: `[user] ${message.content[0].text}`,
    }],
  };
  const tool = {
    type: 'function',
    name: 'local_compaction_gate_tool',
    description: 'A'.repeat(descriptionBytes),
    parameters: {
      type: 'object',
      properties: { value: { type: 'string' } },
      additionalProperties: false,
    },
  };
  const base = {
    model: modelName,
    stream: true,
    reasoning: { effort: 'low', summary: 'auto', context: 'all_turns' },
    stream_options: { reasoning_summary_delivery: 'sequential_cutoff' },
  };

  switch (shape) {
    case 'baseline':
      return { ...base, input: [additionalToolsItem([tool]), message] };
    case 'reduced_current':
      return reducedRequest(base, reducedMessage);
    case 'reduced_no_instructions':
      return reducedRequest(base, reducedMessage, { instructions: false });
    case 'reduced_empty_additional_tools':
      return reducedRequest(base, reducedMessage, { additionalTools: [] });
    case 'reduced_minimal_additional_tool':
      return reducedRequest(base, reducedMessage, { additionalTools: [tool] });
    case 'reduced_empty_top_level_tools':
      return reducedRequest(base, reducedMessage, { topLevelTools: [] });
    case 'reduced_minimal_top_level_tool':
      return reducedRequest(base, reducedMessage, { topLevelTools: [tool] });
    case 'reduced_no_reasoning_context':
      return reducedRequest(base, reducedMessage, { reasoningContext: false });
    case 'reduced_no_stream_options':
      return reducedRequest(base, reducedMessage, { streamOptions: false });
    default:
      fail(`unsupported request shape ${shape}`);
  }
}

function reducedRequest(base, message, shape = {}) {
  const request = { ...base, input: [message] };
  if (shape.instructions !== false) request.instructions = currentRecoveryInstructions;
  if (shape.additionalTools) request.input.unshift(additionalToolsItem(shape.additionalTools));
  if (shape.topLevelTools) request.tools = shape.topLevelTools;
  if (shape.reasoningContext === false) {
    request.reasoning = { effort: request.reasoning.effort, summary: request.reasoning.summary };
  }
  if (shape.streamOptions === false) delete request.stream_options;
  return request;
}

function additionalToolsItem(tools) {
  return { type: 'additional_tools', role: 'developer', tools };
}

function resolveRequestShape(value) {
  const shape = value ?? 'baseline';
  if (!supportedRequestShapes.has(shape)) {
    fail(`request-shape must be one of: ${[...supportedRequestShapes].join(', ')}`);
  }
  return shape;
}

function summarizeResponse(status, contentType, raw) {
  const trimmed = raw.trimStart();
  let outcome;
  if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
    outcome = summarizeJSONResponse(status, raw);
  } else if (trimmed.startsWith('data:') || trimmed.startsWith('event:') || trimmed.startsWith(':') || contentType.includes('text/event-stream')) {
    outcome = summarizeSSEResponse(status, raw);
  } else {
    outcome = {
      http_status: status,
      response_format: 'unknown',
      completed: false,
      terminal_type: null,
      error_code: null,
    };
  }
  return withFailureClass(status, outcome);
}

function summarizeJSONResponse(status, raw) {
  const payload = parseJSON(raw);
  const error = payload?.error ?? payload?.response?.error ?? null;
  const response = payload?.response ?? payload;
  const completed = status >= 200 && status < 300 && response?.status === 'completed';
  return {
    http_status: status,
    response_format: 'json',
    completed,
    terminal_type: response?.type ?? (completed ? 'response.completed' : null),
    error_code: errorCode(error),
  };
}

function summarizeSSEResponse(status, raw) {
  const events = [];
  let eventType = '';
  let dataLines = [];
  const flush = () => {
    if (dataLines.length === 0) return;
    const data = dataLines.join('\n');
    dataLines = [];
    if (!data || data === '[DONE]') return;
    const parsed = parseJSON(data);
    if (parsed) {
      if (!parsed.type && eventType) parsed.type = eventType;
      events.push(parsed);
    }
  };

  for (const line of raw.split(/\r?\n/)) {
    if (line === '') {
      flush();
      eventType = '';
      continue;
    }
    if (line.startsWith('event:')) {
      eventType = line.slice('event:'.length).trim();
      continue;
    }
    if (line.startsWith('data:')) {
      dataLines.push(line.slice('data:'.length).trim());
    }
  }
  flush();

  const terminal = [...events].reverse().find((event) =>
    ['response.completed', 'response.failed', 'response.incomplete', 'response.error', 'error'].includes(event?.type),
  );
  const terminalResponse = terminal?.response ?? terminal;
  const terminalError = terminal?.error ?? terminalResponse?.error ?? null;
  const completed = status >= 200 && status < 300 && terminal?.type === 'response.completed' &&
    (!terminalResponse?.status || terminalResponse.status === 'completed');
  return {
    http_status: status,
    response_format: 'sse',
    completed,
    terminal_type: terminal?.type ?? null,
    event_types: [...new Set(events.map((event) => event?.type).filter(Boolean))],
    error_code: errorCode(terminalError),
  };
}

function summarizeTransportError(error) {
  const transportError = safeErrorName(error);
  const transportErrorCode = safeTransportErrorCode(error);
  return {
    http_status: null,
    response_format: 'transport',
    completed: false,
    terminal_type: null,
    error_code: null,
    transport_error: transportError,
    transport_error_code: transportErrorCode,
    failure_class: transportError === 'TimeoutError' || transportError === 'AbortError'
      ? 'timeout'
      : 'transport_error',
  };
}

function safeTransportErrorCode(error) {
  const allowedCodes = new Set([
    'ECONNREFUSED',
    'ECONNRESET',
    'EAI_AGAIN',
    'ENETUNREACH',
    'ENOTFOUND',
    'ETIMEDOUT',
    'ERR_NETWORK',
    'UND_ERR_BODY_TIMEOUT',
    'UND_ERR_CONNECT_TIMEOUT',
    'UND_ERR_HEADERS_TIMEOUT',
    'UND_ERR_SOCKET',
  ]);
  const candidates = [error?.code, error?.cause?.code, error?.cause?.cause?.code];
  return candidates.find((code) => typeof code === 'string' && allowedCodes.has(code)) ?? null;
}

function withFailureClass(status, outcome) {
  return {
    ...outcome,
    failure_class: classifyFailure(status, outcome),
  };
}

function classifyFailure(status, outcome) {
  if (status >= 200 && status < 300 && outcome.completed) return null;

  const errorCode = outcome.error_code;
  if (status === 413) {
    if (errorCode === 'read_request_body_failed') return 'gateway_request_body_limit';
    if (errorCode === 'upstream_request_too_large') return 'upstream_request_too_large';
    return 'http_413_unknown';
  }
  if (status === 429) return 'http_429';
  if (status === 502) return 'http_502';
  if (status >= 500) return 'http_5xx';
  if (status >= 400 && isProtocolErrorCode(errorCode)) return 'protocol_rejection';
  if (status >= 400) return 'http_4xx';
  return 'responses_terminal_failure';
}

function isProtocolErrorCode(errorCode) {
  return [
    'invalid_request_error',
    'invalid_request',
    'invalid_type',
    'unknown_parameter',
    'unsupported_parameter',
    'unsupported_value',
  ].includes(errorCode);
}

function errorCode(error) {
  if (!error || typeof error !== 'object') return null;
  for (const value of [error.code, error.type, error.param]) {
    if (typeof value === 'string' && value) return value;
  }
  return null;
}

function parseJSON(value) {
  try {
    return JSON.parse(value);
  } catch {
    return null;
  }
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

function requiredNumericOption(parsed, name) {
  const value = requiredOption(parsed, name);
  if (!/^\d+$/.test(value) || Number(value) <= 0) fail(`${name} must be a positive integer`);
  return value;
}

function requiredPositiveInteger(parsed, name) {
  return positiveInteger(requiredOption(parsed, name), name);
}

function positiveInteger(value, name) {
  if (!/^\d+$/.test(value) || Number(value) <= 0 || !Number.isSafeInteger(Number(value))) {
    fail(`${name} must be a positive integer`);
  }
  return Number(value);
}

function resolveToken(parsed) {
  const tokenEnv = requiredOption(parsed, 'token-env');
  if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(tokenEnv)) {
    fail('token environment variable name is invalid');
  }
  const token = process.env[tokenEnv];
  if (!token) fail(`token environment variable ${tokenEnv} is empty`);
  return token;
}

function safeErrorName(error) {
  return typeof error?.name === 'string' && error.name ? error.name : 'request_error';
}

function fail(message) {
  process.stderr.write(`COMPACTION_GATE_FAILED ${message}\n`);
  process.exit(1);
}
