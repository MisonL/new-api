#!/usr/bin/env python3
"""结构完整性检查：文档自身的结构是否自洽（不查事实，只查可机械判定的结构缺陷）。

四类检查：
  A. 表格行序号是否单调递增（新条目插到中间、后来没重排）。
     行首编号支持 `N` 与 `Na`/`Nb` 子编号（9.2.3 表用 5a/5b），排序键 (N, 后缀)。
     既抓乱序，也抓重复；**不查跳号**（见下）。
  B. 表格每个数据行的单元格数是否等于表头列数（未转义的 | 会撑破单元格）
  C. 交叉引用的章节号是否真实存在（误报白名单见 SECTION_ALLOWLIST）
  D. 跨表引用的「未决项 N」是否在被引用的编号体系里存在

**有意不检查的边界**（写在这里，免得被当成「已覆盖」）：
  - A 只在表格有 >= 3 个编号行时才检查。**两行表格反序不被抓住**。
  - A 不查跳号：`1,5,9` 通过。删了一行未重排是缺陷，但「有意留空」与之无法
    从文本区分，故不设为失败条件（有单测钉住此边界）。
  - B 只比数量，不比内容：单元格数正确但文字错位不在此检查范围。
  - C 只验章节号**存在**，不验指向是否正确：引用 §12.2 而内容其实属 §12.1 不被抓住。
  - D 只验编号存在，不验内容与标题是否相符。
  - 四类都不检查正文的语义一致性——那是事实核验的职责，不是本工具的。

用法：
    python3 scripts/relaykit-doc-structure-check.py [文档路径 ...]

默认检查设计文档与 CR 记录。全部通过退出码 0，任一失败退出码 1 并打印位置。
"""

import io
import re
import sys

DEFAULT_TARGETS = [
    "docs/superpowers/specs/2026-09-30-relaykit-migration-design.md",
    "docs/reviews/CR-RELAYKIT-MIGRATION-DESIGN-2026-09-30.md",
]

# 章节号检查的白名单：这些形如 N.M 的串不是章节引用。
SECTION_ALLOWLIST = {
    "5.6",  # gpt-5.6-high 模型名
    "4.5",  # 同时是真实章节号（4.5 本地独有包与文件）与 Opus 4.5 模型名，真实章节已在 defined 里
    "4.6",  # Opus 4.6
    "4.7",  # Opus 4.7
    "4.8",  # Opus 4.8
}

# 未决项编号所在的主表小节（编号体系的正源）
UNRESOLVED_MASTER_HEADING = "## 10. 未决项"


def is_separator(line):
    return re.fullmatch(r"\|[\s\-:|]+\|", line.strip()) is not None


def _fmt(key):
    """(4, '') -> '4'；(5, 'a') -> '5a'。"""
    return f"{key[0]}{key[1]}"


def split_row(line):
    body = line.strip()
    if not (body.startswith("|") and body.endswith("|")):
        return None
    return re.split(r"(?<!\\)\|", body[1:-1])


def check_tables(lines):
    """A + B：表格行序号单调性、单元格数一致性。"""
    problems = []
    i = 0
    while i < len(lines):
        if lines[i].startswith("|") and i + 1 < len(lines) and is_separator(lines[i + 1]):
            header = split_row(lines[i])
            ncols = len(header) if header else 0
            j = i + 2
            nums = []
            while j < len(lines) and lines[j].startswith("|"):
                cells = split_row(lines[j])
                if cells is None:
                    j += 1
                    continue
                if len(cells) != ncols:
                    problems.append(
                        (j + 1, "B", f"单元格数 {len(cells)} != 表头 {ncols}（未转义的 | 会撑破单元格）")
                    )
                # 行首编号支持 `N` 与 `Na`/`Nb` 子编号（设计文档的 9.2.3 表用了
                # 5a/5b）。子编号排序键是 (N, 后缀)，故 5a < 5b < 6。
                m = re.match(r"\s*(\d+)([a-z]?)\s*$", cells[0])
                if m:
                    nums.append((j + 1, (int(m.group(1)), m.group(2))))
                j += 1
            seq = [n for _, n in nums]
            # 必须**严格递增**：既抓乱序，也抓重复编号
            # （[1,2,3,3,5] 是 sorted 的，只查非排序会漏掉重复）。
            if len(seq) >= 3:
                dup = next((seq[k] for k in range(1, len(seq)) if seq[k] == seq[k - 1]), None)
                if seq != sorted(seq):
                    problems.append((i + 1, "A", f"表格行序号非单调：{[_fmt(s) for s in seq]}"))
                elif dup is not None:
                    problems.append((i + 1, "A", f"表格行序号有重复：{_fmt(dup)}（{[_fmt(s) for s in seq]}）"))
            i = j
        else:
            i += 1
    return problems


