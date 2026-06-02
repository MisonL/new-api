#!/usr/bin/env bash
set -euo pipefail

if ! command -v rg >/dev/null 2>&1; then
  echo "rg is required to enumerate Go packages without scanning frontend artifacts" >&2
  exit 1
fi

find_module_root() {
  local dir="$PWD"
  while [ "$dir" != "/" ]; do
    if [ -f "$dir/go.mod" ]; then
      printf '%s\n' "$dir"
      return 0
    fi
    dir="$(dirname "$dir")"
  done
  return 1
}

module_root="$(find_module_root)" || {
  echo "go.mod not found; run this script from inside the Go module" >&2
  exit 1
}
cd "$module_root"

package_file="$(mktemp)"
rg_output="$(mktemp)"
trap 'rm -f "$package_file" "$rg_output"' EXIT
trap 'rm -f "$package_file" "$rg_output"; exit 130' INT
trap 'rm -f "$package_file" "$rg_output"; exit 143' TERM

set +e
rg --files --no-ignore-vcs \
  -g '*.go' \
  -g '!bin/**' \
  -g '!web/node_modules/**' \
  -g '!web/default/node_modules/**' \
  -g '!web/classic/node_modules/**' \
  -g '!web/default/dist/**' \
  -g '!web/classic/dist/**' \
  -g '!.claude/worktrees/**' \
  -g '!.worktrees/**' \
  -g '!**/testdata/**' \
  -g '!**/_*/**' \
  -g '!node_modules/**' > "$rg_output"
rg_status=$?
set -e

if [ "$rg_status" -eq 1 ]; then
  : > "$package_file"
elif [ "$rg_status" -ne 0 ]; then
  echo "rg failed while enumerating Go packages with status $rg_status" >&2
  exit "$rg_status"
else
  awk 'BEGIN{FS="/"} {if (NF == 1) print "."; else {pkg=$1; for (i = 2; i < NF; i++) pkg = pkg "/" $i; print "./" pkg}}' "$rg_output" |
    sort -u > "$package_file"
fi

package_count="$(wc -l < "$package_file" | tr -d ' ')"
if [ "$package_count" -eq 0 ]; then
  echo "no Go packages found" >&2
  exit 1
fi

echo "testing $package_count Go packages"

packages=()
while IFS= read -r package; do
  packages+=("$package")
done < "$package_file"
go test "${packages[@]}" "$@"
