#!/usr/bin/env python3
"""relaykit-gate-selfcheck.py 的单测。

重点：每个解析器都要有一条「输出变了能被发现」的用例——解析器写宽一格，
整类漂移就从门禁里消失，而门禁仍是绿的。

    python3 scripts/relaykit-gate-selfcheck.test.py
"""

import importlib.util
import os
import unittest

_HERE = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location(
    "gate_selfcheck", os.path.join(_HERE, "relaykit-gate-selfcheck.py")
)
selfcheck = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(selfcheck)


CITATION_OUT = (
    "ref=upstream/main  citations=51  ok=51  failed=0\n"
    "baseline=dbfbc096f  host-citations ok=107  failed=0  skipped=0\n"
    "upstream-full ok=24  failed=0\n"
)

DTO_OUT = (
    "计数: fieldSet=33 tag=5 type=6 embed=0 order=4 sig=12 equiv=21\n"
    "strict: counts match the recorded expectation\n"
)


class TestParsers(unittest.TestCase):
    def test_citation_parsed(self):
        self.assertEqual(selfcheck.parse_citation(CITATION_OUT), (51, 107, 24))

    def test_citation_changed_count_is_visible(self):
        out = CITATION_OUT.replace("citations=51", "citations=52")
        self.assertEqual(selfcheck.parse_citation(out), (52, 107, 24))

    def test_citation_garbage_returns_none(self):
        self.assertIsNone(selfcheck.parse_citation("nothing here"))

    def test_ran_parsed(self):
        self.assertEqual(selfcheck.parse_ran("Ran 25 tests in 0.001s"), 25)
        self.assertEqual(selfcheck.parse_ran("Ran 1 test in 0.001s"), 1)

    def test_dto_parsed(self):
        self.assertEqual(selfcheck.parse_dto(DTO_OUT), (33, 5, 6, 0, 4, 12, 21))

    def test_dto_changed_dimension_is_visible(self):
        out = DTO_OUT.replace("equiv=21", "equiv=22")
        self.assertEqual(selfcheck.parse_dto(out), (33, 5, 6, 0, 4, 12, 22))

    def test_dto_garbage_returns_none(self):
        self.assertIsNone(selfcheck.parse_dto("no counts line"))


DOC = """\
`upstream/main` = `789c97019`（2026-09-28）
当前实测三组全绿：**51/51**（`ref=upstream/main`）、**宿主 107/107**（`baseline=dbfbc096f`）、**upstream 全路径 24/24**（**第 38 轮**：第 37 轮为 50/103/24）。
scripts/relaykit-citation-check.py            # 全部引用可解析时退出码 0（relaykit 51 + 宿主 107 + upstream 全路径 24）
python3 scripts/relaykit-citation-check.test.py  # 25 个单测
python3 scripts/relaykit-doc-structure-check.test.py   # 13 个单测
# 计数: fieldSet=33 tag=5 type=6 embed=0 order=4 sig=12 equiv=21
`--strict` 把当前基线（fieldSet=33、tag=5、type=6、embed=0、**order=4**、sig=12、**equiv=21**）变成门禁
本文档 relaykit 行引用现为 **51 处唯一**（**第 38 轮**）；裸宿主引用 **107 处**；全路径 **24 处**。
"""

ACTUAL = {
    "citation": (51, 107, 24),
    "citation_tests": 25,
    "structure_problems": 0,
    "structure_tests": 13,
    "dto": (33, 5, 6, 0, 4, 12, 21),
}


class TestClaims(unittest.TestCase):
    def test_all_claims_found(self):
        claims = selfcheck.extract_claims(DOC)
        descs = sorted(d for _, d, _ in claims)
        self.assertIn("citation 三元组", descs)
        self.assertIn("citation relaykit 分子/分母", descs)
        self.assertIn("citation 宿主 分子/分母", descs)
        self.assertIn("citation 全路径 分子/分母", descs)
        self.assertIn("citation 单测数", descs)
        self.assertIn("结构单测数", descs)
        self.assertIn("dto 七维（命令行注释）", descs)
        self.assertIn("dto 七维（正文）", descs)

    def test_historical_value_is_not_a_claim(self):
        """「第 37 轮为 50/103/24」是历史记录，不该被当成当前声明。"""
        claims = selfcheck.extract_claims(DOC)
        values = [v for _, _, v in claims]
        self.assertNotIn((50, 103, 24), values)

    def test_clean_doc_matches(self):
        claims = selfcheck.extract_claims(DOC)
        self.assertEqual(selfcheck.compare(claims, ACTUAL), [])

    def test_drifted_doc_is_caught(self):
        drifted = DOC.replace("relaykit 51 + 宿主 107", "relaykit 52 + 宿主 107")
        mismatches = selfcheck.compare(selfcheck.extract_claims(drifted), ACTUAL)
        self.assertEqual(len(mismatches), 1)
        self.assertEqual(mismatches[0][2], (52, 107, 24))

    def test_drifted_test_count_is_caught(self):
        drifted = DOC.replace("# 25 个单测", "# 26 个单测")
        mismatches = selfcheck.compare(selfcheck.extract_claims(drifted), ACTUAL)
        self.assertEqual(len(mismatches), 1)
        self.assertEqual(mismatches[0][1], "citation 单测数")

    def test_section11_phrasing_is_extracted(self):
        """§11 用「N 处唯一 / 裸宿主引用 N 处 / 全路径 N 处」表述同一批门禁计数。

        第 38 轮之前抽取器只认 §9.3 的句式，这三处整类漏检——本条钉住不再复发。
        """
        claims = selfcheck.extract_claims(DOC)
        descs = [d for _, d, _ in claims]
        self.assertIn("§11 relaykit 引用数", descs)
        self.assertIn("§11 宿主引用数", descs)
        self.assertIn("§11 全路径引用数", descs)

    def test_section11_drift_is_caught(self):
        drifted = DOC.replace("裸宿主引用 **107 处**", "裸宿主引用 **999 处**")
        mismatches = selfcheck.compare(selfcheck.extract_claims(drifted), ACTUAL)
        self.assertEqual(len(mismatches), 1)
        self.assertEqual(mismatches[0][1], "§11 宿主引用数")

    def test_selfcheck_test_count_is_a_claim(self):
        doc = DOC + "python3 scripts/relaykit-gate-selfcheck.test.py  # 14 个单测\n"
        claims = selfcheck.extract_claims(doc)
        self.assertIn("自检单测数", [d for _, d, _ in claims])

    def test_drifted_selfcheck_count_is_caught(self):
        doc = DOC + "python3 scripts/relaykit-gate-selfcheck.test.py  # 99 个单测\n"
        actual = dict(ACTUAL, selfcheck_tests=14)
        mismatches = selfcheck.compare(selfcheck.extract_claims(doc), actual)
        self.assertEqual(len(mismatches), 1)
        self.assertEqual(mismatches[0][1], "自检单测数")

    def test_skip_recurse_leaves_selfcheck_tests_unchecked(self):
        """置位时 selfcheck_tests 为 None，compare 不得把它当成「不一致」。"""
        doc = DOC + "python3 scripts/relaykit-gate-selfcheck.test.py  # 99 个单测\n"
        actual = dict(ACTUAL, selfcheck_tests=None)
        self.assertEqual(selfcheck.compare(selfcheck.extract_claims(doc), actual), [])

    def test_drifted_dto_is_caught_in_both_places(self):
        drifted = DOC.replace("equiv=21", "equiv=22")
        mismatches = selfcheck.compare(selfcheck.extract_claims(drifted), ACTUAL)
        self.assertEqual(len(mismatches), 2)
        self.assertEqual(sorted(m[1] for m in mismatches),
                         ["dto 七维（命令行注释）", "dto 七维（正文）"])


