#!/usr/bin/env python3
"""Validate future Compose healthchecks, logging limits, and port mappings."""

from __future__ import annotations

import json
import os
import re
import subprocess
import sys
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parents[1]
DOCKER = os.environ.get("COMPOSE_HEALTHCHECK_DOCKER_BIN", "docker")
TIMEOUT_SECONDS = 30
DURATION_PATTERN = re.compile(r"^(?:(?P<hours>[0-9]+)h)?(?:(?P<minutes>[0-9]+)m)?(?:(?P<seconds>[0-9]+)s)?$")


def fail(message: str) -> None:
    print(f"compose healthcheck contract failed: {message}", file=sys.stderr)
    raise SystemExit(1)


def render(compose_file: Path, environment: dict[str, str]) -> dict[str, Any]:
    env = os.environ.copy()
    env.update(environment)
    try:
        result = subprocess.run(
            [DOCKER, "compose", "-f", str(compose_file), "config", "--format", "json"],
            cwd=ROOT,
            env=env,
            capture_output=True,
            text=True,
            timeout=TIMEOUT_SECONDS,
            check=False,
        )
    except FileNotFoundError:
        fail(f"missing command: {DOCKER}")
    except subprocess.TimeoutExpired:
        fail(f"compose render timed out: {compose_file}")
    if result.returncode != 0:
        detail = (result.stderr or result.stdout).strip().splitlines()
        fail(f"compose render failed for {compose_file}: {detail[-1] if detail else 'unknown error'}")
    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        fail(f"compose render was not JSON for {compose_file}: {exc}")


def require(condition: bool, message: str) -> None:
    if not condition:
        fail(message)


def duration_seconds(value: Any, name: str) -> int:
    match = DURATION_PATTERN.fullmatch(str(value))
    require(match is not None, f"{name} duration is invalid: {value}")
    return (
        int(match.group("hours") or 0) * 3600
        + int(match.group("minutes") or 0) * 60
        + int(match.group("seconds") or 0)
    )


def health_command(service: dict[str, Any], name: str) -> str:
    healthcheck = service.get("healthcheck") or {}
    test = healthcheck.get("test")
    require(isinstance(test, list) and len(test) >= 2, f"{name} healthcheck test is incomplete")
    require(test[0] == "CMD-SHELL", f"{name} healthcheck must use CMD-SHELL")
    return " ".join(str(item) for item in test[1:])


def check_new_api_healthcheck(service: dict[str, Any], name: str) -> None:
    healthcheck = service.get("healthcheck") or {}
    test = healthcheck.get("test")
    require(isinstance(test, list) and len(test) >= 2, f"{name} healthcheck test is incomplete")
    require(test[0] == "CMD", f"{name} healthcheck must use CMD")
    # The command is either a direct argv or a shell string: ["CMD", "sh", "-c",
    # "wget -q -T 10 ..."]. Membership checks against the top-level list only see
    # the first form, so split the shell string before looking for wget and -T,
    # otherwise the shell form fails the contract even when it is correct.
    words = [str(item) for item in test[1:]]
    if len(words) >= 2 and words[0] == "sh" and words[1] == "-c":
        words = words[2].split()
    require("wget" in words, f"{name} healthcheck must use wget")
    require("-T" in words, f"{name} healthcheck must set wget timeout")
    timeout_index = words.index("-T") + 1
    require(timeout_index < len(words), f"{name} healthcheck wget timeout is missing")
    try:
        wget_timeout = int(words[timeout_index])
    except (TypeError, ValueError):
        fail(f"{name} healthcheck wget timeout is not numeric")
    require(wget_timeout >= 10, f"{name} healthcheck wget timeout is too short")
    require(duration_seconds(healthcheck.get("timeout"), f"{name} healthcheck timeout") >= 15, f"{name} healthcheck timeout is too short")
    require(int(healthcheck.get("retries", 0)) >= 5, f"{name} healthcheck retries are too low")
    require(duration_seconds(healthcheck.get("start_period"), f"{name} healthcheck start_period") >= 60, f"{name} healthcheck start_period is too short")


