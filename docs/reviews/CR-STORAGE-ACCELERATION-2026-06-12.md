# CR-STORAGE-ACCELERATION-2026-06-12

## Scope

Implement a generic storage acceleration and safe PostgreSQL migration path for
Docker deployments of new-api.

The reviewed design intentionally uses explicit storage migration instead of
transparent caching. It keeps host paths out of the Web UI and fails fast when
marker validation fails.

## Requirements mapping

| Requirement | Evidence |
| --- | --- |
| P0 diagnostic tool | `scripts/storage-probe.sh` |
| Sequential write, 8K random write, and fsync latency probes | `scripts/storage-probe.sh` |
| Optional PostgreSQL container `pg_test_fsync` probe | `scripts/storage-probe.sh` |
| macOS, Windows/WSL2, Linux checks | `scripts/storage-probe.sh`, `scripts/storage-plan-linux.sh`, `scripts/storage-plan-windows.ps1`, `docs/operations/storage-acceleration.md` |
| P1 optional compose overlay | `deploy/compose/docker-compose.storage-postgres-bind.yml` |
| Explicit bind mount variables | `deploy/env/storage-postgres.example.env` |
| Marker validation without fallback | `deploy/compose/docker-compose.storage-postgres-bind.yml` |
| P2 dry-run first migration workflow | `scripts/storage-migrate-postgres.sh` |
| `pg_dump` and `pg_restore` default path | `scripts/storage-migrate-postgres.sh` |
| Table count, latest timestamp, and marker verification | `scripts/storage-verify-postgres.sh` |
| Status, login page, and protected log list verification | `scripts/storage-verify-postgres.sh` |
| Old storage retained | `docs/operations/storage-acceleration.md` |
| Advanced `pg_wal` and Linux cache warnings | `docs/operations/storage-acceleration.md` |
| Observability guidance | `docs/operations/storage-acceleration.md` |

## Safety decisions

- The standard path is full `PGDATA` migration to a bind mount.
- The overlay requires `NEW_API_POSTGRES_DATA_DIR`,
  `NEW_API_POSTGRES_MARKER`, and `NEW_API_POSTGRES_MARKER_VALUE`.
- The marker file is mounted separately from `PGDATA`, so a new empty
  PostgreSQL directory can still initialize safely.
- The marker content must match exactly. Missing or mismatched marker exits the
  PostgreSQL container before startup.
- The overlay and migration script reject non-absolute marker and PGDATA paths,
  plus dot segments such as `/./`, `/../`, `/.`, or `/..`, before checking
  whether the marker is inside PGDATA.
- The migration script defaults to `--dry-run`.
- Execution mode requires `CONFIRM_STORAGE_MIGRATION` to exactly match the
  confirmation value printed by dry-run for the same container, database, and
  target path. A bare `--execute` fails before acquiring the lock or stopping
  any container.
- The final dump is created after `new-api` is stopped, so writes are blocked
  before the restorable snapshot is taken.
- The final dump is streamed directly from `pg_dump` to the host backup path.
  It does not create a large intermediate dump inside the PostgreSQL container
  writable layer.
- A matching `pg_dumpall --globals-only --no-role-passwords` file is written to
  the host backup path for operator review of roles and global privileges.
- On Windows shells, custom-format `pg_dump` is base64-wrapped before writing
  to the host backup file to avoid binary stdout newline conversion.
- The streamed dump is written to a `.incomplete` file first, validated with
  `pg_restore -l`, and only then renamed to the final dump path. Failed dump or
  validation attempts remove the incomplete file.
- Execution mode validates `ENV_FILE`, target PostgreSQL major version, and
  backup-directory free space before services are stopped.
- The restore path recreates the database with the original
  encoding/collation/owner/locale provider/ICU locale and runs
  `pg_restore --exit-on-error`.
- Restore streams the host dump directly into `pg_restore`; it no longer copies
  the dump into the PostgreSQL container writable layer.
- PostgreSQL versions without `pg_database.datlocprovider` are treated as
  libc locale-provider databases, preserving compatibility with older source
  clusters while still retaining locale-provider metadata on PostgreSQL 15+.
- PostgreSQL targets older than major version 15 do not receive
  `createdb --locale-provider` flags. Target major version must be greater than
  or equal to the source major version.
- After compose recreation, the migration checks whether PostgreSQL stayed
  running and prints recent PostgreSQL logs immediately if marker or permission
  checks made the container exit.
- If compose replacement has started and then fails, the script no longer
  assumes the old PostgreSQL container still exists. It reports an explicit
  manual recovery boundary and leaves `new-api` stopped until the operator
  restores PostgreSQL.
- Execution mode refuses a non-empty target data directory unless
  `ALLOW_NON_EMPTY_TARGET=true` is set after manual review.
- Windows drive paths such as `C:/new-api/postgres/data` are accepted only from
  Windows shells. macOS, Linux, and WSL2 runs must use Unix absolute paths.
- The verifier checks the marker mounted at
  `/run/new-api/postgres-storage.marker` when marker environment variables are
  provided together. Without those variables, the marker check is explicitly
  skipped rather than pretending the overlay was validated. Partial marker env
  configuration fails the check.
- The migration script passes source `users`, `channels`, `tokens`, and `logs`
  counts into `scripts/storage-verify-postgres.sh`; the verifier performs exact
  count comparison only when these expected-count variables are provided.
- The old volume or old directory is not automatically deleted.

## Known boundaries

- The scripts target PostgreSQL deployments. SQLite and MySQL deployments need
  separate storage procedures.
- The standard migration uses `pg_dump` and `pg_restore`. Physical PGDATA copy
  is intentionally not automated because WAL and recovery semantics require a
  narrower manual plan.
- `pg_wal` separation is documented as advanced only.
- Docker data-root migration is documented as a host-level expert operation,
  not a new-api runtime setting.
- `NEW_API_POSTGRES_WAL_DIR` and `NEW_API_DOCKER_DATA_DIR` are planning
  variables only. They are not consumed by the standard compose overlay.
