#!/usr/bin/env python3
"""门禁自述计数检查：文档里写的「门禁输出」必须与门禁的实际输出一致。

这是第 38 轮之前反复复发的盲区——「门禁的自述计数是它自己的盲区」（第 36 轮
的 34/15、第 38 轮的 §9.3 47/98/21、以及检查器 docstring 里的 13 citations
across 9 files）。每次都是「门禁改了、描述没改」，且没有任何门禁能发现。

本脚本把这条规则变成门禁：
  1. 实跑四个门禁，解析它们的真实输出；
  2. 从设计文档抽出「当前口径」的自述计数；
  3. 逐一比对，任一不一致即退出码 1。

只检查设计文档里的**当前口径**声明。CR 记录里的计数是历史记录（「第 37 轮为
50/103/24」这类），它们记的是「当时为真」，不随门禁演进而失效，故不在此列。

Usage:
  python3 scripts/relaykit-gate-selfcheck.py [--doc PATH] [--quiet]

Exit codes:
  0  全部门禁自述计数与实际输出一致
  1  至少一处不一致，或某个门禁本身失败
"""

import argparse
import io
import os
import re
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DEFAULT_DOC = "docs/superpowers/specs/2026-09-30-relaykit-migration-design.md"


def run(cmd):
    """跑一条门禁命令，返回 (退出码, 合并后的输出)。"""
    proc = subprocess.run(
        cmd, cwd=ROOT, shell=isinstance(cmd, str),
        stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
    )
    return proc.returncode, proc.stdout


def parse_citation(out):
    """relaykit-citation-check.py 的输出 → (relaykit, host, upstream_full)。"""
    rk = re.search(r"citations=(\d+)\s+ok=(\d+)", out)
    host = re.search(r"host-citations ok=(\d+)", out)
    full = re.search(r"upstream-full ok=(\d+)", out)
    if not (rk and host and full):
        return None
    return int(rk.group(1)), int(host.group(1)), int(full.group(1))


def parse_ran(out):
    """unittest 的输出 → 测试数。"""
    m = re.search(r"Ran (\d+) tests?", out)
    return int(m.group(1)) if m else None


def parse_dto(out):
    """relaykit-dto-compat.sh --strict 的输出 → 7 个维度计数。"""
    m = re.search(
        r"计数:\s*fieldSet=(\d+)\s+tag=(\d+)\s+type=(\d+)\s+embed=(\d+)\s+"
        r"order=(\d+)\s+sig=(\d+)\s+equiv=(\d+)", out,
    )
    if not m:
        return None
    return tuple(int(g) for g in m.groups())


def collect_actual(quiet):
    """实跑四个门禁，返回 (计数 dict, 失败列表)。"""
    failures = []
    actual = {}

    code, out = run([sys.executable, "scripts/relaykit-citation-check.py"])
    if code != 0:
        failures.append(f"relaykit-citation-check.py 退出码 {code}")
    parsed = parse_citation(out)
    if parsed is None:
        failures.append("relaykit-citation-check.py 输出无法解析")
    else:
        actual["citation"] = parsed

    code, out = run([sys.executable, "scripts/relaykit-citation-check.test.py"])
    if code != 0:
        failures.append(f"relaykit-citation-check.test.py 退出码 {code}")
    actual["citation_tests"] = parse_ran(out)

    code, out = run([sys.executable, "scripts/relaykit-doc-structure-check.py"])
    if code != 0:
        failures.append(f"relaykit-doc-structure-check.py 退出码 {code}")
    m = re.search(r"合计 (\d+) 个结构问题", out)
    actual["structure_problems"] = int(m.group(1)) if m else None

    code, out = run([sys.executable, "scripts/relaykit-doc-structure-check.test.py"])
    if code != 0:
        failures.append(f"relaykit-doc-structure-check.test.py 退出码 {code}")
    actual["structure_tests"] = parse_ran(out)

    # 自检脚本自己的单测数。这一条是自指的：selfcheck.test.py 的端到端用例
    # 会反过来调用 collect_actual，故它在调用前设置 RELAYKIT_SELFCHECK_SKIP_RECURSE
    # 断开递归；置位时跳过本段，selfcheck_tests 为 None，compare() 随之跳过该声明。
    if os.environ.get("RELAYKIT_SELFCHECK_SKIP_RECURSE"):
        actual["selfcheck_tests"] = None
    else:
        code, out = run([sys.executable, "scripts/relaykit-gate-selfcheck.test.py"])
        if code != 0:
            failures.append(f"relaykit-gate-selfcheck.test.py 退出码 {code}")
        actual["selfcheck_tests"] = parse_ran(out)

    code, out = run("bash scripts/relaykit-dto-compat.sh --strict")
    if code != 0:
        failures.append(f"relaykit-dto-compat.sh --strict 退出码 {code}")
    actual["dto"] = parse_dto(out)

    if not quiet:
        print("门禁实际输出：")
        print(f"  citation        relaykit={actual['citation'][0]} host={actual['citation'][1]} full={actual['citation'][2]}")
        print(f"  citation 单测   {actual['citation_tests']}")
        print(f"  结构问题        {actual['structure_problems']}")
        print(f"  结构单测        {actual['structure_tests']}")
        print(f"  自检单测        {actual['selfcheck_tests']}")
        print(f"  dto 七维        {actual['dto']}")
        print()
    return actual, failures


