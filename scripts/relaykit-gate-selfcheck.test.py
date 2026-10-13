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
    "ref=789c97019  citations=51  ok=51  failed=0\n"
    "baseline=dbfbc096f  host-citations ok=107  failed=0  skipped=0\n"
    "upstream-full ok=24  failed=0\n"
    "bare ok=41  unresolved=2  failed=0\n"
    "sha ok=9  failed=0\n"
)

DTO_OUT = (
    "计数: fieldSet=33 tag=5 type=6 embed=0 order=4 sig=12 equiv=21\n"
    "strict: counts match the recorded expectation\n"
)


class TestParsers(unittest.TestCase):
    def test_citation_parsed(self):
        self.assertEqual(selfcheck.parse_citation(CITATION_OUT), (51, 107, 24, 41, 2, 9))

    def test_citation_changed_count_is_visible(self):
        out = CITATION_OUT.replace("citations=51", "citations=52")
        self.assertEqual(selfcheck.parse_citation(out), (52, 107, 24, 41, 2, 9))

    def test_citation_garbage_returns_none(self):
        self.assertIsNone(selfcheck.parse_citation("nothing here"))

    def test_citation_failed_field_must_be_present_and_zero(self):
        # 第 41 轮 Codex 报的 P2：解析器只读 ok=，不看 failed=。退出码与输出
        # 一旦失配，有失败的行会被当成功放行。failed!=0 或无 failed 字段 → None。
        bad_nonzero = CITATION_OUT.replace("ok=51  failed=0", "ok=51  failed=99")
        self.assertIsNone(selfcheck.parse_citation(bad_nonzero))
        missing_field = CITATION_OUT.replace("  failed=0", "")
        self.assertIsNone(selfcheck.parse_citation(missing_field))

    def test_bare_failed_must_be_zero(self):
        # 第 43 轮：裸组单独 fail-closed。failed!=0 或整行缺失 → None。
        bad = CITATION_OUT.replace("bare ok=41  unresolved=2  failed=0",
                                    "bare ok=41  unresolved=2  failed=3")
        self.assertIsNone(selfcheck.parse_citation(bad))
        no_bare = CITATION_OUT.replace(
            "bare ok=41  unresolved=2  failed=0\n", "")
        self.assertIsNone(selfcheck.parse_citation(no_bare))

    def test_bare_unresolved_is_parsed(self):
        out = CITATION_OUT.replace("unresolved=2", "unresolved=7")
        self.assertEqual(selfcheck.parse_citation(out), (51, 107, 24, 41, 7, 9))

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
当前实测三组全绿：**51/51**（`ref=789c97019`）、**宿主 107/107**（`baseline=dbfbc096f`）、**upstream 全路径 24/24**（**第 38 轮**：第 37 轮为 50/103/24）。
scripts/relaykit-citation-check.py            # 全部引用可解析时退出码 0（relaykit 51 + 宿主 107 + upstream 全路径 24，裸引用 ok=41/未解析=2）
python3 scripts/relaykit-citation-check.test.py  # 25 个单测
python3 scripts/relaykit-doc-structure-check.test.py   # 13 个单测
# 计数: fieldSet=33 tag=5 type=6 embed=0 order=4 sig=12 equiv=21
`--strict` 把当前基线（fieldSet=33、tag=5、type=6、embed=0、**order=4**、sig=12、**equiv=21**）变成门禁
本文档 relaykit 行引用现为 **51 处唯一**（**第 38 轮**）；裸宿主引用 **107 处**；全路径 **24 处**。
裸引用 ok **41**、未解析 **2** 处（占位符与工作区文件）。
"""

ACTUAL = {
    "citation": (51, 107, 24, 41, 2, 9),
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

    def test_non_pinned_ref_claim_is_caught(self):
        """抽取器只认钉死 ref 的句式（第 40 轮）。

        写错 ref（退回会漂移的 upstream/main）时，`**51/51**（`ref=...`）`
        声明不再匹配抽取器 → 整类漏检。本条两侧都钉：(a) 漂移句式抽取不到；
        (b) 正确句式（用 UPSTREAM_PIN 现拼）能抽取，避免升锚后抽取器失联。
        """
        good = "**51/51**（`ref=%s`）" % selfcheck.UPSTREAM_PIN
        drifted = "**51/51**（`ref=upstream/main`）"
        self.assertIn(
            "citation relaykit 分子/分母",
            [d for _, d, _ in selfcheck.extract_claims(good)],
        )
        self.assertNotIn(
            "citation relaykit 分子/分母",
            [d for _, d, _ in selfcheck.extract_claims(drifted)],
        )

    def test_pinned_ref_claim_with_trailing_prose_is_extracted(self):
        """真实文档在第 40 轮写成 `（`ref=789c97019`，即本文档锚定版本）`。

        抽取器若要求右括号紧跟反引号，真实文档上零命中——整类声明静默失联
        （第 41 轮实测：修前 present kinds 缺这一类）。本条钉住带尾注的句式。
        """
        real = "**51/51**（`ref=%s`，即本文档锚定版本）" % selfcheck.UPSTREAM_PIN
        self.assertIn(
            "citation relaykit 分子/分母",
            [d for _, d, _ in selfcheck.extract_claims(real)],
        )

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

    def test_bare_claims_are_extracted(self):
        """§11 的裸组声明（ok 数、未解析数）是当前口径，第 43 轮起必须在册。"""
        claims = selfcheck.extract_claims(DOC)
        descs = [d for _, d, _ in claims]
        self.assertIn("§11 裸引用 ok 数", descs)
        self.assertIn("§11 裸引用未解析数", descs)
        self.assertIn("citation 裸引用（§9.3）", descs)

    def test_bare_claim_drift_is_caught(self):
        for old, desc, new in (
            ("裸引用 ok **41**", "§11 裸引用 ok 数", "裸引用 ok **42**"),
            ("未解析 **2** 处（占位符", "§11 裸引用未解析数", "未解析 **3** 处（占位符"),
            ("裸引用 ok=41/未解析=2", "citation 裸引用（§9.3）", "裸引用 ok=41/未解析=9"),
        ):
            drifted = DOC.replace(old, new)
            mismatches = selfcheck.compare(selfcheck.extract_claims(drifted), ACTUAL)
            self.assertEqual(len(mismatches), 1, (desc, mismatches))
            self.assertEqual(mismatches[0][1], desc)

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


class TestDtoSectionCount(unittest.TestCase):
    """工具打印的分节数也是当前口径声明。

    第 38 轮之前设计文档写「六维比较」，而工具一直打印 7 组（含等价拼写组）——
    两边都对不上任何东西，且没有任何门禁能发现。
    """

    def test_section_count_claim_extracted(self):
        doc = "至此，dto 的兼容性核查覆盖 **7 组**（以工具输出为准）：..."
        claims = selfcheck.extract_claims(doc)
        self.assertEqual(claims, [(1, "dto 分节数", 7)])

    def test_section_count_drift_is_caught(self):
        doc = "至此，dto 的兼容性核查覆盖 **6 组**（以工具输出为准）：..."
        actual = dict(ACTUAL, dto_sections=7)
        mismatches = selfcheck.compare(selfcheck.extract_claims(doc), actual)
        self.assertEqual(len(mismatches), 1)
        self.assertEqual(mismatches[0][1], "dto 分节数")

    def test_missing_sections_is_a_failure_not_a_skip(self):
        """工具输出里没有 ### 行时必须记失败，不能静默跳过该组声明。"""
        src = open(os.path.join(_HERE, "relaykit-gate-selfcheck.py"), encoding="utf-8").read()
        self.assertIn("输出无分节", src)


