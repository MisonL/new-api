#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-postgres}"
POSTGRES_IMAGE="${POSTGRES_IMAGE:-postgres:15}"

section() {
  printf '\n## %s\n' "$1"
}

print_kv() {
  printf '%s=%s\n' "$1" "$2"
}

have() {
  command -v "$1" >/dev/null 2>&1
}

is_wsl() {
  [ -r /proc/version ] && grep -qi microsoft /proc/version
}

check_path() {
  local name="$1"
  local path="${2:-}"
  section "$name"
  if [ -z "$path" ]; then
    print_kv "value" "unset"
    return
  fi
  print_kv "value" "$path"
  case "$path" in
    /*) print_kv "absolute" "true" ;;
    *) print_kv "absolute" "false" ;;
  esac
  case "$path" in
    *"/../"*|*"/./"*|*"/.."|*"/."|../*|./*|".."|".")
      print_kv "normalized" "false"
      ;;
    *)
      print_kv "normalized" "true"
      ;;
  esac
  if is_wsl; then
    case "$path" in
      /mnt/*) print_kv "wsl_path_warning" "avoid-windows-mounted-filesystems-for-pgdata" ;;
      *) print_kv "wsl_path_warning" "none" ;;
    esac
  fi
  if [ -d "$path" ]; then
    print_kv "exists" "true"
    print_kv "writable" "$([ -w "$path" ] && printf true || printf false)"
    df -h "$path" 2>/dev/null || true
    findmnt -T "$path" -o TARGET,SOURCE,FSTYPE,OPTIONS 2>/dev/null || true
    stat -c 'owner=%U group=%G mode=%a' "$path" 2>/dev/null || true
  else
    print_kv "exists" "false"
    local parent
    parent="$(dirname "$path")"
    if [ -d "$parent" ]; then
      print_kv "parent_exists" "true"
      print_kv "parent_writable" "$([ -w "$parent" ] && printf true || printf false)"
      df -h "$parent" 2>/dev/null || true
      findmnt -T "$parent" -o TARGET,SOURCE,FSTYPE,OPTIONS 2>/dev/null || true
    else
      print_kv "parent_exists" "false"
    fi
  fi
}

check_postgres_identity() {
  section "postgres-identity"
  if ! have docker; then
    print_kv "docker" "missing"
    return
  fi
  if docker inspect "$POSTGRES_CONTAINER" >/dev/null 2>&1; then
    docker exec "$POSTGRES_CONTAINER" sh -ec 'id; id postgres 2>/dev/null || true' 2>/dev/null || true
    return
  fi
  if docker image inspect "$POSTGRES_IMAGE" >/dev/null 2>&1; then
    docker run --rm --entrypoint id "$POSTGRES_IMAGE" postgres 2>/dev/null || true
    return
  fi
  print_kv "postgres_identity" "skipped-image-not-local"
}

check_docker() {
  section "docker"
  if ! have docker; then
    print_kv "docker" "missing"
    return
  fi
  docker info --format 'data_root={{.DockerRootDir}} driver={{.Driver}} os={{.OperatingSystem}} kernel={{.KernelVersion}}' 2>/dev/null || true
}

print_recommendation() {
  section "recommendation"
  printf 'standard=move NEW_API_POSTGRES_DATA_DIR to a fast local SSD path with a marker file\n'
  printf 'logs=set NEW_API_LOG_DIR to a fast local path when log writes are heavy\n'
  printf 'advanced=separate pg_wal only with a stopped-cluster or initdb-level plan\n'
  printf 'expert=Docker data-root, tablespaces, LVM cache, bcache, or ZFS tuning are host administration tasks\n'
  if is_wsl; then
    printf 'wsl2=prefer the distro ext4 filesystem; avoid /mnt/c, OneDrive, and Defender-scanned paths for PGDATA\n'
  fi
}

main() {
  cd "$ROOT_DIR"
  section "summary"
  print_kv "root_dir" "$ROOT_DIR"
  print_kv "date_utc" "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  print_kv "os" "$(uname -s 2>/dev/null || printf unknown)"
  print_kv "wsl" "$(is_wsl && printf true || printf false)"
  check_docker
  check_postgres_identity
  check_path "NEW_API_POSTGRES_DATA_DIR" "${NEW_API_POSTGRES_DATA_DIR:-}"
  check_path "NEW_API_POSTGRES_WAL_DIR" "${NEW_API_POSTGRES_WAL_DIR:-}"
  check_path "NEW_API_DATA_DIR" "${NEW_API_DATA_DIR:-}"
  check_path "NEW_API_LOG_DIR" "${NEW_API_LOG_DIR:-}"
  check_path "NEW_API_DOCKER_DATA_DIR" "${NEW_API_DOCKER_DATA_DIR:-}"
  print_recommendation
}

main "$@"
