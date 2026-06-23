# Storage acceleration and PostgreSQL migration

This document describes the supported storage acceleration path for Docker
deployments of new-api. The default path is intentionally conservative:
move PostgreSQL `PGDATA` to a faster explicit bind mount, verify it, and keep
the old volume or directory for rollback.

It does not provide transparent caching, silent fallback, or Web UI host-path
configuration.

## Design contract

The storage acceleration feature is a deployment-layer enhancement, not a
runtime Web UI setting. Keep host paths in compose env files and shell
environment only.

Supported hot paths:

- `NEW_API_POSTGRES_DATA_DIR`: standard PostgreSQL `PGDATA` bind mount. This
  is the only path consumed by the standard storage overlay.
- `NEW_API_LOG_DIR`: existing new-api log bind mount from the base
  `docker-compose.yml`.
- `NEW_API_DATA_DIR`: existing new-api data bind mount from the base
  `docker-compose.yml`.
- `NEW_API_POSTGRES_WAL_DIR`: planning-only advanced variable. The standard
  overlay does not split `pg_wal`.
- `NEW_API_DOCKER_DATA_DIR`: planning-only expert host variable. Docker
  data-root migration is not a new-api runtime setting.

There is intentionally no `NEW_API_FAST_STORAGE_DIR` catch-all variable.
Each path has different durability, permission, and rollback behavior.

## Supported layers

### Layer 0: diagnosis

Run the probe before changing storage:

```bash
scripts/storage-probe.sh
```

The probe is read-only by default. It reports:

- host OS, memory, swap, filesystem, and disk space
- Docker data root and storage driver
- running Docker containers
- PostgreSQL container image, health, mounts, `PGDATA`, and data directory
- optional sequential write, 8K random write, and fsync latency probes for the
  selected path

Temporary write probes are disabled by default. Enable them explicitly:

```bash
RUN_WRITE_TESTS=true PROBE_DIR=/path/to/candidate scripts/storage-probe.sh
```

The random write probe uses a temporary file and reports 8K write IOPS plus
average, p95, and max fsync latency. It removes the temporary file before exit.

PostgreSQL container fsync testing is also opt-in because it writes inside
`PGDATA`:

```bash
RUN_POSTGRES_FSYNC_TESTS=true PG_TEST_FSYNC_SECONDS=3 scripts/storage-probe.sh
```

Use platform planners to review candidate hot paths before migration:

```bash
scripts/storage-plan-linux.sh
```

On Windows PowerShell:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/storage-plan-windows.ps1
```

The Linux planner covers native Linux and WSL2. The Windows planner covers
Docker Desktop on Windows paths, OneDrive risk, Defender exclusion visibility,
drive type, and WSL2 availability. Both planners are read-only.

Use the probe to separate local storage problems from upstream latency. For
model requests, compare first response time (`frt_ms`) and total request time
(`use_time`) before concluding that disk I/O is the primary bottleneck.

For deployments where `logs` indexes are larger than the table heap, capture a
read-only index report before changing schema:

```bash
POSTGRES_CONTAINER=<postgres-container> POSTGRES_USER=<postgres-user> POSTGRES_DB=<postgres-db> \
  scripts/storage-log-index-report.sh
```

The report prints `logs` heap, index, and total sizes, per-index size and scan
statistics, prefix-contained candidates, representative sample values, and
`EXPLAIN` plans for common log list filters. Treat it as an observation tool,
not a migration script. Do not drop indexes only because `idx_scan=0` after a
recent restart, migration, or statistics reset; collect a real traffic window
first.

After replacement indexes are present and a real traffic window confirms the
legacy indexes are unused, run the maintenance script in dry-run mode:

```bash
POSTGRES_CONTAINER=<postgres-container> POSTGRES_USER=<postgres-user> POSTGRES_DB=<postgres-db> \
  scripts/storage-log-index-maintenance.sh --dry-run
```

The script only considers the known legacy indexes:

- `idx_created_at_id`
- `idx_logs_upstream_request_id_created_at`

It refuses to continue unless replacement indexes
`idx_logs_created_at_id` and `idx_logs_upstream_request_created_at_id` are
present. Execution requires copying the exact
`execute_confirmation_required=...` value from dry-run:

```bash
CONFIRM_LOG_INDEX_MAINTENANCE='execute:<postgres-container>:<postgres-db>:drop-legacy-log-indexes' \
POSTGRES_CONTAINER=<postgres-container> POSTGRES_USER=<postgres-user> POSTGRES_DB=<postgres-db> \
  scripts/storage-log-index-maintenance.sh --execute