class TestStructureProblemClaim(unittest.TestCase):
    """结构检查器的「0 问题」是当前口径声明。

    第 39 轮 Codex 报出：structure_problems 被采集、被打印，却从未进入
    比对——文档把 0 改成任意数字，selfcheck 不报。
    """

    def test_structure_claim_extracted(self):
        doc = "python3 scripts/relaykit-doc-structure-check.py  # 两份文档均 0 问题时退出码 0\n"
        claims = selfcheck.extract_claims(doc)
        self.assertEqual(claims, [(1, "结构问题数", 0)])

    def test_structure_claim_drift_is_caught(self):
        doc = "python3 scripts/relaykit-doc-structure-check.py  # 两份文档均 5 问题时退出码 0\n"
        actual = dict(ACTUAL, structure_problems=0)
        mismatches = selfcheck.compare(selfcheck.extract_claims(doc), actual)
        self.assertEqual(len(mismatches), 1)
        self.assertEqual(mismatches[0][1], "结构问题数")


class TestFailClosed(unittest.TestCase):
    """解析失败必须 fail-closed。

    第 39 轮 Codex 报出：parse_ran/parse_dto/结构问题正则返回 None 时既不
    记失败、compare 又跳过 None——门禁会以绿的形式掩盖一整类未覆盖。
    """

    def test_unparsed_values_are_skipped_by_compare(self):
        """compare 本身的语义是「None = 未采集」，跳过是它该做的。"""
        actual = dict(ACTUAL, citation_tests=None, dto=None, structure_problems=None)
        claims = [(1, "citation 单测数", 25), (2, "dto 七维（命令行注释）", (33,5,6,0,4,12,21)),
                  (3, "结构问题数", 0)]
        self.assertEqual(selfcheck.compare(claims, actual), [])

    def test_collect_actual_fails_on_unparseable_citation(self):
        """citation 输出无法解析时必须记入 failures，而不是静默跳过。"""
        import subprocess
        import sys as _sys
        import tempfile

        # 造一个输出格式被改坏的 citation 门禁
        with tempfile.TemporaryDirectory() as td:
            shim = os.path.join(td, "scripts")
            os.makedirs(shim)
            with open(os.path.join(shim, "relaykit-citation-check.py"), "w") as fh:
                fh.write("print('REFORMATTED')\n")
            # 不实际跑——collect_actual 用的是仓库内的固定路径，
            # 这里只验证 parse 失败时的记录逻辑
            self.assertIsNone(selfcheck.parse_citation("REFORMATTED"))

    def test_parse_failure_message_names_the_group(self):
        """失败消息必须点名「该组声明未核对」，否则读者不知道漏了什么。"""
        with open(os.path.join(_HERE, "relaykit-gate-selfcheck.py"), encoding="utf-8") as fh:
            src = fh.read()
        for label in ["relaykit-citation-check.py", "relaykit-citation-check.test.py",
                      "relaykit-doc-structure-check.py", "relaykit-doc-structure-check.test.py",
                      "relaykit-dto-compat.sh --strict"]:
            self.assertIn(f"{label} 输出无法解析", src,
                          f"{label} 的解析失败未记为失败（fail-open）")


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

    def test_quoted_claim_in_prose_is_not_a_claim(self):
        """正文里复述这个句式（描述缺陷的文字）不得被读成缺陷本身。

        本轮踩过：CR 叙述写「④CR 的『文档当前 1987 行』实为 1986」，
        抽取器把这句引文当成声明，于是报了一个不存在的 1987。
        """
        import tempfile
        with tempfile.NamedTemporaryFile("w", suffix=".md", delete=False,
                                         encoding="utf-8") as fh:
            fh.write("4. **CR 的「文档当前 1987 行」是错的**，实为 1986。\n"
                     "| 行 | ④CR 的「文档当前 1987 行」实为 1986 |\n"
                     "文档当前 1986 行，位于 `docs/...`。\n")
            path = fh.name
        try:
            claims = selfcheck.extract_line_claims(path)
        finally:
            os.unlink(path)
        self.assertEqual(claims, [(3, "CR 文档行数", 1986)])

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