- Linux and Windows platform planning scripts are read-only. They report risks
  but do not change mount options, Defender settings, Docker data-root, or
  PostgreSQL layout.
- The migration source is expected to be an initialized new-api PostgreSQL
  database. Preflight count queries intentionally fail if the required tables
  are missing.
- Production migration must not run concurrently with another Docker rebuild or
  compose operation. `scripts/storage-migrate-postgres.sh --execute` enforces a
  host-local directory lock and checks for Docker build or compose processes
  before it stops services.

## Verification log

Commands run:

```bash
bash -n scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh
bash -n scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh scripts/storage-plan-linux.sh
shellcheck scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh scripts/storage-plan-linux.sh
pwsh -NoProfile -Command '$null = [System.Management.Automation.PSParser]::Tokenize((Get-Content -Raw scripts/storage-plan-windows.ps1), [ref]$null); "powershell-parse-ok"'
docker compose -f docker-compose.yml -f deploy/compose/docker-compose.storage-postgres-bind.yml --env-file <temporary-env> config
RUN_WRITE_TESTS=false scripts/storage-probe.sh
RUN_WRITE_TESTS=true PROBE_DIR=/tmp scripts/storage-probe.sh
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev RUN_WRITE_TESTS=false RUN_POSTGRES_FSYNC_TESTS=true PG_TEST_FSYNC_SECONDS=1 scripts/storage-probe.sh
scripts/storage-plan-linux.sh
pwsh -NoProfile -File scripts/storage-plan-windows.ps1
RUN_WRITE_TESTS=true PROBE_DIR=/tmp/new-api-probe-missing-dir scripts/storage-probe.sh
POSTGRES_CONTAINER=<postgres-container> NEW_API_CONTAINER=<new-api-container> POSTGRES_DB=<postgres-db> NEW_API_POSTGRES_DATA_DIR=<temporary-data> NEW_API_POSTGRES_MARKER=<temporary-marker> NEW_API_POSTGRES_MARKER_VALUE=dry-marker scripts/storage-migrate-postgres.sh --dry-run
POSTGRES_CONTAINER=<postgres-container> NEW_API_CONTAINER=<new-api-container> POSTGRES_DB=<postgres-db> NEW_API_POSTGRES_DATA_DIR=C:/new-api/postgres/data NEW_API_POSTGRES_MARKER=C:/new-api/postgres/storage.marker NEW_API_POSTGRES_MARKER_VALUE=marker scripts/storage-migrate-postgres.sh --dry-run
POSTGRES_CONTAINER=<postgres-container> NEW_API_CONTAINER=<new-api-container> POSTGRES_DB=<postgres-db> NEW_API_POSTGRES_DATA_DIR=<temporary-data> NEW_API_POSTGRES_MARKER=<marker-with-newline> NEW_API_POSTGRES_MARKER_VALUE=dry-marker scripts/storage-migrate-postgres.sh --dry-run
POSTGRES_CONTAINER=<postgres-container> NEW_API_CONTAINER=<new-api-container> POSTGRES_DB=<postgres-db> NEW_API_POSTGRES_DATA_DIR=<path-with-trailing-dot-segment> NEW_API_POSTGRES_MARKER=<temporary-marker> NEW_API_POSTGRES_MARKER_VALUE=marker scripts/storage-migrate-postgres.sh --dry-run
POSTGRES_CONTAINER=<postgres-container> NEW_API_CONTAINER=<new-api-container> POSTGRES_DB=<postgres-db> NEW_API_POSTGRES_DATA_DIR=<temporary-data-with-trailing-slash> NEW_API_POSTGRES_MARKER=<marker-inside-data-dir> NEW_API_POSTGRES_MARKER_VALUE=marker scripts/storage-migrate-postgres.sh --dry-run
POSTGRES_CONTAINER=<postgres-container> NEW_API_CONTAINER=<new-api-container> POSTGRES_DB=<postgres-db> SKIP_DOCKER_ACTIVITY_CHECK=true MIGRATION_LOCK_DIR=<temporary-lock> NEW_API_POSTGRES_DATA_DIR=<temporary-data> NEW_API_POSTGRES_MARKER=<missing-marker> NEW_API_POSTGRES_MARKER_VALUE=missing scripts/storage-migrate-postgres.sh --execute
POSTGRES_CONTAINER=<postgres-container> NEW_API_CONTAINER=<new-api-container> POSTGRES_DB=<postgres-db> MIGRATION_LOCK_DIR=<temporary-lock> NEW_API_POSTGRES_DATA_DIR=<temporary-data> NEW_API_POSTGRES_MARKER=<temporary-marker> NEW_API_POSTGRES_MARKER_VALUE=dry-marker scripts/storage-migrate-postgres.sh --execute
NEW_API_STATUS_URL=not-a-url/api/status scripts/storage-verify-postgres.sh
scripts/storage-verify-postgres.sh
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev NEW_API_STATUS_URL=http://127.0.0.1:3001/api/status scripts/storage-verify-postgres.sh
LC_ALL=C rg -n '[^[:ascii:]]' scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh scripts/storage-plan-linux.sh scripts/storage-plan-windows.ps1 deploy/compose/docker-compose.storage-postgres-bind.yml deploy/env/storage-postgres.example.env docs/operations/storage-acceleration.md docs/reviews/CR-STORAGE-ACCELERATION-2026-06-12.md
claude -p --permission-mode plan --tools ''
gemini -p '<read-only review prompt>' --approval-mode plan --output-format text
coderabbit review --agent --plain
git diff --check
```

Results:

- Shell syntax check passed.
- `shellcheck` passed for the Bash scripts.
- PowerShell parser check passed for `scripts/storage-plan-windows.ps1`.
- Compose config passed with a temporary env file containing explicit marker and
  data paths.
- Storage probe completed against the local Docker environment and reported the
  production PostgreSQL bind mount at `/Volumes/Work/code/new-api/postgres-data`.
