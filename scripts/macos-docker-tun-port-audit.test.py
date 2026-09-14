#!/usr/bin/env python3

import importlib.util
import pathlib
import unittest


MODULE_PATH = pathlib.Path(__file__).with_name("macos-docker-tun-port-audit.py")
SPEC = importlib.util.spec_from_file_location("port_audit", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class PortAuditTests(unittest.TestCase):
    def test_host_scope(self):
        self.assertEqual(MODULE.host_scope("127.0.0.1"), "loopback")
        self.assertEqual(MODULE.host_scope("::1"), "loopback")
        self.assertEqual(MODULE.host_scope("0.0.0.0"), "public")
        self.assertEqual(MODULE.host_scope(""), "public")
        self.assertEqual(MODULE.host_scope("192.0.2.20"), "specific")

    def test_report_keeps_only_safe_compose_metadata(self):
        container = {
            "Name": "/new-api",
            "Config": {
                "Image": "new-api-local:prod",
                "Env": ["SECRET=must-not-appear"],
                "Labels": {
                    "com.docker.compose.project": "new-api",
                    "com.docker.compose.service": "new-api",
                    "com.docker.compose.project.working_dir": "/work",
                    "com.docker.compose.project.config_files": "/work/docker-compose.yml",
                    "unrelated": "ignored",
                },
            },
            "State": {
                "Status": "running",
                "OOMKilled": False,
                "Health": {"Status": "healthy"},
            },
            "RestartCount": 0,
            "HostConfig": {
                "PortBindings": {
                    "3000/tcp": [{"HostIp": "127.0.0.1", "HostPort": "13000"}],
                    "8080/tcp": [{"HostIp": "0.0.0.0", "HostPort": "8080"}],
                    "9000/tcp": [{"HostIp": "192.0.2.20", "HostPort": "9000"}],
                }
            },
        }

        report = MODULE.build_report([container], "2026-08-03T00:00:00Z")
        self.assertEqual(report["public_binding_count"], 1)
        self.assertEqual(report["public_bindings"][0]["host_port"], "8080")
        ports = report["containers"][0]["ports"]
        self.assertEqual([port["scope"] for port in ports], ["loopback", "public", "specific"])
        self.assertNotIn("SECRET=must-not-appear", str(report))
        self.assertNotIn("unrelated", report["containers"][0]["compose"])


if __name__ == "__main__":
    unittest.main()