def defined_sections(text):
    return set(re.findall(r"^#{2,4}\s+(\d+(?:\.\d+)*)", text, re.M))


def check_section_refs(text, defined):
    """C：交叉引用的章节号是否存在。

    defined 是**全部被检查文档的并集**——CR 记录引用设计文档的章节号是合法的，
    单文件自查会把它误报成「未定义」。
    """
    refs = set(re.findall(r"(?<![\d.])(\d+\.\d+(?:\.\d+)?)(?![\d])", text))
    problems = []
    for ref in sorted(refs):
        if ref in defined or ref in SECTION_ALLOWLIST:
            continue
        head = ref.split(".")[0]
        if head in {"3", "4", "5", "6", "7", "8", "9", "12"}:
            problems.append((0, "C", f"引用了未定义的章节号 {ref}"))
    return problems


def unresolved_master_numbers(text):
    """从主表小节抽出已定义的未决项编号；找不到返回 None。"""
    idx = text.find(UNRESOLVED_MASTER_HEADING)
    if idx < 0:
        return None
    end = text.find("\n## ", idx + len(UNRESOLVED_MASTER_HEADING))
    master = text[idx:end if end > 0 else len(text)]
    return {int(m.group(1)) for m in re.finditer(r"^(\d+)\.\s", master, re.M)}


def check_unresolved_refs(text, defined):
    """D：跨表引用的「未决项 N」必须在主表的编号体系里存在。

    defined 是**全部被检查文档的并集**——CR 记录引用设计文档的未决项编号是合法的。
    """
    if defined is None:
        return []
    problems = []
    for m in re.finditer(r"未决项\s*(\d+)", text):
        n = int(m.group(1))
        if n not in defined:
            line = text.count("\n", 0, m.start()) + 1
            top = max(defined) if defined else 0
            problems.append((line, "D", f"引用了「未决项 {n}」，但主表只定义到 {top}"))
    return problems


def run(path, defined, unresolved):
    text = io.open(path, encoding="utf-8").read()
    lines = text.split("\n")
    return (
        check_tables(lines)
        + check_section_refs(text, defined)
        + check_unresolved_refs(text, unresolved)
    )


def main():
    targets = sys.argv[1:] or DEFAULT_TARGETS
    texts = [io.open(path, encoding="utf-8").read() for path in targets]
    defined = set()
    for text in texts:
        defined |= defined_sections(text)
    unresolved = set()
    for text in texts:
        nums = unresolved_master_numbers(text)
        if nums is not None:
            unresolved |= nums
    if not unresolved:
        unresolved = None
    total = 0
    for path, text in zip(targets, texts):
        problems = run(path, defined, unresolved)
        if problems:
            print(f"{path}: {len(problems)} 个结构问题")
            for line, kind, msg in problems[:40]:
                loc = f":{line}" if line else ""
                print(f"  [{kind}]{loc} {msg}")
        else:
            print(f"{path}: ok")
        total += len(problems)
    print(f"合计 {total} 个结构问题")
    return 1 if total else 0


if __name__ == "__main__":
    sys.exit(main())
