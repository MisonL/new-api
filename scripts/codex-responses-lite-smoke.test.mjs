import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { createServer } from "node:http";
import test from "node:test";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const smokeScript = resolve(repoRoot, "scripts/codex-responses-lite-smoke.mjs");

test("tool-search smoke fails when discovery completes without a tool_search_call", async () => {
  const result = await runToolSearchSmoke([
    completedResponse([
      {
        type: "message",
        role: "assistant",
        content: [{ type: "output_text", text: "No tool call." }],
      },
    ]),
  ]);

  assert.notEqual(result.exitCode, 0);
  assert.match(
    result.stderr,
    /tool-search: upstream completed without a tool_search_call call_id/,
  );
  assert.equal(result.requests.length, 1);
});

test("tool-search smoke fails when the loaded deferred tool is not called", async () => {
  const result = await runToolSearchSmoke([
    sse([
      {
        type: "response.output_item.done",
        item: {
          id: "tsc_search",
          type: "tool_search_call",
          status: "completed",
          call_id: "call_search",
          arguments: '{"query":"deferred_lookup"}',
        },
      },
      {
        type: "response.completed",
        response: { id: "resp_discovery", status: "completed", output: [] },
      },
    ]),
    completedResponse([
      {
        type: "message",
        role: "assistant",
        content: [{ type: "output_text", text: "No deferred tool call." }],
      },
    ]),
  ]);

  assert.notEqual(result.exitCode, 0);
  assert.match(result.stderr, /tool-search: deferred_lookup was not callable/);
  assert.equal(result.requests.length, 2);

  const continuation = result.requests[1];
  assert.equal(continuation.tool_choice, "auto");
  assert.equal(continuation.input[0].type, "tool_search_call");
  assert.equal(continuation.input[1].type, "tool_search_output");
  assert.equal(continuation.input[1].tools[0].type, "namespace");
  assert.equal(continuation.input[1].tools[0].name, "smoke_deferred");
  assert.equal(continuation.input[1].tools[0].tools[0].name, "deferred_lookup");
});

test("tool-search smoke accepts Codex event-backed discovery and loaded deferred tool call", async () => {
  const result = await runToolSearchSmoke([
    sse([
      {
        type: "response.output_item.done",
        item: {
          id: "tsc_search",
          type: "tool_search_call",
          status: "completed",
          call_id: "call_search",
          arguments: '{"query":"deferred_lookup"}',
        },
      },
      {
        type: "response.completed",
        response: { id: "resp_discovery", status: "completed", output: [] },
      },
    ]),
    sse([
      {
        type: "response.output_item.done",
        item: {
          id: "fc_deferred",
          type: "function_call",
          status: "completed",
          call_id: "call_deferred",
          name: "deferred_lookup",
          namespace: "smoke_deferred",
          arguments: '{"value":"CODEX_LITE_TOOL_SEARCH_OK"}',
        },
      },
      {
        type: "response.completed",
        response: { id: "resp_loaded", status: "completed", output: [] },
      },
    ]),
  ]);

  assert.equal(result.exitCode, 0);
  assert.equal(result.stderr, "");
  assert.equal(result.requests.length, 2);
  const summary = JSON.parse(result.stdout);
  assert.equal(summary.scenario, "tool-search");
  assert.equal(summary.output_reconstructed_from_events, true);
  assert.equal(summary.loaded_dynamic_tool_called, true);
  assert.deepEqual(summary.tool_names, ["deferred_lookup"]);
  assert.deepEqual(summary.namespaces, ["smoke_deferred"]);
});

test("smoke accepts a token from a named environment variable", async () => {
  const requests = [];
  const server = createServer(async (request, response) => {
    const body = await readRequestBody(request);
    requests.push({
      authorization: request.headers.authorization,
      body: JSON.parse(body),
    });
    response.writeHead(200, { "content-type": "application/json" });
    response.end(JSON.stringify({
      id: "resp_token_env",
      status: "completed",
      output: [],
    }));
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");

  const address = server.address();
  assert.ok(address && typeof address !== "string");
  const child = spawn(
    process.execPath,
    [
      smokeScript,
      "--base-url",
      `http://127.0.0.1:${address.port}`,
      "--token-env",
      "NEWAPI_TEST_SMOKE_TOKEN",
      "--channel",
      "155",
      "--model",
      "gpt-5.6-sol",
      "--scenario",
      "basic",
      "--timeout-ms",
      "5000",
    ],
    {
      cwd: repoRoot,
      env: {
        ...process.env,
        NEWAPI_TEST_SMOKE_TOKEN: "test-token-from-environment",
      },
    },
  );
  const stdout = [];
  const stderr = [];
  child.stdout.on("data", (chunk) => stdout.push(chunk));
  child.stderr.on("data", (chunk) => stderr.push(chunk));
  const [exitCode] = await once(child, "close");
  await new Promise((resolveClose) => server.close(resolveClose));

  assert.equal(exitCode, 0);
  assert.equal(Buffer.concat(stderr).toString("utf8"), "");
  assert.match(Buffer.concat(stdout).toString("utf8"), /"scenario":"basic"/);
  assert.equal(requests.length, 1);
  assert.equal(requests[0].authorization, "Bearer test-token-from-environment-155");
  assert.equal(requests[0].body.model, "gpt-5.6-sol");
});

async function runToolSearchSmoke(responses) {
  const requests = [];
  const server = createServer(async (request, response) => {
    const body = await readRequestBody(request);
    requests.push(JSON.parse(body));
    const nextResponse = responses.shift();
    if (!nextResponse) {
      response.writeHead(500, { "content-type": "application/json" });
      response.end('{"error":{"message":"unexpected request"}}');
      return;
    }
    response.writeHead(200, { "content-type": "text/event-stream" });
    response.end(nextResponse);
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");

  const address = server.address();
  assert.ok(address && typeof address !== "string");
  const child = spawn(
    process.execPath,
    [
      smokeScript,
      "--base-url",
      `http://127.0.0.1:${address.port}`,
      "--token",
      "test-token",
      "--channel",
      "auto",
      "--model",
      "gpt-5.6-sol",
      "--scenario",
      "tool-search",
      "--timeout-ms",
      "5000",
    ],
    { cwd: repoRoot },
  );
  const stdout = [];
  const stderr = [];
  child.stdout.on("data", (chunk) => stdout.push(chunk));
  child.stderr.on("data", (chunk) => stderr.push(chunk));
  const [exitCode] = await once(child, "close");
  await new Promise((resolveClose) => server.close(resolveClose));

  return {
    exitCode,
    requests,
    stdout: Buffer.concat(stdout).toString("utf8"),
    stderr: Buffer.concat(stderr).toString("utf8"),
  };
}

function completedResponse(output) {
  return sse([
    {
      type: "response.completed",
      response: { id: "resp_complete", status: "completed", output },
    },
  ]);
}

function sse(events) {
  return events.map((event) => `data: ${JSON.stringify(event)}\n\n`).join("");
}

function readRequestBody(request) {
  return new Promise((resolveBody, rejectBody) => {
    const chunks = [];
    request.on("data", (chunk) => chunks.push(chunk));
    request.on("end", () => resolveBody(Buffer.concat(chunks).toString("utf8")));
    request.on("error", rejectBody);
  });
}
