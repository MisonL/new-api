#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-postgres}"
NEW_API_CONTAINER="${NEW_API_CONTAINER:-new-api}"
PROBE_WRITE_MB="${PROBE_WRITE_MB:-64}"
PROBE_WRITE_MB_REQUESTED="$PROBE_WRITE_MB"
PROBE_DIR="${PROBE_DIR:-}"
RUN_WRITE_TESTS="${RUN_WRITE_TESTS:-false}"
RUN_POSTGRES_FSYNC_TESTS="${RUN_POSTGRES_FSYNC_TESTS:-false}"
PG_TEST_FSYNC_SECONDS="${PG_TEST_FSYNC_SECONDS:-3}"
PROBE_ERRORS=()

section() {
  printf '\n## %s\n' "$1"
}

have() {
  command -v "$1" >/dev/null 2>&1
}

run_or_note() {
  if "$@"; then
    return 0
  fi
  printf 'command failed: %s\n' "$*" >&2
  PROBE_ERRORS+=("$*")
  return 1
}

dd_supports_conv_fsync() {
  dd if=/dev/null of=/dev/null bs=1 count=0 conv=fsync >/dev/null 2>&1
}

print_kv() {
  printf '%s=%s\n' "$1" "$2"
}

detect_os() {
  uname -s 2>/dev/null || printf 'unknown'
}

docker_json() {
  local format="$1"
  shift
  docker inspect --format "$format" "$@" 2>/dev/null || true
}

probe_write_path() {
  local dir="$1"
  local size_mb="$2"
  local file
  file="$(mktemp "$dir/new-api-storage-probe.XXXXXX")"
  cleanup_probe_write_path() {
    [ -n "$file" ] && [ -e "$file" ] && rm -f "$file"
  }
  trap cleanup_probe_write_path RETURN
  printf 'write_path=%s\n' "$dir"
  if have dd; then
    if dd_supports_conv_fsync; then
      run_or_note dd if=/dev/zero of="$file" bs=1048576 count="$size_mb" conv=fsync || true
    else
      printf 'dd_conv_fsync=unsupported\n'
      if run_or_note dd if=/dev/zero of="$file" bs=1048576 count="$size_mb"; then
        if have sync; then
          run_or_note sync || true
        else
          printf 'sync=missing\n'
        fi
      fi
    fi
  else
    printf 'dd=missing\n'
  fi
  cleanup_probe_write_path
  trap - RETURN
}

probe_random_8k_and_fsync() {
  local dir="$1"
  local size_mb="$2"
  local python_bin=""

  if have python3; then
    python_bin="python3"
  elif have python; then
    python_bin="python"
  fi

  if [ -z "$python_bin" ]; then
    printf 'random_8k_write=skipped python-missing\n'
    printf 'fsync_latency=skipped python-missing\n'
    return
  fi

  if ! "$python_bin" - "$dir" "$size_mb" <<'PY'
import math
import os
import random
import statistics
import sys
import tempfile
import time

directory = sys.argv[1]
size_mb = int(sys.argv[2])
size_bytes = size_mb * 1024 * 1024
block_size = 8192
ops = min(1024, max(128, size_bytes // block_size))
payload = b"\0" * block_size

path = None
try:
    fd, path = tempfile.mkstemp(prefix="new-api-storage-rand8k.", dir=directory)
    with os.fdopen(fd, "r+b", buffering=0) as handle:
        handle.truncate(size_bytes)
        os.fsync(handle.fileno())

        write_durations = []
        fsync_durations = []
        combined_durations = []
        start = time.perf_counter()
        for _ in range(ops):
            offset = random.randrange(0, size_bytes // block_size) * block_size
            before = time.perf_counter()
            handle.seek(offset)
            handle.write(payload)
            after_write = time.perf_counter()
            os.fsync(handle.fileno())
            after_fsync = time.perf_counter()
            write_durations.append((after_write - before) * 1000)
            fsync_durations.append((after_fsync - after_write) * 1000)
            combined_durations.append((after_fsync - before) * 1000)
        elapsed = time.perf_counter() - start

    def percentile(values, ratio):
        sorted_values = sorted(values)
        idx = max(0, min(len(sorted_values) - 1, math.ceil(len(sorted_values) * ratio) - 1))
        return sorted_values[idx]

    mib = (ops * block_size) / (1024 * 1024)
    print(f"random_8k_write_ops={ops}")
    print(f"random_8k_write_mib={mib:.2f}")
    print(f"random_8k_write_seconds={elapsed:.3f}")
    print(f"random_8k_write_iops={ops / elapsed:.2f}")
    print(f"write_latency_avg_ms={statistics.mean(write_durations):.3f}")
    print(f"write_latency_p95_ms={percentile(write_durations, 0.95):.3f}")
    print(f"fsync_latency_avg_ms={statistics.mean(fsync_durations):.3f}")
    print(f"fsync_latency_p95_ms={percentile(fsync_durations, 0.95):.3f}")
    print(f"fsync_latency_max_ms={max(fsync_durations):.3f}")
    print(f"write_and_fsync_latency_avg_ms={statistics.mean(combined_durations):.3f}")
    print(f"write_and_fsync_latency_p95_ms={percentile(combined_durations, 0.95):.3f}")
    print(f"write_and_fsync_latency_max_ms={max(combined_durations):.3f}")
except Exception as exc:
    print(f"python_probe_error={type(exc).__name__}: {exc}", file=sys.stderr)
    sys.exit(1)
finally:
    if path:
        try:
            os.unlink(path)
        except FileNotFoundError:
            pass
PY
  then
    PROBE_ERRORS+=("probe_random_8k_and_fsync dir=$dir")
  fi
}

probe_platform() {
  local os_name="$1"
  section "platform"
  print_kv "os" "$os_name"

  case "$os_name" in
    Darwin)
      sw_vers 2>/dev/null || true
      sysctl hw.memsize 2>/dev/null || true
      vm_stat 2>/dev/null | sed -n '1,12p' || true
      df -h / /Volumes 2>/dev/null || true
      if [ -d /Volumes ]; then
        find /Volumes -maxdepth 1 -mindepth 1 -type d -print 2>/dev/null | sort || true
      fi
      ;;
    Linux)
      uname -a 2>/dev/null || true
      free -h 2>/dev/null || true
      swapon --show 2>/dev/null || true
      findmnt -T "$ROOT_DIR" -o TARGET,SOURCE,FSTYPE,OPTIONS 2>/dev/null || true
      df -h "$ROOT_DIR" 2>/dev/null || true
      ;;
    MINGW*|MSYS*|CYGWIN*)
      systeminfo 2>/dev/null | sed -n '1,40p' || true
      wmic logicaldisk get caption,freespace,size 2>/dev/null || true
      ;;
    *)
      uname -a 2>/dev/null || true
      df -h "$ROOT_DIR" 2>/dev/null || true
      ;;
  esac
}

