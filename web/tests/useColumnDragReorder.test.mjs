import test from "node:test";
import assert from "node:assert/strict";

import { getNextPointerDragState } from "../classic/src/hooks/usage-logs/useColumnDragReorder.js";

test("getNextPointerDragState records a new target", () => {
  assert.deepEqual(
    getNextPointerDragState(
      { sourceKey: "model", targetKey: "", position: "before" },
      "model",
      { targetKey: "cost", position: "after" },
    ),
    { sourceKey: "model", targetKey: "cost", position: "after" },
  );
});

test("getNextPointerDragState clears stale target when no current target exists", () => {
  assert.deepEqual(
    getNextPointerDragState(
      { sourceKey: "model", targetKey: "cost", position: "after" },
      "model",
      null,
    ),
    { sourceKey: "model", targetKey: "", position: "before" },
  );
});

test("getNextPointerDragState skips unchanged source-only state", () => {
  assert.equal(
    getNextPointerDragState(
      { sourceKey: "model", targetKey: "", position: "before" },
      "model",
      null,
    ),
    null,
  );
});
