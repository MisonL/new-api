#!/usr/bin/env bash
set -euo pipefail

if ! command -v rg >/dev/null 2>&1; then
  echo "rg is required to enumerate Go packages without scanning frontend artifacts" >&2
  exit 1
fi

find_module_root() {
  if [ -n "${REPO_ROOT:-}" ]; then
    if [ -f "$REPO_ROOT/go.mod" ]; then
      printf '%s\n' "$REPO_ROOT"
      return 0
    fi
    echo "REPO_ROOT does not contain go.mod, falling back to auto-detect: $REPO_ROOT" >&2
  fi
  if git_root="$(git rev-parse --show-toplevel 2>/dev/null)" && [ -f "$git_root/go.mod" ]; then
    printf '%s\n' "$git_root"
    return 0
  fi
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
# 嵌套 Go 模块（tools/relaykit-dto-compat）不能按主模块的包路径枚举，否则
# go test 报 "does not contain package" 而整体 FAIL。主循环只扫主模块，
# 嵌套模块在结尾用自己的 go.mod 单独跑。
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
  -g '!node_modules/**' \
  -g '!tools/relaykit-dto-compat/**' > "$rg_output"
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

# 嵌套模块：每个子目录有自己的 go.mod，必须在它自己的模块根里跑。
nested_modules=()
while IFS= read -r module_file; do
  # 根模块已在主循环覆盖；rg 对根文件输出 "go.mod" 而非 "./go.mod"，
  # glob 排不掉，这里显式跳过。
  [ "$module_file" = "go.mod" ] && continue
  [ -n "$module_file" ] && nested_modules+=("$(dirname "$module_file")")
done < <(rg --files --no-ignore-vcs -g 'go.mod' -g '!web/**' -g '!node_modules/**' -g '!.claude/**' -g '!.worktrees/**')

if [ "${#nested_modules[@]}" -gt 0 ]; then
  for module_dir in "${nested_modules[@]}"; do
    echo "testing nested module $module_dir"
    (cd "$module_dir" && go test ./... "$@")
  done
fi
