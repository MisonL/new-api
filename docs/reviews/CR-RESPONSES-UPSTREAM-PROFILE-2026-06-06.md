# CR-RESPONSES-UPSTREAM-PROFILE-2026-06-06

## Scope

本记录覆盖 `ResponsesUpstreamProfile` 对 OpenAI-compatible 上游中转站的治理验证：

- `generic_proxy` / `chat_only_proxy` 显式声明为非官方或未知中转站。
- profile 驱动 encrypted reasoning context strip，不依赖旧 `strip_codex_encrypted_context` 开关。
- profile 强制 OpenAI-compatible Responses Compact 使用 synthetic summary，上游请求路径为 `/v1/responses`。
- synthetic compact state 使用实例隔离 ID 与 v2 marker，支持本地 marker 续接，foreign v2 marker 不误判为本地 state。
- `3001` 隔离 dev 使用当前工作区 dirty 构建验证，正式容器未参与验证请求。

## Build And Runtime Boundary

隔离 dev 重新构建与重建：

```bash
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
curl -fsS http://127.0.0.1:3001/api/status
```

结果：

- Docker build exit code: 0。
- `3001` build-info: `version=v1.1.0`, current dirty commit, `date=2026-06-06T13:57:00Z`。
- `new-api-dev-isolated-new-api-1` health: `healthy`。
- `/api/status` returned `success=true`, `version=v1.1.0`, `setup=true`。
- Docker image: `new-api-local:dev` built successfully.

## Copied Channel Fixture

从正式 PostgreSQL 复制了一组已有可用或候选渠道到隔离 dev PostgreSQL，使用管道 `COPY` 传输敏感列，不在终端或文档打印 `key`、`base_url`、`header_override` 内容。

复制结果已去标识化：隔离 dev 中覆盖了 `generic_proxy` 与 `chat_only_proxy` 两类 fixture，状态均为可验证状态，组名和 channel 编号保存在内部审计记录中。

验证前删除目标 fixture 的旧 `strip_codex_encrypted_context` 字段，仅保留 profile 控制输入；两个目标 fixture 分别保持 `generic_proxy` / `chat_only_proxy`，compact setting 为 `auto`。

## L0 And L1 Validation

已执行并通过：

```bash
go test ./dto ./model ./relay/common ./relay/channel/openai ./relay/channel/codex ./service -count=1 -run 'Test.*Responses|Test.*SyntheticCompact|TestShouldConvertResponsesRequest'
cd web/default && bun test tests/channel-responses-compact.test.ts
git diff --check
go test ./controller ./model ./relay/common ./relay/helper ./service
go test ./relay/channel/openai ./relay/channel/codex -count=1
cd web/default && bun run lint
cd web/default && bun run build
go test ./relay/channel/openai ./service ./model -count=1 -run 'TestSyntheticCompact|TestOaiSyntheticResponsesCompactionHandlerReturnsSyntheticCompactionOutput|TestChannelOtherSettingsResponsesProxyProfile|TestChannelOtherSettingsTrustedResponsesProfiles'
```

补充复核：

```bash
go test ./dto ./model ./relay/common ./service -count=1 -run 'TestChannelOtherSettingsResponsesProxyProfileForcesSyntheticCompact|TestShouldConvertResponsesRequest|TestResponsesCompact'
go test ./model ./relay/common ./relay/channel/openai ./relay/channel/codex ./service -count=1 -run 'Test.*Responses|Test.*SyntheticCompact|TestShouldConvertResponsesRequest'
cd web/default && bun test tests/channel-responses-compact.test.ts
git diff --check
```

结果：exit code 0。

## Independent Reviewer Passes

本轮额外使用同一份完整 uncommitted diff 审查包复核，审查包包含 tracked diff 与 untracked 新文件：

- `service/responses_synthetic_compact_instance.go`
- `docs/reviews/CR-RESPONSES-UPSTREAM-PROFILE-2026-06-06.md`

审查包敏感形态扫描结果：`sk_like=0`, `bearer_like=0`, `auth_header_lines=0`。

Claude review:

- No hidden fallback, billing path, conversion-chain, frontend serialization, or marker-isolation correctness issue was confirmed.
- Claude raised one Critical finding about `syntheticCompactMarkerInstanceMatches` using `context.Background()`. This was fixed in code by threading request context through synthetic compact reference and marker checks; the remaining store-context timeout wrapper for DB persistence is intentional and still matches the existing synthetic compact persistence pattern.
- Claude raised a warning about `model.Option` uniqueness. Cross-check: `model.Option.Key` is declared `gorm:"primaryKey"`, so the cross-process create/reload path has a unique key boundary.
- Claude's remaining notes were migration or UX observations, not blocking correctness findings.