def check_logging(service: dict[str, Any], name: str) -> None:
    logging = service.get("logging") or {}
    require(logging.get("driver") == "json-file", f"{name} logging driver is not json-file")
    options = logging.get("options") or {}
    require(options.get("max-size") == "10m", f"{name} max-size is not 10m")
    require(options.get("max-file") == "3", f"{name} max-file is not 3")


def check_port(service: dict[str, Any], name: str, target: int, published: str) -> None:
    ports = service.get("ports") or []
    matches = [
        port
        for port in ports
        if port.get("target") == target and str(port.get("published")) == published
    ]
    require(matches, f"{name} is missing loopback port {published}:{target}")
    require(
        all(port.get("host_ip") == "127.0.0.1" for port in matches),
        f"{name} port {published}:{target} is not loopback-only",
    )


def check_default_public_port(service: dict[str, Any], name: str, target: int, published: str) -> None:
    ports = service.get("ports") or []
    matches = [
        port
        for port in ports
        if port.get("target") == target and str(port.get("published")) == published
    ]
    require(matches, f"{name} is missing default port {published}:{target}")
    host_ips = {str(port.get("host_ip") or "") for port in matches}
    require(
        host_ips <= {"", "0.0.0.0", "::"},
        f"{name} default port {published}:{target} is not publicly bound",
    )


def check_compose(
    label: str,
    compose_file: str,
    environment: dict[str, str],
    expected_ports: dict[str, tuple[int, str]],
    required_source_markers: tuple[str, ...],
) -> None:
    path = ROOT / compose_file
    require(path.is_file(), f"missing Compose file: {compose_file}")
    source = path.read_text(encoding="utf-8")
    for marker in required_source_markers:
        require(marker in source, f"{label} is not fail-closed for {marker}")

    config = render(path, environment)
    services = config.get("services") or {}
    for service_name in ("new-api", "redis", "postgres"):
        require(service_name in services, f"{label} is missing service {service_name}")
        check_logging(services[service_name], f"{label}/{service_name}")

    check_new_api_healthcheck(services["new-api"], f"{label}/new-api")

    redis_command = health_command(services["redis"], f"{label}/redis")
    require("REDISCLI_AUTH" in redis_command, f"{label}/redis does not authenticate")
    require("redis-cli" in redis_command and "ping" in redis_command, f"{label}/redis does not ping")
    require("grep -Fxq PONG" in redis_command, f"{label}/redis does not require exact PONG")
    require("NOAUTH" not in redis_command and "|| true" not in redis_command, f"{label}/redis masks failure")

    postgres_command = health_command(services["postgres"], f"{label}/postgres")
    for marker in ("PGPASSWORD", "psql", "ON_ERROR_STOP=1", "SELECT 1", "grep -Fxq 1"):
        require(marker in postgres_command, f"{label}/postgres healthcheck lacks {marker}")
    require("pg_isready" not in postgres_command, f"{label}/postgres uses pg_isready instead of SQL")

    depends_on = services["new-api"].get("depends_on") or {}
    for dependency in ("redis", "postgres"):
        require(
            (depends_on.get(dependency) or {}).get("condition") == "service_healthy",
            f"{label}/new-api does not wait for healthy {dependency}",
        )

    for service_name, (target, published) in expected_ports.items():
        check_port(services[service_name], f"{label}/{service_name}", target, published)
    print(f"compose healthcheck contract passed: {label}")