- The PostgreSQL `pg_test_fsync` probe path was validated only against the
  isolated development PostgreSQL container, not production PGDATA. The probe
  now creates a temporary directory inside `PGDATA` and removes it on exit.
- `scripts/storage-plan-linux.sh` completed in read-only mode and reported the
  current Docker Desktop data-root, PostgreSQL UID/GID, and unset candidate
  storage variables.
- `scripts/storage-plan-windows.ps1` completed in read-only mode under
  PowerShell and reported Docker data-root, WSL availability, unset candidate
  variables, and Windows-specific recommendations.
- Dry-run completed without stopping containers or copying dump files.
  It now prints target directory checks, marker checks, available space, current
  PGDATA size, current mounts, table counts, original database
  encoding/collation/owner, target version plan, backup-space plan, globals
  dump plan, and planned operations.
- Dry-run rejects marker content with a trailing newline because marker
  comparison is byte-for-byte.
- Dry-run rejects non-normalized data or marker paths containing dot segments
  such as `/./`, `/../`, `/.`, or `/..`.
- The migration script trims trailing slashes before marker-inside-PGDATA
  checks, so `/data/` and `/data` are treated as the same target boundary.
- On macOS, a dry-run using `C:/new-api/postgres/data` was rejected before
  Docker inspection, preventing accidental relative `C:` directories on
  non-Windows hosts.
- The storage overlay reports marker bind-mount directories with exit code
  `69`, making the Docker-created-directory case explicit.
- Execution mode rejects a missing marker before `docker stop`, `pg_dump`, or
  compose recreation, and the host-local migration lock is removed on failure.
- Execution mode rejects a missing `CONFIRM_STORAGE_MIGRATION` before acquiring
  the lock, stopping containers, running `pg_dump`, or compose recreation.
- The verifier rejects invalid `NEW_API_BASE_URL` or `NEW_API_STATUS_URL`
  values before running HTTP checks.
- The verifier explicitly reports `postgres-storage-marker` as skipped when
  marker env values are not provided. When marker env values are provided it
  requires all three marker variables and an exact byte-for-byte match inside
  the PostgreSQL container.
- The verifier strips carriage returns from `psql` output, which keeps numeric
  comparisons stable when invoked from Windows shells.
- ASCII scan passed for the storage scripts, compose overlay, env example, and
  storage operation/review documents.
- Production read-only verification passed with `users=4`, `channels=77`,
  `tokens=8`, and `logs=855023` at the time sampled. The protected log list
  check is skipped unless a real authenticated cookie and `New-Api-User` value
  are provided.
- A non-destructive restore command check was run against `3001` by dumping
  `new-api-dev`, creating a temporary database with the original
  encoding/collation/owner, restoring with `pg_restore --exit-on-error`,
  checking `users=8` and `logs=411406`, then dropping the temporary database.
- `3001` isolated dev migration rehearsal was executed with the storage overlay.
  The final scripted run restored `users=8`, `channels=99`, `tokens=15`, and
  `logs=411406`, then `/api/status` returned success.
- After the rehearsal, `3001` was restored to its original
  `deploy/env/dev-isolated.env` PostgreSQL data directory and verified healthy.

Issue found during rehearsal:

- The first implementation waited on local socket `pg_isready`, which can
  connect to the PostgreSQL official image temporary init server. This raced
  with `POSTGRES_DB` creation. The script now waits on TCP
  `pg_isready -h 127.0.0.1` before restore.
- A follow-up review found that `scripts/storage-verify-postgres.sh` could
  print success labels from some checks when `psql` failed inside a function
  invoked through `run_check`. The verifier now explicitly returns failure for
  failed `psql_query` calls. This was validated by intentionally running the
  verifier with a wrong PostgreSQL role and confirming a non-zero exit plus
  `check_failed` output.

External review follow-up:

- Gemini flagged the original container-local `/tmp` dump as a production disk
  risk. The migration script now streams `pg_dump` directly to the host backup
  path and validates the incomplete dump before renaming it.
- Gemini flagged trailing-slash path bypass risk. The migration script now
  trims trailing slashes before marker-inside-PGDATA checks.
- Gemini flagged inconsistent marker examples. The env example now uses the
  same `new-api-prod-postgres-fast-storage-20260612` value as the operation
  document.
- Claude flagged compose replacement failure recovery. The script now treats
  post-replacement-start failures as manual recovery boundaries instead of
  assuming the old PostgreSQL container can be restarted.
- Claude flagged incomplete dump cleanup. The script removes `.incomplete`
  dumps on dump or validation failure.
- Claude flagged database locale-provider preservation. The script now captures
  and passes `datlocprovider` and `daticulocale` to `createdb`.
- CodeRabbit flagged verifier URL validation and cleanup consistency. The
  verifier now validates HTTP/HTTPS base URLs and centralizes temporary file
  cleanup through its exit trap.
- CodeRabbit flagged implicit `URLS_READY` state and redundant curl checks. The
  verifier now sets URL readiness directly from `init_urls` and keeps curl
  availability as a single preflight.
- CodeRabbit flagged that target data directory creation should be explicit in
  the env example. The example now tells operators to create the directory and
  use the migration script to populate it.
- CodeRabbit flagged container temp dump cleanup. The restore path now removes
  the need for `/tmp/new-api-storage-migration.dump` entirely by streaming the
  host dump directly into `pg_restore`.
- CodeRabbit flagged late `docker compose` failures. The migration script now
  checks `docker compose version` during tool preflight.
- CodeRabbit suggested changing the default PostgreSQL user to `postgres`; this
  was not applied because this repository's compose configuration uses `root`
  for the production and isolated-development PostgreSQL user.
- Gemini flagged PostgreSQL 14 compatibility for `datlocprovider`; the script
  now detects whether the column exists and falls back to libc provider metadata

## Development validation 2026-06-13

Sample time: `2026-06-13T11:30:52Z`.

Commands run:

```bash
bash -n scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh scripts/storage-plan-linux.sh
shellcheck scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh scripts/storage-plan-linux.sh
pwsh -NoProfile -Command '$errors=$null; $null = [System.Management.Automation.PSParser]::Tokenize((Get-Content -Raw scripts/storage-plan-windows.ps1), [ref]$errors); if ($errors) { $errors | Format-List *; exit 1 }; "powershell-parse-ok"'
git diff --check -- .gitignore scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh scripts/storage-plan-linux.sh scripts/storage-plan-windows.ps1 deploy/compose/docker-compose.storage-postgres-bind.yml deploy/env/storage-postgres.example.env docs/operations/storage-acceleration.md docs/reviews/CR-STORAGE-ACCELERATION-2026-06-12.md
docker compose -f deploy/compose/dev-isolated.yml -f deploy/compose/docker-compose.storage-postgres-bind.yml --env-file <temporary-env> config
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev scripts/storage-verify-postgres.sh
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev RUN_WRITE_TESTS=false RUN_POSTGRES_FSYNC_TESTS=true PG_TEST_FSYNC_SECONDS=1 scripts/storage-probe.sh
scripts/storage-plan-linux.sh
pwsh -NoProfile -File scripts/storage-plan-windows.ps1
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 NEW_API_CONTAINER=new-api-dev-isolated-new-api-1 POSTGRES_DB=new-api-dev POSTGRES_USER=root POSTGRES_IMAGE=postgres:15 NEW_API_POSTGRES_DATA_DIR=<temporary-target> NEW_API_POSTGRES_MARKER=<temporary-marker> NEW_API_POSTGRES_MARKER_VALUE=marker-ok BACKUP_DIR=<temporary-backups> scripts/storage-migrate-postgres.sh --dry-run
CONFIRM_STORAGE_MIGRATION=execute:<dev-postgres>:<dev-new-api>:new-api-dev:<temporary-target> POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 NEW_API_CONTAINER=new-api-dev-isolated-new-api-1 POSTGRES_DB=new-api-dev POSTGRES_USER=root POSTGRES_IMAGE=postgres:15 NEW_API_POSTGRES_DATA_DIR=<temporary-target> NEW_API_POSTGRES_MARKER=<temporary-marker> NEW_API_POSTGRES_MARKER_VALUE=marker-ok ENV_FILE=<missing-env> BACKUP_DIR=<temporary-backups> scripts/storage-migrate-postgres.sh --execute
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev NEW_API_POSTGRES_MARKER_VALUE=marker-ok scripts/storage-verify-postgres.sh
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev NEW_API_EXPECT_USERS_COUNT=999999 scripts/storage-verify-postgres.sh
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev NEW_API_EXPECT_USERS_COUNT=8 NEW_API_EXPECT_CHANNELS_COUNT=99 NEW_API_EXPECT_TOKENS_COUNT=15 scripts/storage-verify-postgres.sh
COMPOSE_PROJECT_NAME=new-api-storage-migrate-test COMPOSE_FILES=<temporary-compose>:deploy/compose/docker-compose.storage-postgres-bind.yml ENV_FILE=<temporary-env> POSTGRES_CONTAINER=new-api-storage-migrate-test-postgres NEW_API_CONTAINER=new-api-storage-migrate-test-new-api POSTGRES_DB=new-api-dev POSTGRES_USER=root POSTGRES_IMAGE=postgres:15 NEW_API_POSTGRES_DATA_DIR=<temporary-target> NEW_API_POSTGRES_MARKER=<temporary-marker> NEW_API_POSTGRES_MARKER_VALUE=marker-ok BACKUP_DIR=<temporary-backups> MIGRATION_LOCK_DIR=<temporary-lock> NEW_API_STATUS_URL=http://127.0.0.1:31234/api/status NEW_API_LOGIN_URL=http://127.0.0.1:31234/login CONFIRM_STORAGE_MIGRATION=execute:new-api-storage-migrate-test-postgres:new-api-storage-migrate-test-new-api:new-api-dev:<temporary-target> scripts/storage-migrate-postgres.sh --execute
COMPOSE_PROJECT_NAME=new-api-storage-migrate-test COMPOSE_FILES=<temporary-compose>:deploy/compose/docker-compose.storage-postgres-bind.yml ENV_FILE=<temporary-env> POSTGRES_CONTAINER=new-api-storage-migrate-test-postgres NEW_API_CONTAINER=new-api-storage-migrate-test-new-api POSTGRES_DB=new-api-dev POSTGRES_USER=root POSTGRES_IMAGE=postgres:15 NEW_API_POSTGRES_DATA_DIR=<temporary-target> NEW_API_POSTGRES_MARKER=<temporary-marker> NEW_API_POSTGRES_MARKER_VALUE=marker-ok BACKUP_DIR=<temporary-backups> MIGRATION_LOCK_DIR=<temporary-lock> NEW_API_STATUS_URL=http://127.0.0.1:31234/api/status NEW_API_LOGIN_URL=http://127.0.0.1:31234/login CONFIRM_STORAGE_MIGRATION=execute:new-api-storage-migrate-test-postgres:new-api-storage-migrate-test-new-api:new-api-dev:<temporary-target> SKIP_DOCKER_ACTIVITY_CHECK=true scripts/storage-migrate-postgres.sh --execute
go test ./controller ./model ./relay/common ./relay/helper ./service ./relay/channel -count=1 -timeout=120s
cd web/default && bun run lint
cd web/default && bun run typecheck
cd web/default && bun test src/features/channels/lib/channel-form.test.ts
cd web/default && bun run build
cd web/classic && bun run lint
cd web/classic && bun test src/components/table/channels/modals/headerProfile.helpers.test.js
cd web/classic && bun run build
curl -fsS http://127.0.0.1:3001/api/status
curl -fsS http://127.0.0.1:3000/api/status
```

Results:

- Bash syntax, `shellcheck`, PowerShell parser, and scoped `git diff --check`
  passed.
- Storage compose overlay config passed with a temporary env containing explicit
  development bind mount and marker paths.
- Isolated development PostgreSQL verification passed with
  `users=8`, `channels=99`, `tokens=15`, and `logs=411416`.
