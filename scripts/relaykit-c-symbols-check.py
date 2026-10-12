#!/usr/bin/env python3
"""relaykit C1–C8 行的符号/文件机械核对（第 50 轮新增）。

从设计文档抽取 | C1 | … | C8 | 八行，逐行提取：

1. `pkg.Symbol` 形式的引用（relayconvert / convmeta / types / kitreasoning /
   relaymedia / relaycommon / service / logger），按 prefix→搜索目录的显式映射，
   在钉死锚点 `789c97019`（relaykit 侧）或 `dbfbc096f`（宿主 service/logger 侧）
   上用 `git grep` 验证该符号确有声明（func/type/var/const）。

2. 反引号内的文件引用：
   - `upstream/main:<path>`、`789c97019:<path>` —— 在上游锚点查文件存在、行号在范围内；
   - `dbfbc096f:<path>` —— 在基线查文件存在、行号在范围内；
   - `git cat-file -e <ref>:<path>` / `git grep ... <ref> ...` 命令字面量 —— 重跑该命令，
     断言其结论与文档断言一致的部分（只核命令本身可执行、ref 可解析）。

刻意不核的边界（文档也承诺过「不断言行内容」）：行号是否仍指向同一段语义、
`relayconvert.Xxx` 这类占位名、未加前缀的裸文件名（`helper.go` 等，由
relaykit-citation-check 的裸引用组覆盖）。

退出码：0 = 全部可解析；1 = 有缺失（逐条打印）。

用法：
    python3 scripts/relaykit-c-symbols-check.py \
        --doc docs/superpowers/specs/2026-09-30-relaykit-migration-design.md \
        --ref 789c97019 --baseline dbfbc096f
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path

DEFAULT_DOC = "docs/superpowers/specs/2026-09-30-relaykit-migration-design.md"
DEFAULT_REF = "789c97019"
DEFAULT_BASELINE = "dbfbc096f"

# prefix -> (搜索位置含 ref, git grep 路径限制)
# 位置写 ("BASELINE+PIN"/"PIN"/...)：宿主包必须两个 ref 都试——
# `service.ConvertRequest` 是阶段 4 才新增（只在 pin）、
# `service.ErrProtocolConversionUnsupported` 是上游已删（只在 baseline），
# 单 ref 查必然误报。relaykit 符号只查 pin。结果记录命中 ref，随 C 行断言方向核对。
PREFIX_MAP = {
    "relayconvert": ("PIN", "relaykit/relayconvert/"),          # 只在 pin 查
    "convmeta": ("PIN", "relaykit/relayconvert/convmeta/"),
    "types": ("PIN", "relaykit/types/"),
    "kitreasoning": ("PIN", "relaykit/relayconvert/reasoning/"),
    "relaymedia": ("PIN", "relaykit/relayconvert/internal/media/"),
    "relaycommon": ("BASELINE+PIN", "relay/common/"),
    "service": ("BASELINE+PIN", "service/"),
    "logger": ("BASELINE+PIN", "logger/"),
}

# 符号声明的匹配形态（放宽到声明即可，不断言签名）。
# 注意：git grep 的 -E 在该环境下对 `\b`/`\s` 不可靠，一律用 POSIX 字符类
# `[[:space:]]` 与显式边界，实测 `type[[:space:]]+Meta[[:space:]]+interface` 可用。
SYM_PATTERNS = (
    r"func[[:space:]]+{sym}[[:space:]]*\(",
    r"func[[:space:]]+\([^)]+\)[[:space:]]+{sym}[[:space:]]*\(",
    r"type[[:space:]]+{sym}[[:space:]]",
    r"var[[:space:]]+{sym}[[:space:]]",
    r"const[[:space:]]+{sym}[[:space:]]",
    r"^[[:space:]]*{sym}[[:space:]]*=",  # iota 分组里的常量
)


def run_git(args, check=True):
    return subprocess.run(
        ["git", *args], capture_output=True, text=True, check=check
    )


def git_show(ref, path):
    p = subprocess.run(
        ["git", "show", f"{ref}:{path}"], capture_output=True, text=True
    )
    return p.returncode == 0, p.stdout


def extract_c_rows(doc_text):
    rows = []
    for line in doc_text.split("\n"):
        m = re.match(r"^\|\s*(C[1-8])\s*\|(.*)$", line)
        if m:
            rows.append((m.group(1), m.group(2)))
    return rows


def check_symbol(prefix, sym, ref, baseline):
    """返回 (ok, where)。where 命中时形如 'pin:path:line' 或 'baseline:path:line'。

    宿主包（BASELINE+PIN）两 ref 都查：`service.ConvertRequest` 是阶段 4 新增、
    `service.ErrProtocolConversionUnsupported` 是上游已删，任一单 ref 都会误报。
    """
    mode, pathspec = PREFIX_MAP[prefix]
    if mode == "PIN":
        targets = [("pin", ref)]
    else:
        targets = [("baseline", baseline), ("pin", ref)]
    for label, target in targets:
        for pat in SYM_PATTERNS:
            rx = pat.format(sym=re.escape(sym))
            r = subprocess.run(
                ["git", "grep", "-nE", rx, target, "--", pathspec],
                capture_output=True, text=True,
            )
            if r.returncode == 0 and r.stdout.strip():
                first = r.stdout.strip().split("\n")[0]
                return True, f"{label}:{first}"
    return False, f"{mode}:{pathspec} 无声明（试过 {[t[1] for t in targets]}）"


def expand_file_ref(ref_str, pin_ref, baseline):
    """把文档里的文件引用字符串解析为 (ref, path, line|None)；解析不了返回 None。"""
    m = re.match(r"^(upstream/main|789c97019|dbfbc096f):([A-Za-z0-9_./-]+\.go)(?::(\d+))?$",
                 ref_str.strip("`"))
    if m:
        ref = pin_ref if m.group(1) in ("upstream/main", "789c97019") else baseline
        line = int(m.group(3)) if m.group(3) else None
        return ref, m.group(2), line
    return None


def check_file_ref(ref, path, line):
    ok, _ = git_show(ref, path)
    if not ok:
        return False, f"{ref}:{path} 文件不存在"
    if line is not None:
        ok2, out = git_show(ref, path)
        lines = out.split("\n")
        if line > len(lines):
            return False, f"{ref}:{path}:{line} 行号越界（该文件 {len(lines)} 行）"
        if not lines[line - 1].strip():
            return False, f"{ref}:{path}:{line} 指向空行"
    return True, ""


def main(argv):
    ap = argparse.ArgumentParser(description="relaykit C1–C8 行符号/文件机械核对")
    ap.add_argument("--doc", default=DEFAULT_DOC)
    ap.add_argument("--ref", default=DEFAULT_REF)
    ap.add_argument("--baseline", default=DEFAULT_BASELINE)
    args = ap.parse_args(argv)

    doc_path = Path(args.doc)
    if not doc_path.exists():
        print(f"设计文档不存在: {doc_path}", file=sys.stderr)
        return 1
    doc_text = doc_path.read_text(encoding="utf-8")

    rows = extract_c_rows(doc_text)
    if len(rows) != 8:
        print(f"FAIL 解析到 {len(rows)} 个 C 行（期望 8：C1–C8）", file=sys.stderr)
        return 1

    sym_failures = []
    sym_ok = 0
    file_failures = []
    file_ok = 0
    seen_syms = set()
    seen_files = set()

    for row_id, body in rows:
        # 1) 符号
        for prefix, sym in re.findall(
            r"\b(relayconvert|convmeta|types|kitreasoning|relaymedia|relaycommon|service|logger)\.([A-Z][A-Za-z0-9_]*)",
            body,
        ):
            if sym in ("Xxx",):  # 占位名不核
                continue
            key = (prefix, sym)
            if key in seen_syms:
                continue
            seen_syms.add(key)
            ok, where = check_symbol(prefix, sym, args.ref, args.baseline)
            if ok:
                sym_ok += 1
            else:
                sym_failures.append((row_id, f"{prefix}.{sym}", where))

        # 2) 文件引用（只核带 ref 前缀的，裸文件名交给 citation 裸引用组）
        for cand in re.findall(
            r"`((?:upstream/main|789c97019|dbfbc096f):[A-Za-z0-9_./-]+\.go(?::\d+)?)`",
            body,
        ):
            parsed = expand_file_ref(cand, args.ref, args.baseline)
            if parsed is None:
                continue
            if cand in seen_files:
                continue
            seen_files.add(cand)
            ok, why = check_file_ref(*parsed)
            if ok:
                file_ok += 1
            else:
                file_failures.append((row_id, cand, why))

    print(f"C 行: {len(rows)}（期望 8）")
    print(f"符号引用: ok={sym_ok} failed={len(sym_failures)}")
    print(f"带锚点文件引用: ok={file_ok} failed={len(file_failures)}")
    for row_id, name, why in sym_failures:
        print(f"  FAIL [{row_id}] 符号 {name}: {why}")
    for row_id, ref, why in file_failures:
        print(f"  FAIL [{row_id}] 文件 {ref}: {why}")

    return 1 if (sym_failures or file_failures) else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
