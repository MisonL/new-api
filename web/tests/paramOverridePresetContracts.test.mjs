import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repoRoot = resolve(import.meta.dirname, "..", "..");

function readRepoFile(path) {
  return readFileSync(resolve(repoRoot, path), "utf8");
}

function findMatchingBracket(source, openIndex) {
  let depth = 0;
  let quote = "";
  let escaped = false;
  for (let index = openIndex; index < source.length; index += 1) {
    const char = source[index];
    if (quote) {
      if (escaped) {
        escaped = false;
      } else if (char === "\\") {
        escaped = true;
      } else if (char === quote) {
        quote = "";
      }
      continue;
    }
    if (char === "'" || char === '"' || char === "`") {
      quote = char;
      continue;
    }
    if (char === "[") depth += 1;
    if (char === "]") {
      depth -= 1;
      if (depth === 0) return index;
    }
  }
  return -1;
}

function countTopLevelObjects(arrayBody) {
  let depth = 0;
  let count = 0;
  let quote = "";
  let escaped = false;
  for (let index = 0; index < arrayBody.length; index += 1) {
    const char = arrayBody[index];
    if (quote) {
      if (escaped) {
        escaped = false;
      } else if (char === "\\") {
        escaped = true;
      } else if (char === quote) {
        quote = "";
      }
      continue;
    }
    if (char === "'" || char === '"' || char === "`") {
      quote = char;
      continue;
    }
    if (char === "{" || char === "[") {
      if (char === "{" && depth === 0) count += 1;
      depth += 1;
      continue;
    }
    if (char === "}" || char === "]") {
      depth -= 1;
    }
  }
  return count;
}

function extractOperationsObjectCounts(source) {
  const counts = [];
  const pattern = /operations\s*:\s*\[/g;
  let match;
  while ((match = pattern.exec(source))) {
    const openIndex = source.indexOf("[", match.index);
    const closeIndex = findMatchingBracket(source, openIndex);
    if (closeIndex < 0) continue;
    counts.push(countTopLevelObjects(source.slice(openIndex + 1, closeIndex)));
    pattern.lastIndex = closeIndex + 1;
  }
  return counts;
}

const presetSources = [
  "web/default/src/features/channels/components/dialogs/param-override-editor-dialog.tsx",
  "web/classic/src/components/table/channels/modals/ParamOverrideEditorModal.jsx",
  "web/default/src/features/system-settings/general/channel-affinity/constants.ts",
  "web/classic/src/constants/channel-affinity-template.constants.js",
];

test("advanced parameter override presets do not expose combined templates", () => {
  const bannedPatterns = [
    "codexHeadersWithoutImageTool",
    "codex_cli_headers_without_image_tool",
    "Headers + Remove Image Tool",
    "透传 + 移除图片",
    "AWS_BEDROCK_ANTHROPIC_COMPAT_TEMPLATE",
  ];

  for (const sourcePath of presetSources) {
    const source = readRepoFile(sourcePath);
    for (const pattern of bannedPatterns) {
      assert.equal(
        source.includes(pattern),
        false,
        `${sourcePath} still contains combined preset marker ${pattern}`,
      );
    }
  }
});

test("AWS Bedrock compatibility presets are split into single-operation templates", () => {
  const defaultSource = readRepoFile(
    "web/default/src/features/channels/components/dialogs/param-override-editor-dialog.tsx",
  );
  const classicSource = readRepoFile(
    "web/classic/src/components/table/channels/modals/ParamOverrideEditorModal.jsx",
  );

  for (const source of [defaultSource, classicSource]) {
    assert.match(source, /AWS_BEDROCK_ANTHROPIC_BETA_TEMPLATE/);
    assert.match(source, /AWS_BEDROCK_REMOVE_INPUT_EXAMPLES_TEMPLATE/);
    assert.match(source, /aws_bedrock_anthropic_beta_override/);
    assert.match(source, /aws_bedrock_remove_input_examples/);
  }
});

test("advanced preset operation arrays contain one top-level rule", () => {
  for (const sourcePath of presetSources) {
    const source = readRepoFile(sourcePath);
    const counts = extractOperationsObjectCounts(source);
    assert.ok(counts.length > 0, sourcePath);
    assert.ok(
      counts.some((count) => count === 1),
      `${sourcePath} does not contain a literal single-operation preset`,
    );
    for (const count of counts) {
      assert.ok(count <= 1, `${sourcePath} contains ${count} operations`);
    }
  }
});