- `pg_test_fsync` completed against `new-api-dev-isolated-postgres-1`; the
  temporary `pg_test_fsync.*` directory was removed after the run.
- Linux and Windows planning scripts completed in read-only mode.
- Dry-run migration passed when the marker file was written without a trailing
  newline. A marker file containing a trailing newline was separately rejected
  by the exact byte comparison.
- `--execute` rejected a missing `ENV_FILE` before stopping any service.
- Marker partial-env verification failed as expected.
- Expected-count mismatch failed as expected; matching expected counts passed.
- The first temporary full `--execute` attempt failed before stopping services
  because the concurrent Docker activity guard detected a Compose process.
- A second temporary full `--execute` run used
  `SKIP_DOCKER_ACTIVITY_CHECK=true` only after the guard behavior was validated.
  It stopped a temporary HTTP fixture, dumped a temporary PostgreSQL database,
  recreated PostgreSQL with the storage overlay, restored via streaming
  `pg_restore`, restarted the fixture, and post-verified marker, HTTP status,
  and exact table counts. Restored counts were `users=2`, `channels=2`,
  `tokens=1`, and `logs=3`.
- Temporary `new-api-storage-migrate-test-*` containers and temporary test
  networks were cleaned up after the execute validation.
- Go tests passed for `./controller`, `./model`, `./relay/common`,
  `./relay/helper`, `./service`, and `./relay/channel`.
- `web/default` passed ESLint, TypeScript build, targeted `channel-form` tests,
  production build, and Safari compatibility checks.
- `web/classic` passed Prettier check, targeted header profile tests
  (`90 pass`), production build, and Safari compatibility checks.
- Development `http://127.0.0.1:3001/api/status` and production
  `http://127.0.0.1:3000/api/status` returned successfully after validation.

## Development cache path validation 2026-06-13

Sample time: `2026-06-13T12:11:45Z`.

Target path:

```text
NEW_API_POSTGRES_DATA_DIR=/Volumes/Data/new-api-caches/postgres/data
NEW_API_POSTGRES_MARKER=/Volumes/Data/new-api-caches/postgres/storage.marker
NEW_API_POSTGRES_MARKER_VALUE=new-api-dev-postgres-data-cache-20260613
```

Commands and observations:

```bash
node <http-load-script> # 1000 requests, concurrency 10, before migration
docker exec new-api-dev-isolated-postgres-1 pgbench -U root -i -s 2 <temporary-db>
docker exec new-api-dev-isolated-postgres-1 pgbench -U root -c 8 -j 2 -T 20 -r <temporary-db>
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev RUN_POSTGRES_FSYNC_TESTS=true PG_TEST_FSYNC_SECONDS=1 scripts/storage-probe.sh
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 NEW_API_CONTAINER=new-api-dev-isolated-new-api-1 POSTGRES_DB=new-api-dev scripts/storage-migrate-postgres.sh --dry-run
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 NEW_API_CONTAINER=new-api-dev-isolated-new-api-1 POSTGRES_DB=new-api-dev scripts/storage-migrate-postgres.sh --execute
node <http-load-script> # 1000 requests, concurrency 10, after migration
node <http-load-script-with-rotating-xff> # 2000 requests, concurrency 50, after migration
```

Results:

- Before migration, the isolated development PostgreSQL container used
  `/Volumes/Work/code/new-api/.dev-docker/runtime/isolated/postgres`.
- Dry-run and execute migration to `/Volumes/Data/new-api-caches/postgres/data`
  passed. Post-migration verification restored exact source counts:
  `users=9`, `channels=102`, `tokens=15`, and `logs=411416`.
- The running isolated development PostgreSQL container now has bind mounts:
  `/Volumes/Data/new-api-caches/postgres/data -> /var/lib/postgresql/data` and
  `/Volumes/Data/new-api-caches/postgres/storage.marker ->
  /run/new-api/postgres-storage.marker`.
- `deploy/env/dev-isolated.env` was updated only for non-secret path variables:
  `DEV_POSTGRES_DATA_DIR`, `NEW_API_POSTGRES_DATA_DIR`,
  `NEW_API_POSTGRES_MARKER`, and `NEW_API_POSTGRES_MARKER_VALUE`.
- The same-IP HTTP load test hit application rate limiting and is not a clean
  storage benchmark. Before migration, 1000 requests at concurrency 10 returned
  `/api/status`: `7` HTTP 200 and `993` HTTP 429; `/login`: `1000` HTTP 429.
  After migration, the same test returned `/api/status`: `181` HTTP 200 and
  `819` HTTP 429; `/login`: `60` HTTP 200 and `940` HTTP 429.
- With rotating `X-Forwarded-For` to avoid single-IP rate limiting after
  migration, 2000 requests at concurrency 50 returned all HTTP 200. Measured
  latencies were `/api/status` p50 `20.02ms`, p95 `74.26ms`, p99 `109.42ms`;
  `/login` p50 `20.17ms`, p95 `43.17ms`, p99 `61.58ms`.
- `pgbench` before migration: `1706.081971` TPS, average latency `4.689ms`.
- `pgbench` after migration: `1530.915552` TPS, average latency `5.226ms`.
- `pg_test_fsync` before migration showed one-8KB-write `fdatasync` at
  `2866.289 ops/sec` and `349 usecs/op`.
- `pg_test_fsync` after migration showed one-8KB-write `fdatasync` at
  `2463.443 ops/sec` and `406 usecs/op`.
- Conclusion: the requested `/Volumes/Data/new-api-caches` path is configured
  and functional for isolated development, but on this host it did not improve
  PostgreSQL hot-write performance versus the previous
  `/Volumes/Work/code/new-api/.dev-docker/runtime/isolated/postgres` path.
  The measured database write path was slightly slower after migration.
- Temporary `storage_perf_*` pgbench databases and `pg_test_fsync.*`
  directories were removed. Development and production status checks returned
  successfully after validation; production storage was not changed.

## Development PostgreSQL tuning validation 2026-06-14

Sample time: `2026-06-14T01:10:00Z`.

