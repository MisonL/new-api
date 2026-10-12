#!/usr/bin/env python3
"""relaykit-c-symbols-check.py 的单测。

覆盖三类：
1. 纯函数（extract_c_rows / expand_file_ref）的解析行为；
2. 对真实设计文档跑一次端到端，断言退出码 0（C1–C8 八行齐全、符号与带锚点文件引用
   全部可解析）；
3. 反向验证：把文档内容换成「C 行数不足」「符号不存在」「行号越界」的变体，
   断言检查器确实变红——一个只会点头的检查器比没有更糟。
"""

import importlib.util
import pathlib
import subprocess
import sys
import tempfile
import unittest


MODULE_PATH = pathlib.Path(__file__).with_name("relaykit-c-symbols-check.py")
SPEC = importlib.util.spec_from_file_location("relaykit_c_symbols_check", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)

REPO_ROOT = MODULE_PATH.parent.parent
DOC = REPO_ROOT / "docs/superpowers/specs/2026-09-30-relaykit-migration-design.md"


class ExtractCRowsTests(unittest.TestCase):
    def test_extracts_all_eight_rows(self):
        text = "\n".join(f"| C{i} | body {i} |" for i in range(1, 9))
        rows = MODULE.extract_c_rows(text)
        self.assertEqual([r[0] for r in rows], [f"C{i}" for i in range(1, 9)])

    def test_ignores_other_tables(self):
        text = "| C1 | a |\n| 16 | b |\n| C2 | c |"
        self.assertEqual([r[0] for r in MODULE.extract_c_rows(text)], ["C1", "C2"])

    def test_c9_not_matched(self):
        self.assertEqual(MODULE.extract_c_rows("| C9 | x |"), [])


class ExpandFileRefTests(unittest.TestCase):
    def test_pin_prefix(self):
        got = MODULE.expand_file_ref("789c97019:relay/x.go:12", "PINREF", "BASE")
        self.assertEqual(got, ("PINREF", "relay/x.go", 12))

    def test_upstream_main_prefix(self):
        got = MODULE.expand_file_ref("upstream/main:relay/x.go", "PINREF", "BASE")
        self.assertEqual(got, ("PINREF", "relay/x.go", None))

    def test_baseline_prefix(self):
        got = MODULE.expand_file_ref("dbfbc096f:service/y.go:3", "PINREF", "BASE")
        self.assertEqual(got, ("BASE", "service/y.go", 3))

    def test_bare_name_rejected(self):
        self.assertIsNone(MODULE.expand_file_ref("relay/x.go:12", "PINREF", "BASE"))

    def test_strips_backticks(self):
        got = MODULE.expand_file_ref("`789c97019:a/b.go`", "P", "B")
        self.assertEqual(got, ("P", "a/b.go", None))


class EndToEndTests(unittest.TestCase):
    def _run_on(self, doc_text):
        with tempfile.NamedTemporaryFile(
            "w", suffix=".md", delete=False, encoding="utf-8"
        ) as fh:
            fh.write(doc_text)
            path = fh.name
        try:
            return subprocess.run(
                [
                    sys.executable, "-I", str(MODULE_PATH),
                    "--doc", path,
                    "--ref", MODULE.DEFAULT_REF,
                    "--baseline", MODULE.DEFAULT_BASELINE,
                ],
                capture_output=True, text=True, cwd=REPO_ROOT,
            )
        finally:
            pathlib.Path(path).unlink()

    def test_real_doc_is_green(self):
        r = subprocess.run(
            [sys.executable, "-I", str(MODULE_PATH)],
            capture_output=True, text=True, cwd=REPO_ROOT,
        )
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_missing_c_row_fails(self):
        text = DOC.read_text(encoding="utf-8")
        text = "\n".join(
            l for l in text.split("\n") if not l.startswith("| C8 |")
        )
        self.assertEqual(self._run_on(text).returncode, 1)

    def test_bogus_symbol_fails(self):
        text = DOC.read_text(encoding="utf-8").replace(
            "`relayconvert.NewResponseStreamState`",
            "`relayconvert.NewResponseStreamStateBOGUS`",
            1,
        )
        r = self._run_on(text)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("BOGUS", r.stdout)

    def test_out_of_range_line_fails(self):
        text = DOC.read_text(encoding="utf-8").replace(
            "`dbfbc096f:relay/channel/claude/relay-claude.go:1134`",
            "`dbfbc096f:relay/channel/claude/relay-claude.go:99999`",
            1,
        )
        r = self._run_on(text)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("行号越界", r.stdout)


if __name__ == "__main__":
    unittest.main()
