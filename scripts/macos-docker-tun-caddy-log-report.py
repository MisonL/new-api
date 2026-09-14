#!/usr/bin/env python3
"""Read-only classifier for the Caddy reverse-proxy JSON log."""

from __future__ import annotations

import argparse
import json
import os
import sys
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Iterable, Optional


CLASSIFICATIONS = (
    "context_canceled",
    "connection_refused",
    "upstream_terminated",
    "http_5xx",
    "http_4xx",
    "http_2xx",
    "http_other",
    "other_error",
    "informational",
)


def classify_record(record: dict[str, Any]) -> str:
    message = " ".join(
        str(record.get(key) or "")
        for key in ("msg", "error", "err")
    ).lower()
    if "context canceled" in message or "client gone" in message:
        return "context_canceled"
    if "connection refused" in message or "connect: connection refused" in message:
        return "connection_refused"
    if any(
        marker in message
        for marker in (
            "unexpected eof",
            "connection reset by peer",
            "broken pipe",
            "stream reset",
            "upstream reset",
        )
    ) or message.rstrip().endswith(" eof") or message == "eof":
        return "upstream_terminated"

    status = record.get("status")
    if isinstance(status, bool):
        status = None
    if isinstance(status, (int, float)):
        if 500 <= status <= 599:
            return "http_5xx"
        if 400 <= status <= 499:
            return "http_4xx"
        if 200 <= status <= 299:
            return "http_2xx"
        if 100 <= status <= 999:
            return "http_other"
    if record.get("level") in {"error", "warn"} or message:
        return "other_error"
    return "informational"


def parse_status(record: dict[str, Any]) -> Optional[int]:
    status = record.get("status")
    if isinstance(status, bool):
        return None
    if isinstance(status, (int, float)) and int(status) == status:
        return int(status)
    return None


def parse_timestamp(record: dict[str, Any]) -> Optional[float]:
    timestamp = record.get("ts")
    if isinstance(timestamp, bool):
        return None
    if isinstance(timestamp, (int, float)):
        return float(timestamp)
    return None


def duration_seconds(record: dict[str, Any]) -> Optional[float]:
    duration = record.get("duration")
    if isinstance(duration, bool):
        return None
    if isinstance(duration, (int, float)) and duration >= 0:
        return float(duration)
    return None


def empty_report(checked_at_utc: str, source_name: str) -> dict[str, Any]:
    return {
        "schema_version": 1,
        "checked_at_utc": checked_at_utc,
        "source_name": source_name,
        "records": 0,
        "malformed_lines": 0,
        "timestamp_missing": 0,
        "status_missing": 0,
        "classification_counts": {classification: 0 for classification in CLASSIFICATIONS},
        "http_status_counts": {},
        "http_status_counts_by_classification": {
            classification: {}
            for classification in ("http_5xx", "http_4xx", "http_2xx", "http_other")
        },
        "duration_seconds": {
            "count": 0,
            "total": 0.0,
            "max": 0.0,
        },
        "evidence_limits": {
            "context_canceled_requires_application_correlation": True,
            "listener_health_requires_socket_and_http_probe": True,
            "raw_request_fields_omitted": True,
            "sensitive_request_values_omitted": True,
        },
        "sensitive_request_observations": {
            "nonempty_headers": 0,
            "query_in_uri": 0,
        },
    }


def build_report(
    lines: Iterable[str],
    checked_at_utc: str,
    source_name: str,
    since_epoch: Optional[float] = None,
    until_epoch: Optional[float] = None,
) -> dict[str, Any]:
    report = empty_report(checked_at_utc, source_name)
    classifications: Counter[str] = Counter()
    status_counts: Counter[str] = Counter()
    classified_status_counts: dict[str, Counter[str]] = {
        classification: Counter()
        for classification in ("http_5xx", "http_4xx", "http_2xx", "http_other")
    }
    duration_count = 0
    duration_total = 0.0
    duration_max = 0.0

    for line in lines:
        try:
            record = json.loads(line)
        except (json.JSONDecodeError, TypeError):
            report["malformed_lines"] += 1
            continue
        if not isinstance(record, dict):
            report["malformed_lines"] += 1
            continue

        timestamp = parse_timestamp(record)
        if timestamp is None:
            report["timestamp_missing"] += 1
        elif since_epoch is not None and timestamp < since_epoch:
            continue
        elif until_epoch is not None and timestamp > until_epoch:
            continue

        request = record.get("request")
        if isinstance(request, dict):
            if request.get("headers"):
                report["sensitive_request_observations"]["nonempty_headers"] += 1
            uri = request.get("uri")
            if isinstance(uri, str) and "?" in uri and uri.split("?", 1)[1]:
                report["sensitive_request_observations"]["query_in_uri"] += 1

        report["records"] += 1
        classification = classify_record(record)
        classifications[classification] += 1

        status = parse_status(record)
        if status is None:
            report["status_missing"] += 1
        else:
            status_counts[str(status)] += 1
            if classification in classified_status_counts:
                classified_status_counts[classification][str(status)] += 1

        duration = duration_seconds(record)
        if duration is not None:
            duration_count += 1
            duration_total += duration
            duration_max = max(duration_max, duration)

    report["classification_counts"] = {
        classification: classifications.get(classification, 0)
        for classification in CLASSIFICATIONS
    }
    report["http_status_counts"] = dict(sorted(status_counts.items()))
    report["http_status_counts_by_classification"] = {
        classification: dict(sorted(counts.items()))
        for classification, counts in classified_status_counts.items()
    }
    report["duration_seconds"] = {
        "count": duration_count,
        "total": round(duration_total, 6),
        "max": round(duration_max, 6),
    }
    return report


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    default_path = os.environ.get(
        "NEW_API_CADDY_LOG_FILE",
        os.path.expanduser(
            "~/Library/Application Support/New API/caddy/new-api-macos-tun-caddy-3000.log"
        ),
    )
    parser.add_argument("log_file", nargs="?", default=default_path)
    parser.add_argument("--since-epoch", type=float)
    parser.add_argument("--until-epoch", type=float)
    parser.add_argument("--pretty", action="store_true")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    path = Path(args.log_file)
    if not path.is_file():
        print(
            f"caddy_log_report_status=error reason=log_missing source={path.name}",
            file=sys.stderr,
        )
        return 2
    if args.since_epoch is not None and args.until_epoch is not None:
        if args.since_epoch > args.until_epoch:
            print(
                "caddy_log_report_status=error reason=invalid_time_window",
                file=sys.stderr,
            )
            return 2

    try:
        with path.open("r", encoding="utf-8") as handle:
            report = build_report(
                handle,
                datetime.now(timezone.utc).isoformat(timespec="seconds").replace(
                    "+00:00", "Z"
                ),
                path.name,
                args.since_epoch,
                args.until_epoch,
            )
    except (OSError, UnicodeError) as exc:
        print(
            f"caddy_log_report_status=error reason=read_failed:{type(exc).__name__}",
            file=sys.stderr,
        )
        return 2

    indent = 2 if args.pretty else None
    print(json.dumps(report, ensure_ascii=True, indent=indent, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
