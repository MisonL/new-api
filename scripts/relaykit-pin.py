#!/usr/bin/env python3
"""Print the relaykit anchors declared by the gate scripts.

repo-gates 调 relaykit-citation-check.py 时要传 --ref。该 ref 必须等于
gate-selfcheck 的 UPSTREAM_PIN：此前 workflow 里手写第二份，升锚漏改一处
就分叉（citation 在旧锚点上跑，selfcheck 在新锚点上核对）。本脚本从常量
取值打印，workflow 用 $(python3 -I scripts/relaykit-pin.py) 消费。

`--baseline` 打印宿主基线（citation-check 的 DEFAULT_BASELINE）。CI 要按
完整 SHA 取回两个锚点提交（GitHub 不接受按短 SHA fetch），两个锚点因此
都必须从同一处取值，而不是在 workflow 里再抄一份。

Usage:
  python3 -I scripts/relaykit-pin.py              # 上游锚点
  python3 -I scripts/relaykit-pin.py --baseline   # 宿主基线
"""

from __future__ import annotations

import argparse
import importlib.util
from pathlib import Path

HERE = Path(__file__).resolve().parent
SELFCHECK = HERE / "relaykit-gate-selfcheck.py"
CITATION_CHECK = HERE / "relaykit-citation-check.py"


def _load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def load_selfcheck():
    return _load("relaykit_gate_selfcheck", SELFCHECK)


def load_citation_check():
    return _load("relaykit_citation_check", CITATION_CHECK)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--baseline",
        action="store_true",
        help="打印宿主基线锚点（citation-check 的 DEFAULT_BASELINE）",
    )
    args = parser.parse_args()
    if args.baseline:
        print(load_citation_check().DEFAULT_BASELINE)
    else:
        print(load_selfcheck().UPSTREAM_PIN)
