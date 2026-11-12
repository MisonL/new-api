#!/usr/bin/env python3
"""Print the relaykit anchors declared by the gate scripts.

repo-gates 调 relaykit-citation-check.py 时要传 --ref。该 ref 必须等于
gate-selfcheck 的 UPSTREAM_PIN：此前 workflow 里手写第二份，升锚漏改一处
就分叉（citation 在旧锚点上跑，selfcheck 在新锚点上核对）。本脚本从常量
取值打印，workflow 用 $(python3 -I scripts/relaykit-pin.py) 消费。

`--baseline` 打印宿主基线（citation-check 的 DEFAULT_BASELINE）。CI 要按
完整 SHA 取回两个锚点提交（GitHub 不接受按短 SHA fetch），两个锚点因此
都必须从同一处取值，而不是在 workflow 里再抄一份。

`--full` / `--full --baseline` 从固定长度打印完整 40 位 SHA。短 SHA 只能
在本地仓库里 `git rev-parse` 解析；CI 的 checkout 里没有这两个对象，
`git rev-parse 789c97019` 直接报 unknown revision，取回步骤于是自己失败。

Usage:
  python3 -I scripts/relaykit-pin.py                         # 上游锚点（短）
  python3 -I scripts/relaykit-pin.py --baseline              # 宿主基线（短）
  python3 -I scripts/relaykit-pin.py --full                  # 上游锚点（完整）
  python3 -I scripts/relaykit-pin.py --full --baseline       # 宿主基线（完整）
"""

from __future__ import annotations

import argparse
import importlib.util
from pathlib import Path

HERE = Path(__file__).resolve().parent
SELFCHECK = HERE / "relaykit-gate-selfcheck.py"
CITATION_CHECK = HERE / "relaykit-citation-check.py"

# 常量里只写短 SHA（可读、与设计文档一致）。workflow 需要完整 SHA 时由这里
# 的显式映射补全：GitHub 按完整 SHA fetch，本地又无法从短 SHA 推导出完整
# 值（对象不在 CI 的 checkout 里）。新增锚点时必须同时登记两项，缺一项即
# 在 CI 失败，不会静默用错值。
FULL_SHA = {
    "upstream": "789c970199ea527e6a26e071915f4a4cd2c64178",
    "baseline": "dbfbc096f48e7eb97e1e71256158d020661c105e",
}


def _load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def load_selfcheck():
    return _load("relaykit_gate_selfcheck", SELFCHECK)


def load_citation_check():
    return _load("relaykit_citation_check", CITATION_CHECK)


def short_pin(which):
    if which == "baseline":
        return load_citation_check().DEFAULT_BASELINE
    return load_selfcheck().UPSTREAM_PIN


def full_pin(which):
    short = short_pin(which)
    full = FULL_SHA.get(which, "")
    if not full:
        raise SystemExit(
            f"relaykit-pin.py: 未登记 {which} 锚点的完整 SHA（FULL_SHA）")
    if not full.startswith(short):
        raise SystemExit(
            f"relaykit-pin.py: {which} 的完整 SHA {full} 与短锚点 {short} 不一致")
    return full


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--baseline",
        action="store_true",
        help="打印宿主基线锚点（citation-check 的 DEFAULT_BASELINE）",
    )
    parser.add_argument(
        "--full",
        action="store_true",
        help="打印完整 40 位 SHA（CI 按完整 SHA fetch 时使用）",
    )
    args = parser.parse_args()
    which = "baseline" if args.baseline else "upstream"
    print(full_pin(which) if args.full else short_pin(which))