Gemini review:

- No Critical or Warning issue was reported.
- Gemini specifically confirmed proxy profile forced synthetic summary, encrypted reasoning strip, auto fallback suppression, v2 marker foreign-instance pass-through, conversion-chain logging, and frontend settings cleanup.

CodeRabbit review:

- Reported 4 findings on the uncommitted diff.
- The `model.Option` critical finding was cross-checked against `model.Option.Key gorm:"primaryKey"`; the code was still tightened to update the loaded option value in place and a regression test was added for invalid stored instance options.
- The v2 marker parsing path was tightened further: v2 markers now require the marker instance and state id instance to match, and malformed v2 markers without an embedded id instance stay visible instead of being treated as local state references.
- The duplicate request-context helper was moved to `relay/common.GinRequestContext`.
- The compatibility `HasSyntheticCompactReference` wrapper now returns `(bool, error)`; no production caller remains on the old bool-only path.
- The synthetic compact response test now unmarshals output as `map[string]interface{}` to avoid string-only test assumptions.

## L2 Runtime Validation

验证方式：

- 在隔离 dev DB 中创建临时管理员 token。
- 使用临时 token 的 specific-channel 形式固定命中指定 dev fixture；真实 token 值和 channel id 不写入仓库。
- 请求结束后删除临时 token。
- 请求体写入临时文件，Authorization 通过 `curl -K -` 的 stdin 输入，不打印 token。

### Profile-driven encrypted reasoning strip

请求：

- Channel: de-identified `generic_proxy` dev fixture.
- Profile: `generic_proxy`
- Old strip switch: absent
- Endpoint: `POST http://127.0.0.1:3001/v1/responses`
- Model: `gpt-5.4-mini`
- Input includes one `reasoning` item with `encrypted_content` plus one user message.

Result:

- HTTP status: `200`
- Response object: `response`
- Response model prefix: `gpt-5.4-mini-2026-03-17`
- Runtime log:

```text
responses encrypted reasoning context removed for OpenAI-compatible channel: channel_id=<dev-fixture> model=gpt-5.4-mini encrypted_reasoning_items=1 remaining_items=1
```

This proves `generic_proxy` alone triggers encrypted reasoning stripping after the legacy strip flag was removed from the fixture.

### Synthetic compact on generic proxy

请求：

- Channel: de-identified `generic_proxy` dev fixture.
- Profile: `generic_proxy`
- Endpoint: `POST http://127.0.0.1:3001/v1/responses/compact`
- Model: `gpt-5.4-mini`

Result:

- HTTP status: `200`
- Response id prefix: `resp_newapi_synthcmp_n<local-instance>_...`
- Output marker prefix: `newapi.synthetic.compact:v2:n<local-instance>:resp_newapi_synthcmp_...`
- Consume log:

```text
Responses Compact mode=synthetic_summary setting=auto path=/v1/responses
```

DB `logs.other` confirms the de-identified fixture used model `gpt-5.4-mini-openai-compact`, compact mode `synthetic_summary`, compact setting `auto`, and upstream path `/v1/responses`.

### Local marker continuation

请求：

- Channel: de-identified `generic_proxy` dev fixture.
- Endpoint: `POST /v1/responses`
- Input includes the returned local v2 compact marker plus a user message.

Result:

- HTTP status: `200`
- Response object: `response`
- Consume log request conversion: `["OpenAI Responses Compact", "OpenAI Responses"]`

DB synthetic state records were created with local `resp_newapi_synthcmp_n<local-instance>_...` ids, the expected compact model, and the de-identified user/token/group/channel scope. Full row evidence belongs in the internal audit record.

### Foreign marker handling

请求：

- Channel: de-identified `generic_proxy` dev fixture.
- Endpoint: `POST /v1/responses/compact`
- Input includes a foreign v2 marker:
  `newapi.synthetic.compact:v2:nffffffffffffffffffffffffffffffff:resp_newapi_synthcmp_nffffffffffffffffffffffffffffffff_deadbeef`
- Input also includes visible user text.

Result:

- HTTP status: `200`
- Response id prefix: local `resp_newapi_synthcmp`
- Output marker prefix: local `newapi.synthetic.compact:v2:`
- No `synthetic compact state not found or expired` error occurred.

This proves the foreign v2 marker is not treated as a local state reference.

### Chat-only proxy compact path

请求：

- Channel: de-identified `chat_only_proxy` dev fixture.
- Profile: `chat_only_proxy`
- Old strip switch: absent
- Endpoint: `POST /v1/responses/compact`
- Model: `claude-haiku-4-5-20251001`

Result:

- HTTP status: `500`
- Error prefix: `not implemented`
- Runtime and DB logs still show profile-driven compact governance:

```text
Responses Compact mode=synthetic_summary setting=auto path=/v1/responses
```

DB `logs.other` confirms the de-identified fixture used the compact-suffixed model, compact mode `synthetic_summary`, compact setting `auto`, upstream path `/v1/responses`, and upstream result `not implemented`.

This proves `chat_only_proxy` selects synthetic compact governance. The remaining failure is upstream capability for this copied channel/model, not local route selection.

## Latest Revalidation

Current build rerun after the compact conversion-chain fix, context propagation fix, v2 marker strictness fix, and CodeRabbit follow-up:

- `3001` build-info: `version=v1.1.0`, current dirty commit, `date=2026-06-06T15:21:28Z`.
- Docker image: `new-api-local:dev` built successfully.
- `new-api-dev-isolated-new-api-1` health: `healthy`.
- `/api/status`: `success=true`, `version=v1.1.0`, `setup=true`, `theme=classic`.

Fresh local validation after the final code changes:

```bash
git diff --check
go test ./dto ./model ./relay/common ./relay/channel/openai ./relay/channel/codex ./service -count=1 -run 'Test.*Responses|Test.*SyntheticCompact|TestShouldConvertResponsesRequest'
go test ./controller ./model ./relay/common ./relay/helper ./service -count=1
cd web/default && bun test tests/channel-responses-compact.test.ts
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
curl -fsS http://127.0.0.1:3001/api/status
```

Result: all commands exited 0. The final Docker build emitted `built image=new-api-local:dev version=v1.1.0 commit=current dirty commit date=2026-06-06T15:21:28Z`.

## 2026-06-07 Final Isolated Dev Smoke

Purpose:

- Verify the rebuilt `3001` isolated dev container, not just local unit tests or frontend build output.
- Verify `generic_proxy` forces synthetic Responses Compact to upstream `/v1/responses`.
- Verify `responses_compact_mode=disabled` excludes compact routing while ordinary `/v1/responses` can still use the same channel.
- Keep production separate from this dev smoke.

Runtime boundary:

- `3001` build-info after rebuild and smoke: `version=v1.1.0`, current dirty commit, `date=2026-06-06T16:18:49Z`.
- `3001` `/api/status`: `success=true`, `version=v1.1.0`, `setup=true`.
- Production `new-api` build-info during the same check remained on its earlier build; production was not rebuilt.
- Fake upstream container ran only on the isolated dev Docker network.

Temporary fixture:

- Channels: one `generic_proxy` fixture with `responses_compact_mode=auto`, and one disabled compact fixture with `responses_compact_mode=disabled`.
- Abilities: one ordinary model route and one disabled-compact test route, both de-identified.
- Token: one temporary admin token; token name and value are not kept in this repo.
- After inserting the fixture, `new-api-dev-isolated-new-api-1` was force recreated so the initial channel cache sync loaded the temporary route data.

Smoke results:

| Case | Request | Expected boundary | Result |
| --- | --- | --- | --- |
| Generic proxy compact | `POST /v1/responses/compact`, `generic_proxy` fixture, model `gpt-5.5` | Synthetic compact, upstream `/v1/responses` | HTTP `200`, `object=response`, id prefix `resp_newapi_synthcmp_`, marker prefix `newapi.synthetic.compact:v2:` |
| Disabled compact route | `POST /v1/responses/compact`, no specific channel, model `gpt-smoke-disabled-20260607` | No compact route available | HTTP `503`, error included `No available channel for model gpt-smoke-disabled-20260607-openai-compact under group default` |
| Disabled base route | `POST /v1/responses`, disabled compact fixture, model `gpt-5.5` | Ordinary responses still route | HTTP `200`, `object=response`, upstream response id `resp_fake_smoke`, model `gpt-5.5` |

Captured dev evidence:

```text
synthetic_final_http=200
synthetic_object=response
synthetic_id_prefix=resp_newapi_synthcmp_
synthetic_marker_prefix=newapi.synthetic.compact:v2:ne9f
```

Dev DB `logs.other` for the synthetic runs confirmed the de-identified fixture used `gpt-5.5-openai-compact`, compact mode `synthetic_summary`, compact setting `auto`, and upstream path `/v1/responses`.

Fake upstream logs during the smoke:

```text
{"method":"POST","url":"/v1/responses","bodyLength":745}
{"method":"POST","url":"/v1/responses","bodyLength":142}
{"method":"POST","url":"/v1/responses","bodyLength":749}
{"method":"POST","url":"/v1/responses","bodyLength":744}
```

There was no fake upstream request to `/v1/responses/compact`. The four `/v1/responses` entries are from three synthetic compact runs, including marker extraction retries, plus one ordinary disabled-channel `/v1/responses` run.

Cleanup:

- Deleted temporary smoke logs, abilities, channels, and token from isolated dev PostgreSQL.
- Removed the temporary fake upstream container.
- Force recreated `new-api-dev-isolated-new-api-1` again so the in-memory channel cache matched the cleaned database.
- Post-cleanup counts: `channels=0`, `abilities=0`, `tokens=0`, `logs=0` for the smoke fixture.
- Post-cleanup `3001` health: `healthy`.

Boundary note: this fake upstream smoke validates gateway routing, synthetic compact marker/state construction, and disabled compact route semantics. It does not validate real external upstream quality or availability.

## 2026-06-07 Confirmation Revalidation

After the final user confirmation request, the validation chain was rerun without additional source edits.

Commands and results:

| Command | Result |
| --- | --- |
| `git diff --check` | Exit code 0 |
| `go test ./controller ./model ./relay/common ./relay/helper ./service` | Exit code 0 |
| `go test ./relay/channel/openai ./relay/channel/codex -count=1` | Exit code 0 |
| `cd web/default && bun test tests/channel-responses-compact.test.ts` | Exit code 0, 16 pass, 230 assertions |
| `cd web/default && bun run lint` | Exit code 0 |
| `cd web/default && bun run i18n:sync` | Exit code 0 |
| `cd web/default && node -e '...'` against `_sync-report.json` | All locales had `missingCount=0`, `extrasCount=0`, `untranslatedCount=0` |
| `cd web/default && bun run typecheck` | Exit code 0 |
| `cd web/default && VITE_REACT_APP_VERSION=codex-verify-<timestamp> bun run build` | Exit code 0, build completed, Safari compatibility rewrite/check passed |

The first plain `bun run build` attempt in this confirmation pass became idle in the build process tree with no active `rsbuild` child; it was terminated and replaced by an isolated-version build using the existing `performance.buildCache.cacheDigest` input. This did not require a source change and the replacement build passed.

Environment checks:

- `3001` isolated dev build-info: `version=v1.1.0`, current dirty commit, `date=2026-06-06T16:18:49Z`.
- `3001` `/api/status`: `success=true`, `version=v1.1.0`, `setup=true`.
- Production `new-api` build-info remained on its earlier build; production was not rebuilt.
- `new-api-dev-isolated-new-api-1`, PostgreSQL, and Redis were `healthy`.
- No temporary fake upstream container remained.

Current smoke attempts after the rebuild were blocked by the copied upstream fixtures, not by the local code path:

- A de-identified `generic_proxy` fixture returned `403`, `Insufficient account balance`; its compact log still showed `Responses Compact mode=synthetic_summary setting=auto path=/v1/responses`.
- Other copied fixtures returned upstream/account-state failures such as missing subscription, temporary service unavailability, or daily limit exhaustion.

Current DB `logs.other` for the blocked compact attempt still shows the intended routing: model `gpt-5.4-mini-openai-compact`, request conversion `["OpenAI Responses Compact", "OpenAI Responses"]`, compact mode `synthetic_summary`, setting `auto`, upstream path `/v1/responses`.

## Boundary And Residual Risk

- Production `new-api` was not rebuilt and was not used as the target of validation requests.
- Sensitive production channel fields were copied through DB pipes and were not printed.
- The strongest local evidence after the rebuild is the router and log path: `ResponsesUpstreamProfile` still forces synthetic compact routing, and the logs confirm the conversion chain now goes through `["OpenAI Responses Compact", "OpenAI Responses"]` with `/v1/responses`.
- The post-rebuild runtime smoke is still blocked by copied upstream state on the fixtures: account balance, subscription, availability, and daily-limit errors prevent a fresh full 200-path rerun.