probe_wsl() {
  section "wsl"
  if [ -r /proc/version ] && grep -qi microsoft /proc/version; then
    print_kv "wsl" "true"
    printf 'Prefer paths inside the WSL2 ext4 filesystem. Avoid /mnt/c, OneDrive, and Defender-scanned paths for PGDATA.\n'
    df -h / /mnt/c 2>/dev/null || true
  else
    print_kv "wsl" "false"
  fi
}

probe_docker() {
  section "docker"
  if ! have docker; then
    print_kv "docker" "missing"
    return
  fi

  docker version --format 'client={{.Client.Version}} server={{.Server.Version}}' 2>/dev/null || docker version 2>/dev/null || true
  docker info --format 'data_root={{.DockerRootDir}} driver={{.Driver}} os={{.OperatingSystem}} kernel={{.KernelVersion}}' 2>/dev/null || true
  docker system df 2>/dev/null || true

  section "containers"
  docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}' 2>/dev/null || true

  section "postgres-container"
  local pg_mounts pg_image pg_state
  pg_image="$(docker_json '{{.Config.Image}}' "$POSTGRES_CONTAINER")"
  pg_state="$(docker_json '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}' "$POSTGRES_CONTAINER")"
  pg_mounts="$(docker_json '{{range .Mounts}}{{println .Type .Source "->" .Destination}}{{end}}' "$POSTGRES_CONTAINER")"
  print_kv "postgres_container" "$POSTGRES_CONTAINER"
  print_kv "postgres_image" "${pg_image:-not-found}"
  print_kv "postgres_state" "${pg_state:-not-found}"
  printf '%s\n' "$pg_mounts"

  section "new-api-container"
  local api_image api_state
  api_image="$(docker_json '{{.Config.Image}}' "$NEW_API_CONTAINER")"
  api_state="$(docker_json '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}' "$NEW_API_CONTAINER")"
  print_kv "new_api_container" "$NEW_API_CONTAINER"
  print_kv "new_api_image" "${api_image:-not-found}"
  print_kv "new_api_state" "${api_state:-not-found}"
}

