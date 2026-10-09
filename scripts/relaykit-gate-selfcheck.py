#!/usr/bin/env python3
"""门禁自述计数检查：文档里写的「门禁输出」必须与门禁的实际输出一致。

这是第 38 轮之前反复复发的盲区——「门禁的自述计数是它自己的盲区」（第 36 轮
的 34/15、第 38 轮的 §9.3 47/98/21、以及检查器 docstring 里的 13 citations
across 9 files）。每次都是「门禁改了、描述没改」，且没有任何门禁能发现。

本脚本把这条规则变成门禁：
  1. 实跑各门禁脚本与单测（条数见运行输出，不在此复述），解析真实输出；
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
import shlex
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DEFAULT_DOC = "docs/superpowers/specs/2026-09-30-relaykit-migration-design.md"
CR_DOC = "docs/reviews/CR-RELAYKIT-MIGRATION-DESIGN-2026-09-30.md"
# 文档自己声明的上游锚点（第 40 轮）。上游 main 漂到 04c597bf6 后，默认 ref
# 下 citation 与 dto-compat 都会因移动的 ref 变红，而文档的计数声明是对钉死
# 锚点的声明——用 upstream/main 去核对钉死声明必然报错。升降锚点只改这里
# 加文档里对应的一处。
UPSTREAM_PIN = "789c97019"

# 设计文档当前必须携带的声明类别（第 41 轮）。extract_claims 抽不到某一类，
# 说明文档里那条「门禁自述」被删了——此时 compare([], actual) 恒为空，门禁会
# 以全绿放行一份不再自述的文档。类别级检查就是防这个：删掉一整类声明必须变红，
# 而不只是「没有不一致」。
EXPECTED_CLAIM_KINDS = (
    "citation 三元组",
    "citation relaykit 分子/分母",
    "citation 宿主 分子/分母",
    "citation 全路径 分子/分母",
    "citation 单测数",
    "结构单测数",
    "自检单测数",
    "§11 relaykit 引用数",
    "§11 宿主引用数",
    "§11 全路径引用数",
    "§11 裸引用 ok 数",
    "§11 裸引用未解析数",
    "citation 裸引用（§9.3）",
    "dto 分节数",
    "结构问题数",
    "dto 七维（命令行注释）",
    "dto 七维（正文）",
)


def read_text(path):
    """显式关闭的读文件——`io.open(p).read()` 依赖引用计数回收，在
    `-W error::ResourceWarning` 下会报 unclosed file（第 39 轮 Codex 报出）。"""
    with io.open(path, encoding="utf-8") as fh:
        return fh.read()


def run(cmd):
    """跑一条门禁命令，返回 (退出码, 合并后的输出)。"""
    proc = subprocess.run(
        cmd, cwd=ROOT, shell=isinstance(cmd, str),
        stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
    )
    return proc.returncode, proc.stdout


def parse_citation(out):
    """relaykit-citation-check.py 的输出 → (relaykit, host, upstream_full, bare_ok)。

    四行的 failed 必须存在且为 0。退出码与输出通常一致，但一旦失配
    （例如子进程被包装层改写退出码、或输出被部分截断），只读 ok= 会把
    「有失败」当成功放行——解析层自己也要 fail-closed，不能只押退出码。
    裸引用组（第 43 轮）的 failed 为 0 是硬条件；unresolved 单独解析：
    它不是失败（占位符与工作区文件落在那里是设计如此），而是文档第 11 节
    必须声明的数字——声明与实测不符由 compare 抓。
    """
    rk = re.search(r"citations=(\d+)\s+ok=(\d+)\s+failed=(\d+)", out)
    host = re.search(r"host-citations ok=(\d+)\s+failed=(\d+)", out)
    full = re.search(r"upstream-full ok=(\d+)\s+failed=(\d+)", out)
    bare = re.search(r"bare ok=(\d+)\s+unresolved=(\d+)\s+failed=(\d+)", out)
    if not (rk and host and full and bare):
        return None
    if int(rk.group(3)) or int(host.group(2)) or int(full.group(2)) or int(bare.group(3)):
        return None
    return (
        int(rk.group(1)),
        int(host.group(1)),
        int(full.group(1)),
        int(bare.group(1)),
        int(bare.group(2)),
    )


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


def extract_line_claims(cr_path):
    """从 CR 记录抽出「文档当前 N 行」这类当前口径声明。"""
    if not os.path.exists(cr_path):
        return []
    claims = []
    for i, line in enumerate(read_text(cr_path).split("\n"), 1):
        # 只认**行首的规范句式**「文档当前 N 行，位于 ...」——正文里复述这个
        # 句式（如本轮叙述「④CR 的『文档当前 1987 行』实为 1986」）不得被
        # 当成声明。不锚行首就会把「描述缺陷的文字」读成缺陷本身。
        m = re.match(r"文档当前 (\d+) 行，位于 ", line)
        if m:
            claims.append((i, "CR 文档行数", int(m.group(1))))
    return claims


def collect_actual(quiet):
    """实跑各门禁脚本与单测，返回 (计数 dict, 失败列表)。

    **fail-closed**：任何一条门禁的输出解析失败都记为失败，不放行。解析
    失败意味着「这条声明没有被核对」，若静默跳过，门禁会以绿的形式掩盖
    一整类未覆盖——这正是本脚本要防的病，不能在自己身上复发。
    唯一的例外是递归守卫下的 selfcheck_tests（见下），且它在输出里显式
    标注「未核对」。
    """
    failures = []
    actual = {}

    def parsed_or_fail(label, value, key, fail_msg):
        if value is None:
            failures.append(fail_msg)
        else:
            actual[key] = value
        return value

    # citation 必须钉在设计文档自己声明的锚点上跑（第 40 轮；UPSTREAM_PIN）。
    code, out = run([sys.executable, "scripts/relaykit-citation-check.py",
                     "--ref", UPSTREAM_PIN])
    if code != 0:
        failures.append(f"relaykit-citation-check.py 退出码 {code}")
    parsed_or_fail("citation", parse_citation(out), "citation",
                   "relaykit-citation-check.py 输出无法解析（该组声明未核对）")

    code, out = run([sys.executable, "scripts/relaykit-citation-check.test.py"])
    if code != 0:
        failures.append(f"relaykit-citation-check.test.py 退出码 {code}")
    parsed_or_fail("citation_tests", parse_ran(out), "citation_tests",
                   "relaykit-citation-check.test.py 输出无法解析（该组声明未核对）")

    code, out = run([sys.executable, "scripts/relaykit-doc-structure-check.py"])
    if code != 0:
        failures.append(f"relaykit-doc-structure-check.py 退出码 {code}")
    m = re.search(r"合计 (\d+) 个结构问题", out)
    parsed_or_fail("structure_problems", int(m.group(1)) if m else None, "structure_problems",
                   "relaykit-doc-structure-check.py 输出无法解析（该组声明未核对）")

    code, out = run([sys.executable, "scripts/relaykit-doc-structure-check.test.py"])
    if code != 0:
        failures.append(f"relaykit-doc-structure-check.test.py 退出码 {code}")
    parsed_or_fail("structure_tests", parse_ran(out), "structure_tests",
                   "relaykit-doc-structure-check.test.py 输出无法解析（该组声明未核对）")

    # 自检脚本自己的单测数。这一条是自指的：selfcheck.test.py 的端到端用例
    # 会反过来调用 collect_actual，故它在调用前设置 RELAYKIT_SELFCHECK_SKIP_RECURSE
    # 断开递归；置位时跳过本段，selfcheck_tests 为 None，compare() 随之跳过该声明。
    # **这是唯一的合法跳过**，且主流程会在总结里显式标注「未核对」。
    if os.environ.get("RELAYKIT_SELFCHECK_SKIP_RECURSE"):
        actual["selfcheck_tests"] = None
    else:
        code, out = run([sys.executable, "scripts/relaykit-gate-selfcheck.test.py"])
        if code != 0:
            failures.append(f"relaykit-gate-selfcheck.test.py 退出码 {code}")
        parsed_or_fail("selfcheck_tests", parse_ran(out), "selfcheck_tests",
                       "relaykit-gate-selfcheck.test.py 输出无法解析（该组声明未核对）")

    # 「文档当前 N 行」——CR 记录里的当前口径声明，实算为 wc -l 口径
    # （s.count("\n")），不是 split("\n") 的长度：后者在文件末尾有换行时多算 1，
    # 第 38 轮就是这么写错了一次。
    doc_lines = read_text(os.path.join(ROOT, DEFAULT_DOC)).count("\n")
    actual["doc_lines"] = doc_lines

    # 环境变量逃生通道用于本地配平/调试；CI 必须钉死。设了就按环境走，
    # 未设一律用文档锚点（第 40 轮修掉「注释钉死、代码仍用 upstream/main」）。
    kit_ref = os.environ.get("KIT_REF", UPSTREAM_PIN)
    code, out = run(f"KIT_REF={shlex.quote(kit_ref)} bash scripts/relaykit-dto-compat.sh --strict")
    if code != 0:
        failures.append(f"relaykit-dto-compat.sh --strict 退出码 {code}")
    parsed_or_fail("dto", parse_dto(out), "dto",
                   "relaykit-dto-compat.sh --strict 输出无法解析（该组声明未核对）")
    # 工具打印的分节数（`### ` 行）。设计文档自称「覆盖 N 组」，N 应与它一致——
    # 第 38 轮之前文档写「六维」，而工具一直打印 7 组，两边都对不上任何东西。
    sections = len(re.findall(r"^### ", out, re.M))
    if sections == 0:
        failures.append("relaykit-dto-compat.sh 输出无分节（### 行），组数声明无法核对")
    else:
        actual["dto_sections"] = sections

    if not quiet:
        print("门禁实际输出：")
        cite = actual.get("citation")
        print(f"  citation        "
              f"{('relaykit=%d host=%d full=%d bare_ok=%d unresolved=%d' % cite) if cite else '无法解析'}")
        print(f"  citation 单测   {actual.get('citation_tests', '无法解析')}")
        print(f"  结构问题        {actual.get('structure_problems', '无法解析')}")
        print(f"  结构单测        {actual.get('structure_tests', '无法解析')}")
        print(f"  自检单测        {actual.get('selfcheck_tests', '无法解析')}")
        print(f"  dto 七维        {actual.get('dto', '无法解析')}")
        print(f"  dto 分节数      {actual.get('dto_sections', '无法解析')}")
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
        # 「**51/51**（`ref=<UPSTREAM_PIN>`）」——ref 必须钉文档锚点（第 40 轮；
        # 旧句式 `ref=upstream/main` 随上游漂移已从文档移除，不作兼容）。
        # 正则从 UPSTREAM_PIN 现拼，升锚时不会漏改这里。
        for m in re.finditer(
            r"\*\*(\d+)/(\d+)\*\*（`ref=" + re.escape(UPSTREAM_PIN) + r"`[^）]*）", line,
        ):
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
        # 「本文档 relaykit 行引用现为 **51 处唯一**」（§11 口径，与 §9.3 的
        # 「relaykit 51 + 宿主 107 + ...」是同一批门禁计数，但句式不同——
        # 第 38 轮补入：此前抽取器只认 §9.3 句式，§11 这三处整类漏检）
        for m in re.finditer(r"现为 \*\*(\d+) 处唯一\*\*", line):
            add(i, "§11 relaykit 引用数", int(m.group(1)))
        for m in re.finditer(r"裸宿主引用 \*\*(\d+) 处\*\*", line):
            add(i, "§11 宿主引用数", int(m.group(1)))
        for m in re.finditer(r"全路径 \*\*(\d+) 处\*\*", line):
            add(i, "§11 全路径引用数", int(m.group(1)))
        # 第 43 轮：裸 `X.go:NN` 组进门禁后，§11 必须声明 ok 数与未解析数。
        # 检查器的 docstring 承诺了这条对账——声明消失或漂移都要变红。
        for m in re.finditer(r"裸引用 ok \*\*(\d+)\*\*", line):
            add(i, "§11 裸引用 ok 数", int(m.group(1)))
        for m in re.finditer(r"未解析 \*\*(\d+)\*\* 处", line):
            add(i, "§11 裸引用未解析数", int(m.group(1)))
        # §9.3 门禁命令注释里的裸组计数：裸引用 ok=N/未解析=M
        for m in re.finditer(r"裸引用 ok=(\d+)/未解析=(\d+)", line):
            add(i, "citation 裸引用（§9.3）",
                (int(m.group(1)), int(m.group(2))))

        # 「至此，dto 的兼容性核查覆盖 **7 组**」——工具分节数的当前口径声明
        for m in re.finditer(r"dto 的兼容性核查覆盖 \*\*(\d+) 组\*\*", line):
            add(i, "dto 分节数", int(m.group(1)))

        # 「# 两份文档均 0 问题时退出码 0」——结构检查器的当前口径声明。
        # 第 39 轮补入：此前 structure_problems 被采集、被打印，却从未进入
        # 比对——文档把 0 改成任意数字，selfcheck 不报。
        for m in re.finditer(r"两份文档均 (\d+) 问题时退出码 0", line):
            add(i, "结构问题数", int(m.group(1)))

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
            # 比对前三位：第 43 轮起 parse_citation 追加了裸组两位，三元组声明
            # 仍是前三维的声明，不因位宽变化误报。
            if real is not None and claimed != real[:3]:
                mismatches.append((line_no, desc, claimed, real[:3]))
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
        elif desc == "dto 分节数":
            real = actual.get("dto_sections")
            if real is not None and claimed != real:
                mismatches.append((line_no, desc, claimed, real))
        elif desc == "结构问题数":
            real = actual.get("structure_problems")
            if real is not None and claimed != real:
                mismatches.append((line_no, desc, claimed, real))
        elif desc == "CR 文档行数":
            real = actual.get("doc_lines")
            if real is not None and claimed != real:
                mismatches.append((line_no, desc, claimed, real))
        elif desc == "§11 relaykit 引用数":
            real = actual.get("citation")
            if real is not None and claimed != real[0]:
                mismatches.append((line_no, desc, claimed, real[0]))
        elif desc == "§11 宿主引用数":
            real = actual.get("citation")
            if real is not None and claimed != real[1]:
                mismatches.append((line_no, desc, claimed, real[1]))
        elif desc == "§11 全路径引用数":
            real = actual.get("citation")
            if real is not None and claimed != real[2]:
                mismatches.append((line_no, desc, claimed, real[2]))
        elif desc == "§11 裸引用 ok 数":
            real = actual.get("citation")
            if real is not None and claimed != real[3]:
                mismatches.append((line_no, desc, claimed, real[3]))
        elif desc == "§11 裸引用未解析数":
            real = actual.get("citation")
            if real is not None and claimed != real[4]:
                mismatches.append((line_no, desc, claimed, real[4]))
        elif desc == "citation 裸引用（§9.3）":
            real = actual.get("citation")
            if real is not None and claimed != (real[3], real[4]):
                mismatches.append((line_no, desc, claimed, (real[3], real[4])))
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
    doc = read_text(doc_path)
    claims = extract_claims(doc)
    cr_path = os.path.join(ROOT, CR_DOC)
    cr_claims = extract_line_claims(cr_path)
    mismatches = compare(claims + cr_claims, actual)

    if not args.quiet:
        print(f"文档自述计数 {len(claims)} 处（{args.doc}）")

    # 类别级 fail-closed：与 compare 的逐条比对正交。compare 只在「声明存在
    # 且实际值已采到」时才能发现问题；整类声明消失时它无话可说，必须由这里兜。
    # CR 行数声明不在此列：设计文档可以合法地不留行数声明，compare 已核对在册
    # 的每一条。
    present_kinds = {desc for _, desc, _ in claims}
    missing_kinds = [k for k in EXPECTED_CLAIM_KINDS if k not in present_kinds]
    if missing_kinds:
        failures.append("设计文档缺少声明类别：" + "、".join(missing_kinds)
                        + "（整类删除声明必须失败，而不是放行）")

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
            # 跳过 ≠ 通过：置位递归守卫时「自检单测数」整类未被核对，
            # 此时不能打印「全部一致」——那会让一次未覆盖的检查读起来像已覆盖。
            if actual.get("selfcheck_tests") is None:
                print("已核对的声明与实际输出一致；**自检单测数未核对**"
                      "（RELAYKIT_SELFCHECK_SKIP_RECURSE 已置位，该条声明被跳过而非放行）")
            else:
                print("全部门禁自述计数与实际输出一致")
        return 0
    return 1


if __name__ == "__main__":
    sys.exit(main())
