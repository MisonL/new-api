#!/usr/bin/env node

const baseUrl = (process.env.NEWAPI_BASE_URL ?? 'http://127.0.0.1:3001').replace(/\/$/, '');
const token = process.env.NEWAPI_SMOKE_TOKEN;
if (!token) {
  console.error('NEWAPI_SMOKE_TOKEN is required');
  process.exit(2);
}

const model = process.env.NEWAPI_SMOKE_MODEL ?? 'gpt-5.6-sol';
const headers = {
  Authorization: `Bearer ${token}`,
  'Content-Type': 'application/json',
  Accept: 'text/event-stream, application/json',
  Originator: 'codex-tui',
  'User-Agent': 'codex-tui/0.145.0-alpha.18 (compatibility-event-probe)',
  'X-OpenAI-Internal-Codex-Responses-Lite': 'true',
};

const searchTool = {
  type: 'tool_search',
  execution: 'client',
  description: 'Search deferred smoke-test tools.',
  parameters: {
    type: 'object',
    properties: {
      query: { type: 'string' },
      limit: { type: 'number' },
    },
    required: ['query'],
    additionalProperties: false,
  },
};

const message = {
  type: 'message',
  role: 'user',
  content: [{
    type: 'input_text',
    text: 'You must call tool_search with query deferred_lookup. Do not answer directly.',
  }],
};

const additionalTools = {
  type: 'additional_tools',
  role: 'developer',
  tools: [searchTool],
};

const first = await post({
  model,
  input: [additionalTools, message],
  tool_choice: 'auto',
  reasoning: { effort: 'low', summary: 'auto', context: 'all_turns' },
  stream: true,
});

const firstOutput = continuationOutput(first.events);
const searchCall = firstOutput.find((item) => item?.type === 'tool_search_call');
const result = {
  discovery: summarize(first),
  discovery_continuation_output_types: firstOutput.map((item) => item?.type).filter(Boolean),
  discovery_tool_search_call: summarizeItem(searchCall),
};

if (searchCall?.call_id) {
  const second = await post({
    model,
    input: [
      ...firstOutput,
      {
        type: 'tool_search_output',
        call_id: searchCall.call_id,
        status: 'completed',
        execution: 'client',
        tools: [
          {
            type: 'namespace',
            name: 'smoke_deferred',
            description: 'Deferred smoke-test tools.',
            tools: [
              {
                type: 'function',
                name: 'deferred_lookup',
                description: 'Return a requested deferred smoke-test value.',
                defer_loading: true,
                strict: true,
                parameters: {
                  type: 'object',
                  properties: { value: { type: 'string' } },
                  required: ['value'],
                  additionalProperties: false,
                },
              },
            ],
          },
        ],
      },
    ],
    tool_choice: 'auto',
    reasoning: { effort: 'low', summary: 'auto', context: 'all_turns' },
    stream: true,
  });
  result.loaded = summarize(second);
  result.loaded_continuation_output_types = continuationOutput(second.events)
    .map((item) => item?.type)
    .filter(Boolean);
}

console.log(JSON.stringify(result));

async function post(body) {
  const response = await fetch(`${baseUrl}/v1/responses`, {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  });
  const raw = await response.text();
  return {
    status: response.status,
    events: parseEvents(raw),
  };
}

function parseEvents(raw) {
  return raw
    .split(/\r?\n/)
    .filter((line) => line.startsWith('data:'))
    .map((line) => line.slice(5).trim())
    .filter((data) => data && data !== '[DONE]')
    .map((data) => {
      try {
        return JSON.parse(data);
      } catch {
        return { type: 'invalid_json' };
      }
    });
}

function continuationOutput(events) {
  const terminal = [...events].reverse().find((event) => event.type === 'response.completed');
  if (Array.isArray(terminal?.response?.output) && terminal.response.output.length > 0) {
    return terminal.response.output;
  }
  return events
    .filter((event) => event.type === 'response.output_item.done' && event.item)
    .map((event) => event.item);
}

function summarize(request) {
  const terminal = [...request.events].reverse().find((event) => event.type === 'response.completed');
  return {
    http_status: request.status,
    event_types: request.events.map((event) => event.type).filter(Boolean),
    output_item_done: request.events
      .filter((event) => event.type === 'response.output_item.done')
      .map((event) => summarizeItem(event.item)),
    terminal: terminal
      ? {
          status: terminal.response?.status,
          output_count: Array.isArray(terminal.response?.output)
            ? terminal.response.output.length
            : null,
          output_types: Array.isArray(terminal.response?.output)
            ? terminal.response.output.map((item) => item?.type).filter(Boolean)
            : [],
        }
      : null,
  };
}

function summarizeItem(item) {
  if (!item) return null;
  return {
    type: item.type,
    name: item.name,
    namespace: item.namespace,
    has_call_id: Boolean(item.call_id),
    id_prefix: typeof item.id === 'string' ? item.id.split('_', 1)[0] : null,
  };
}
