# CR-DEV-ISOLATED-FULLCHECK-2026-05-23

## Scope

- Time: 2026-05-23
- Branch: `codex/protocol-conversion-webui`
- Commit: `ac0d7ad16c3ec0a53cacceebcd1ff7c486030484`
- Target: isolated development stack on host port `3001`
- Frontend audit target: `web/default` dev server on host port `5176`, proxied to `3001`

## Runtime Evidence

`docker exec new-api-dev-isolated-new-api-1 /new-api --build-info`:

```text
version=v1.1.0
commit=ac0d7ad16c3ec0a53cacceebcd1ff7c486030484
date=2026-05-23T00:59:20Z
source=https://github.com/MisonL/new-api
```

`curl -fsS http://127.0.0.1:3001/api/status` returned `success=true`.

The runtime status reported `theme=classic`, so `3001` was used for the deployed backend and classic frontend. `web/default` was checked separately through:

```bash
cd web/default
VITE_REACT_APP_SERVER_URL=http://127.0.0.1:3001 bun run dev --host 127.0.0.1 --port 5176
```

## Automated Verification

| Command | Result |
| --- | --- |
| `go test ./... -timeout=60s` | Passed |
| `cd web/default && bun run lint` | Passed |
| `cd web/default && bun run build` | Passed |

The first plain `go test ./...` run produced no output for a long period and was terminated before rerunning with an explicit timeout. The timeout run completed successfully and covered all Go packages.

## Runtime Smoke

The isolated `3001` backend was verified with a temporary fake upstream and temporary database rows for channel, token, and ability routing. Results:

- Unsupported compact route returned HTTP 400 and did not trigger an upstream business request.
- Native compact route returned HTTP 200 and forwarded to `/v1/responses/compact`.
- The upstream capture preserved `previous_response_id` and `input[0].type = "compaction"`.
- Temporary smoke rows were cleaned up after verification.

## Browser And UI Checks

Checked pages:

- `http://127.0.0.1:3001/`
- `http://127.0.0.1:5176/`
- `http://127.0.0.1:5176/pricing`
- `http://127.0.0.1:5176/sign-in`

Screenshots:

- `.dev-docker/runtime/isolated/classic-home-desktop.png`
- `.dev-docker/runtime/isolated/classic-home-mobile.png`
- `.dev-docker/runtime/isolated/default-home-desktop.png`
- `.dev-docker/runtime/isolated/default-home-mobile.png`
- `.dev-docker/runtime/isolated/default-pricing-desktop.png`
- `.dev-docker/runtime/isolated/default-signin-desktop.png`
- `.dev-docker/runtime/isolated/default-signin-mobile.png`

## Findings

1. `web/default` emits many `i18next::translator: missingKey zh-CN translation ...` console logs on public pages. The repeated keys observed were navigation labels such as `主页`, `控制台`, `模型广场`, `排行`, `文档`, and `关于`.
2. `web/default/sign-in` password input triggers a browser issue because the password field lacks an `autocomplete` attribute. Chrome suggested `current-password`.
3. `web/default` dev server renders TanStack Query and Router Devtools floating controls. These controls overlay the page during visual inspection, especially on mobile. This was treated as dev-server audit noise, not deployed product behavior.
4. `3001` classic mobile header keeps all top navigation items in one row, causing truncation such as `模型广...` on a 390px viewport.
5. `3001` classic home page accessibility tree exposes the API endpoint selector and readonly base URL input with a combined accessible name like `/v1/chat/completions/v1/responses/v1/res`. The visual rendering is usable, but the accessible naming is noisy.

## Conclusion

The isolated development deployment is running the expected commit and passed backend, frontend lint, and frontend build verification. Core compact routing smoke passed against the real `3001` runtime with temporary data and a fake upstream.

The remaining issues are frontend quality items: `web/default` i18n console noise, login password autocomplete, dev-tool overlay during local visual audits, classic mobile navigation truncation, and classic API endpoint accessible naming.

## Follow-up Fix Verification

After the first review, the frontend quality issues were addressed and redeployed to the isolated `3001` stack.

Changed areas:

- `web/default/src/components/layout/components/public-header.tsx`: stopped translating already-translated public navigation titles a second time.
- `web/default/src/features/auth/sign-in/components/user-auth-form.tsx`: added `autocomplete="username"` and `autocomplete="current-password"`.
- `web/classic/src/components/layout/headerbar/Navigation.jsx`: replaced the cramped mobile public navigation row with a menu button.
- `web/classic/src/pages/Home/index.jsx`: gave the API base URL input a stable accessible name and marked the rotating endpoint text as decorative.

Verification:

| Command or check | Result |
| --- | --- |
| `cd web/default && bun run lint` | Passed |
| `cd web/default && bun run build` | Passed |
| `cd web/classic && bun run eslint` | Passed |
| `cd web/classic && bun run build` | Passed |
| `scripts/build-docker-local.sh new-api-local:dev` | Passed |
| `docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api` | Passed |
| `docker exec new-api-dev-isolated-new-api-1 /new-api --build-info` | `ac0d7ad16c3ec0a53cacceebcd1ff7c486030484-dirty` |
| `curl -fsS http://127.0.0.1:3001/api/status` | Passed |

Browser recheck:

- `http://127.0.0.1:3001/` at 390px mobile now exposes a compact `打开导航菜单` button and a menu containing 首页、控制台、模型广场、文档、关于.
- `http://127.0.0.1:3001/` now exposes the readonly base URL textbox as `API 基址`; the copy button is exposed as `复制 API 基址`.
- `http://127.0.0.1:5176/` no longer emits `i18next::translator: missingKey` logs for public navigation labels. The only observed console message was the dev-server WebSocket connection.
- `http://127.0.0.1:5176/sign-in` no longer emits the Chrome autocomplete issue. The username input has `autocomplete="username"` and the password input has `autocomplete="current-password"`.

New screenshot:

- `.dev-docker/runtime/isolated/classic-home-mobile-fixed.png`

## Second Full Redeploy And Recheck

Time: 2026-05-23 10:14-10:24 Asia/Shanghai.

Current repository state:

- Branch: `codex/protocol-conversion-webui`
- `HEAD`: `ac0d7ad16c3ec0a53cacceebcd1ff7c486030484`
- `origin/main`: `13bba9b91f17a3f5df9014e08cc98067255f124f`
- Dirty runtime build expected because local frontend fixes and this report were not committed.

Target disambiguation:

- Isolated dev: `new-api-dev-isolated-new-api-1`, image `new-api-local:dev`, host port `3001`
- Production: `new-api`, image `new-api-local:prod-main`, host port `3000`
- Only the isolated dev `new-api` service was recreated.

Automated verification:

| Command or check | Result |
| --- | --- |
| `go test ./...` | Passed |
| `go test ./controller ./model ./relay/common ./relay/helper ./service` | Passed |
| `cd web/default && bun run lint` | Passed |
| `cd web/default && bun run build` | Passed |
| `cd web/classic && bun run eslint` | Passed |
| `cd web/classic && bun run build` | Passed |
| `scripts/build-docker-local.sh new-api-local:dev` | Passed |
| `docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api` | Passed |

Runtime evidence after redeploy:

```text
version=v1.1.0
commit=ac0d7ad16c3ec0a53cacceebcd1ff7c486030484-dirty
date=2026-05-23T02:12:43Z
source=https://github.com/MisonL/new-api
```

Health and API probes:

| Probe | Result |
| --- | --- |
| Container health | `healthy` |
| `GET /api/status` | HTTP 200, `success=true`, `theme=classic` |
| `GET /api/models` | HTTP 401 |
| `GET /api/user/self` | HTTP 401 |
| `GET /v1/models` | HTTP 401 |
| `POST /v1/chat/completions` without token | HTTP 401 |
| `GET /pricing` | HTTP 200 |
| `GET /about` | HTTP 200 |
| `GET /console` | HTTP 200 shell, unauthenticated frontend redirects at app layer |

Container log review:

- Startup showed PostgreSQL, migrations, Redis, i18n, memory cache, channel sync, dashboard task, and batch update task starting normally.
- No panic, fatal error, HTTP 5xx, or unexpected warning was observed in the redeploy window.
- Two `Invalid token` log entries were caused by deliberate unauthenticated API probes and matched the HTTP 401 checks.

