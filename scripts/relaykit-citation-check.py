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
DEFAULT_BASELINE = "dbfbc096f"
DEFAULT_DOC = "docs/superpowers/specs/2026-09-30-relaykit-migration-design.md"

# ---------------------------------------------------------------------------
# Host paths (round 36)
# ---------------------------------------------------------------------------
# The design doc also cites HOST files -- relay/, service/, dto/, ... -- which
# this checker has always ignored (the original brief was upstream relaykit
# line refs only). Two rounds of review found citations that resolve at NEITHER
# ref, silently: a wrong line number (service/convert.go:533 for a function at
# :526), and upstream-only paths written without the upstream/main: prefix
# (resolving only because a same-named file happens to exist locally is exactly
# what the gate must not tolerate).
#
# Host citations resolve at the local BASELINE ref declared in the doc header.
# Citations spelled "upstream/main:<path>:NN" resolve at upstream/main.
# A bare host path that only resolves upstream is reported as needing its
# prefix -- the message says so, which is the intended use for the deliberate
# full-format cites.
HOST_DIRS = (
    "relay", "service", "controller", "common", "setting", "model", "dto",
    "types", "constant", "middleware", "logger", "bin", "tools", "web",
    "scripts", "docs", "deploy",
)
HOST_SUFFIX = r"\.(?:go|sh|py|yml|yaml|md|json)"

# Not preceded by a path char or ':' -- so "upstream/main:service/x.go:15"
# and "dbfbc096f:relay/y.go:670" are excluded (those are covered by
# extract_upstream_full / their sha form respectively).
HOST_CITATION = re.compile(
    r"(?<![A-Za-z0-9_./:-])((?:" + "|".join(HOST_DIRS) + r")/[A-Za-z0-9_./-]+"
    + HOST_SUFFIX + r"):(\d+)"
)

# Full-format upstream citations for non-relaykit host paths. relaykit/ paths
# under this prefix are already captured by CITATION (it has no lookbehind);
# duplicates are dropped by (path, line) in main().
UPSTREAM_FULL = re.compile(
    r"upstream/main:([A-Za-z0-9_./-]+" + HOST_SUFFIX + r"):(\d+)"
)


def extract_host_citations(text):
    """Bare host citations as sorted unique (path, line) pairs."""
    seen = {}
    for path, line in HOST_CITATION.findall(text):
        seen.setdefault((path, int(line)), None)
    return sorted(seen)


def extract_upstream_full(text):
    """upstream/main:<path>:<line> citations, excluding relaykit/ paths.

    relaykit/ paths under this prefix are already captured by CITATION (which
    has no lookbehind), so they are dropped here to avoid double counting.
    """
    seen = {}
    for path, line in UPSTREAM_FULL.findall(text):
        if path.startswith("relaykit/"):
            continue
        seen.setdefault((path, int(line)), None)
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


def check_host(doc_text, ref, upstream_ref=DEFAULT_REF, reader=read_at):
    """Resolve bare host citations at ref; report upstream-only ones as such.

    Returns (ok_count, failures, skipped_count). skipped = doc/ files that do
    not exist at ref (documentation-only citations, e.g. path:NN into a doc's
    own sections) -- they are skipped only when absent at BOTH refs; an
    existing file with a bad line still fails.
    """
    failures = []
    ok = skipped = 0
    for path, line in extract_host_citations(doc_text):
        lines = reader(ref, path)
        if lines is None:
            up = reader(upstream_ref, path)
            if path.startswith("docs/"):
                skipped += 1
                continue
            if up is not None:
                failures.append(
                    (path, line, f"not found at baseline {ref} but found at "
                                 f"{upstream_ref}: add an {upstream_ref}: prefix")
                )
            else:
                failures.append((path, line, f"file not found at {ref}"))
            continue
        if not 1 <= line <= len(lines):
            failures.append(
                (path, line, f"out of range at {ref} (file has {len(lines)} lines)")
            )
            continue
        if not lines[line - 1].strip():
            failures.append((path, line, f"cited line blank at {ref}"))
            continue
        ok += 1
    return ok, failures, skipped


def check_upstream_full(doc_text, ref, reader=read_at):
    """Resolve upstream/main:<path>:<line> citations at ref."""
    failures = []
    ok = 0
    for path, line in extract_upstream_full(doc_text):
        lines = reader(ref, path)
        if lines is None:
            failures.append((path, line, "file not found at ref"))
            continue
        if not 1 <= line <= len(lines):
            failures.append(
                (path, line, f"out of range (file has {len(lines)} lines)")
            )
            continue
        if not lines[line - 1].strip():
            failures.append((path, line, "cited line is blank"))
            continue
        ok += 1
    return ok, failures


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
    parser.add_argument("--baseline", default=DEFAULT_BASELINE,
                        help="local baseline ref for host-path citations")
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

    # Host paths (round 36): bare host refs resolve at the local baseline;
    # upstream/main:<path>:NN refs resolve at the upstream ref.
    h_ok, h_failures, h_skipped = check_host(doc_text, args.baseline, args.ref)
    u_ok, u_failures = check_upstream_full(doc_text, args.ref)
    print(f"baseline={args.baseline}  host-citations ok={h_ok}  failed={len(h_failures)}  skipped={h_skipped}")
    print(f"upstream-full ok={u_ok}  failed={len(u_failures)}")
    for path, line, why in h_failures:
        print(f"  FAIL host {path}:{line}  {why}", file=sys.stderr)
    for path, line, why in u_failures:
        print(f"  FAIL upstream {path}:{line}  {why}", file=sys.stderr)

    return 1 if (failures or h_failures or u_failures) else 0


if __name__ == "__main__":
    sys.exit(main())
