#!/usr/bin/env python3
"""Check that every relaykit line citation in the migration design doc still
resolves at the pinned upstream commit.

The design doc cites upstream relaykit by `path:line` (13 citations across 9
files). Those line numbers are only valid at the pinned commit recorded in
section 1 (`upstream/main` = 789c97019). Upstream relaykit keeps moving, so a
later bump silently invalidates them -- and nothing detected that until now.

This script extracts every `relaykit/<path>.go:<line>` from the doc and
verifies, against the pinned ref, that:

  1. the file exists at that ref
  2. the line number is in range
  3. the cited line is not blank

It does NOT assert what the line says -- that is the reviewer's job when a
citation moves. It only catches the mechanical failure (file gone, line out of
range, blank line) that means "the doc's anchor has drifted".

Usage:
  scripts/relaykit-citation-check.py [--ref REF] [--doc PATH]

Exit codes:
  0  every citation resolves
  1  at least one citation does not resolve
  2  the doc has no citations (something is wrong with the doc or the parser)
"""

import argparse
import re
import subprocess
import sys

CITATION = re.compile(r"relaykit/([A-Za-z0-9_./-]+\.go):(\d+)")

# The doc also cites relaykit files by shorthand that omits the
# `relaykit/relayconvert/` prefix -- e.g. `internal/oai_chat/foo.go:12`,
# `oai_chat/foo.go:12`, `convmeta/meta.go:58`, `shared/claude/schema.go:3`.
# Those were invisible to CITATION (which requires the full prefix), so 23
# unique shorthand citations sat outside the gate while it still reported
# green. SHORT_PACKAGES lists the package roots that exist only under
# relaykit, each mapped to its real directory -- verified against the
# upstream tree, not guessed. Never relay/, service/, controller/ or a bare
# dto/: the host has its own dto/ and the doc's `dto/...:NN` refs are the
# host's.
SHORT_PACKAGES = {
    "internal": "relaykit/relayconvert/internal/",
    "oai_chat": "relaykit/relayconvert/internal/oai_chat/",
    "oai_responses": "relaykit/relayconvert/internal/oai_responses/",
    "claude_messages": "relaykit/relayconvert/internal/claude_messages/",
    "gemini_chat": "relaykit/relayconvert/internal/gemini_chat/",
    "shared": "relaykit/relayconvert/internal/shared/",
    "media": "relaykit/relayconvert/internal/media/",
    "convdiag": "relaykit/relayconvert/internal/convdiag/",
    "convmeta": "relaykit/relayconvert/convmeta/",
    "kitutil": "relaykit/relayconvert/kitutil/",
    "reasonmap": "relaykit/reasonmap/",
}

SHORT_CITATION = re.compile(
    r"(?<![A-Za-z0-9_./-])"
    r"((?:" + "|".join(SHORT_PACKAGES) + r")/[A-Za-z0-9_./-]+\.go):(\d+)"
)

DEFAULT_REF = "upstream/main"
DEFAULT_DOC = "docs/superpowers/specs/2026-09-30-relaykit-migration-design.md"


def _expand(path):
    """Map a shorthand path to its real relaykit path via SHORT_PACKAGES."""
    root = path.split("/", 1)[0]
    base = SHORT_PACKAGES.get(root)
    if base is None:
        return None
    return base + path.split("/", 1)[1]


def extract_citations(text):
    """Return sorted unique (path, line) pairs, path relative to repo root."""
    seen = {}
    for path, line in CITATION.findall(text):
        seen.setdefault(("relaykit/" + path, int(line)), None)
    for path, line in SHORT_CITATION.findall(text):
        expanded = _expand(path)
        if expanded is not None:
            seen.setdefault((expanded, int(line)), None)
    return sorted(seen)


def read_at(ref, path):
    """Return the file's lines at ref, or None if it does not exist there."""
    result = subprocess.run(
        ["git", "show", f"{ref}:{path}"],
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        return None
    return result.stdout.split("\n")


def check(doc_text, ref, reader=read_at):
    """Return (ok_count, failures). reader(ref, path) -> lines or None."""
    failures = []
    ok = 0
    for path, line in extract_citations(doc_text):
        lines = reader(ref, path)
        if lines is None:
            failures.append((path, line, "file not found at ref"))
            continue
        if line < 1 or line > len(lines):
            failures.append((path, line, f"out of range (file has {len(lines)} lines)"))
            continue
        if not lines[line - 1].strip():
            failures.append((path, line, "cited line is blank"))
            continue
        ok += 1
    return ok, failures


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ref", default=DEFAULT_REF, help="pinned upstream ref")
    parser.add_argument("--doc", default=DEFAULT_DOC, help="design doc path")
    args = parser.parse_args(argv)

    with open(args.doc, encoding="utf-8") as handle:
        doc_text = handle.read()

    citations = extract_citations(doc_text)
    if not citations:
        print("no relaykit citations found in the doc", file=sys.stderr)
        return 2

    ok, failures = check(doc_text, args.ref)
    print(f"ref={args.ref}  citations={len(citations)}  ok={ok}  failed={len(failures)}")
    for path, line, why in failures:
        print(f"  FAIL {path}:{line}  {why}", file=sys.stderr)
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
