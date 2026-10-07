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


def run_text(text, defined=None, unresolved=None):
    lines = text.split("\n")
    if defined is None:
        defined = checker.defined_sections(text)
    if unresolved is None:
        unresolved = checker.unresolved_master_numbers(text)
    return (
        checker.check_tables(lines)
        + checker.check_section_refs(text, defined)
        + checker.check_unresolved_refs(text, unresolved)
    )


class TestTables(unittest.TestCase):
    def test_out_of_order_rows_caught(self):
        text = "| # | x |\n|---|---|\n| 1 | a |\n| 3 | a |\n| 2 | a |\n"
        self.assertEqual(kinds(run_text(text)), ["A"])

    def test_duplicate_rows_caught(self):
        """[1,2,3,3,5] 是 sorted 的——只查非排序会漏掉重复编号。"""
        text = "| # | x |\n|---|---|\n| 1 | a |\n| 2 | a |\n| 3 | a |\n| 3 | a |\n| 5 | a |\n"
        self.assertEqual(kinds(run_text(text)), ["A"])

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

    def test_no_master_table_skips_check(self):
        text = "# x\n\n见未决项 9。\n"
        self.assertEqual(run_text(text, unresolved=None), [])


class TestRealDocuments(unittest.TestCase):
    """两份真实文档当前必须零结构问题——这条同时防止检查器本身误报。"""

    def test_default_targets_are_clean(self):
        root = os.path.dirname(_HERE)
        texts = [
            open(os.path.join(root, t), encoding="utf-8").read()
            for t in checker.DEFAULT_TARGETS
        ]
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


if __name__ == "__main__":
    unittest.main(verbosity=2)
