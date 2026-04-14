#!/usr/bin/env python3
from __future__ import annotations

import json
import subprocess
import sys
import time
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parent.parent
COMPOSE_FILE = REPO_ROOT / "docker-compose.yml"
CONTAINER_NAME = "new-api"
EXPECTED_PROJECT = "new-api"
EXPECTED_SERVICE = "new-api"


def run_command(args: list[str], timeout: int = 30, check: bool = True) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        args,
        cwd=REPO_ROOT,
        text=True,
        capture_output=True,
        timeout=timeout,
        check=check,
    )


def inspect_labels() -> dict[str, str]:
    result = run_command(
        ["docker", "inspect", CONTAINER_NAME, "--format", "{{json .Config.Labels}}"],
        timeout=15,
        check=False,
    )
    if result.returncode != 0:
        return {}
    output = result.stdout.strip()
    if not output or output == "null":
        return {}
    return json.loads(output)


def container_exists() -> bool:
    result = run_command(
        ["docker", "ps", "-a", "--filter", f"name=^/{CONTAINER_NAME}$", "--format", "{{.Names}}"],
        timeout=15,
        check=True,
    )
    return CONTAINER_NAME in {line.strip() for line in result.stdout.splitlines()}


def wait_until_removed(deadline_seconds: int = 45) -> None:
    deadline = time.time() + deadline_seconds
    while time.time() < deadline:
        if not container_exists():
            return
        time.sleep(1)
    raise RuntimeError(f"容器 {CONTAINER_NAME} 在 {deadline_seconds} 秒内未删除")


def remove_standalone_container() -> None:
    if not container_exists():
        return
    for attempt in range(1, 4):
        result = run_command(
            ["docker", "rm", "-f", CONTAINER_NAME],
            timeout=20,
            check=False,
        )
        if result.returncode == 0:
            wait_until_removed()
            return
        time.sleep(attempt)
    raise RuntimeError(f"删除容器 {CONTAINER_NAME} 失败")


def compose_up() -> None:
    result = run_command(
        ["docker", "compose", "-f", str(COMPOSE_FILE), "up", "-d", CONTAINER_NAME],
        timeout=120,
        check=False,
    )
    if result.returncode != 0:
        raise RuntimeError(result.stderr.strip() or result.stdout.strip() or "docker compose up 失败")


def wait_until_healthy(deadline_seconds: int = 90) -> None:
    deadline = time.time() + deadline_seconds
    while time.time() < deadline:
        result = run_command(
            [
                "docker",
                "inspect",
                CONTAINER_NAME,
                "--format",
                "{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}",
            ],
            timeout=15,
            check=False,
        )
        if result.returncode == 0:
            status = result.stdout.strip()
            if status == "running healthy":
                return
        time.sleep(2)
    raise RuntimeError(f"容器 {CONTAINER_NAME} 在 {deadline_seconds} 秒内未就绪")


def ensure_compose_labels() -> dict[str, str]:
    labels = inspect_labels()
    if labels.get("com.docker.compose.project") != EXPECTED_PROJECT:
        raise RuntimeError("未检测到 Compose project 标签")
    if labels.get("com.docker.compose.service") != EXPECTED_SERVICE:
        raise RuntimeError("未检测到 Compose service 标签")
    return labels


def main() -> int:
    labels = inspect_labels()
    if labels.get("com.docker.compose.project") == EXPECTED_PROJECT and labels.get(
        "com.docker.compose.service"
    ) == EXPECTED_SERVICE:
        compose_up()
        wait_until_healthy()
        labels = ensure_compose_labels()
        print(
            json.dumps(
                {
                    "status": "already_managed",
                    "project": labels["com.docker.compose.project"],
                    "service": labels["com.docker.compose.service"],
                },
                ensure_ascii=False,
            )
        )
        return 0

    remove_standalone_container()
    compose_up()
    wait_until_healthy()
    labels = ensure_compose_labels()
    print(
        json.dumps(
            {
                "status": "taken_over",
                "project": labels["com.docker.compose.project"],
                "service": labels["com.docker.compose.service"],
                "container": CONTAINER_NAME,
            },
            ensure_ascii=False,
        )
    )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(str(exc), file=sys.stderr)
        raise SystemExit(1)
