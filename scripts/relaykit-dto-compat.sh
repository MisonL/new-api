#!/usr/bin/env bash
# Flatten the local dto and the relaykit dto from git, then compare them
# across every dimension a name-only field diff misses. Run it after bumping
# the pinned upstream relaykit commit to see exactly what widened.
#
# Usage:
#   scripts/relaykit-dto-compat.sh [--strict] [--markdown]
#
# --strict exits non-zero unless the counts match the expectation recorded in
# the design doc (section 12.0.6). Use it in CI once the relaykit module lands.
#
# Local side is read from HEAD, never the worktree: other sessions edit this
# checkout concurrently, and a worktree read would move the numbers under us.
set -euo pipefail

LOCAL_REF="${LOCAL_REF:-HEAD}"
KIT_REF="${KIT_REF:-upstream/main}"
KIT_DIR="${KIT_DIR:-relaykit/dto}"
LOCAL_DIR="${LOCAL_DIR:-dto}"

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
trap 'rm -rf "$tmp"; exit 130' INT
trap 'rm -rf "$tmp"; exit 143' TERM

dump_tree() {
  local ref="$1" dir="$2" out="$3"
  mkdir -p "$out"
  local count=0
  while IFS= read -r f; do
    case "$f" in
      *_test.go) continue ;;
      *.go) ;;
      *) continue ;;
    esac
    git show "$ref:$f" > "$out/$(basename "$f")"
    count=$((count + 1))
  done < <(git ls-tree -r --name-only "$ref" -- "$dir/")
  if [ "$count" -eq 0 ]; then
    echo "no Go sources found at $ref:$dir" >&2
    exit 1
  fi
  echo "$count"
}

local_count="$(dump_tree "$LOCAL_REF" "$LOCAL_DIR" "$tmp/local")"
kit_count="$(dump_tree "$KIT_REF" "$KIT_DIR" "$tmp/kit")"
echo "local=$LOCAL_REF:$LOCAL_DIR ($local_count files)  kit=$KIT_REF:$KIT_DIR ($kit_count files)" >&2

args=(-local "$tmp/local" -kit "$tmp/kit")
for a in "$@"; do
  case "$a" in
    --strict) args+=(-strict) ;;
    --markdown) args+=(-format markdown) ;;
    *) echo "unknown flag: $a" >&2; exit 2 ;;
  esac
done

# The tool is its own module so it stays out of the root module's ./... build;
# run it from its directory.
cd "$repo_root/tools/relaykit-dto-compat"
go run . "${args[@]}"
