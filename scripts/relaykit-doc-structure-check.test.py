#!/usr/bin/env python3
"""relaykit-doc-structure-check.py 的单测。

每个检查类都要有一条「能抓出问题」的用例，否则该类等于没检查。
另有一条「干净输入必须通过」的用例，防止检查器本身误报。

    python3 scripts/relaykit-doc-structure-check.test.py
"""

import importlib.util
import os
import unittest

_HERE = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location(
    "doc_structure_check", os.path.join(_HERE, "relaykit-doc-structure-check.py")
)
checker = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(checker)


def kinds(problems):
    return sorted({k for _, k, _ in problems})


def run_text(text, defined=None, unresolved=None, master_present=True):
    lines = text.split("\n")
    if defined is None:
        defined = checker.defined_sections(text)
    if unresolved is None:
        unresolved = checker.unresolved_master_numbers(text)
    problems = (
        checker.check_tables(lines)
        + checker.check_section_refs(text, defined)
        + checker.check_unresolved_refs(text, unresolved)
    )
    if not master_present:
        problems += checker.check_unresolved_master_present([text])
    return problems


class TestTables(unittest.TestCase):
    def test_out_of_order_rows_caught(self):
        text = "| # | x |\n|---|---|\n| 1 | a |\n| 3 | a |\n| 2 | a |\n"
        self.assertEqual(kinds(run_text(text)), ["A"])

    def test_duplicate_rows_caught(self):
        """[1,2,3,3,5] 是 sorted 的——只查非排序会漏掉重复编号。"""
        text = "| # | x |\n|---|---|\n| 1 | a |\n| 2 | a |\n| 3 | a |\n| 3 | a |\n| 5 | a |\n"
        self.assertEqual(kinds(run_text(text)), ["A"])

    def test_lettered_sub_rows_ordered(self):
        """9.2.3 表用 5a/5b 子编号，排序键是 (N, 后缀)：5a < 5b < 6。"""
        text = "| # | x |\n|---|---|\n| 1 | a |\n| 5a | a |\n| 5b | a |\n| 6 | a |\n"
        self.assertEqual(run_text(text), [])

    def test_lettered_sub_rows_out_of_order_caught(self):
        text = "| # | x |\n|---|---|\n| 1 | a |\n| 5b | a |\n| 5a | a |\n| 6 | a |\n"
        self.assertEqual(kinds(run_text(text)), ["A"])

    def test_orphan_sub_letter_caught(self):
        """`5b` 无 `5a` 多半是「补记的行被误标成 5b」——读者会去找不存在的 5a。"""
        text = "| # | x |\n|---|---|\n| 1 | a |\n| 2 | a |\n| 5b | a |\n| 6 | a |\n"
        self.assertEqual(kinds(run_text(text)), ["A"])

    def test_legit_split_without_base_row_passes(self):
        """9.2.3 把第 5 条拆成 5a/5b，没有裸 5 行——这是合法的。"""
        text = "| # | x |\n|---|---|\n| 1 | a |\n| 5a | a |\n| 5b | a |\n| 6 | a |\n"
        self.assertEqual(run_text(text), [])

    def test_two_row_table_is_a_known_blind_spot(self):
        """已知边界（有意保留）：少于 3 个编号行的表格不检查行序。

        两行表格反序（2 然后 1）当前**不被抓住**——阈值是 len(seq) >= 3。
        此用例把边界钉住：若将来改了阈值，这里会红，提醒同步文档说明。
        """
        text = "| # | x |\n|---|---|\n| 2 | a |\n| 1 | a |\n"
        self.assertEqual(run_text(text), [])

    def test_number_gap_is_a_known_blind_spot(self):
        """已知边界（有意保留）：只查单调与重复，不查跳号（1,5,9 通过）。

        跳号在手工编辑里完全可能是有意的（删了一行没重排是缺陷，但删了一行
        本就该重排、而「有意留空」无法与缺陷区分），故不设为失败条件。
        """
        text = "| # | x |\n|---|---|\n| 1 | a |\n| 5 | a |\n| 9 | a |\n"
        self.assertEqual(run_text(text), [])

    def test_monotonic_rows_pass(self):
        text = "| # | x |\n|---|---|\n| 1 | a |\n| 2 | a |\n| 3 | a |\n"
        self.assertEqual(run_text(text), [])

    def test_column_mismatch_caught(self):
        text = "| # | x | y |\n|---|---|---|\n| 1 | a | b |\n| 2 | a | b | c |\n"
        self.assertEqual(kinds(run_text(text)), ["B"])

    def test_escaped_pipe_does_not_split_cell(self):
        text = "| # | cmd |\n|---|---|\n| 1 | `a \\| b` |\n| 2 | `c` |\n"
        self.assertEqual(run_text(text), [])