The `/Volumes/Data/new-api-caches` path was kept unchanged. The only runtime
control input was explicit PostgreSQL tuning through
`NEW_API_POSTGRES_TUNING_ARGS` in the isolated development environment:

```text
checkpoint_timeout=15min
max_wal_size=4GB
min_wal_size=512MB
wal_compression=on
effective_io_concurrency=32
random_page_cost=1.5
log_min_duration_statement=500ms
```

The storage overlay now rejects unsupported tuning keys with exit code `70`.
Default deployments that leave `NEW_API_POSTGRES_TUNING_ARGS` empty still use
the PostgreSQL image defaults.

Validation commands:

```bash
bash -n scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh scripts/storage-plan-linux.sh
git diff --check -- .gitignore scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh scripts/storage-plan-linux.sh scripts/storage-plan-windows.ps1 deploy/compose/docker-compose.storage-postgres-bind.yml deploy/env/storage-postgres.example.env docs/operations/storage-acceleration.md docs/reviews/CR-STORAGE-ACCELERATION-2026-06-12.md
docker compose -f deploy/compose/dev-isolated.yml -f deploy/compose/docker-compose.storage-postgres-bind.yml --env-file <temporary-env-with-empty-tuning> config
docker compose -f deploy/compose/dev-isolated.yml -f deploy/compose/docker-compose.storage-postgres-bind.yml --env-file <temporary-env-with-invalid-tuning> run --rm --no-deps postgres
docker compose -f deploy/compose/dev-isolated.yml -f deploy/compose/docker-compose.storage-postgres-bind.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate postgres
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev NEW_API_STATUS_URL=http://127.0.0.1:3001/api/status NEW_API_POSTGRES_DATA_DIR=/Volumes/Data/new-api-caches/postgres/data NEW_API_POSTGRES_MARKER=/Volumes/Data/new-api-caches/postgres/storage.marker NEW_API_POSTGRES_MARKER_VALUE=new-api-dev-postgres-data-cache-20260613 scripts/storage-verify-postgres.sh
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev RUN_POSTGRES_FSYNC_TESTS=true PG_TEST_FSYNC_SECONDS=1 scripts/storage-probe.sh
docker exec new-api-dev-isolated-postgres-1 pgbench -U root -i -s 2 <temporary-db>
docker exec new-api-dev-isolated-postgres-1 pgbench -U root -c 8 -j 2 -T 20 -r <temporary-db>
node <http-load-script-with-rotating-xff>
go test ./model ./service -count=1
```

Observed results:

- Invalid tuning key `not_allowed=1` returned exit code `70` before starting
  PostgreSQL.
- The recreated isolated PostgreSQL container stayed `running healthy`.
- `scripts/storage-verify-postgres.sh` reported the marker as matching and now
  prints `postgres-tuning`, `postgres-write-stats`, and
  `log-storage-profile`.
- Effective settings included `checkpoint_timeout=900 s`,
  `max_wal_size=4096 MB`, `min_wal_size=512 MB`,
  `wal_compression=pglz`, `effective_io_concurrency=32`,
  `random_page_cost=1.5`, and `log_min_duration_statement=500 ms`.
- `synchronous_commit` remained `on`; the standard profile does not trade
  durability for latency.
- The current `logs` table profile was heap `362 MB`, indexes `513 MB`, total
  `875 MB`, and `39` indexes, so log write amplification remains the next
  optimization candidate after path and PostgreSQL parameter tuning.
- `pg_test_fsync` inside PGDATA showed one-8KB-write `fdatasync` at
  `2455.959 ops/sec` and `407 usecs/op`, which is close to the post-migration
  baseline and does not indicate a new storage-path regression.
- Same `pgbench` shape as the previous path validation improved from the
  post-migration baseline `1530.915552` TPS and `5.226ms` average latency to
  `1831.598388` TPS and `4.368ms` average latency.
- Rotating-XFF HTTP load after tuning returned all HTTP 200. `/api/status`
  measured p50 `19.94ms`, p95 `33.81ms`, p99 `108.86ms`; `/login` measured
  p50 `21.67ms`, p95 `43.94ms`, p99 `112.31ms`.
- Development and production `/api/status` returned successfully after
  validation. Production storage and production PostgreSQL parameters were not
  changed.

## Log index profile validation 2026-06-14

Sample time: `2026-06-14T01:45:00Z`.

The next optimization loop focused on `logs` write amplification. No production
schema was changed.

Changes:

- Added `scripts/storage-log-index-report.sh`, a read-only PostgreSQL report for
  `logs` index size, scan counters, prefix-contained candidates, sample values,
  and representative `EXPLAIN` plans.
- Added a new `idx_logs_created_at_id` model index with the intended
  `(created_at, id)` column order for recent log listing.
- Added a new `idx_logs_upstream_request_created_at_id` model index with
  `(upstream_request_id, created_at, id)` for upstream request lookup.
- Kept existing indexes intact. In particular, the old
  `idx_created_at_id` and `idx_logs_upstream_request_id_created_at` indexes
  were not dropped in this loop.

Observed baseline before the new indexes:

- `logs_rows=411416`, heap `362 MB`, indexes `513 MB`, total `875 MB`, index
  count `39`.
- `admin-recent` used `idx_created_at_type` plus incremental sort instead of a
  true `(created_at, id)` index.
- `upstream-request-id` used `idx_logs_upstream_request_id_created_at`, but the
  database definition showed that index only covered `upstream_request_id`,
  matching `idx_logs_upstream_request_id` and not its name.

Development PostgreSQL verification:

```bash
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev REPORT_LIMIT=25 scripts/storage-log-index-report.sh
docker exec new-api-dev-isolated-postgres-1 psql -U root -d new-api-dev -v ON_ERROR_STOP=1 -c 'CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_logs_created_at_id ON logs (created_at, id);' -c 'CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_logs_upstream_request_created_at_id ON logs (upstream_request_id, created_at, id);'
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev REPORT_LIMIT=20 scripts/storage-log-index-report.sh
go test ./model -run 'TestRuntimePollingIndexesAutoMigrate|TestSumUsedQuota|TestRecordConsumeLogCopiesUpstreamRequestIdFromOther' -count=1
```

