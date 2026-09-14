#!/usr/bin/env python3
"""Send a sanitized new-api monitor status to an explicit HTTP(S) endpoint."""

from __future__ import annotations

import argparse
import json
import os
import re
import stat
import sys
from pathlib import Path
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.parse import urlparse
from urllib.request import Request, urlopen


MAX_STATUS_BYTES = 65536
STATUS_VALUES = {"pass", "warn", "fail"}
SAFE_KEYS = (
    "schema_version",
    "checked_at_utc",
    "monitor_status",
    "exit_code",
    "watchdog_exit_code",
    "resource_failure",
    "resource_warning",
    "resource_failure_reasons",
    "resource_warning_reasons",
    "probe_snapshot_status",
    "probe_state",
    "probe_docker_daemon",
    "probe_postgres",
    "probe_redis",
    "probe_postgres_docker_health",
    "probe_redis_docker_health",
    "probe_backend",
    "probe_public",
    "probe_lan",
    "probe_app_state",
    "probe_app_health",
    "probe_app_restart_count",
    "probe_app_oom",
    "probe_failure_reason",
    "probe_action",
)
PROBE_KEYS = tuple(key for key in SAFE_KEYS if key.startswith("probe_"))
KEY_PATTERN = re.compile(r"^[a-z][a-z0-9_]*$")
TIMESTAMP_PATTERN = re.compile(r"^[0-9T:+.Z-]{10,64}$")
RESOURCE_URL_PATTERN = re.compile(r"https?://[^;,)\s]+")
RESOURCE_PATH_PATTERN = re.compile(r"(?P<prefix>^|(?<=[\s:=]))(?P<path>(?:~)?/(?:[^\s:;,)]*))")


class AlertError(RuntimeError):
    """Raised when the alert input or endpoint is unsafe or invalid."""


def read_status(path: Path) -> dict[str, str]:
    try:
        metadata = path.stat()
    except OSError as exc:
        raise AlertError(f"status_file_unreadable:{type(exc).__name__}") from exc
    if not stat.S_ISREG(metadata.st_mode):
        raise AlertError("status_file_not_regular")
    if stat.S_IMODE(metadata.st_mode) & 0o077:
        raise AlertError("status_file_permissions_too_open")
    if metadata.st_size > MAX_STATUS_BYTES:
        raise AlertError("status_file_too_large")
    try:
        raw = path.read_text(encoding="utf-8")
    except (OSError, UnicodeError) as exc:
        raise AlertError(f"status_file_unreadable:{type(exc).__name__}") from exc

    values: dict[str, str] = {}
    for line in raw.splitlines():
        key, separator, value = line.partition("=")
        if not separator or not KEY_PATTERN.fullmatch(key):
            continue
        if key in SAFE_KEYS:
            values[key] = value[:256]

    for required in ("schema_version", "checked_at_utc", "monitor_status", "exit_code"):
        if not values.get(required):
            raise AlertError(f"status_field_missing:{required}")
    if values["schema_version"] != "1":
        raise AlertError("status_schema_unsupported")
    if values["monitor_status"] not in STATUS_VALUES:
        raise AlertError("monitor_status_invalid")
    if not TIMESTAMP_PATTERN.fullmatch(values["checked_at_utc"]):
        raise AlertError("checked_at_invalid")
    try:
        int(values["exit_code"])
    except ValueError as exc:
        raise AlertError("exit_code_invalid") from exc
    return values


def sanitize_resource_reason(value: str) -> str:
    """Keep reason categories and metrics while removing host paths and URLs."""
    sanitized = RESOURCE_URL_PATTERN.sub("<redacted>", value)
    return RESOURCE_PATH_PATTERN.sub(lambda match: f"{match.group('prefix')}<redacted>", sanitized)


def build_payload(values: dict[str, str]) -> dict[str, Any]:
    resource = {
        key.removeprefix("resource_"): sanitize_resource_reason(values[key])
        for key in (
            "resource_failure",
            "resource_warning",
            "resource_failure_reasons",
            "resource_warning_reasons",
        )
        if key in values
    }
    probes = {key.removeprefix("probe_"): values[key] for key in PROBE_KEYS if key in values}
    payload: dict[str, Any] = {
        "schema_version": 1,
        "source": "new-api-macos-tun-monitor",
        "checked_at_utc": values["checked_at_utc"],
        "status": values["monitor_status"],
        "exit_code": int(values["exit_code"]),
        "resource": resource,
        "probes": probes,
    }
    if "watchdog_exit_code" in values:
        payload["watchdog_exit_code"] = int(values["watchdog_exit_code"])
    return payload


def validate_endpoint(url: str) -> None:
    parsed = urlparse(url)
    if parsed.scheme not in {"http", "https"} or not parsed.hostname:
        raise AlertError("alert_endpoint_must_be_http_or_https")
    if parsed.username or parsed.password:
        raise AlertError("alert_endpoint_must_not_contain_credentials")


def validate_bearer_transport(url: str, bearer_token: str) -> None:
    if bearer_token and urlparse(url).scheme != "https":
        raise AlertError("alert_bearer_requires_https")


def post_payload(url: str, payload: dict[str, Any], timeout: float, bearer_token: str) -> int:
    validate_endpoint(url)
    validate_bearer_transport(url, bearer_token)
    body = json.dumps(payload, ensure_ascii=True, separators=(",", ":")).encode("utf-8")
    headers = {
        "Content-Type": "application/json",
        "User-Agent": "new-api-monitor-alert/1",
    }
    if bearer_token:
        headers["Authorization"] = f"Bearer {bearer_token}"
    request = Request(url, data=body, headers=headers, method="POST")
    try:
        with urlopen(request, timeout=timeout) as response:
            status = int(response.status)
            response.read(4096)
    except HTTPError as exc:
        raise AlertError(f"alert_http_status:{exc.code}") from exc
    except (OSError, URLError, TimeoutError) as exc:
        raise AlertError(f"alert_request_failed:{type(exc).__name__}") from exc
    if status < 200 or status >= 300:
        raise AlertError(f"alert_http_status:{status}")
    return status


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--status-file",
        default=os.environ.get("NEW_API_MONITOR_STATUS_FILE", ""),
    )
    parser.add_argument(
        "--url",
        default=os.environ.get("NEW_API_MONITOR_ALERT_URL", ""),
    )
    parser.add_argument(
        "--bearer-token",
        default=os.environ.get("NEW_API_MONITOR_ALERT_BEARER", ""),
    )
    parser.add_argument(
        "--timeout",
        type=float,
        default=float(os.environ.get("NEW_API_MONITOR_ALERT_TIMEOUT_SECONDS", "5")),
    )
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--pretty", action="store_true")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if not args.status_file:
        print("alert_status=error reason=status_file_required", file=sys.stderr)
        return 2
    if args.timeout <= 0:
        print("alert_status=error reason=invalid_timeout", file=sys.stderr)
        return 2
    try:
        values = read_status(Path(args.status_file))
        payload = build_payload(values)
        if args.dry_run:
            print("alert_status=dry-run")
            print(json.dumps(payload, ensure_ascii=True, indent=2 if args.pretty else None, sort_keys=True))
            return 0
        if not args.url:
            print("alert_status=disabled reason=endpoint_unconfigured")
            return 0
        response_status = post_payload(args.url, payload, args.timeout, args.bearer_token)
    except AlertError as exc:
        print(f"alert_status=error reason={exc}", file=sys.stderr)
        return 2
    except ValueError:
        print("alert_status=error reason=invalid_timeout", file=sys.stderr)
        return 2
    print(f"alert_status=sent http_status={response_status} status={values['monitor_status']}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
