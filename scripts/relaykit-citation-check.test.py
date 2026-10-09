#!/usr/bin/env python3

import importlib.util
import pathlib
import unittest


MODULE_PATH = pathlib.Path(__file__).with_name("relaykit-citation-check.py")
SPEC = importlib.util.spec_from_file_location("relaykit_citation_check", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class ExtractCitationsTests(unittest.TestCase):
    def test_extracts_path_and_line(self):
        text = "see relaykit/relayconvert/convmeta/meta.go:18 for details"
        self.assertEqual(
            MODULE.extract_citations(text),
            [("relaykit/relayconvert/convmeta/meta.go", 18)],
        )

    def test_deduplicates_repeated_citations(self):
        text = "relaykit/dto/values.go:32 and again relaykit/dto/values.go:32"
        self.assertEqual(MODULE.extract_citations(text), [("relaykit/dto/values.go", 32)])

    def test_sorts_by_path_then_line(self):
        text = "relaykit/b.go:9 relaykit/a.go:1 relaykit/a.go:20"
        self.assertEqual(
            MODULE.extract_citations(text),
            [
                ("relaykit/a.go", 1),
                ("relaykit/a.go", 20),
                ("relaykit/b.go", 9),
            ],
        )

    def test_ignores_non_relaykit_line_refs(self):
        text = "service/convert.go:730 and docs/foo.md:12"
        self.assertEqual(MODULE.extract_citations(text), [])

    def test_ignores_bare_relaykit_dir_without_line(self):
        self.assertEqual(MODULE.extract_citations("relaykit/dto/ has types"), [])

    def test_expands_short_internal_paths(self):
        # internal/... is a shorthand for relaykit/relayconvert/internal/...
        text = "see internal/oai_chat/to_claude_messages_resp.go:345 for the gate"
        self.assertEqual(
            MODULE.extract_citations(text),
            [("relaykit/relayconvert/internal/oai_chat/to_claude_messages_resp.go", 345)],
        )

    def test_expands_bare_package_paths(self):
        # oai_chat/... has no internal/ component, so it maps under
        # relaykit/relayconvert/internal/ as well.
        text = "internal and bare: oai_chat/to_claude_messages_req.go:277"
        self.assertEqual(
            MODULE.extract_citations(text),
            [("relaykit/relayconvert/internal/oai_chat/to_claude_messages_req.go", 277)],
        )

    def test_expands_convmeta_and_shared_paths(self):
        text = "convmeta/meta.go:58 shared/claude/schema.go:3 shared/gemini/request.go:85"
        self.assertEqual(
            MODULE.extract_citations(text),
            [
                ("relaykit/relayconvert/convmeta/meta.go", 58),
                ("relaykit/relayconvert/internal/shared/claude/schema.go", 3),
                ("relaykit/relayconvert/internal/shared/gemini/request.go", 85),
            ],
        )

    def test_does_not_expand_host_paths(self):
        # relay/, service/, controller/ and a bare dto/ are host paths; the
        # checker must not claim them.
        text = "relay/claude_handler.go:55 service/convert.go:749 dto/gemini.go:112"
        self.assertEqual(MODULE.extract_citations(text), [])

    def test_keeps_full_relaykit_paths_unchanged(self):
        text = "relaykit/relayconvert/convmeta/options.go:9"
        self.assertEqual(
            MODULE.extract_citations(text),
            [("relaykit/relayconvert/convmeta/options.go", 9)],
        )


class CheckTests(unittest.TestCase):
    def test_resolvable_citation_passes(self):
        doc = "relaykit/dto/values.go:2"
        lines = ["package dto", "type IntValue int", ""]

        ok, failures = MODULE.check(doc, "ref", reader=lambda r, p: lines)

        self.assertEqual(ok, 1)
        self.assertEqual(failures, [])

    def test_missing_file_fails(self):
        doc = "relaykit/dto/gone.go:1"

        ok, failures = MODULE.check(doc, "ref", reader=lambda r, p: None)

        self.assertEqual(ok, 0)
        self.assertIn("file not found", failures[0][2])

    def test_line_out_of_range_fails(self):
        doc = "relaykit/dto/values.go:99"
        lines = ["package dto"]

        ok, failures = MODULE.check(doc, "ref", reader=lambda r, p: lines)

        self.assertEqual(ok, 0)
        self.assertIn("out of range", failures[0][2])

    def test_blank_cited_line_fails(self):
        doc = "relaykit/dto/values.go:2"
        lines = ["package dto", "   ", ""]

        ok, failures = MODULE.check(doc, "ref", reader=lambda r, p: lines)

        self.assertEqual(ok, 0)
        self.assertIn("blank", failures[0][2])

    def test_mixed_pass_and_fail_reports_each(self):
        doc = "relaykit/a.go:1 relaykit/b.go:1 relaykit/c.go:1"

        def reader(ref, path):
            return ["package p", "type T int"] if path == "relaykit/a.go" else None

        ok, failures = MODULE.check(doc, "ref", reader=reader)

        self.assertEqual(ok, 1)
        self.assertEqual(len(failures), 2)


class HostCitationTests(unittest.TestCase):
    """Round 36: host paths (relay/, service/, dto/, ...) are now gated.

    Before this, the checker only claimed relaykit/ paths, so a wrong host line
    number or an upstream-only path written without its prefix passed silently.
    """

    def test_extracts_host_paths(self):
        text = "see service/convert.go:526 and relay/common/relay_info.go:358"
        self.assertEqual(
            MODULE.extract_host_citations(text),
            [("relay/common/relay_info.go", 358), ("service/convert.go", 526)],
        )

    def test_does_not_extract_upstream_prefixed_host_paths(self):
        # upstream/main:service/x.go:15 is handled by extract_upstream_full,
        # not by the bare-host extractor.
        text = "upstream/main:service/request_converter.go:50"
        self.assertEqual(MODULE.extract_host_citations(text), [])

    def test_does_not_extract_sha_prefixed_host_paths(self):
        text = "dbfbc096f:relay/common/relay_info.go:670"
        self.assertEqual(MODULE.extract_host_citations(text), [])

    def test_extracts_upstream_full_paths(self):
        text = "upstream/main:relay/claude_handler.go:41"
        self.assertEqual(
            MODULE.extract_upstream_full(text),
            [("relay/claude_handler.go", 41)],
        )

    def test_upstream_full_ignores_relaykit_paths(self):
        # relaykit/ paths under the same prefix are already claimed by CITATION.
        text = "upstream/main:relaykit/dto/values.go:2"
        self.assertEqual(MODULE.extract_upstream_full(text), [])

    def test_check_host_passes_resolvable(self):
        doc = "service/convert.go:2"
        lines = ["package service", "func A() {}", ""]

        ok, failures, skipped = MODULE.check_host(
            doc, "base", reader=lambda r, p: lines
        )

        self.assertEqual((ok, failures, skipped), (1, [], 0))

    def test_check_host_reports_wrong_line(self):
        doc = "service/convert.go:99"
        lines = ["package service", "func A() {}"]

        ok, failures, _ = MODULE.check_host(doc, "base", reader=lambda r, p: lines)

        self.assertEqual(ok, 0)
        self.assertIn("out of range", failures[0][2])

    def test_check_host_reports_missing_prefix_when_only_upstream_has_it(self):
        # The file does not exist at the baseline but does upstream: the
        # failure must name the prefix fix, not just "file not found".
        doc = "service/request_converter.go:50"

        def reader(ref, path):
            return ["package service", "func ClaudeToOpenAIRequest() {}"] if ref == "upstream/main" else None

        ok, failures, _ = MODULE.check_host(
            doc, "base", upstream_ref="upstream/main", reader=reader
        )

        self.assertEqual(ok, 0)
        self.assertIn("upstream/main: prefix", failures[0][2])

    def test_check_host_skips_doc_paths_absent_everywhere(self):
        doc = "docs/operations/pricing-maintenance.md:12"

        ok, failures, skipped = MODULE.check_host(doc, "base", reader=lambda r, p: None)

        self.assertEqual((ok, failures, skipped), (0, [], 1))

    def test_check_host_fails_doc_path_absent_at_baseline_but_present_upstream(self):
        # 第 41 轮 Codex 报的 fail-open：docs/ 路径以前无条件跳过，于是
        # 「上游有、基线没有、行号还是错的」也被静默放过。它其实可解析——
        # 必须要求 upstream/main: 前缀，而不是 skip。
        doc = "docs/translation-glossary.md:999"

        def reader(ref, path):
            return ["a", "b", "c"] if ref == "upstream/main" else None

        ok, failures, skipped = MODULE.check_host(
            doc, "base", upstream_ref="upstream/main", reader=reader
        )

        self.assertEqual(ok, 0)
        self.assertEqual(skipped, 0)
        self.assertIn("upstream/main: prefix", failures[0][2])

    def test_check_upstream_full_resolves(self):
        doc = "upstream/main:relay/claude_handler.go:2"
        lines = ["package relay", "func ClaudeHelper() {}"]

        ok, failures = MODULE.check_upstream_full(
            doc, "upstream/main", reader=lambda r, p: lines
        )

        self.assertEqual((ok, failures), (1, []))


if __name__ == "__main__":
    unittest.main()