## 2026-06-09 Handoff Revalidation

Sampling time: `2026-06-09 12:27:01 CST`.

This pass resumed the uncommitted work after the previous session handoff and verified the latest dirty worktree against the isolated dev environment.

Runtime boundary:

- `3001` isolated dev was rebuilt from the dirty worktree and reported `version=v1.1.0`, `commit=5f693fa9e...-dirty`, `date=2026-06-09T03:50:22Z`.
- `3001 /api/status` returned successfully.
- The default frontend dev server was served from `http://127.0.0.1:5176` with `VITE_REACT_APP_SERVER_URL=http://127.0.0.1:3001`.
- Production `3000` was not rebuilt and was not used for frontend runtime validation.

Browser validation:

- Channel edit drawer for channel `206` on `5176 -> 3001` showed `Responses upstream profile` and `Responses Compact capability`.
- The same drawer showed the expected compact capability description and the current auto compact value.
- Usage logs were filtered to `2026-05-11 00:00:00 ~ 2026-06-09 23:59:59`, model `gpt-5.5`.
- The log API response contained real rows with `other.reasoning_effort`, including `low` and `high`.
- The usage-log table rendered `推理 low` / `推理 high` badges beside the model name.
- The log detail dialog rendered `推理强度: low` for request `202605310123049292041658268d9d6SFfcW8UI`.

Additional source cleanup:

- Added upstream profile UI text to the dynamic i18n key coverage list.
- Added the upstream profile labels and descriptions to `en`, `zh`, `fr`, `ja`, `ru`, and `vi` locale files.
- Re-ran `bun run i18n:sync`; `_sync-report.json` showed every locale with `missingCount=0`, `extrasCount=0`, and `untranslatedCount=0`.

Latest validation commands:

```bash
go test ./controller ./model ./relay/common ./relay/helper ./service
go test ./relay ./relay/channel/openai ./relay/channel/codex ./middleware
cd web/default && bun test tests/channel-responses-compact.test.ts
cd web/default && bun run i18n:sync
node -e '...' # verifies _sync-report.json has no missing/extras/untranslated entries
cd web/default && bun run lint
cd web/default && bun run typecheck
cd web/default && bun run build
git diff --check
```

Result: all commands exited 0. The frontend build completed and the Safari compatibility rewrite/check passed.

## Production Compact Stopgap And Final Revalidation

Sampling time: `2026-06-06 23:58:28 CST`.

Production compacting was adjusted through a temporary operational stopgap.
Detailed production channel identifiers, fallback timestamps, backup table names, and manual reason strings are intentionally not kept in this repo.
Full evidence belongs in the internal ticket or audit record.

Production compact logs after `2026-06-06 23:18:00 +08`:

| Mode | Upstream path | Log type | Count |
| --- | --- | ---: | ---: |
| `synthetic_summary` | `/v1/responses` | 2 | 3 |
| `synthetic_summary` | `/v1/responses` | 5 | 6 |

The same log window had no matches for these old native failure signals:

- `synthetic compact state scope mismatch`
- `无法解析载荷`
- `malformed compact output`
- `path=/v1/responses/compact`
- `Responses Compact mode=native`

Observed residual failures in that window are synthetic upstream failures, not native compact routing:

- `synthetic compact upstream response has no summary text`
- `upstream error: do request failed`
- `bad response status code 502`

Final local validation after restoring `disabled` compact semantics and adding scope-model validation:

```bash
git diff --check
go test ./controller ./model ./relay/common ./relay/helper ./service
go test ./relay/channel/openai ./relay/channel/codex -count=1
cd web/default && bun run i18n:sync
cd web/default && bun test tests/channel-responses-compact.test.ts
cd web/default && bun run lint
cd web/default && bun run build
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
curl -fsS http://127.0.0.1:3001/api/status
```

Result: all commands exited 0.

Runtime boundary:

- `3001` isolated dev build-info: `version=v1.1.0`, current dirty commit, `date=2026-06-06T15:52:40Z`.
- `new-api-dev-isolated-new-api-1` health: `healthy`.
- `3001 /api/status`: `success=true`, `version=v1.1.0`, `setup=true`.
- Production `new-api` build-info remained on its earlier build; production was not rebuilt.
