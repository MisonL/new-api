#!/usr/bin/env sh
set -eu
LC_ALL=C
export LC_ALL

# Writes schema 1 release metadata. See docs/operations/frontend-release-metadata.md.

usage() {
  echo "usage: write-frontend-release-metadata.sh <frontend> <dist-dir> <version> [build-commit] [build-date]" >&2
}

frontend="${1:-}"
dist_dir="${2:-}"
version="${3:-}"
build_commit="${4:-unknown}"
build_date="${5:-unknown}"

if [ "$#" -lt 3 ] || [ "$#" -gt 5 ] || [ -z "$frontend" ] || [ -z "$dist_dir" ] || [ -z "$version" ]; then
  usage
  exit 1
fi

case "$frontend" in
  default|classic) ;;
  *)
    echo "invalid frontend: $frontend" >&2
    exit 1
    ;;
esac

if [ ! -d "$dist_dir" ]; then
  echo "missing dist directory for $frontend: $dist_dir" >&2
  exit 1
fi

validate_release_field() {
  field_name="$1"
  field_value="$2"
  case "$field_value" in
    *[!-A-Za-z0-9._:@/+~]*)
      echo "invalid $field_name: only release-safe ASCII characters are allowed" >&2
      exit 1
      ;;
  esac
}

validate_release_field "version" "$version"
validate_release_field "build commit" "$build_commit"
validate_release_field "build date" "$build_date"

json_escape() {
  awk 'BEGIN {
    for (i = 1; i < ARGC; i++) {
      value = ARGV[i]
      gsub(/\\/, "\\\\", value)
      gsub(/"/, "\\\"", value)
      gsub(/\r/, "\\r", value)
      gsub(/\t/, "\\t", value)
      gsub(/\n/, "\\n", value)
      printf "%s", value
    }
  }' "$1"
}

frontend_json="$(json_escape "$frontend")"
version_json="$(json_escape "$version")"
build_commit_json="$(json_escape "$build_commit")"
build_date_json="$(json_escape "$build_date")"

metadata_file="$dist_dir/new-api-release.json"
tmp_file="$(mktemp "${metadata_file}.tmp.XXXXXX")" || {
  echo "failed to create temp file for $metadata_file" >&2
  exit 1
}
trap 'rm -f "$tmp_file"' EXIT
trap 'rm -f "$tmp_file"; exit 129' HUP
trap 'rm -f "$tmp_file"; exit 130' INT
trap 'rm -f "$tmp_file"; exit 143' TERM

if ! cat > "$tmp_file" <<EOF
{
  "schema": 1,
  "app": "new-api",
  "frontend": "$frontend_json",
  "version": "$version_json",
  "build_commit": "$build_commit_json",
  "build_date": "$build_date_json"
}
EOF
then
  echo "failed to write frontend release metadata for $frontend: $tmp_file" >&2
  exit 1
fi

if ! mv "$tmp_file" "$metadata_file"; then
  echo "failed to install frontend release metadata for $frontend: $metadata_file" >&2
  exit 1
fi
