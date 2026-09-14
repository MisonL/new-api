#!/usr/bin/env python3

import importlib.util
import json
import os
import pathlib
import stat
import subprocess
import tempfile
import unittest
from unittest import mock


MODULE_PATH = pathlib.Path(__file__).with_name("macos-docker-tun-monitor-alert.py")
SPEC = importlib.util.spec_from_file_location("monitor_alert", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


STATUS = """schema_version=1
checked_at_utc=2026-08-03T14:10:00Z
monitor_status=fail
exit_code=1
watchdog_exit_code=0
resource_failure=1
resource_warning=0
resource_failure_reasons=swap-usage-high:95.01%
resource_warning_reasons=none
probe_snapshot_status=pass
probe_state=healthy
probe_docker_daemon=pass
probe_postgres=pass
probe_redis=pass
probe_backend=pass
probe_public=pass
probe_lan=pass
probe_app_state=running
probe_app_health=healthy
probe_app_restart_count=0
probe_app_oom=false
probe_failure_reason=none
probe_action=none
secret_token=must-not-appear
"""


class MonitorAlertTests(unittest.TestCase):
    def make_status_file(self, directory: str, mode: int = 0o600) -> pathlib.Path:
        path = pathlib.Path(directory) / "monitor.status"
        path.write_text(STATUS, encoding="utf-8")
        os.chmod(path, mode)
        return path

    def test_payload_is_allowlisted_and_omits_secret(self):
        with tempfile.TemporaryDirectory() as directory:
            values = MODULE.read_status(self.make_status_file(directory))
        payload = MODULE.build_payload(values)
        rendered = json.dumps(payload, sort_keys=True)
        self.assertEqual(payload["status"], "fail")
        self.assertEqual(payload["probes"]["redis"], "pass")
        self.assertNotIn("secret_token", rendered)
        self.assertNotIn("must-not-appear", rendered)
        self.assertNotIn("monitor.status", rendered)

    def test_payload_redacts_resource_paths_and_urls(self):
        with tempfile.TemporaryDirectory() as directory:
            values = MODULE.read_status(self.make_status_file(directory))
        values["resource_failure_reasons"] = (
            "disk-usage-high:/srv/example/logs:95%;"
            "endpoint=https://internal.example/hook"
        )
        values["resource_warning_reasons"] = "log-missing:/home/example/private.log"
        payload = MODULE.build_payload(values)
        rendered = json.dumps(payload, sort_keys=True)
        self.assertNotIn("/srv/example", rendered)
        self.assertNotIn("/home/example", rendered)
        self.assertNotIn("internal.example", rendered)
        self.assertIn("disk-usage-high:<redacted>:95%", rendered)
        self.assertIn("endpoint=<redacted>", rendered)

    def test_payload_redacts_paths_after_whitespace(self):
        with tempfile.TemporaryDirectory() as directory:
            values = MODULE.read_status(self.make_status_file(directory))
        values["resource_failure_reasons"] = "failed path /Users/example/private.log"
        payload = MODULE.build_payload(values)
        rendered = json.dumps(payload, sort_keys=True)
        self.assertNotIn("/Users/example", rendered)
        self.assertIn("failed path <redacted>", rendered)

    def test_rejects_open_status_permissions(self):
        with tempfile.TemporaryDirectory() as directory:
            path = self.make_status_file(directory, stat.S_IRUSR | stat.S_IWUSR | stat.S_IRGRP)
            with self.assertRaisesRegex(MODULE.AlertError, "permissions_too_open"):
                MODULE.read_status(path)

    def test_rejects_endpoint_credentials_and_non_http_scheme(self):
        with self.assertRaisesRegex(MODULE.AlertError, "credentials"):
            MODULE.validate_endpoint("https://user:secret@example.test/hook")
        with self.assertRaisesRegex(MODULE.AlertError, "http_or_https"):
            MODULE.validate_endpoint("file:///tmp/alert")

    def test_bearer_requires_https(self):
        with self.assertRaisesRegex(MODULE.AlertError, "bearer_requires_https"):
            MODULE.post_payload("http://monitor.example/hook", {}, 1, "bearer-secret")

    def test_post_uses_json_and_optional_bearer_without_printing_payload(self):
        class Response:
            status = 204

            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return False

            def read(self, _limit):
                return b""

        with tempfile.TemporaryDirectory() as directory:
            values = MODULE.read_status(self.make_status_file(directory))
        payload = MODULE.build_payload(values)
        with mock.patch.object(MODULE, "urlopen", return_value=Response()) as opener:
            status = MODULE.post_payload("https://monitor.example/hook", payload, 1, "bearer-secret")
        self.assertEqual(status, 204)
        request = opener.call_args.args[0]
        self.assertEqual(request.get_header("Authorization"), "Bearer bearer-secret")
        body = request.data.decode("utf-8")
        self.assertNotIn("secret_token", body)
        self.assertNotIn("bearer-secret", body)

    def test_cli_dry_run_and_unconfigured_endpoint(self):
        with tempfile.TemporaryDirectory() as directory:
            path = self.make_status_file(directory)
            dry_run = subprocess.run(
                [str(MODULE_PATH), "--status-file", str(path), "--dry-run"],
                capture_output=True,
                text=True,
                check=False,
            )
            disabled = subprocess.run(
                [str(MODULE_PATH), "--status-file", str(path)],
                capture_output=True,
                text=True,
                check=False,
            )
        self.assertEqual(dry_run.returncode, 0)
        self.assertIn("alert_status=dry-run", dry_run.stdout)
        self.assertNotIn("must-not-appear", dry_run.stdout)
        self.assertEqual(disabled.returncode, 0)
        self.assertIn("endpoint_unconfigured", disabled.stdout)


if __name__ == "__main__":
    unittest.main()
