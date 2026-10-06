#!/usr/bin/env python3
"""Unit checks for check_new_api_healthcheck's two accepted healthcheck forms.

The compose healthcheck may be written either as a direct argv
["CMD", "wget", "-q", "-T", "10", ...] or as a shell string
["CMD", "sh", "-c", "wget -q -T 10 ..."]. Membership checks against the top-level
list only match the first form, so the second form must be split before the
flags are looked for.
"""

from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location(
    "compose_healthcheck_contract", HERE / "compose-healthcheck-contract.py"
)
if spec is None or spec.loader is None:
    raise SystemExit("cannot load compose-healthcheck-contract")
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)

HEALTHY = {
    "interval": "30s",
    "timeout": "15s",
    "retries": 5,
    "start_period": "60s",
}


def with_test(test: list) -> dict:
    return {"healthcheck": dict(HEALTHY, test=test)}


def expect_ok(name: str, service: dict) -> None:
    try:
        mod.check_new_api_healthcheck(service, name)
    except (Exception, SystemExit) as exc:  # the contract reports via SystemExit
        print(f"FAIL {name}: expected pass, got {exc}")
        raise SystemExit(1)
    print(f"ok   {name}")


def expect_fail(name: str, service: dict) -> None:
    try:
        mod.check_new_api_healthcheck(service, name)
    except (Exception, SystemExit) as exc:  # the contract reports via SystemExit
        print(f"ok   {name} (rejected: {exc})")
        return
    print(f"FAIL {name}: expected a contract failure but it passed")
    raise SystemExit(1)


SHELL_FORM = [
    "CMD",
    "sh",
    "-c",
    "wget -q -T 10 -O /dev/null http://127.0.0.1:${PORT:-3000}/api/healthz",
]

# Both real-world forms must pass.
expect_ok("direct argv", with_test(["CMD", "wget", "-q", "-T", "10", "-O", "/dev/null", "http://127.0.0.1:3000/api/healthz"]))
expect_ok("shell -c string (current docker-compose.yml and dev-isolated)", with_test(SHELL_FORM))

# The semantics the assertions protect must still reject bad healthchecks.
expect_fail("no wget at all", with_test(["CMD", "sh", "-c", "true"]))
expect_fail(
    "short wget timeout -T 5",
    with_test(["CMD", "sh", "-c", "wget -q -T 5 -O /dev/null http://127.0.0.1:3000/api/healthz"]),
)
expect_fail(
    "missing -T",
    with_test(["CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:3000/api/healthz"]),
)
expect_fail("retries too low", {"healthcheck": dict(HEALTHY, test=SHELL_FORM, retries=2)})
expect_fail("timeout too short", {"healthcheck": dict(HEALTHY, test=SHELL_FORM, timeout="5s")})

print("all checks passed")
sys.exit(0)
