#!/usr/bin/env python3
"""Print the relaykit upstream anchor declared by relaykit-gate-selfcheck.py.

repo-gates 调 relaykit-citation-check.py 时要传 --ref。该 ref 必须等于
gate-selfcheck 的 UPSTREAM_PIN：此前 workflow 里手写第二份，升锚漏改一处
就分叉（citation 在旧锚点上跑，selfcheck 在新锚点上核对）。本脚本从常量
取值打印，workflow 用 $(python3 -I scripts/relaykit-pin.py) 消费。

Usage:
  python3 -I scripts/relaykit-pin.py
"""

from __future__ import annotations

import importlib.util
from pathlib import Path

HERE = Path(__file__).resolve().parent
SELFCHECK = HERE / "relaykit-gate-selfcheck.py"


def load_selfcheck():
    spec = importlib.util.spec_from_file_location("relaykit_gate_selfcheck", SELFCHECK)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


if __name__ == "__main__":
    print(load_selfcheck().UPSTREAM_PIN)