probe_postgres() {
  section "postgres-runtime"
  if ! have docker; then
    return
  fi
  if ! docker inspect "$POSTGRES_CONTAINER" >/dev/null 2>&1; then
    print_kv "postgres_runtime" "container-not-found"
    return
  fi
  local state
  state="$(docker inspect --format '{{.State.Status}}' "$POSTGRES_CONTAINER" 2>/dev/null || true)"
  if [ "$state" != "running" ]; then
    print_kv "postgres_runtime" "container-not-running"
    print_kv "postgres_state" "${state:-unknown}"
    return
  fi
  docker exec "$POSTGRES_CONTAINER" sh -ec '
    echo "uid_gid=$(id)"
    echo "PGDATA=${PGDATA:-/var/lib/postgresql/data}"
    df -h "${PGDATA:-/var/lib/postgresql/data}" 2>/dev/null || true
    du -sh "${PGDATA:-/var/lib/postgresql/data}" 2>/dev/null || true
    psql -U "${POSTGRES_USER:-root}" -d "${POSTGRES_DB:-new-api}" -Atc "select version();" 2>/dev/null || true
    psql -U "${POSTGRES_USER:-root}" -d "${POSTGRES_DB:-new-api}" -Atc "show data_directory; show wal_sync_method; show fsync;" 2>/dev/null || true
  ' 2>/dev/null || true

  section "postgres-fsync-test"
  if [ "$RUN_POSTGRES_FSYNC_TESTS" != "true" ]; then
    printf 'pg_test_fsync=skipped (set RUN_POSTGRES_FSYNC_TESTS=true to run inside PGDATA)\n'
    return
  fi
  case "$PG_TEST_FSYNC_SECONDS" in
    ''|*[!0-9]*)
      printf 'pg_test_fsync=failed invalid PG_TEST_FSYNC_SECONDS: %s\n' "$PG_TEST_FSYNC_SECONDS" >&2
      PROBE_ERRORS+=("postgres pg_test_fsync invalid seconds")
      return
      ;;
  esac
  if ! docker exec "$POSTGRES_CONTAINER" sh -ec '
    if ! command -v pg_test_fsync >/dev/null 2>&1; then
      echo "pg_test_fsync=missing"
      exit 0
    fi
    pgdata="${PGDATA:-/var/lib/postgresql/data}"
    tmpdir="$(mktemp -d "$pgdata/pg_test_fsync.XXXXXX")"
    cleanup_pg_test_fsync() {
      rm -rf "$tmpdir"
    }
    trap cleanup_pg_test_fsync EXIT HUP INT TERM
    echo "pg_test_fsync_dir=$tmpdir"
    cd "$tmpdir"
    pg_test_fsync -s "$1"
  ' sh "$PG_TEST_FSYNC_SECONDS"; then
    PROBE_ERRORS+=("postgres pg_test_fsync")
  fi
}

normalize_probe_settings() {
  case "$PROBE_WRITE_MB" in
    ''|*[!0-9]*)
      printf 'PROBE_WRITE_MB must be a positive integer, got: %s\n' "$PROBE_WRITE_MB" >&2
      PROBE_ERRORS+=("invalid PROBE_WRITE_MB")
      PROBE_WRITE_MB=64
      ;;
  esac
  if [ "$PROBE_WRITE_MB" -lt 8 ]; then
    PROBE_WRITE_MB=8
  fi
}

probe_io() {
  section "io"
  if [ -n "$PROBE_DIR" ]; then
    print_kv "probe_dir" "$PROBE_DIR"
  else
    PROBE_DIR="$ROOT_DIR"
    print_kv "probe_dir" "$PROBE_DIR"
  fi
  df -h "$PROBE_DIR" 2>/dev/null || true

  if [ "$RUN_WRITE_TESTS" != "true" ]; then
    printf 'write_tests=skipped (set RUN_WRITE_TESTS=true to write a temporary probe file)\n'
    return
  fi

  if [ ! -d "$PROBE_DIR" ]; then
    printf 'write_tests=failed probe_dir_not_found: %s\n' "$PROBE_DIR" >&2
    PROBE_ERRORS+=("probe_io probe_dir_not_found=$PROBE_DIR")
    return 0
  fi
  if [ ! -w "$PROBE_DIR" ]; then
    printf 'write_tests=failed probe_dir_not_writable: %s\n' "$PROBE_DIR" >&2
    PROBE_ERRORS+=("probe_io probe_dir_not_writable=$PROBE_DIR")
    return 0
  fi

  probe_write_path "$PROBE_DIR" "$PROBE_WRITE_MB"
  probe_random_8k_and_fsync "$PROBE_DIR" "$PROBE_WRITE_MB"
}

main() {
  cd "$ROOT_DIR"
  normalize_probe_settings
  local os_name
  os_name="$(detect_os)"
  section "summary"
  print_kv "root_dir" "$ROOT_DIR"
  print_kv "date_utc" "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  print_kv "run_write_tests" "$RUN_WRITE_TESTS"
  print_kv "run_postgres_fsync_tests" "$RUN_POSTGRES_FSYNC_TESTS"
  print_kv "probe_write_mb_requested" "$PROBE_WRITE_MB_REQUESTED"
  print_kv "probe_write_mb_actual" "$PROBE_WRITE_MB"
  probe_platform "$os_name"
  probe_wsl
  probe_docker
  probe_postgres
  probe_io
  if [ "${#PROBE_ERRORS[@]}" -gt 0 ]; then
    section "probe-errors"
    printf '%s\n' "${PROBE_ERRORS[@]}"
    exit 1
  fi
}

main "$@"
