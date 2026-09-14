#!/usr/bin/env python3
"""Read-only, bounded Time Machine status report for the new-api host."""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


class TimeMachineReportError(RuntimeError):
    """Raised when a Time Machine command cannot be read safely."""


def run_command(command: list[str], timeout: float) -> str:
    try:
        result = subprocess.run(
            command,
            check=False,
            capture_output=True,
            text=True,
            timeout=timeout,
        )
    except FileNotFoundError as exc:
        raise TimeMachineReportError(f"command_not_found:{command[0]}") from exc
    except subprocess.TimeoutExpired as exc:
        raise TimeMachineReportError(f"command_timeout:{command[0]}") from exc
    if result.returncode != 0:
        raise TimeMachineReportError(f"command_failed:{command[0]}:{result.returncode}")
    return result.stdout


def parse_scalar(output: str, key: str) -> str | None:
    match = re.search(rf"\b{re.escape(key)}\s*=\s*([^;]+);", output)
    if match is None:
        return None
    return match.group(1).strip().strip('"')


def parse_number(output: str, key: str) -> float | None:
    value = parse_scalar(output, key)
    if value is None:
        return None
    try:
        return float(value)
    except ValueError:
        return None


def parse_integer(output: str, key: str) -> int | None:
    value = parse_number(output, key)
    if value is None or not value.is_integer():
        return None
    return int(value)


def parse_status(output: str) -> dict[str, Any]:
    phase = parse_scalar(output, "BackupPhase")
    running = parse_integer(output, "Running")
    percent = parse_number(output, "Percent")
    total_bytes = parse_integer(output, "totalBytes")
    completed_bytes = parse_integer(output, "bytes")
    total_files = parse_integer(output, "totalFiles")
    completed_files = parse_integer(output, "files")
    time_remaining = parse_number(output, "TimeRemaining")
    if running not in {0, 1}:
        raise TimeMachineReportError("status_fields_missing")
    if phase is None:
        if running == 0:
            phase = "Idle"
        else:
            raise TimeMachineReportError("status_fields_missing")
    return {
        "phase": phase,
        "running": running,
        "percent": round(percent * 100, 4)
        if percent is not None and 0 <= percent <= 1
        else percent,
        "time_remaining_seconds": time_remaining,
        "completed_bytes": completed_bytes,
        "total_bytes": total_bytes,
        "completed_files": completed_files,
        "total_files": total_files,
    }


def parse_excluded(output: str) -> str:
    match = re.search(r"^\[(Included|Excluded)\]", output, flags=re.MULTILINE)
    if match is None:
        raise TimeMachineReportError("path_status_missing")
    return match.group(1)


def build_report(
    status_output: str,
    path_statuses: dict[str, str],
    checked_at_utc: str,
    required_included: tuple[str, ...] = (),
) -> dict[str, Any]:
    required_excluded = sorted(
        label for label in required_included if path_statuses.get(label) != "Included"
    )
    return {
        "schema_version": 1,
        "checked_at_utc": checked_at_utc,
        "time_machine": parse_status(status_output),
        "paths": dict(sorted(path_statuses.items())),
        "coverage": {
            "required_included": list(required_included),
            "required_excluded": required_excluded,
            "status": "fail" if required_excluded else "pass",
        },
        "evidence_limits": {
            "destination_details_omitted": True,
            "raw_paths_omitted": True,
            "read_only": True,
        },
    }


def parse_path_spec(spec: str) -> tuple[str, str]:
    label, separator, path = spec.partition("=")
    if not separator or not re.fullmatch(r"[A-Za-z0-9_.-]+", label) or not path:
        raise ValueError("path_spec_must_be_label_equals_path")
    return label, path


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--tmutil-bin",
        default=os.environ.get("NEW_API_TMUTIL_BIN", "tmutil"),
    )
    parser.add_argument(
        "--timeout",
        type=float,
        default=float(os.environ.get("NEW_API_TMUTIL_TIMEOUT_SECONDS", "5")),
    )
    parser.add_argument(
        "--path",
        action="append",
        dest="paths",
        metavar="LABEL=PATH",
        help="Path to query; only LABEL is emitted in the report.",
    )
    parser.add_argument(
        "--require-included",
        action="append",
        dest="required_included",
        metavar="LABEL",
        help="Fail when a queried label is not included in Time Machine.",
    )
    parser.add_argument("--pretty", action="store_true")
    return parser.parse_args()


def default_paths() -> list[tuple[str, str]]:
    root = Path(__file__).resolve().parents[1]
    return [
        ("logs", str(root / "logs")),
        ("data", str(root / "data")),
        ("repo", str(root)),
    ]


def main() -> int:
    args = parse_args()
    if args.timeout <= 0:
        print("tm_report_status=error reason=invalid_timeout", file=sys.stderr)
        return 2
    try:
        specs = (
            [parse_path_spec(spec) for spec in args.paths]
            if args.paths
            else default_paths()
        )
        required_included = tuple(dict.fromkeys(args.required_included or ()))
        labels = {label for label, _path in specs}
        missing_labels = sorted(set(required_included) - labels)
        if missing_labels:
            raise ValueError(
                "required_path_label_missing:" + ",".join(missing_labels)
            )
        status_output = run_command([args.tmutil_bin, "status"], args.timeout)
        path_statuses = {
            label: parse_excluded(
                run_command([args.tmutil_bin, "isexcluded", path], args.timeout)
            )
            for label, path in specs
        }
        report = build_report(
            status_output,
            path_statuses,
            datetime.now(timezone.utc).isoformat(timespec="seconds").replace(
                "+00:00", "Z"
            ),
            required_included,
        )
    except (TimeMachineReportError, ValueError) as exc:
        print(f"tm_report_status=error reason={exc}", file=sys.stderr)
        return 2

    indent = 2 if args.pretty else None
    required_excluded = report["coverage"]["required_excluded"]
    if required_excluded:
        print(
            "tm_report_status=fail reason=required-path-excluded:"
            + ",".join(required_excluded)
        )
        print(json.dumps(report, ensure_ascii=True, indent=indent, sort_keys=True))
        return 1

    print("tm_report_status=pass")
    print(json.dumps(report, ensure_ascii=True, indent=indent, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
