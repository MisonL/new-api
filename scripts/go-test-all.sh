#!/usr/bin/env bash
set -euo pipefail

# 枚举仓库文件，每行一个相对路径。
# 优先 rg（本地快）；没有 rg 时退回 git ls-files / find —— GitHub 的 ubuntu
# runner 不预装 ripgrep，CI 门禁不能因为枚举工具缺失就直接红。
list_files() {
  if [ "${1:-}" = "--go-mod" ]; then
    shift
    if command -v rg >/dev/null 2>&1; then
      rg --files --no-ignore-vcs -g 'go.mod'
    elif git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
      { git ls-files 2>/dev/null || true; git ls-files --others --exclude-standard 2>/dev/null || true; } |
        sort -u | grep -E '(^|/)go\.mod$' || true
    else
      find . -name go.mod -type f | sed 's|^\./||'
    fi
    return
  fi
  if command -v rg >/dev/null 2>&1; then
    rg --files --no-ignore-vcs "$@"
    return
  fi
  if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    # 已跟踪 + 未跟踪（新增的 .go 文件也要测）。
    { git ls-files; git ls-files --others --exclude-standard; } | sort -u
    return
  fi
  find . -type f | sed 's|^\./||'
}

# 与 rg 分支等价的路径过滤（stdin -> stdout）。前缀列表与 rg 分支的
# -g '!xxx/**' 一一对应，改一处要同步另一处。
filter_non_source() {
  grep -vE '^(bin|node_modules|web/node_modules|web/default/node_modules|web/classic/node_modules|web/default/dist|web/classic/dist|\.claude|\.worktrees)/' |
    grep -vE '(^|/)(testdata|_[^/]*)/'
}

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
file_list="$(mktemp)"
trap 'rm -f "$package_file" "$file_list"' EXIT
trap 'rm -f "$package_file" "$file_list"; exit 130' INT
trap 'rm -f "$package_file" "$file_list"; exit 143' TERM

# 嵌套 Go 模块（子目录自带 go.mod，如 tools/relaykit-dto-compat）不能按主模块
# 的包路径枚举，否则 go test 报 "does not contain package" 整体 FAIL。先找出
# 全部嵌套模块，把它们的目录从主枚举排除，结尾再按各自 go.mod 分跑。
nested_module_dirs=()
module_list="$(list_files --go-mod)"
while IFS= read -r module_file; do
  [ -n "$module_file" ] || continue
  module_dir="$(dirname "$module_file")"
  [ "$module_dir" = "." ] && continue
  nested_module_dirs+=("$module_dir")
done <<< "$module_list"
if command -v rg >/dev/null 2>&1; then
  go_globs=(
    -g '*.go'
    -g '!bin/**'
    -g '!web/node_modules/**'
    -g '!web/default/node_modules/**'
    -g '!web/classic/node_modules/**'
    -g '!web/default/dist/**'
    -g '!web/classic/dist/**'
    -g '!.claude/worktrees/**'
    -g '!.worktrees/**'
    -g '!**/testdata/**'
    -g '!**/_*/**'
    -g '!node_modules/**'
  )
  for module_dir in "${nested_module_dirs[@]}"; do
    go_globs+=(-g "!$module_dir/**")
  done
  list_files "${go_globs[@]}" > "$file_list"
else
  list_files | filter_non_source | grep '\.go$' | {
    if [ "${#nested_module_dirs[@]}" -gt 0 ]; then
      while IFS= read -r path; do
        skip=0
        for module_dir in "${nested_module_dirs[@]}"; do
          case "$path" in
            "$module_dir"/*) skip=1; break ;;
          esac
        done
        [ "$skip" -eq 1 ] && continue
        printf '%s\n' "$path"
      done
    else
      cat
    fi
  } | sort -u > "$file_list"
fi

# 归并到包目录（同目录多文件只测一次）。
awk 'BEGIN{FS="/"} {if (NF == 1) print "."; else {pkg=$1; for (i = 2; i < NF; i++) pkg = pkg "/" $i; print "./" pkg}}' "$file_list" |
  sort -u > "$package_file"

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

if [ "${#nested_module_dirs[@]}" -gt 0 ]; then
  for module_dir in "${nested_module_dirs[@]}"; do
    echo "testing nested module $module_dir"
    (cd "$module_dir" && go test ./... "$@")
  done
fi