Observed result after adding the indexes in the isolated development database:

- `logs` index count increased from `39` to `41`, and index size increased from
  `513 MB` to `541 MB`.
- `admin-recent` switched to `Index Only Scan Backward using
  idx_logs_created_at_id` without an extra sort.
- `upstream-request-id` switched to `Index Only Scan Backward using
  idx_logs_upstream_request_created_at_id`.
- The targeted Go model tests passed.

Residual gate:

- Because the new indexes increase write amplification until old redundant
  indexes are removed, production rollout should not stop at adding indexes.
  A future migration must collect real production traffic statistics first,
  then explicitly decide whether to drop or replace `idx_created_at_id`,
  `idx_logs_upstream_request_id_created_at`, and other prefix-contained
  candidates. This loop intentionally did not perform destructive index drops.

## Log index maintenance validation 2026-06-14

Sample time: `2026-06-14T02:15:00Z`.

The follow-up loop added an explicit dry-run-first maintenance script for the
two known legacy indexes. Production was not changed.

Changes:

- Removed the old `idx_created_at_id` AutoMigrate tag from `Log`. The intended
  replacement is `idx_logs_created_at_id(created_at, id)`.
- Removed the old `idx_logs_upstream_request_id_created_at` AutoMigrate tag
  from `Log`. The intended replacement is
  `idx_logs_upstream_request_created_at_id(upstream_request_id, created_at,
  id)`.
- Added `scripts/storage-log-index-maintenance.sh`. It only manages the two
  known legacy indexes, defaults to `--dry-run`, verifies replacement indexes,
  prints candidate size and usage, and requires an exact confirmation string
  for `--execute`.

Validation commands:

```bash
bash -n scripts/storage-log-index-maintenance.sh scripts/storage-log-index-report.sh scripts/storage-verify-postgres.sh
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev scripts/storage-log-index-maintenance.sh --dry-run
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev CONFIRM_LOG_INDEX_MAINTENANCE=wrong scripts/storage-log-index-maintenance.sh --execute
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev CONFIRM_LOG_INDEX_MAINTENANCE=execute:new-api-dev-isolated-postgres-1:new-api-dev:drop-legacy-log-indexes scripts/storage-log-index-maintenance.sh --execute
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev REPORT_LIMIT=15 scripts/storage-log-index-report.sh
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev NEW_API_STATUS_URL=http://127.0.0.1:3001/api/status NEW_API_POSTGRES_DATA_DIR=/Volumes/Data/new-api-caches/postgres/data NEW_API_POSTGRES_MARKER=/Volumes/Data/new-api-caches/postgres/storage.marker NEW_API_POSTGRES_MARKER_VALUE=new-api-dev-postgres-data-cache-20260613 scripts/storage-verify-postgres.sh
go test ./model -count=1
```

Observed result:

- Dry-run required replacement indexes and reported the exact confirmation
  string `execute:new-api-dev-isolated-postgres-1:new-api-dev:drop-legacy-log-indexes`.
- Wrong confirmation returned exit code `3` and did not drop indexes.
- Execute used `DROP INDEX CONCURRENTLY IF EXISTS` for
  `idx_created_at_id` and `idx_logs_upstream_request_id_created_at`.
- In isolated development, `logs` index size moved from `541 MB` to `526 MB`
  and index count moved from `41` to `39`.
- Post-maintenance plans still used `idx_logs_created_at_id` for
  `admin-recent` and `idx_logs_upstream_request_created_at_id` for
  `upstream-request-id`.
- `scripts/storage-verify-postgres.sh` reported marker match and
  `logs_index_count=39`.
- `go test ./model -count=1` passed.
- Development and production `/api/status` returned successfully after
  validation.

Production gate:

- Before running this maintenance script in production, capture
  `scripts/storage-log-index-report.sh` during a real traffic window and keep
  the report with the operation record. The script is intentionally not called
  by service startup or AutoMigrate.
- Claude's final read-only pass reported no blocking issues. It noted the
  compose overlay command replacement boundary; the operation guide now calls
  out that custom PostgreSQL command flags must be mirrored in the overlay.
- Gemini's follow-up review flagged restore-time container writable-layer bloat
  and missing execute-time `ENV_FILE` validation. Both were fixed.
- Claude's follow-up review flagged target PostgreSQL major-version checks,
  backup free-space checks, immediate post-compose container-exit diagnostics,
  global role backup visibility, and partial marker env handling. These were
  fixed or documented with explicit operator boundaries.
- Gemini's second follow-up flagged Windows binary dump streaming and migration
  count false-success risk. Windows dump creation now uses base64 wrapping, and
  migration-time verification now compares restored table counts with source
  counts. The same review's `template0` and TCP-auth claims were checked
  against the running PostgreSQL containers and were not valid for this
  deployment.
- Final Claude file-content review reported no remaining P0/P1 defects.
- Final Gemini read-only follow-up returned no remaining P0/P1 defects.

## Log hot-path and script target hardening 2026-06-14

Sample time: `2026-06-14T02:45:00Z`.

This loop reviewed synchronous work left in the request consumption log path
and the operational safety of the log-index scripts. Production was not
changed.

Changes:

- `RecordConsumeLog` and `RecordErrorLog` now read `RecordIpLog` from
  `ContextKeyUserSetting` first. Token authentication already stores the parsed
  user setting in the request context, and relay payload-audit setup already
  consumes that context value. Falling back to `GetUserSetting(userId, false)`
  is preserved for non-request or legacy call sites.
- Added model tests that cover context-enabled IP logging, context-disabled IP
  logging overriding a stored true value, and fallback to stored setting when
  no context value exists.
- `scripts/storage-log-index-report.sh` and
  `scripts/storage-log-index-maintenance.sh` now require explicit
  `POSTGRES_CONTAINER` and `POSTGRES_DB`. A dry-run without explicit target had
  reached the default `postgres/new-api` container; it made no changes, but the
  behavior was too easy to misread in production work.