def extract_claims(doc):
    """从设计文档抽出「当前口径」的自述计数，返回 [(行号, 描述, 值)]。"""
    lines = doc.split("\n")
    claims = []

    def add(line_no, desc, value):
        claims.append((line_no, desc, value))

    for i, line in enumerate(lines, 1):
        # 「（relaykit 51 + 宿主 107 + upstream 全路径 24）」
        for m in re.finditer(r"relaykit (\d+) \+ 宿主 (\d+) \+ upstream 全路径 (\d+)", line):
            add(i, "citation 三元组", tuple(int(g) for g in m.groups()))
        # 「**51/51**（`ref=upstream/main`）」
        for m in re.finditer(r"\*\*(\d+)/(\d+)\*\*（`ref=upstream/main`）", line):
            add(i, "citation relaykit 分子/分母", (int(m.group(1)), int(m.group(2))))
        # 「**宿主 107/107**（`baseline=dbfbc096f`）」
        for m in re.finditer(r"\*\*宿主 (\d+)/(\d+)\*\*（`baseline=dbfbc096f`）", line):
            add(i, "citation 宿主 分子/分母", (int(m.group(1)), int(m.group(2))))
        # 「**upstream 全路径 24/24**」
        for m in re.finditer(r"\*\*upstream 全路径 (\d+)/(\d+)\*\*", line):
            add(i, "citation 全路径 分子/分母", (int(m.group(1)), int(m.group(2))))
        # 「relaykit-citation-check.test.py  # 25 个单测」
        for m in re.finditer(r"relaykit-citation-check\.test\.py[^\n]*?(\d+) 个单测", line):
            add(i, "citation 单测数", int(m.group(1)))
        # 「relaykit-doc-structure-check.test.py   # 13 个单测」
        for m in re.finditer(r"relaykit-doc-structure-check\.test\.py[^\n]*?(\d+) 个单测", line):
            add(i, "结构单测数", int(m.group(1)))
        # 「relaykit-gate-selfcheck.test.py   # 14 个单测」
        for m in re.finditer(r"relaykit-gate-selfcheck\.test\.py[^\n]*?(\d+) 个单测", line):
            add(i, "自检单测数", int(m.group(1)))
        # 「# 计数: fieldSet=33 tag=5 type=6 embed=0 order=4 sig=12 equiv=21」
        for m in re.finditer(
            r"#\s*计数:\s*fieldSet=(\d+)\s+tag=(\d+)\s+type=(\d+)\s+embed=(\d+)\s+"
            r"order=(\d+)\s+sig=(\d+)\s+equiv=(\d+)", line,
        ):
            add(i, "dto 七维（命令行注释）", tuple(int(g) for g in m.groups()))
        # 「--strict` 把当前基线（fieldSet=33、tag=5、…）」
        for m in re.finditer(
            r"当前基线（fieldSet=(\d+)、tag=(\d+)、type=(\d+)、embed=(\d+)、"
            r"\*\*order=(\d+)\*\*、sig=(\d+)、\*\*equiv=(\d+)\*\*）", line,
        ):
            add(i, "dto 七维（正文）", tuple(int(g) for g in m.groups()))
    return claims


def compare(claims, actual):
    """返回不一致列表。"""
    mismatches = []
    for line_no, desc, claimed in claims:
        if desc == "citation 三元组":
            real = actual.get("citation")
            if real is not None and claimed != real:
                mismatches.append((line_no, desc, claimed, real))
        elif desc == "citation relaykit 分子/分母":
            real = actual.get("citation")
            if real is not None and claimed != (real[0], real[0]):
                mismatches.append((line_no, desc, claimed, (real[0], real[0])))
        elif desc == "citation 宿主 分子/分母":
            real = actual.get("citation")
            if real is not None and claimed != (real[1], real[1]):
                mismatches.append((line_no, desc, claimed, (real[1], real[1])))
        elif desc == "citation 全路径 分子/分母":
            real = actual.get("citation")
            if real is not None and claimed != (real[2], real[2]):
                mismatches.append((line_no, desc, claimed, (real[2], real[2])))
        elif desc == "citation 单测数":
            real = actual.get("citation_tests")
            if real is not None and claimed != real:
                mismatches.append((line_no, desc, claimed, real))
        elif desc == "结构单测数":
            real = actual.get("structure_tests")
            if real is not None and claimed != real:
                mismatches.append((line_no, desc, claimed, real))
        elif desc == "自检单测数":
            real = actual.get("selfcheck_tests")
            if real is not None and claimed != real:
                mismatches.append((line_no, desc, claimed, real))
        elif desc in ("dto 七维（命令行注释）", "dto 七维（正文）"):
            real = actual.get("dto")
            if real is not None and claimed != real:
                mismatches.append((line_no, desc, claimed, real))
    return mismatches


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--doc", default=DEFAULT_DOC)
    ap.add_argument("--quiet", action="store_true")
    args = ap.parse_args()

    actual, failures = collect_actual(args.quiet)

    doc_path = args.doc if os.path.isabs(args.doc) else os.path.join(ROOT, args.doc)
    doc = io.open(doc_path, encoding="utf-8").read()
    claims = extract_claims(doc)
    mismatches = compare(claims, actual)

    if not args.quiet:
        print(f"文档自述计数 {len(claims)} 处（{args.doc}）")

    if failures:
        print("\n门禁自身失败：")
        for f in failures:
            print(f"  {f}")

    if mismatches:
        print("\n自述计数与实际输出不一致：")
        for line_no, desc, claimed, real in mismatches:
            print(f"  {args.doc}:{line_no}  {desc}: 文档写 {claimed}，实际 {real}")

    if not failures and not mismatches:
        if not args.quiet:
            print("全部门禁自述计数与实际输出一致")
        return 0
    return 1


if __name__ == "__main__":
    sys.exit(main())