```

The execute path uses `DROP INDEX CONCURRENTLY IF EXISTS` and then prints the
post-drop `logs` index size and index count. It still changes database schema,
so run it in a maintenance window or low-traffic window and keep the pre-run
index report with the operation record.

`scripts/storage-log-index-report.sh` and
`scripts/storage-log-index-maintenance.sh` intentionally require explicit
`POSTGRES_CONTAINER` and `POSTGRES_DB`. They do not guess defaults because
large log-table reports and schema maintenance must always target a named
deployment.

### Layer 1: standard storage profile

The standard storage profile moves PostgreSQL `PGDATA` to a bind mount:

```bash
cp deploy/env/storage-postgres.example.env deploy/env/storage-postgres.env
```

Edit `deploy/env/storage-postgres.env`:

```bash
NEW_API_POSTGRES_DATA_DIR=/absolute/path/to/fast/postgres/data
NEW_API_POSTGRES_MARKER=/absolute/path/to/fast/postgres/storage.marker
NEW_API_POSTGRES_MARKER_VALUE=new-api-prod-postgres-fast-storage-20260612
```

Create the marker before starting the overlay:

```bash
mkdir -p /absolute/path/to/fast/postgres
printf '%s' 'new-api-prod-postgres-fast-storage-20260612' \
  > /absolute/path/to/fast/postgres/storage.marker
```

`NEW_API_POSTGRES_MARKER` must not be inside `PGDATA`. For example,
`<PGDATA>/storage.marker` is rejected with failure code `66`. Use
`printf '%s'` so the marker has no trailing newline or extra whitespace; marker
comparison is byte-for-byte.

Optional PostgreSQL tuning can be enabled explicitly with
`NEW_API_POSTGRES_TUNING_ARGS`. When unset, the overlay keeps the PostgreSQL
image defaults. For values without spaces, the value may be a space-separated
list of `key=value` entries. Use `|` as the separator when any value contains
spaces, for example `log_min_duration_statement=100 ms`. The overlay rejects
any key outside this allowlist:

- `shared_buffers`
- `effective_cache_size`
- `maintenance_work_mem`
- `work_mem`
- `checkpoint_timeout`
- `max_wal_size`
- `min_wal_size`
- `checkpoint_completion_target`
- `wal_compression`
- `effective_io_concurrency`
- `random_page_cost`
- `log_min_duration_statement`

A conservative write-heavy starting point for a small Docker deployment is:

```bash
NEW_API_POSTGRES_TUNING_ARGS='checkpoint_timeout=15min|max_wal_size=4GB|min_wal_size=512MB|wal_compression=on|effective_io_concurrency=32|random_page_cost=1.5|log_min_duration_statement=500ms'
```

This is a starting point, not a universal recommendation. Measure before and
after with `scripts/storage-verify-postgres.sh`,
`RUN_POSTGRES_FSYNC_TESTS=true scripts/storage-probe.sh`, and application HTTP
latency. `max_wal_size` can increase temporary disk usage. Do not disable
`synchronous_commit` in production as a default latency optimization; that
trades durability for latency and must be an explicit operator decision outside
this standard profile.

Start with the storage overlay:

```bash
docker compose \
  -f docker-compose.yml \
  -f deploy/compose/docker-compose.storage-postgres-bind.yml \
  --env-file deploy/env/storage-postgres.env \
  up -d