class TestSectionRefs(unittest.TestCase):
    def test_undefined_section_caught(self):
        text = "## 12. 决策\n\n见 §12.99。\n"
        self.assertEqual(kinds(run_text(text)), ["C"])

    def test_defined_section_passes(self):
        text = "## 12. 决策\n\n### 12.5 x\n\n见 §12.5。\n"
        self.assertEqual(run_text(text), [])

    def test_allowlist_does_not_swallow_a_real_section(self):
        """4.6 在白名单里（Opus 4.6），但若文档真的定义了 4.6，引用它不该报警。"""
        text = "## 4. 本地\n\n### 4.6 真章节\n\n见 §4.6。\n"
        self.assertEqual(run_text(text), [])

    def test_prefixed_dotted_ref_of_any_depth_caught(self):
        # 旧正则最多捕三段，四段编号的尾段被数字断开后整个不匹配；
        # 第 41 轮补：带「§」的引用检查任意段数。
        # 顶层号 2/10/12 都须是本组文档的真实章节头，引用才可判定。
        defined = checker.defined_sections("## 2. x\n## 10. y\n## 12.15.4\n")
        for ref in ("§10.999", "§2.999", "§12.15.4.99"):
            self.assertIn("C", kinds(run_text(f"见 {ref}。", defined=defined)))

    def test_ref_into_a_third_document_is_not_a_section_ref(self):
        # CR 会引用第三份文档（如 CR-FULL-AUDIT 的 §22.4、§26.1），那些章节头
        # 不在本组文档里；顶层号不在 defined 的无点号项里就不算引用。
        defined = checker.defined_sections("## 12. 决策\n")
        self.assertEqual(run_text("见 §22.4 与 §26.1。", defined=defined), [])

    def test_bare_version_numbers_are_not_section_refs(self):
        # 裸点号数只认顶层号在章节头集合里的——v1.52.0 / go 1.25.1 与章节号
        # 同形，实测放宽会误报 12 个版本号。
        text = "go 1.25.1，v1.52.0 与 v0.35.0 与 §4.5。\n"
        self.assertEqual(run_text(text, defined={"4.5"}), [])

    def test_bare_version_numbers_are_not_section_refs(self):
        # 裸点号数只认顶层号在章节头集合里的——v1.52.0 / go 1.25.1 与章节号
        # 同形，实测放宽会误报 12 个版本号。
        text = "go 1.25.1，v1.52.0 与 v0.35.0 与 §4.5。\n"
        self.assertEqual(run_text(text, defined={"4.5"}), [])

    def test_cross_file_defined_section_passes(self):
        """CR 引用设计文档的章节号：defined 是并集，不该误报。"""
        cr = "见 §12.20.1。\n"
        self.assertEqual(run_text(cr, defined={"12.20.1"}), [])


