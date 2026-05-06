import test from "node:test";
import assert from "node:assert/strict";

import {
  MERMAID_SECURITY_LEVEL,
  renderMermaidCode,
} from "../classic/src/components/common/markdown/mermaidRenderer.js";

test("mermaid security level keeps final SVG sanitization enabled", () => {
  assert.equal(MERMAID_SECURITY_LEVEL, "strict");
});

test("renderMermaidCode renders from source code instead of reparsing target text", async () => {
  const calls = [];
  const mermaid = {
    async render(id, code) {
      calls.push({ id, code });
      return {
        svg: `<svg data-id="${id}"><text>${code}</text></svg>`,
        bindFunctions(target) {
          target.bound = true;
        },
      };
    },
    async run() {
      throw new Error("run should not be used");
    },
  };
  const target = {
    innerHTML: "previous rendered svg text",
  };

  await renderMermaidCode(mermaid, target, "graph TD\n  A-->B\n");

  assert.equal(calls.length, 1);
  assert.equal(calls[0].code, "graph TD\n  A-->B\n");
  assert.match(calls[0].id, /^mermaid-/);
  assert.match(target.innerHTML, /^<svg data-id="mermaid-/);
  assert.equal(target.bound, true);
});