class TestCRLineCount(unittest.TestCase):
    """CR 的「文档当前 N 行」是当前口径声明，必须与实算一致。

    第 38 轮写错过一次：用 split("\n") 的长度（1987）而非 wc -l 口径
    （1986）——文件末尾有换行时前者多算 1。
    """

    def test_line_claim_extracted(self):
        import tempfile
        with tempfile.NamedTemporaryFile("w", suffix=".md", delete=False,
                                         encoding="utf-8") as fh:
            fh.write("...\n文档当前 123 行，位于 `docs/...`。\n")
            path = fh.name
        try:
            claims = selfcheck.extract_line_claims(path)
        finally:
            os.unlink(path)
        self.assertEqual(claims, [(2, "CR 文档行数", 123)])

    def test_line_claim_drift_is_caught(self):
        actual = dict(ACTUAL, doc_lines=1986)
        claims = [(7, "CR 文档行数", 9999)]
        mismatches = selfcheck.compare(claims, actual)
        self.assertEqual(len(mismatches), 1)
        self.assertEqual(mismatches[0][2], 9999)

    def test_missing_cr_file_yields_no_claims(self):
        self.assertEqual(selfcheck.extract_line_claims("/nonexistent/x.md"), [])


class TestSkipIsNotPass(unittest.TestCase):
    """置位递归守卫时该条声明「跳过」，主流程不得打印「全部一致」。

    跳过与放行必须可区分：否则一次未覆盖的检查会读起来像已覆盖。
    """

    def test_skipped_claim_does_not_match(self):
        doc = DOC + "python3 scripts/relaykit-gate-selfcheck.test.py  # 99 个单测\n"
        actual = dict(ACTUAL, selfcheck_tests=None)
        self.assertEqual(selfcheck.compare(selfcheck.extract_claims(doc), actual), [])

    def test_summary_text_distinguishes_skip_from_pass(self):
        import io as _io
        import contextlib
        import subprocess
        import sys as _sys

        env = dict(os.environ, RELAYKIT_SELFCHECK_SKIP_RECURSE="1")
        proc = subprocess.run(
            [_sys.executable, os.path.join(_HERE, "relaykit-gate-selfcheck.py"),
             "--doc", os.path.join(os.path.dirname(_HERE), selfcheck.DEFAULT_DOC)],
            cwd=os.path.dirname(_HERE), env=env,
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
        )
        self.assertEqual(proc.returncode, 0)
        self.assertIn("自检单测数未核对", proc.stdout)
        self.assertNotIn("全部门禁自述计数与实际输出一致", proc.stdout)


class TestRealDoc(unittest.TestCase):
    def test_real_doc_matches_real_gates(self):
        """端到端：实跑四个门禁，与设计文档的自述计数比对。

        置 RELAYKIT_SELFCHECK_SKIP_RECURSE 断开自指：selfcheck 会跑本文件，
        本用例又调 collect_actual——不置位就是无限递归。
        """
        os.environ["RELAYKIT_SELFCHECK_SKIP_RECURSE"] = "1"
        try:
            actual, failures = selfcheck.collect_actual(quiet=True)
        finally:
            del os.environ["RELAYKIT_SELFCHECK_SKIP_RECURSE"]
        self.assertEqual(failures, [])
        doc_path = os.path.join(
            os.path.dirname(_HERE), selfcheck.DEFAULT_DOC
        )
        with open(doc_path, encoding="utf-8") as fh:
            doc = fh.read()
        mismatches = selfcheck.compare(selfcheck.extract_claims(doc), actual)
        self.assertEqual(mismatches, [], f"自述计数漂移：{mismatches[:5]}")


if __name__ == "__main__":
    unittest.main(verbosity=2)
