#!/usr/bin/env python3

import importlib.util
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest


MODULE_PATH = pathlib.Path(__file__).with_name(
    "macos-docker-tun-time-machine-report.py"
)
SPEC = importlib.util.spec_from_file_location("time_machine_report", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


STATUS_OUTPUT = """Backup session status:
{
    BackupPhase = Copying;
    DestinationMountPoint = "/Volumes/private-backup";
    Progress =     {
        Percent = "0.8394379396523703";
        TimeRemaining = "4133.847502844361";
        bytes = 41493540168;
        files = 241077;
        totalBytes = 1775123107840;
        totalFiles = 11377458;
    };
    Running = 1;
}
"""

IDLE_STATUS_OUTPUT = """Backup session status:
{
    ClientID = "com.apple.backupd";
    Percent = "-1";
    Running = 0;
}
"""


class TimeMachineReportTests(unittest.TestCase):
    def test_parses_status_without_emitting_destination(self):
        status = MODULE.parse_status(STATUS_OUTPUT)
        self.assertEqual(status["phase"], "Copying")
        self.assertEqual(status["running"], 1)
        self.assertAlmostEqual(status["percent"], 83.9438, places=4)
        self.assertEqual(status["total_bytes"], 1775123107840)
        report = MODULE.build_report(
            STATUS_OUTPUT,
            {"logs": "Excluded", "data": "Included", "repo": "Included"},
            "now",
        )
        rendered = str(report)
        self.assertNotIn("private-backup", rendered)
        self.assertTrue(report["evidence_limits"]["destination_details_omitted"])
        self.assertEqual(report["coverage"]["status"], "pass")

    def test_required_included_reports_excluded_data(self):
        report = MODULE.build_report(
            STATUS_OUTPUT,
            {"logs": "Excluded", "data": "Excluded"},
            "now",
            ("data",),
        )
        self.assertEqual(report["coverage"]["status"], "fail")
        self.assertEqual(report["coverage"]["required_excluded"], ["data"])

    def test_parses_idle_status_without_backup_phase(self):
        status = MODULE.parse_status(IDLE_STATUS_OUTPUT)
        self.assertEqual(status["phase"], "Idle")
        self.assertEqual(status["running"], 0)
        self.assertEqual(status["percent"], -1)

    def test_path_status_parser_accepts_only_included_or_excluded(self):
        self.assertEqual(
            MODULE.parse_excluded("[Excluded]  /private/path\n"),
            "Excluded",
        )
        self.assertEqual(
            MODULE.parse_excluded("[Included]  /private/path\n"),
            "Included",
        )
        with self.assertRaises(MODULE.TimeMachineReportError):
            MODULE.parse_excluded("unexpected output\n")

    def test_path_spec_requires_safe_label(self):
        self.assertEqual(
            MODULE.parse_path_spec("logs=/srv/example/logs"),
            ("logs", "/srv/example/logs"),
        )
        with self.assertRaises(ValueError):
            MODULE.parse_path_spec("logs with spaces=/tmp/logs")

    def test_command_timeout_is_bounded(self):
        with self.assertRaises(MODULE.TimeMachineReportError) as context:
            MODULE.run_command(
                [sys.executable, "-c", "import time; time.sleep(1)"],
                0.01,
            )
        self.assertIn("command_timeout", str(context.exception))

    def test_cli_reports_error_without_raw_path(self):
        result = subprocess.run(
            [
                sys.executable,
                str(MODULE_PATH),
                "--tmutil-bin",
                "/path/that/does/not/exist",
                "--path",
                "secret=/private/secret",
            ],
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(result.returncode, 2)
        self.assertIn("tm_report_status=error", result.stderr)
        self.assertNotIn("/private/secret", result.stderr)

    def test_cli_fails_required_excluded_path_without_raw_path(self):
        with tempfile.TemporaryDirectory() as directory:
            fake_tmutil = pathlib.Path(directory) / "tmutil"
            fake_tmutil.write_text(
                "#!/usr/bin/env python3\n"
                "import sys\n"
                "if sys.argv[1] == 'status':\n"
                "    print('Backup session status: { Running = 0; }')\n"
                "elif sys.argv[1] == 'isexcluded':\n"
                "    print('[Excluded]')\n"
                "else:\n"
                "    raise SystemExit(1)\n",
                encoding="utf-8",
            )
            os.chmod(fake_tmutil, 0o700)
            result = subprocess.run(
                [
                    sys.executable,
                    str(MODULE_PATH),
                    "--tmutil-bin",
                    str(fake_tmutil),
                    "--path",
                    "data=/private/secret",
                    "--require-included",
                    "data",
                ],
                capture_output=True,
                text=True,
                check=False,
            )
        self.assertEqual(result.returncode, 1)
        self.assertIn("tm_report_status=fail reason=required-path-excluded:data", result.stdout)
        self.assertNotIn("/private/secret", result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