- `scripts/storage-migrate-postgres.sh` now also requires explicit
  `POSTGRES_CONTAINER`, `NEW_API_CONTAINER`, and `POSTGRES_DB`; its execute-time
  post-verify call passes the same target values to
  `scripts/storage-verify-postgres.sh`.
- Migration dry-run planned commands now use the `would_run:` prefix. Execute
  mode uses `run:`. This avoids mistaking dry-run output for actual stopped
  containers or started compose commands.
- Migration dry-run now prints `target_data_dir_looks_like_pgdata=true` and
  `target_data_dir_pg_version=<major>` when the target already contains a
  PostgreSQL `PG_VERSION` file.
- `scripts/storage-verify-postgres.sh` prints an explicit `target` section with
  PostgreSQL container, database, user, and New API status URL, plus
  `target_defaults_used` and per-field source markers so defaulted targets are
  visible during review.
- The operation guide now uses explicit target placeholders for log-index
  report, maintenance, and PostgreSQL verification examples.
- Migration dry-run now prefixes the final verifier invocation with
  `would_run:` and includes the full explicit verification environment inline.

Validation commands:

```bash
go test ./model -count=1
bash -n scripts/storage-log-index-maintenance.sh scripts/storage-log-index-report.sh scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh scripts/storage-plan-linux.sh
scripts/storage-log-index-maintenance.sh --dry-run
scripts/storage-log-index-report.sh
NEW_API_POSTGRES_DATA_DIR=/tmp/new-api-storage-test NEW_API_POSTGRES_MARKER=/tmp/new-api-storage-test.marker NEW_API_POSTGRES_MARKER_VALUE=test scripts/storage-migrate-postgres.sh --dry-run
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev scripts/storage-log-index-maintenance.sh --dry-run
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev REPORT_LIMIT=15 scripts/storage-log-index-report.sh
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev NEW_API_STATUS_URL=http://127.0.0.1:3001/api/status NEW_API_POSTGRES_DATA_DIR=/Volumes/Data/new-api-caches/postgres/data NEW_API_POSTGRES_MARKER=/Volumes/Data/new-api-caches/postgres/storage.marker NEW_API_POSTGRES_MARKER_VALUE=new-api-dev-postgres-data-cache-20260613 scripts/storage-verify-postgres.sh
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev CONFIRM_LOG_INDEX_MAINTENANCE=execute:new-api-dev-isolated-postgres-1:new-api-dev:drop-legacy-log-indexes scripts/storage-log-index-maintenance.sh --execute
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env restart new-api
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev scripts/storage-log-index-maintenance.sh --dry-run
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev REPORT_LIMIT=15 scripts/storage-log-index-report.sh
POSTGRES_CONTAINER=new-api-dev-isolated-postgres-1 POSTGRES_USER=root POSTGRES_DB=new-api-dev NEW_API_STATUS_URL=http://127.0.0.1:3001/api/status NEW_API_POSTGRES_DATA_DIR=/Volumes/Data/new-api-caches/postgres/data NEW_API_POSTGRES_MARKER=/Volumes/Data/new-api-caches/postgres/storage.marker NEW_API_POSTGRES_MARKER_VALUE=new-api-dev-postgres-data-cache-20260613 scripts/storage-verify-postgres.sh
go test ./controller ./model ./relay/common ./relay/helper ./service -count=1
git diff --check -- model/log.go model/log_admin_audit_test.go model/runtime_polling_index_test.go scripts/storage-log-index-maintenance.sh scripts/storage-log-index-report.sh scripts/storage-probe.sh scripts/storage-migrate-postgres.sh scripts/storage-verify-postgres.sh scripts/storage-plan-linux.sh scripts/storage-plan-windows.ps1 docs/operations/storage-acceleration.md docs/reviews/CR-STORAGE-ACCELERATION-2026-06-12.md deploy/compose/docker-compose.storage-postgres-bind.yml deploy/env/storage-postgres.example.env .gitignore
```

Observed result:

- The two script calls without `POSTGRES_CONTAINER` and `POSTGRES_DB` exit
  with status `2` before any PostgreSQL query.
- `scripts/storage-migrate-postgres.sh --dry-run` without
  `POSTGRES_CONTAINER`, `NEW_API_CONTAINER`, and `POSTGRES_DB` now exits with
  `error: POSTGRES_CONTAINER is required`.
- The explicit dev-isolated migration dry-run prints
  `execute:new-api-dev-isolated-postgres-1:new-api-dev-isolated-new-api-1:new-api-dev:/Volumes/Data/new-api-caches/postgres/data`
  and does not stop containers in dry-run mode. Planned commands are printed
  with `would_run:`.
- The same dry-run reports `target_data_dir_looks_like_pgdata=true` and
  `target_data_dir_pg_version=15` for the already-migrated dev target.
- During validation, the dev-isolated `new-api` container had been rebuilt from
  an older image at `2026-06-14T02:13:38Z`, and startup recreated
  `idx_created_at_id` and `idx_logs_upstream_request_id_created_at`. This
  confirmed why dropping the old indexes must be paired with a current image.
- Rebuilt `new-api-local:dev` from the current working tree at
  `2026-06-14T02:32:50Z`, replaced only `new-api-dev-isolated-new-api-1`,
  executed the dev maintenance script, and restarted the dev container.
- After restart, the dev-isolated maintenance dry-run is idempotent and reports
  `drop_candidate_total_bytes=0`.
- The dev-isolated report still shows `idx_logs_created_at_id` for
  `admin-recent` and `idx_logs_upstream_request_created_at_id` for
  `upstream-request-id`.
- The verifier reports the `/Volumes/Data/new-api-caches/postgres/data` marker
  as matched and shows `logs_index_count=39`.
- Direct index check only returns `idx_logs_created_at_id` and
  `idx_logs_upstream_request_created_at_id`; the two old index names are absent.