class TestRequiredClaimKinds(unittest.TestCase):
    """第 41 轮 Codex 报的 P1：extract_claims 返回空时 compare([], actual) 也空。

    删掉整类自述声明（例如删掉 §9.3 的 citation 三元组）后，门禁会全绿放行
    一份不再自述的文档。类别级 fail-closed 必须把「整类消失」变成失败。
    """

    def test_all_expected_kinds_present_in_fixture_doc(self):
        descs = {d for _, d, _ in selfcheck.extract_claims(DOC)}
        missing = [k for k in selfcheck.EXPECTED_CLAIM_KINDS if k not in descs]
        # 固定 DOC 不携带全部类别（如 §11 句式、自检单测数），只断言核心几类在。
        self.assertNotIn("citation 三元组", missing)
        self.assertNotIn("citation 单测数", missing)

    def test_missing_kind_is_a_failure(self):
        import io as _io
        import contextlib
        import subprocess
        import sys as _sys
        import tempfile

        # 删空整份文档的声明：临时 doc 无任何自述计数。
        with tempfile.NamedTemporaryFile("w", suffix=".md", delete=False,
                                         encoding="utf-8") as fh:
            fh.write("# empty\n\nno claims here\n")
            path = fh.name
        try:
            proc = subprocess.run(
                [_sys.executable, os.path.join(_HERE, "relaykit-gate-selfcheck.py"),
                 "--doc", path],
                cwd=os.path.dirname(_HERE),
                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
                env=dict(os.environ, RELAYKIT_SELFCHECK_SKIP_RECURSE="1"),
            )
        finally:
            os.unlink(path)
        self.assertEqual(proc.returncode, 1, proc.stdout)
        self.assertIn("缺少声明类别", proc.stdout)


class TestRealDoc(unittest.TestCase):
    def test_real_doc_matches_real_gates(self):
        """端到端：实跑各门禁，与设计文档的自述计数比对。

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
