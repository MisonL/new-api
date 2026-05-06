import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const testDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(testDir, "../..");

test("frontend dist embed includes underscore-prefixed Vite chunks", async () => {
  const mainSource = await readFile(resolve(repoRoot, "main.go"), "utf8");

  assert.match(mainSource, /\/\/go:embed all:web\/default\/dist/);
  assert.match(mainSource, /\/\/go:embed all:web\/classic\/dist/);
});