```

The overlay fails fast if the marker file is missing or the marker content does
not match `NEW_API_POSTGRES_MARKER_VALUE`. This prevents Docker from silently
creating a new empty directory when the fast disk is not mounted.
`NEW_API_POSTGRES_DATA_DIR` and `NEW_API_POSTGRES_MARKER` must be absolute,
normalized paths without `/./` or `/../` segments. Unix-style absolute paths
and Windows drive absolute paths such as `C:/new-api/postgres/data` are
accepted.
The migration script trims trailing slashes before it checks whether the
marker is inside `PGDATA`.
This overlay replaces the PostgreSQL service entrypoint and command so it can
run marker checks before `docker-entrypoint.sh postgres`. The repository's
default PostgreSQL service does not define custom PostgreSQL command flags. If
an installation has added custom `postgres -c ...` flags to the base compose
file, mirror those flags in this overlay before migration.

To check for custom flags, inspect the base compose file's `postgres` service
for `command`, `entrypoint`, or `postgres -c` lines. If the base service uses
`postgres -c shared_buffers=512MB -c max_connections=200`, copy the same
`-c shared_buffers=512MB -c max_connections=200` arguments into this overlay's
final `exec docker-entrypoint.sh postgres ...` command in the same order. The
review boundary for this repository is recorded in
`docs/reviews/CR-STORAGE-ACCELERATION-2026-06-12.md`: the default compose file
has no custom PostgreSQL command flags.

The standard overlay does not consume `NEW_API_POSTGRES_WAL_DIR`. WAL
separation is intentionally documented as an advanced manual path because
existing clusters need stopped-cluster or backup/restore handling and mount
ordering guarantees.

Overlay startup failure codes:

- `64`: marker file missing
- `65`: marker content mismatch
- `66`: marker path is inside `PGDATA`
- `67`: `PGDATA` is not writable
- `68`: path is not absolute or contains `/./`, `/../`, `/.`, or `/..`
- `69`: marker bind mount target is a directory, usually because Docker
  created a host directory when the marker file was missing
- `70`: `NEW_API_POSTGRES_TUNING_ARGS` contains an unsupported tuning key

### Layer 2: standard migration workflow

The migration script defaults to dry-run mode:

```bash
NEW_API_POSTGRES_DATA_DIR=/absolute/path/to/fast/postgres/data \
NEW_API_POSTGRES_MARKER=/absolute/path/to/fast/postgres/storage.marker \
NEW_API_POSTGRES_MARKER_VALUE=new-api-prod-postgres-fast-storage-20260612 \
POSTGRES_CONTAINER=<postgres-container> \
NEW_API_CONTAINER=<new-api-container> \
POSTGRES_DB=<postgres-db> \
scripts/storage-migrate-postgres.sh --dry-run
```

Dry-run prints the target directory existence, writability, entry count,
available space, marker existence, marker match status, current PostgreSQL data
size, current mounts, table counts, and the exact planned stop/recreate/restore
steps. Planned commands, including the final post-restore verifier invocation,
are prefixed with `would_run:`. It also prints
`execute_confirmation_required=...`. It does not stop containers or copy dump
files.
When the target directory already contains `PG_VERSION`, dry-run also prints
`target_data_dir_looks_like_pgdata=true` and the detected PostgreSQL major
version.
Dry-run fails if the marker is missing or if marker bytes do not exactly match
`NEW_API_POSTGRES_MARKER_VALUE`.

The source database must already be an initialized new-api PostgreSQL database.
Dry-run intentionally fails if the required `users`, `channels`, `tokens`, and
`logs` tables are missing, because that means the migration source is not the
expected production or rehearsal database.
Execution mode has the same marker gate and does not create the marker
automatically. It also requires an exact confirmation string tied to the
PostgreSQL container, new-api container, database name, and target data path.
Copy the `execute_confirmation_required=...` value from a successful dry-run
for the same target and pass only the value after `=` as
`CONFIRM_STORAGE_MIGRATION` before running `--execute`.

The confirmation format is:

```text
execute:<POSTGRES_CONTAINER>:<NEW_API_CONTAINER>:<POSTGRES_DB>:<NEW_API_POSTGRES_DATA_DIR>
```

For example,
`execute:postgres:new-api:new-api:/absolute/path/to/fast/postgres/data` means:

- `execute`: the only accepted execution action
- `postgres`: the PostgreSQL container name
- `new-api`: the new-api container name
- `new-api`: the database name
- `/absolute/path/to/fast/postgres/data`: the normalized target PGDATA path

Set `POSTGRES_IMAGE` when dump verification should use a different PostgreSQL
image from the default `postgres:15`. In `--execute`, the script checks the
target image's PostgreSQL major version before stopping services. The target
major version must be greater than or equal to the source major version.

`ENV_FILE` defaults to `deploy/env/storage-postgres.env`. In `--execute`, that
file must already exist and be readable unless `ENV_FILE` is set to an empty
string and all required compose variables are supplied another way.

`BACKUP_MIN_FREE_PERCENT` defaults to `30`. In `--execute`, the script checks
that `BACKUP_DIR` has free space of at least that percentage of current
`PGDATA` before it stops services.

Execution mode performs the migration:

```bash
NEW_API_POSTGRES_DATA_DIR=/absolute/path/to/fast/postgres/data \
NEW_API_POSTGRES_MARKER=/absolute/path/to/fast/postgres/storage.marker \
NEW_API_POSTGRES_MARKER_VALUE=new-api-prod-postgres-fast-storage-20260612 \
POSTGRES_CONTAINER=<postgres-container> \
NEW_API_CONTAINER=<new-api-container> \
POSTGRES_DB=<postgres-db> \
CONFIRM_STORAGE_MIGRATION='execute:<postgres-container>:<new-api-container>:<postgres-db>:/absolute/path/to/fast/postgres/data' \
scripts/storage-migrate-postgres.sh --execute
```

The script:

1. reads current container mounts, counts, and latest log timestamp
2. checks target PostgreSQL major version, compose env file availability, and
   backup directory free space before stopping services
3. stops `new-api` to block writes
4. creates a `pg_dumpall --globals-only --no-role-passwords` file for
   operator review of roles and global privileges
5. creates a final custom-format `pg_dump` by streaming directly to the host
   backup path, avoiding a large temporary dump inside the container writable
   layer. Windows shells use base64 wrapping for the binary custom-format dump
   stream to avoid stdout newline conversion
6. validates the dump with `pg_restore -l`
7. recreates PostgreSQL with the storage overlay and immediately checks whether
   the recreated container stayed running; if it exited, the script prints the
   recent PostgreSQL logs instead of waiting for a generic readiness timeout
8. waits for PostgreSQL readiness with TCP `pg_isready -h 127.0.0.1` rather
   than the local Unix socket, avoiding the official image temporary init-server
   race before `POSTGRES_DB` is created
9. ensures the original database owner role exists in the target cluster
10. recreates the database with the original database settings
11. streams the host dump directly into `pg_restore --exit-on-error`; it does
    not copy the dump into the container writable layer
12. restarts `new-api` and verifies that the container stayed running
13. runs `scripts/storage-verify-postgres.sh`

Database recreation preserves the original database encoding, collation,
ctype, owner, locale provider, and ICU locale when applicable.
For source clusters older than PostgreSQL 15 that do not expose
`pg_database.datlocprovider`, the migration treats the database as libc-locale.
When the target image is older than PostgreSQL 15, the script omits
`createdb --locale-provider` flags because those flags are not supported.

During `--execute`, the post-restore verifier receives the source `users`,
`channels`, `tokens`, and `logs` counts and fails if the restored database does
not match those counts. Running `scripts/storage-verify-postgres.sh` directly
does not enforce non-zero counts by default, so it remains usable for fresh
deployments where some tables may legitimately be empty.

If the script fails before PostgreSQL replacement, it attempts to restart the
stopped `new-api` container. Once PostgreSQL replacement has started, rollback
requires explicit operator action using the retained old storage.

The target data directory must be empty unless
`ALLOW_NON_EMPTY_TARGET=true` is set after manual review.

Manual review for `ALLOW_NON_EMPTY_TARGET=true` means:

- confirm the target directory is the intended PostgreSQL cluster or trusted
  rollback target
- confirm the marker path and marker value identify the same target
- confirm there is a separate backup outside the target directory
- confirm no live PostgreSQL process is using the target directory: inspect
  `postmaster.pid`, check `global/pg_control` and `base/*` ownership and
  timestamps, and use `lsof <target-dir>` or the platform equivalent
- accept that `scripts/storage-migrate-postgres.sh` will drop and recreate
  `POSTGRES_DB` inside that target cluster before `pg_restore`

This override is for repeat migration rehearsals or explicit emergency recovery.
It is not recommended for the standard production path.

### Layer 3: advanced and expert paths

These are not default product paths.

- `pg_wal` on a separate device can improve write isolation, but a WAL device
  failure has a different recovery model from full `PGDATA` migration. Treat it
  as an advanced operation with an explicit backup and restore plan.
- Linux Docker data-root migration affects all containers on the host. It is a
  host administration operation, not a new-api runtime setting.
- Physical PGDATA copy is intentionally not automated because it has WAL,
  checkpoint, timeline, and point-in-time recovery requirements that are easy
  to get wrong. If physical migration is required, use a maintenance window
  with `pg_basebackup` or a stopped-cluster `rsync`, keep the WAL/restore plan
  with the backup, rehearse restore separately, and keep the old storage
  untouched until the new cluster has been verified.
- Linux filesystem tuning is environment-specific:
  - XFS and ext4 are the safest general choices.
  - Btrfs should disable CoW for PostgreSQL data before initialization.
  - ZFS needs recordsize and sync behavior review.
  - LVM cache and bcache can corrupt data if configured with unsafe writeback
    settings and no reliable power-loss protection.
- macOS and Windows should not use block-level cache as the default answer.
  Prefer explicit PostgreSQL data directory migration.

## Platform notes

### macOS

- Use an explicit `/Volumes/<disk>/...` path.
- Confirm the volume is mounted before starting Docker.
- Docker Desktop storage driver and file sharing mode can dominate `fsync`
  latency. Use diagnosis before assuming the physical SSD is the only issue.
- Keep the marker outside `PGDATA` so PostgreSQL can initialize an empty data
  directory.
- If Docker Desktop's own data root is slow or nearly full, treat that as a
  separate host setting. Do not mix Docker data-root migration into the
  PostgreSQL PGDATA migration step.

### Windows and WSL2

- Prefer WSL2 ext4 paths inside the Linux distro.
- Avoid `/mnt/c`, OneDrive, and Defender-scanned paths for PostgreSQL data.
- If using Windows paths with Docker Desktop, prefer local SSD or NVMe NTFS
  paths and compose long-syntax bind mounts. Avoid network shares.
- Run `scripts/storage-plan-windows.ps1` from PowerShell to inspect candidate
  paths, volume type, Defender exclusion visibility, OneDrive placement, Docker
  data-root, and WSL2 availability.
- Run `scripts/storage-migrate-postgres.sh` with `C:/...` paths only from a
  Windows shell such as Git Bash or MSYS. On macOS, Linux, and WSL2 the script
  rejects Windows drive paths to avoid creating a relative `C:` directory.

### Linux

- Ensure bind-mounted PostgreSQL data can be initialized by the actual
  container user model. This repository's default compose runs the official
  PostgreSQL image entrypoint as container root, and the entrypoint prepares
  `PGDATA` for the postgres user inside the container. Verify the runtime
  users instead of assuming a fixed UID/GID:

```bash
docker exec postgres id
docker exec postgres id postgres
docker exec postgres stat -c 'owner=%U group=%G mode=%a' "${PGDATA:-/var/lib/postgresql/data}"
```

  At the time this review was written, the tested `postgres:15` image commonly
  used postgres UID/GID `999:999`, but image versions, rootless Docker, and
  user namespace remapping can change that. Permission problems normally show
  up as `permission denied`, `fixing permissions on existing directory ...
  failed`, or early container startup failures in `docker logs postgres`.
- Check filesystem type and mount options:

```bash
findmnt -T /path/to/postgres/data -o TARGET,SOURCE,FSTYPE,OPTIONS
```

- For Btrfs, disable CoW before initializing the directory.
- Do not use LVM cache or bcache writeback mode without a tested power-loss and
  recovery plan.
- Run `scripts/storage-plan-linux.sh` to inspect candidate paths, mount
  options, filesystem type, WSL2 `/mnt/*` risks, Docker data-root, and
  container PostgreSQL UID/GID.

## Verification

Run:

```bash
POSTGRES_CONTAINER=<postgres-container> POSTGRES_USER=<postgres-user> POSTGRES_DB=<postgres-db> \
NEW_API_STATUS_URL=<new-api-base-url>/api/status \
  scripts/storage-verify-postgres.sh
```

The verifier reports:

- the selected target and whether any key target value came from a script
  default instead of an explicit environment variable. Production checks should
  pass explicit `POSTGRES_CONTAINER`, `POSTGRES_DB`, and `NEW_API_STATUS_URL`
  and expect `target_defaults_used=false`.
- PostgreSQL data directory and mount sources
- selected PostgreSQL tuning values, including checkpoint, WAL, planner, and
  slow-query threshold settings
- PostgreSQL write counters from `pg_stat_bgwriter` and `pg_stat_wal`
- `logs` table heap size, index size, total size, and index count, which helps
  separate storage-path latency from application log write amplification
- storage marker match status when `NEW_API_POSTGRES_DATA_DIR`,
  `NEW_API_POSTGRES_MARKER`, and `NEW_API_POSTGRES_MARKER_VALUE` are provided
  together; when they are not provided, it explicitly reports
  `postgres-storage-marker` as skipped rather than pretending validation
  occurred
- `users`, `channels`, `tokens`, and `logs` counts
- latest log and channel timestamps
- `/api/status` response
- login page response. Override with `NEW_API_LOGIN_URL` when the deployment
  uses a non-default login route.
- protected log list response when `NEW_API_AUTH_COOKIE` and `NEW_API_USER_ID`
  are provided
- exact count comparison for `users`, `channels`, `tokens`, and `logs` only
  when `NEW_API_EXPECT_USERS_COUNT`, `NEW_API_EXPECT_CHANNELS_COUNT`,
  `NEW_API_EXPECT_TOKENS_COUNT`, and `NEW_API_EXPECT_LOGS_COUNT` are provided

The verifier exits non-zero and prints `check_failed=<name>` when a PostgreSQL
query, required table check, HTTP status check, or protected log-list check
fails.

Protected log list verification:

```bash
NEW_API_AUTH_COOKIE='session-cookie-value' \
NEW_API_USER_ID=1 \
scripts/storage-verify-postgres.sh
```

For production, also keep an observation window after migration. Use at least
15 minutes for a small local deployment, 30 minutes for normal production, and
60 minutes when changing physical disks or Docker data paths.

- `docker logs postgres`: no repeated restart, auth, permission, or checkpoint
  error loops
- `docker logs new-api`: no new DB connection loops or migration errors
- `/api/status`: collect a pre-migration p95 baseline for at least 5-10
  minutes. Investigate post-migration p95 above `max(baseline + 50ms, 500ms)`
  for at least 2 continuous minutes. Example: baseline p95 200 ms means
  sustained p95 above 250 ms is a regression signal.
- GORM slow SQL: investigate sustained increases above baseline for at least 2
  continuous minutes, especially repeated `>= 500ms` reads or writes
- request logs: compare `frt_ms` and `use_time`; high `frt_ms` usually points
  to upstream latency rather than local disk I/O

If core checks fail during the window, stop new writes and roll back to the old
storage before collecting deeper diagnostics.

## Rollback boundary

The migration scripts do not delete the old Docker volume or old `PGDATA`.
Rollback means pointing compose back to the old storage and recreating the
PostgreSQL container. Do not clean the old storage until the new path has been
observed under real traffic and a separate backup exists.

Minimal rollback pattern:

1. Stop `new-api` to block writes.
2. Point the PostgreSQL service back to the old storage:
   - named volume rollback: remove the storage overlay and use
     `docker-compose.yml` with the retained `pg_data` volume
   - bind mount rollback: set `NEW_API_POSTGRES_DATA_DIR` and
     `NEW_API_POSTGRES_MARKER` back to the old recorded paths
3. Recreate only PostgreSQL.
4. Verify table counts and latest timestamps.
5. Start `new-api`.

Example:

```bash
docker stop new-api
docker compose -f docker-compose.yml up -d --no-deps --force-recreate postgres
scripts/storage-verify-postgres.sh
docker start new-api
```

Before a production migration, confirm there is no concurrent Docker rebuild or
`docker compose up` from another session.

`scripts/storage-migrate-postgres.sh --execute` creates
`/tmp/new-api-storage-migrate.lock` by default and checks for concurrent Docker
build or compose processes. It is strongly recommended not to skip this check
in production. Set `SKIP_DOCKER_ACTIVITY_CHECK=true` only after manual process
review:

```bash
docker ps
pgrep -af 'docker|docker-compose|compose' || true
ps aux | grep -E 'docker|compose' | grep -v grep || true
```

If any active docker or compose processes are found, stop this script
immediately. The default lock is a host-local directory lock and does not
protect multiple hosts unless moved to a shared filesystem.