Browser and UI/UX recheck:

- `3001` classic desktop home: no horizontal overflow at 1440px, public navigation and CTA layout remained aligned.
- `3001` classic mobile home: 390px viewport uses `打开导航菜单`; menu expands to 首页、控制台、模型广场、文档、关于 without truncation.
- `3001` classic home accessibility: readonly base URL textbox is exposed as `API 基址`; copy button is exposed as `复制 API 基址`.
- `3001` classic login: username input has `autocomplete="username"` and password input has `autocomplete="current-password"`.
- `web/default` was checked through a dev server proxied to `3001`. Requested port `5176` was occupied, so Rsbuild used `http://127.0.0.1:5177/`.
- `web/default` desktop and mobile home: no horizontal overflow observed; hierarchy, CTA grouping, cards, and responsive stacking were visually coherent.
- `web/default/sign-in`: no horizontal overflow; username/password autocomplete values are present.
- `web/default/dashboard` while unauthenticated redirects to `/sign-in?redirect=%2Fdashboard`.
- Browser console: classic deployed pages had no console messages after reload; default dev pages only showed the Rsbuild WebSocket info message. No repeated `i18next missingKey` logs or Chrome autocomplete issue appeared.
- Default dev screenshots still show TanStack devtools floating controls. This is local dev-server audit noise, not deployed `3001` product behavior.

Screenshot artifacts:

- `docs/reviews/artifacts/dev-3001-classic-desktop-home-2026-05-23.png`
- `docs/reviews/artifacts/dev-3001-classic-mobile-home-2026-05-23.png`
- `docs/reviews/artifacts/dev-3001-classic-mobile-menu-2026-05-23.png`
- `docs/reviews/artifacts/dev-default-desktop-home-2026-05-23.png`
- `docs/reviews/artifacts/dev-default-mobile-home-2026-05-23.png`

Conclusion:

The isolated development environment on `3001` was rebuilt from the current dirty worktree and is running the expected dirty build. Backend package tests, full Go tests, both frontend lint/build paths, Docker build, container health, HTTP probes, and browser UI/UX checks passed. No new blocking backend, runtime, visual, accessibility, or console issue was found in this recheck.

## Review Fix Follow-up

Time: 2026-05-23 11:46 Asia/Shanghai.

Review finding:

- The new classic accessibility labels `API 基址`, `复制 API 基址`, and `打开导航菜单` were used through `t(...)` but were missing from classic locale files.

Fix:

- Added those three keys to all classic locales: `en`, `fr`, `ja`, `ru`, `vi`, `zh`, `zh-CN`, and `zh-TW`.

Verification:

| Command or check | Result |
| --- | --- |
| Parse every `web/classic/src/i18n/locales/*.json` with `JSON.parse` | Passed |
| Confirm all three keys exist in every classic locale JSON file | Passed |
| `cd web/classic && bun run eslint` | Passed |
| `cd web/classic && bun run build` | Passed |
| `scripts/build-docker-local.sh new-api-local:dev` | Passed |
| Recreate `new-api-dev-isolated-new-api-1` | Passed |
| Container health | `healthy` |
| `docker exec new-api-dev-isolated-new-api-1 /new-api --build-info` | `ac0d7ad16c3ec0a53cacceebcd1ff7c486030484-dirty`, built `2026-05-23T03:46:12Z` |
| `GET /api/status` | HTTP 200 |

Browser recheck:

- Forced `i18nextLng=en` on `http://127.0.0.1:3001/` at 390px mobile.
- Navigation menu button was exposed as `Open navigation menu`.
- Base URL textbox was exposed as `API Base URL`.
- Copy button was exposed as `Copy API Base URL`.
- Expanded menu items were exposed in English: Home, Console, Model Marketplace, Documentation, About.
- Browser console only showed the existing `WE LOVE NEWAPI` log; no missing i18n key log was observed.

## Third Full Redeploy And Recheck

Time: 2026-05-23 13:28-13:35 Asia/Shanghai.

Current repository state:

- Branch: `codex/protocol-conversion-webui`
- `HEAD`: `ac0d7ad16c3ec0a53cacceebcd1ff7c486030484`
- `origin/main`: `13bba9b91f17a3f5df9014e08cc98067255f124f`
- Dirty runtime build expected because local frontend fixes, classic locale additions, and this report were not committed.

Target disambiguation:

- Isolated dev: `new-api-dev-isolated-new-api-1`, image `new-api-local:dev`, host port `3001`
- Production: `new-api`, image `new-api-local:prod-main`, host port `3000`
- Only the isolated dev `new-api` service was recreated.

Automated verification:

| Command or check | Result |
| --- | --- |
| Parse every `web/classic/src/i18n/locales/*.json` with `JSON.parse` | Passed |
| Confirm `API 基址`, `复制 API 基址`, and `打开导航菜单` exist in every classic locale JSON file | Passed |
| `go test ./...` | Passed |
| `cd web/default && bun run lint` | Passed |
| `cd web/default && bun run build` | Passed |
| `cd web/classic && bun run eslint` | Passed |
| `cd web/classic && bun run build` | Passed |
| `scripts/build-docker-local.sh new-api-local:dev` | Passed |
| `docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api` | Passed |

Runtime evidence after redeploy:

```text
version=v1.1.0
commit=ac0d7ad16c3ec0a53cacceebcd1ff7c486030484-dirty
date=2026-05-23T05:28:33Z
source=https://github.com/MisonL/new-api
```

Runtime and API probes:

| Probe | Result |
| --- | --- |
| Container health | `healthy` |
| `GET /api/status` | HTTP 200, `success=true`, `theme=classic` |
| `GET /api/models` | HTTP 401 |
| `GET /api/user/self` | HTTP 401 |
| `GET /v1/models` | HTTP 401 |
| `POST /v1/chat/completions` without token | HTTP 401 |
| `GET /pricing` | HTTP 200 |
| `GET /about` | HTTP 200 |
| `GET /console` | HTTP 200 shell |
| `GET /login` | HTTP 200 shell |

Container log review:

- Startup showed PostgreSQL, migrations, Redis, i18n, memory cache, channel sync, dashboard task, and batch update task starting normally.
- No panic, fatal error, HTTP 5xx, unexpected warning, or missing i18n key log was observed.
- Two `Invalid token` log entries were caused by deliberate unauthenticated API probes and matched the HTTP 401 checks.

Browser and UI/UX recheck:

- `3001` classic mobile home at 390px: navigation menu button exposed as `打开导航菜单`; expanded menu contains 首页、控制台、模型广场、文档、关于 without truncation.
- `3001` classic home accessibility: base URL textbox exposed as `API 基址`; copy button exposed as `复制 API 基址`.
- `3001` classic login: username input has `autocomplete="username"` and password input has `autocomplete="current-password"`.
- Classic browser console only showed the existing `WE LOVE NEWAPI` log.
- `web/default` dev server was started with `VITE_REACT_APP_SERVER_URL=http://127.0.0.1:3001` on `http://127.0.0.1:5176/`.
- `web/default` desktop home at 1440px and mobile home at 390px: no horizontal overflow; hierarchy, CTA grouping, cards, and responsive stacking remained coherent.
- `web/default/sign-in`: no horizontal overflow; username/password autocomplete values are present.
- `web/default/dashboard` while unauthenticated redirects to `/sign-in?redirect=%2Fdashboard`.
- Default browser console showed only development server and i18next initialization messages; no `missingKey` log appeared.

Screenshot artifacts:

- `docs/reviews/artifacts/dev-3001-classic-mobile-recheck-2026-05-23-1331.png`
- `docs/reviews/artifacts/dev-default-desktop-recheck-2026-05-23-1332.png`
- `docs/reviews/artifacts/dev-default-mobile-recheck-2026-05-23-1332.png`

Conclusion:

The isolated development environment on `3001` was rebuilt again from the current dirty worktree and is running the expected build dated `2026-05-23T05:28:33Z`. Backend tests, frontend lint/build, locale checks, Docker build, runtime health, HTTP/API probes, logs, and browser UI/UX checks all passed. No new blocking issue was found.