def main() -> None:
    check_compose(
        "root",
        "docker-compose.yml",
        {
            "NEW_API_IMAGE": "new-api-local:contract",
            "NEW_API_PORT_MAPPING": "127.0.0.1:13000:3000",
            "NEW_API_DATA_DIR": "/tmp/new-api-contract-data",
            "NEW_API_LOG_DIR": "/tmp/new-api-contract-logs",
            "SESSION_SECRET": "contract-session",
            "CRYPTO_SECRET": "contract-crypto",
            "SQL_DSN": "postgres://root:contract@postgres:5432/new-api",
            "REDIS_CONN_STRING": "redis://:contract@redis:6379/0",
            "REDIS_PASSWORD": "contract-redis-password",
            "POSTGRES_USER": "root",
            "POSTGRES_PASSWORD": "contract-postgres-password",
            "POSTGRES_DB": "new-api",
        },
        {"new-api": (3000, "13000")},
        ("REDIS_PASSWORD:?set REDIS_PASSWORD", "POSTGRES_PASSWORD:?set POSTGRES_PASSWORD"),
    )
    root_default = render(
        ROOT / "docker-compose.yml",
        {
            "NEW_API_IMAGE": "new-api-local:contract",
            "NEW_API_PORT_MAPPING": "",
            "NEW_API_DATA_DIR": "/tmp/new-api-contract-data",
            "NEW_API_LOG_DIR": "/tmp/new-api-contract-logs",
            "SESSION_SECRET": "contract-session",
            "CRYPTO_SECRET": "contract-crypto",
            "SQL_DSN": "postgres://root:contract@postgres:5432/new-api",
            "REDIS_CONN_STRING": "redis://:contract@redis:6379/0",
            "REDIS_PASSWORD": "contract-redis-password",
            "POSTGRES_USER": "root",
            "POSTGRES_PASSWORD": "contract-postgres-password",
            "POSTGRES_DB": "new-api",
        },
    )
    check_default_public_port(
        root_default["services"]["new-api"],
        "root/new-api",
        3000,
        "3000",
    )
    print("compose healthcheck contract passed: root/default-port")
    check_compose(
        "dev-isolated",
        "deploy/compose/dev-isolated.yml",
        {
            "DEV_NEW_API_IMAGE": "new-api-local:contract",
            "DEV_NEW_API_PORT": "3001",
            "DEV_NEW_API_DATA_DIR": "/tmp/new-api-contract-data",
            "DEV_NEW_API_LOG_DIR": "/tmp/new-api-contract-logs",
            "DEV_SESSION_SECRET": "contract-session",
            "DEV_CRYPTO_SECRET": "contract-crypto",
            "DEV_SQL_DSN": "postgres://root:contract@postgres:5432/new-api-dev",
            "DEV_REDIS_CONN_STRING": "redis://:contract@redis:6379/0",
            "DEV_REDIS_PASSWORD": "contract-redis-password",
            "DEV_POSTGRES_USER": "root",
            "DEV_POSTGRES_PASSWORD": "contract-postgres-password",
            "DEV_POSTGRES_DB": "new-api-dev",
            "DEV_POSTGRES_DATA_DIR": "/tmp/new-api-contract-postgres",
        },
        {
            "new-api": (3000, "3001"),
            "redis": (6379, "6380"),
            "postgres": (5432, "5433"),
        },
        ("DEV_REDIS_PASSWORD:?set DEV_REDIS_PASSWORD", "DEV_POSTGRES_PASSWORD:?set DEV_POSTGRES_PASSWORD"),
    )
    check_compose(
        "legacy-dev",
        "docker-compose.dev.yml",
        {
            "DEV_NEW_API_IMAGE": "new-api-local:contract",
            "DEV_LEGACY_NEW_API_PORT": "3002",
            "DEV_LEGACY_POSTGRES_PORT": "5434",
            "DEV_LEGACY_REDIS_PORT": "6381",
            "DEV_SESSION_SECRET": "contract-session",
            "DEV_CRYPTO_SECRET": "contract-crypto",
            "DEV_SQL_DSN": "postgres://root:contract@postgres:5432/new-api-dev",
            "DEV_REDIS_CONN_STRING": "redis://:contract@redis:6379/0",
            "DEV_REDIS_PASSWORD": "contract-redis-password",
            "DEV_POSTGRES_USER": "root",
            "DEV_POSTGRES_PASSWORD": "contract-postgres-password",
            "DEV_POSTGRES_DB": "new-api-dev",
        },
        {
            "new-api": (3000, "3002"),
            "redis": (6379, "6381"),
            "postgres": (5432, "5434"),
        },
        ("DEV_REDIS_PASSWORD:?set DEV_REDIS_PASSWORD", "DEV_POSTGRES_PASSWORD:?set DEV_POSTGRES_PASSWORD"),
    )


if __name__ == "__main__":
    main()
