import test from "node:test";
import assert from "node:assert/strict";

import {
  applyColumnOrder,
  getMovableColumnKeys,
  moveColumnKey,
  normalizeColumnOrder,
} from "../classic/src/hooks/usage-logs/columnPreferences.js";

test("normalizeColumnOrder preserves saved order and appends missing defaults", () => {
  const order = normalizeColumnOrder(
    ["cost", "time", "time", "unknown"],
    ["time", "model", "cost", "ip"],
  );

  assert.deepEqual(order, ["cost", "time", "model", "ip"]);
});

test("normalizeColumnOrder falls back to defaults for invalid saved order", () => {
  assert.deepEqual(normalizeColumnOrder(null, ["time", "model", "cost"]), [
    "time",
    "model",
    "cost",
  ]);
  assert.deepEqual(normalizeColumnOrder([], ["time", "model", "cost"]), [
    "time",
    "model",
    "cost",
  ]);
});

test("normalizeColumnOrder returns empty order when defaults are unavailable", () => {
  assert.deepEqual(normalizeColumnOrder(null, null), []);
  assert.deepEqual(normalizeColumnOrder([], []), []);
  assert.deepEqual(normalizeColumnOrder(["cost", "time"], null), []);
  assert.deepEqual(normalizeColumnOrder(["cost", "time"], []), []);
});

test("moveColumnKey moves a column by one slot and clamps boundaries", () => {
  assert.deepEqual(moveColumnKey(["time", "model", "cost"], "cost", "up"), [
    "time",
    "cost",
    "model",
  ]);
  assert.deepEqual(moveColumnKey(["time", "model", "cost"], "time", "up"), [
    "time",
    "model",
    "cost",
  ]);
  assert.deepEqual(moveColumnKey(["time", "model", "cost"], "model", "down"), [
    "time",
    "cost",
    "model",
  ]);
  assert.deepEqual(moveColumnKey(["time", "model", "cost"], "ip", "down"), [
    "time",
    "model",
    "cost",
  ]);
});

test("moveColumnKey skips non-movable columns when moving inside a filtered selector", () => {
  // moveColumnKey swaps with the adjacent movable key in a filtered selector,
  // while non-movable keys keep their relative slot in the full table order.
  assert.deepEqual(
    moveColumnKey(["time", "admin_only", "model", "cost"], "model", "up", [
      "time",
      "model",
      "cost",
    ]),
    ["model", "admin_only", "time", "cost"],
  );
});

test("getMovableColumnKeys excludes fixed edge columns", () => {
  const columns = [
    { key: "time", fixed: true },
    { key: "model" },
    { key: "details", fixed: "right" },
  ];

  assert.deepEqual(getMovableColumnKeys(columns), ["model"]);
});

test("applyColumnOrder handles empty columns", () => {
  assert.deepEqual(applyColumnOrder([], { time: true }, ["time"]), []);
});

test("applyColumnOrder returns empty list when all columns are hidden", () => {
  const columns = [{ key: "time" }, { key: "model" }];
  const visibleColumns = { time: false, model: false };

  assert.deepEqual(
    applyColumnOrder(columns, visibleColumns, ["model"]).map(
      (column) => column.key,
    ),
    [],
  );
});

test("applyColumnOrder ignores invisible saved keys and backfills visible columns", () => {
  const columns = [{ key: "time" }, { key: "model" }, { key: "cost" }];
  const visibleColumns = { time: true, model: false, cost: true };

  assert.deepEqual(
    applyColumnOrder(columns, visibleColumns, ["model"]).map(
      (column) => column.key,
    ),
    ["time", "cost"],
  );
});

test("applyColumnOrder filters visibility after ordering and backfills unordered columns", () => {
  const columns = [
    { key: "time" },
    { key: "model" },
    { key: "cost" },
    { key: "ip" },
  ];
  const visibleColumns = {
    time: true,
    model: true,
    cost: true,
    ip: false,
  };

  assert.deepEqual(
    applyColumnOrder(columns, visibleColumns, ["cost", "time"]).map(
      (column) => column.key,
    ),
    ["cost", "time", "model"],
  );
});

test("applyColumnOrder keeps right fixed columns at the end", () => {
  const columns = [
    { key: "time" },
    { key: "model" },
    { key: "cost" },
    { key: "details", fixed: "right" },
  ];
  const visibleColumns = {
    time: true,
    model: true,
    cost: true,
    details: true,
  };

  assert.deepEqual(
    applyColumnOrder(columns, visibleColumns, [
      "details",
      "cost",
      "time",
      "model",
    ]).map((column) => column.key),
    ["cost", "time", "model", "details"],
  );
});
