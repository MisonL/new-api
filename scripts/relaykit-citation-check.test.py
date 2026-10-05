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


if __name__ == "__main__":
    unittest.main()
