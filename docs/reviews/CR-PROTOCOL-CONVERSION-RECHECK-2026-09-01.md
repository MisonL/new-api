# Protocol conversion recheck - 2026-09-01

## Scope

This review covers OpenAI, Claude, AWS Bedrock Converse, Dify, Mistral,
Ollama, Gemini, PaLM, Tencent, Vertex, Baidu, Cloudflare, Jina, MokaAI,
Xunfei, and Zhipu protocol conversion paths. The primary regression target is
preserving Claude thinking and OpenAI `reasoning_content` when the request is
converted through Chat Completions. The sample time for this update is
2026-09-01 16:42 CST (Asia/Shanghai).

## Changes checked

- Claude request and response conversion now preserves visible thinking and
  rejects opaque or malformed blocks instead of silently dropping them.
- The shared message accessor now recognizes `reasoning_content`,
  `thinking_content`, and `reasoning`; Gemini and Ollama request conversion
  preserve all three aliases, and Responses-to-Chat merging canonicalizes an
  existing alias before appending new reasoning text. Dify and AWS Converse
  reject the same aliases explicitly when their upstream protocols cannot
  carry provider reasoning state.
- SenseNova DeepSeek V4 mapping follows the documented model split: Flash
  uses `low`/`medium`/`high`/`none`, while Pro uses `low`/`high`/`max`; legacy
  `max`, `medium`, and `xhigh` inputs are normalized without sending an
  unsupported effort to the upstream.
- Claude tool choice mapping validates unknown values and keeps the documented
  `auto`, `any`, `tool`, and `none` shapes.
- Ollama request conversion preserves assistant reasoning, tool call IDs and
  indexes; stream and non-stream handlers aggregate split tool arguments and
  retain content or thinking carried by the final frame.
- AWS Converse conversion validates message, tool, sampling, image, and JSON
  schema fields and maps tool use, tool results, usage, and reasoning blocks.
- Text-only Dify and Mistral paths reject unsupported history metadata and
  tool features explicitly.
- Unsupported Claude conversion entrypoints in Baidu, Cloudflare, Dify, Jina,
  Mistral, MokaAI, and Tencent return errors instead of panicking.

## Verification evidence

The following commands were run on 2026-09-01 (Asia/Shanghai):

```text
go test -count=1 ./setting/reasoning ./relay/channel/openai ./service/openaicompat  # exit 0
go test -count=1 ./relay/channel/claude ./relay/channel/gemini ./relay/channel/ollama ./relay/channel/aws ./relay/channel/dify  # exit 0
go test -count=1 ./relay/...  # exit 0
git diff --check  # exit 0
```

`go test -count=1 ./service` compiled but exited 1 in three pre-existing dirty
task-route tests (`TestTaskRouteIdentityFromSnapshotRejectsFingerprintMismatch`,
`TestTaskRouteIdentityFromSnapshotUsesPhaseSpecificOperations`, and
`TestBuildTaskRouteSnapshotRequestHeadersRejectsTamperedHeaderPolicy`). Each
failed before reaching this review's protocol assertions with
`task route snapshot provider runtime is incomplete`; this failure is recorded
separately and is not used as protocol-conversion evidence.

The isolated image was rebuilt with `scripts/build-docker-local.sh
new-api-local:dev` and the `new-api-dev-isolated-new-api-1` container was
recreated from that image. Runtime checks passed:

```text
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
  version=v1.1.0
  commit=f825f23d870df24e38362a7bd1d0a2cd4423a1a6-dirty
curl --connect-timeout 5 --max-time 15 http://127.0.0.1:3001/api/status
  HTTP 200, success=true
curl --connect-timeout 5 --max-time 15 http://127.0.0.1:3001/api/healthz
  HTTP 200
```

The isolated container is `running`, `healthy`, and has restart count 0. The
production container on port 13000 was only probed read-only and remained
`running`, `healthy`, restart count 0, and on image
`new-api-local:prod-codex-lite-4527d766`; bounded `/api/status` and
`/api/healthz` probes both returned HTTP 200.

## Official references

- Anthropic Messages SDK types:
  https://raw.githubusercontent.com/anthropics/anthropic-sdk-typescript/main/src/resources/messages/messages.ts
- Ollama tool calling:
  https://docs.ollama.com/capabilities/tool-calling.md
- SenseNova API and DeepSeek V4 parameter documentation:
  https://platform.sensenova.cn/docs

All three official references returned HTTP 200 when checked on 2026-09-01.

## Boundaries

No real billable inference was sent to AWS, Dify, Mistral, Ollama, or other
upstream channels in this review. Production port 13000 was not rebuilt,
recreated, or written to. The evidence therefore covers source semantics,
regression tests, isolated container startup, and local health probes, not
real upstream acceptance.

## Follow-up closure

Follow-up verification at 2026-09-01 19:35 CST resolved the three task-route
test failures recorded above. A compile-only naming typo in
`middleware/distributor.go` was corrected so the route-snapshot wrapper calls
the exported credential-aware setup function.

The following checks now exit 0:

```text
89 Go packages, excluding desktop/tauri-app/src-tauri/target: go test -p=1 -count=1
core backend packages: go vet -p=1 ./middleware ./controller ./model ./relay ./relay/common ./relay/helper ./service ./service/openaicompat
gofmt -d on the changed Go files
git diff --check on the tracked task files
```

Bounded read-only probes also returned HTTP 200 for `/api/status` and
`/api/healthz` on both `127.0.0.1:13000` and `127.0.0.1:3001`. Production was
not rebuilt or restarted; port 13000 still runs image
`new-api-local:prod-codex-lite-4527d7666`, while the isolated container runs
the current dirty worktree commit.