class TestUnresolvedRefs(unittest.TestCase):
    MASTER = "## 10. 未决项\n\n1. a\n2. b\n\n## 11. 事实依据\n"

    def test_ref_beyond_master_caught(self):
        text = self.MASTER + "见未决项 9。\n"
        self.assertEqual(kinds(run_text(text)), ["D"])

    def test_ref_within_master_passes(self):
        text = self.MASTER + "见未决项 2。\n"
        self.assertEqual(run_text(text), [])

    def test_no_master_table_fails_check(self):
        # 第 41 轮 Codex 报的 D 假阴性：主表小节缺失（改名/删除）时检查器
        # 静默返回空——「未决项 N」引用无人校验，门禁照样全绿。现在判失败。
        text = "# x\n\n见未决项 9。\n"
        self.assertIn("D", kinds(run_text(text, master_present=False)))

    def test_no_master_table_passes_when_master_elsewhere(self):
        # 主表存在性看的是被检查文档**的并集**（CR 的编号由设计文档供给），
        # 不是单文件：CR 自身没有第 10 节不得因此判失败。
        text = "# x\n\n见未决项 9。\n"
        master = "## 10. 未决项\n\n1. a\n2. b\n"
        self.assertEqual(
            checker.check_unresolved_master_present([text, master]),
            [],
        )
        self.assertEqual(checker.unresolved_master_numbers(master), {1, 2})


class TestRealDocuments(unittest.TestCase):
    """两份真实文档当前必须零结构问题——这条同时防止检查器本身误报。"""

    def test_default_targets_are_clean(self):
        root = os.path.dirname(_HERE)
        texts = []
        for t in checker.DEFAULT_TARGETS:
            with open(os.path.join(root, t), encoding="utf-8") as fh:
                texts.append(fh.read())
        defined = set()
        for text in texts:
            defined |= checker.defined_sections(text)
        unresolved = set()
        for text in texts:
            nums = checker.unresolved_master_numbers(text)
            if nums is not None:
                unresolved |= nums
        for path, text in zip(checker.DEFAULT_TARGETS, texts):
            problems = run_text(text, defined=defined, unresolved=unresolved or None)
            self.assertEqual(problems, [], f"{path} 有结构问题：{problems[:5]}")


class TestCLIWiring(unittest.TestCase):
    """端到端钉住主表缺失必须非零退出——不能只测 helper。

    第 41 轮反向验证发现：helper 的单测(passing)不覆盖编排层；把 main 里的
    check_unresolved_master_present 调用删掉后所有单测仍绿。必须有一个真跑
    CLI 的用例，否则「删掉第 10 节」的修复可被无声移除。
    """

    def test_cli_exits_nonzero_when_master_heading_absent(self):
        import subprocess
        import sys
        import tempfile

        with tempfile.NamedTemporaryFile("w", suffix=".md", delete=False,
                                         encoding="utf-8") as fh:
            fh.write("# 文档\n\n见「未决项 9」。\n")
            path = fh.name
        try:
            proc = subprocess.run(
                [sys.executable, os.path.join(_HERE, "relaykit-doc-structure-check.py"), path],
                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
            )
        finally:
            os.unlink(path)
        self.assertEqual(proc.returncode, 1, proc.stdout)
        self.assertIn("无正源", proc.stdout)


class TestNoResourceLeak(unittest.TestCase):
    """读文件必须用 with——第 39 轮 Codex 报出 line 141 泄漏文件句柄。"""

    def test_real_doc_read_has_no_unclosed_file(self):
        import gc
        import warnings

        root = os.path.dirname(_HERE)
        with warnings.catch_warnings(record=True) as caught:
            warnings.simplefilter("always", ResourceWarning)
            texts = []
            for t in checker.DEFAULT_TARGETS:
                with open(os.path.join(root, t), encoding="utf-8") as fh:
                    texts.append(fh.read())
            gc.collect()
        leaks = [w for w in caught if issubclass(w.category, ResourceWarning)]
        self.assertEqual(leaks, [])


if __name__ == "__main__":
    unittest.main(verbosity=2)
