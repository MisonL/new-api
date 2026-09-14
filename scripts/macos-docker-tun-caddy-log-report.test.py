#!/usr/bin/env python3

import importlib.util
import json
import pathlib
import tempfile
import unittest


MODULE_PATH = pathlib.Path(__file__).with_name("macos-docker-tun-caddy-log-report.py")
SPEC = importlib.util.spec_from_file_location("caddy_log_report", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class CaddyLogReportTests(unittest.TestCase):
    def test_classifies_context_canceled_without_calling_it_a_caddy_outage(self):
        record = {
            "level": "warn",
            "msg": "aborting with incomplete response",
            "error": "reading: context canceled",
            "ts": 100.0,
            "duration": 3.5,
        }
        self.assertEqual(MODULE.classify_record(record), "context_canceled")
        report = MODULE.build_report([json.dumps(record)], "now", "fixture.log")
        self.assertEqual(report["classification_counts"]["context_canceled"], 1)
        self.assertTrue(
            report["evidence_limits"][
                "context_canceled_requires_application_correlation"
            ]
        )

    def test_client_gone_is_reported_as_cancellation_evidence(self):
        self.assertEqual(
            MODULE.classify_record(
                {"level": "warn", "error": "client gone while streaming"}
            ),
            "context_canceled",
        )

    def test_classifies_upstream_and_connection_failures(self):
        records = [
            {"level": "error", "msg": "dial tcp: connection refused"},
            {"level": "error", "msg": "upstream reset", "error": "unexpected EOF"},
            {"level": "error", "status": 502, "msg": "upstream returned"},
            {"level": "info", "status": 200, "msg": "request completed"},
        ]
        report = MODULE.build_report(
            (json.dumps(record) for record in records), "now", "fixture.log"
        )
        self.assertEqual(report["classification_counts"]["connection_refused"], 1)
        self.assertEqual(report["classification_counts"]["upstream_terminated"], 1)
        self.assertEqual(report["classification_counts"]["http_5xx"], 1)
        self.assertEqual(report["classification_counts"]["http_2xx"], 1)
        self.assertEqual(report["http_status_counts"], {"200": 1, "502": 1})
        self.assertEqual(
            report["http_status_counts_by_classification"]["http_5xx"],
            {"502": 1},
        )

    def test_malformed_lines_and_time_window_are_visible(self):
        lines = [
            "not-json\n",
            json.dumps({"ts": 10, "msg": "old"}) + "\n",
            json.dumps({"ts": 20, "msg": "in-window"}) + "\n",
            json.dumps({"msg": "missing timestamp"}) + "\n",
        ]
        report = MODULE.build_report(
            lines, "now", "fixture.log", since_epoch=15, until_epoch=25
        )
        self.assertEqual(report["malformed_lines"], 1)
        self.assertEqual(report["records"], 2)
        self.assertEqual(report["timestamp_missing"], 1)
        self.assertEqual(report["status_missing"], 2)
        self.assertEqual(
            report["sensitive_request_observations"]["query_in_uri"],
            0,
        )

    def test_cli_omits_raw_request_fields(self):
        record = {
            "ts": 1,
            "msg": "context canceled",
            "request": {
                "remote_ip": "192.0.2.10",
                "uri": "/v1/responses?secret=value",
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "caddy.log"
            path.write_text(json.dumps(record) + "\n", encoding="utf-8")
            report = MODULE.build_report(path.read_text().splitlines(), "now", path.name)
        rendered = json.dumps(report)
        self.assertNotIn("192.0.2.10", rendered)
        self.assertNotIn("secret=value", rendered)
        self.assertTrue(report["evidence_limits"]["raw_request_fields_omitted"])
        self.assertEqual(
            report["sensitive_request_observations"]["query_in_uri"],
            1,
        )

    def test_sensitive_request_observations_omit_header_values(self):
        record = {
            "ts": 1,
            "request": {
                "headers": {"Authorization": ["Bearer header-secret"]},
                "uri": "/v1/responses?token=query-secret",
            },
        }
        report = MODULE.build_report([json.dumps(record)], "now", "fixture.log")
        rendered = json.dumps(report)
        self.assertEqual(
            report["sensitive_request_observations"]["nonempty_headers"],
            1,
        )
        self.assertEqual(report["sensitive_request_observations"]["query_in_uri"], 1)
        self.assertNotIn("header-secret", rendered)
        self.assertNotIn("query-secret", rendered)
        self.assertTrue(
            report["evidence_limits"]["sensitive_request_values_omitted"]
        )

    def test_canceled_502_is_not_counted_as_classified_http_5xx(self):
        record = {
            "level": "warn",
            "status": 502,
            "msg": "aborting with incomplete response",
            "error": "reading: context canceled",
        }
        report = MODULE.build_report([json.dumps(record)], "now", "fixture.log")
        self.assertEqual(report["http_status_counts"], {"502": 1})
        self.assertEqual(report["classification_counts"]["context_canceled"], 1)
        self.assertEqual(report["http_status_counts_by_classification"]["http_5xx"], {})


if __name__ == "__main__":
    unittest.main()
