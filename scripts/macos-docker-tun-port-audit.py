#!/usr/bin/env python3
"""Read-only inventory of Docker published ports and Compose ownership."""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
from datetime import datetime, timezone
from typing import Any


LABEL_KEYS = (
    "com.docker.compose.project",
    "com.docker.compose.service",
    "com.docker.compose.project.working_dir",
    "com.docker.compose.project.config_files",
)


class AuditError(RuntimeError):
    """Raised when the Docker inventory cannot be read safely."""


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
        raise AuditError(f"command not found: {command[0]}") from exc
    except subprocess.TimeoutExpired as exc:
        raise AuditError(f"command timed out: {command[0]}") from exc
    if result.returncode != 0:
        detail = (result.stderr or result.stdout).strip().replace("\n", " ")
        raise AuditError(
            f"command failed exit={result.returncode}: {detail[:240]}"
        )
    return result.stdout


def host_scope(host_ip: str) -> str:
    if host_ip in {"127.0.0.1", "::1", "localhost"}:
        return "loopback"
    if host_ip in {"", "0.0.0.0", "::", "[::]"}:
        return "public"
    return "specific"


def parse_port_bindings(container: dict[str, Any]) -> list[dict[str, str]]:
    bindings = container.get("HostConfig", {}).get("PortBindings") or {}
    ports: list[dict[str, str]] = []
    for container_port, entries in sorted(bindings.items()):
        for entry in entries or []:
            host_ip = str(entry.get("HostIp") or "")
            host_port = str(entry.get("HostPort") or "")
            ports.append(
                {
                    "container_port": str(container_port),
                    "host_ip": host_ip,
                    "host_port": host_port,
                    "scope": host_scope(host_ip),
                }
            )
    return ports


def container_record(container: dict[str, Any]) -> dict[str, Any]:
    config = container.get("Config") or {}
    labels = config.get("Labels") or {}
    state = container.get("State") or {}
    health = state.get("Health") or {}
    selected_labels = {
        key: labels[key] for key in LABEL_KEYS if labels.get(key) is not None
    }
    ports = parse_port_bindings(container)
    return {
        "name": str(container.get("Name") or "").lstrip("/"),
        "image": str(config.get("Image") or ""),
        "state": str(state.get("Status") or "unknown"),
        "restart_count": int(container.get("RestartCount") or 0),
        "oom_killed": bool(state.get("OOMKilled")),
        "health": str(health.get("Status") or "none"),
        "compose": selected_labels,
        "ports": ports,
    }


def build_report(containers: list[dict[str, Any]], checked_at: str) -> dict[str, Any]:
    records = [container_record(container) for container in containers]
    public_bindings = [
        {
            "container": record["name"],
            "host_ip": port["host_ip"],
            "host_port": port["host_port"],
            "container_port": port["container_port"],
            "compose": record["compose"],
        }
        for record in records
        for port in record["ports"]
        if port["scope"] == "public"
    ]
    return {
        "schema_version": 1,
        "checked_at_utc": checked_at,
        "docker": "pass",
        "container_count": len(records),
        "public_binding_count": len(public_bindings),
        "public_bindings": public_bindings,
        "containers": records,
    }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    default_docker = os.environ.get("NEW_API_PORT_AUDIT_DOCKER_BIN") or shutil.which(
        "docker"
    ) or "docker"
    parser.add_argument("--docker-bin", default=default_docker)
    parser.add_argument("--timeout", type=float, default=10.0)
    parser.add_argument("--pretty", action="store_true")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        ids_output = run_command(
            [args.docker_bin, "ps", "-q", "--no-trunc"], args.timeout
        )
        container_ids = [line for line in ids_output.splitlines() if line]
        containers: list[dict[str, Any]] = []
        if container_ids:
            inspect_output = run_command(
                [args.docker_bin, "inspect", *container_ids], args.timeout
            )
            containers = json.loads(inspect_output)
        checked_at = datetime.now(timezone.utc).isoformat(timespec="seconds").replace(
            "+00:00", "Z"
        )
        report = build_report(containers, checked_at)
    except (AuditError, json.JSONDecodeError, TypeError, ValueError) as exc:
        print(f"port_audit_status=error reason={exc}", file=sys.stderr)
        return 2

    indent = 2 if args.pretty else None
    print(json.dumps(report, ensure_ascii=True, indent=indent, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
