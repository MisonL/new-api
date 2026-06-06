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
    const body = source.slice(openIndex + 1, closeIndex);
    counts.push({ count: countTopLevelObjects(body), body });
    pattern.lastIndex = closeIndex + 1;
  }
  return counts;
}

function isCodexSessionFallbackOperations(entry) {
  return (
    entry.count === 2 &&
    entry.body.includes("pass_headers") &&
    entry.body.includes("CODEX_SESSION_ID_FALLBACK_OPERATION")
  );
}

function extractStructuredValueEmitBlock(source) {
  const componentIndex = source.indexOf("StructuredValueNodeEditor");
  assert.notEqual(componentIndex, -1);
  const emitIndex = source.indexOf(
    "const emitNode = useCallback",
    componentIndex,
  );
  assert.notEqual(emitIndex, -1);
  const updateIndex = source.indexOf(
    "const updateNode = useCallback",
    emitIndex,
  );
  assert.notEqual(updateIndex, -1);
  return source.slice(emitIndex, updateIndex);
}

function extractStructuredValueBuildBlock(source) {
  const buildIndex = source.indexOf("const buildStructuredValue =");
  assert.notEqual(buildIndex, -1);
  const quoteIndex = source.indexOf(
    "const shouldQuoteStructuredString",
    buildIndex,
  );
  assert.notEqual(quoteIndex, -1);
  return source.slice(buildIndex, quoteIndex);
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
      counts.some((entry) => entry.count === 1),
      `${sourcePath} does not contain a literal single-operation preset`,
    );
    for (const entry of counts) {
      assert.ok(
        entry.count <= 1 || isCodexSessionFallbackOperations(entry),
        `${sourcePath} contains ${entry.count} operations`,
      );
    }
  }
});

test("classic visual editor exposes non-json controls for every param override shape", () => {
  const source = readRepoFile(
    "web/classic/src/components/table/channels/modals/ParamOverrideEditorModal.jsx",
  );

  assert.match(source, /const LegacyOverrideEditor/);
  assert.match(source, /旧格式字段覆盖/);
  assert.match(source, /parseStructuredValueText\(entry\.value_text\)/);
  assert.match(source, /parseStructuredValueText\(condition\.value_text\)/);
  assert.match(source, /parseStructuredValueText\(valueRaw\)/);
  assert.match(source, /sourceKey: 'names'/);
  assert.match(source, /buildPassHeadersValueText/);
  assert.match(source, /对象名称/);
  assert.match(source, /单个请求头/);
  assert.match(source, /headerRows\.map/);
  assert.match(source, /MAX_STRUCTURED_VALUE_DEPTH/);
  assert.match(source, /isCompleteStructuredNumberText/);
});

test("classic visual editor serializes generic operation values as typed values", () => {
  const source = readRepoFile(
    "web/classic/src/components/table/channels/modals/ParamOverrideEditorModal.jsx",
  );

  assert.match(source, /mode === 'pass_headers'/);
  assert.match(source, /mode === 'set_header'/);
  assert.match(source, /mode === 'return_error'/);
  assert.match(source, /mode === 'prune_objects'/);
  assert.match(
    source,
    /payload\.value = parseLooseValue\(operation\.value_text\);/,
  );
  assert.match(
    source,
    /payload\.value = parseStructuredValueText\(operation\.value_text\);/,
  );
});

test("visual editors preserve set_header direct values as strings", () => {
  const defaultSource = readRepoFile(
    "web/default/src/features/channels/components/dialogs/param-override-editor-dialog.tsx",
  );
  const classicSource = readRepoFile(
    "web/classic/src/components/table/channels/modals/ParamOverrideEditorModal.jsx",
  );

  for (const source of [defaultSource, classicSource]) {
    assert.match(
      source,
      /draft\.mode === 'direct'[\s\S]*?JSON\.stringify\(.*draft\.directText/,
    );
  }
});

test("structured value editors gate parent commits without swallowing change errors", () => {
  const defaultSource = readRepoFile(
    "web/default/src/features/channels/components/dialogs/param-override-editor-dialog.tsx",
  );
  const classicSource = readRepoFile(
    "web/classic/src/components/table/channels/modals/ParamOverrideEditorModal.jsx",
  );

  for (const source of [defaultSource, classicSource]) {
    const emitBlock = extractStructuredValueEmitBlock(source);
    const guardIndex = emitBlock.indexOf("canSerializeStructuredValueNode");
    const changeIndex = emitBlock.search(
      /(?:structuredValueNodeEditorProps\.)?onChange\([^)]+\)/,
    );

    assert.match(source, /const canSerializeStructuredValueNode/);
    assert.match(
      emitBlock,
      /if \(!canSerializeStructuredValueNode\([^)]+\)\)\s*\{\s*return;?\s*\}/,
    );
    assert.ok(
      guardIndex >= 0 && changeIndex > guardIndex,
      "structured editor must gate parent change before calling onChange",
    );
    assert.match(
      emitBlock,
      /(?:structuredValueNodeEditorProps\.)?onChange\([^)]+\)/,
    );
    assert.match(
      source,
      /canSerializeStructuredValueNode\((node|currentNode)\)/,
    );
    assert.doesNotMatch(emitBlock, /\btry\s*\{/);
    assert.doesNotMatch(
      source,
      /Keep invalid in-progress numeric input local until it becomes valid/,
    );
  }
});

test("structured value serialization reports malformed nodes explicitly", () => {
  const defaultSource = readRepoFile(
    "web/default/src/features/channels/components/dialogs/param-override-editor-dialog.tsx",
  );
  const classicSource = readRepoFile(
    "web/classic/src/components/table/channels/modals/ParamOverrideEditorModal.jsx",
  );

  for (const source of [defaultSource, classicSource]) {
    const buildBlock = extractStructuredValueBuildBlock(source);

    assert.match(source, /(function|const) assertStructuredValueInvariant/);
    assert.match(source, /Invalid structured value node text/);
    assert.match(source, /Invalid structured value object entries/);
    assert.match(source, /Invalid structured value array items/);
    assert.match(source, /Invalid structured value kind/);
    assert.doesNotMatch(buildBlock, /Boolean\(node\.boolValue\)/);
    assert.doesNotMatch(buildBlock, /String\(node\.text \?\? ''\)/);
    assert.doesNotMatch(buildBlock, /node\.objectEntries \|\| \[\]/);
    assert.doesNotMatch(buildBlock, /node\.arrayItems \|\| \[\]/);
  }
});

test("structured value import enforces nesting depth before serialization", () => {
  const defaultSource = readRepoFile(
    "web/default/src/features/channels/components/dialogs/param-override-editor-dialog.tsx",
  );
  const classicSource = readRepoFile(
    "web/classic/src/components/table/channels/modals/ParamOverrideEditorModal.jsx",
  );

  for (const source of [defaultSource, classicSource]) {
    assert.match(source, /STRUCTURED_VALUE_DEPTH_ERROR/);
    assert.match(source, /depth > MAX_STRUCTURED_VALUE_DEPTH/);
    assert.match(source, /normalizeStructuredValueNode\(item, depth \+ 1\)/);
    assert.match(source, /isJsonLikeStructuredValueText/);
    assert.match(source, /parseStructuredValueNodeForDisplay/);
    assert.doesNotMatch(source, /node=\{parseStructuredValueNode\(/);
  }
});

test("visual editors accept legacy msg field for return_error values", () => {
  const defaultSource = readRepoFile(
    "web/default/src/features/channels/components/dialogs/param-override-editor-dialog.tsx",
  );
  const classicSource = readRepoFile(
    "web/classic/src/components/table/channels/modals/ParamOverrideEditorModal.jsx",
  );

  for (const source of [defaultSource, classicSource]) {
    assert.match(
      source,
      /parsedObject\.message !== undefined[\s\S]*?parsedObject\.msg|parsed\.message !== undefined[\s\S]*?parsed\.msg/,
    );
  }
});

test("default visual editor parses all supported pass_headers value shapes", () => {
  const source = readRepoFile(
    "web/default/src/features/channels/components/dialogs/param-override-editor-dialog.tsx",
  );

  assert.match(source, /obj\.names !== undefined/);
  assert.match(source, /sourceKey: 'names'/);
  assert.match(source, /sourceKey: 'header'/);
});

test("visual editors expose prune_objects recursion and conditions without raw JSON editing", () => {
  const defaultSource = readRepoFile(
    "web/default/src/features/channels/components/dialogs/param-override-editor-dialog.tsx",
  );
  const classicSource = readRepoFile(
    "web/classic/src/components/table/channels/modals/ParamOverrideEditorModal.jsx",
  );

  for (const source of [defaultSource, classicSource]) {
    assert.match(source, /mode: 'prune_objects'/);
    assert.match(source, /simpleMode: false/);
    assert.match(source, /recursive: false/);
    assert.match(source, /Add Condition|新增条件/);
    assert.match(source, /Current Level Only|仅当前层/);
    assert.match(source, /Additional Conditions|附加条件/);
    assert.match(source, /buildPruneObjectsValueText/);
    assert.match(source, /parseStructuredValueText\(valueRaw\)/);
  }
});

test("visual editors parse condition object shorthand without raw JSON editing", () => {
  const defaultSource = readRepoFile(
    "web/default/src/features/channels/components/dialogs/param-override-editor-dialog.tsx",
  );
  const classicSource = readRepoFile(
    "web/classic/src/components/table/channels/modals/ParamOverrideEditorModal.jsx",
  );

  for (const source of [defaultSource, classicSource]) {
    assert.match(source, /normalizeConditionList/);
    assert.match(source, /Object\.entries\(rawConditions/);
    assert.match(source, /conditions: normalizeConditionList/);
  }
});

test("visual editors expose runtime header copy and move operations", () => {
  const defaultSource = readRepoFile(
    "web/default/src/features/channels/components/dialogs/param-override-editor-dialog.tsx",
  );
  const classicSource = readRepoFile(
    "web/classic/src/components/table/channels/modals/ParamOverrideEditorModal.jsx",
  );

  for (const source of [defaultSource, classicSource]) {
    assert.match(source, /value: 'copy_header'/);
    assert.match(source, /value: 'move_header'/);
    assert.match(
      source,
      /copy_header: \{ from: true, to: true, keepOrigin: true, pathAlias: true \}/,
    );
    assert.match(
      source,
      /move_header: \{ from: true, to: true, keepOrigin: true, pathAlias: true \}/,
    );
    assert.match(source, /'copy_header',/);
    assert.match(source, /'move_header',/);
    assert.match(
      source,
      /if \(!payload\.from && pathValue\) payload\.from = pathValue|if \(!payload\.from && pathValue\) \{\s*payload\.from = pathValue;\s*\}/s,
    );
    assert.match(
      source,
      /if \(!payload\.to && pathValue\) payload\.to = pathValue|if \(!payload\.to && pathValue\) \{\s*payload\.to = pathValue;\s*\}/s,
    );
  }
});

test("classic visual editor does not reject header path aliases during validation", () => {
  const classicSource = readRepoFile(
    "web/classic/src/components/table/channels/modals/ParamOverrideEditorModal.jsx",
  );

  assert.match(
    classicSource,
    /if \(meta\.from && !fromValue && !\(meta\.pathAlias && pathValue\)\)/,
  );
  assert.match(
    classicSource,
    /if \(meta\.to && !toValue && !\(meta\.pathAlias && pathValue\)\)/,
  );
});

test("visual editors preserve mixed legacy fields next to operations", () => {
  const defaultSource = readRepoFile(
    "web/default/src/features/channels/components/dialogs/param-override-editor-dialog.tsx",
  );
  const classicSource = readRepoFile(
    "web/classic/src/components/table/channels/modals/ParamOverrideEditorModal.jsx",
  );

  assert.match(defaultSource, /getLegacyEntriesFromObject/);
  assert.match(defaultSource, /excludeOperations: true/);
  assert.match(defaultSource, /buildLegacyOverridePayload/);
  assert.match(defaultSource, /Top-level Field Overrides/);
  assert.match(
    defaultSource,
    /\{\s*\.\.\.legacyPayload\.value,\s*\.\.\.operationsPayload\s*\}/s,
  );

  assert.match(classicSource, /getLegacyEntriesFromObject/);
  assert.match(classicSource, /excludeOperations: true/);
  assert.match(classicSource, /buildLegacyOverridePayload/);
  assert.match(classicSource, /顶层字段覆盖/);
  assert.match(
    classicSource,
    /\{\s*\.\.\.legacyPayload\.value,\s*\.\.\.operationsPayload\s*\}/s,
  );
});

test("append operation templates keep existing visual legacy fields", () => {
  const defaultSource = readRepoFile(
    "web/default/src/features/channels/components/dialogs/param-override-editor-dialog.tsx",
  );
  const classicSource = readRepoFile(
    "web/classic/src/components/table/channels/modals/ParamOverrideEditorModal.jsx",
  );
  const defaultAppendStart = defaultSource.indexOf(
    "const operationsPayload = ((payload as Record<string, unknown>)",
  );
  const defaultAppendEnd = defaultSource.indexOf(
    "      } else {",
    defaultAppendStart,
  );
  const classicAppendStart = classicSource.indexOf(
    "const appendOperationsTemplate = (operationsPayload) =>",
  );
  const classicAppendEnd = classicSource.indexOf(
    "  const clearValue = () =>",
    classicAppendStart,
  );

  assert.notEqual(defaultAppendStart, -1);
  assert.notEqual(defaultAppendEnd, -1);
  assert.notEqual(classicAppendStart, -1);
  assert.notEqual(classicAppendEnd, -1);

  const defaultAppendBlock = defaultSource.slice(
    defaultAppendStart,
    defaultAppendEnd,
  );
  const classicAppendBlock = classicSource.slice(
    classicAppendStart,
    classicAppendEnd,
  );

  assert.doesNotMatch(
    defaultAppendBlock,
    /setLegacyEntries\(\[createDefaultLegacyEntry\(\)\]\)/,
  );
  assert.doesNotMatch(
    classicAppendBlock,
    /setLegacyEntries\(\[createDefaultLegacyEntry\(\)\]\)[\s\S]*?setJsonText\(''\);/,
  );
  assert.doesNotMatch(
    classicAppendBlock,
    /setLegacyValue\(''\)[\s\S]*?setJsonText\(''\);/,
  );
});

test("template library uses explicit replace or add actions", () => {
  const defaultSource = readRepoFile(
    "web/default/src/features/channels/components/dialogs/param-override-editor-dialog.tsx",
  );
  const classicSource = readRepoFile(
    "web/classic/src/components/table/channels/modals/ParamOverrideEditorModal.jsx",
  );

  assert.match(defaultSource, /Preset Rule Library/);
  assert.match(defaultSource, /Recommended Scenarios/);
  assert.match(defaultSource, /Advanced Compatibility/);
  assert.match(defaultSource, /Examples and Starting Points/);
  assert.match(defaultSource, /Replace Current Rules/);
  assert.match(defaultSource, /Append to Existing Rules/);
  assert.match(
    defaultSource,
    /Pick a scenario first\. It will not change this channel until you apply it\./,
  );
  assert.match(
    defaultSource,
    /Replace Current Rules removes existing rules first\. Append keeps existing rules and adds the selected preset after them\./,
  );
  assert.match(defaultSource, /applyTemplate\('replace'\)/);
  assert.match(defaultSource, /applyTemplate\('add'\)/);
  assert.doesNotMatch(defaultSource, /Clear Existing and Apply/);
  assert.doesNotMatch(defaultSource, /Keep Existing and Append/);

  assert.match(classicSource, /selectTemplatePreset/);
  assert.match(classicSource, /replaceWithSelectedTemplate/);
  assert.match(classicSource, /addSelectedTemplate/);
  assert.match(classicSource, /推荐场景/);
  assert.match(classicSource, /高级兼容/);
  assert.match(classicSource, /示例起点/);
  assert.match(
    classicSource,
    /先选择方案。只有点击应用按钮后，才会修改当前规则。/,
  );
  assert.match(classicSource, /预置规则方案库/);
  assert.match(classicSource, /替换当前规则/);
  assert.match(classicSource, /追加到现有规则/);
  assert.match(
    classicSource,
    /替换当前规则会先删除现有规则；追加到现有规则会把所选方案添加到规则末尾。/,
  );
  assert.match(
    classicSource,
    /onClick=\{\(\) => selectTemplatePreset\(presetKey\)\}/,
  );
  assert.doesNotMatch(
    classicSource,
    /onClick=\{\(\) => applyTemplatePreset\(presetKey, 'fill'\)\}/,
  );
  assert.doesNotMatch(classicSource, /t\('填充模板'\)/);
  assert.doesNotMatch(classicSource, /t\('追加模板'\)/);
  assert.doesNotMatch(classicSource, /t\('清空并套用'\)/);
  assert.doesNotMatch(classicSource, /t\('保留并添加到末尾'\)/);
});

test("recommended param override presets prioritize high-value scenarios", () => {
  const defaultSource = readRepoFile(
    "web/default/src/features/channels/components/dialogs/param-override-editor-dialog.tsx",
  );
  const classicSource = readRepoFile(
    "web/classic/src/components/table/channels/modals/ParamOverrideEditorModal.jsx",
  );

  for (const source of [defaultSource, classicSource]) {
    const quickStart = source.indexOf("const QUICK_TEMPLATE_PRESETS = [");
    assert.notEqual(quickStart, -1);
    const quickOpen = source.indexOf("[", quickStart);
    const quickClose = findMatchingBracket(source, quickOpen);
    assert.notEqual(quickClose, -1);
    const quickBlock = source.slice(quickOpen, quickClose + 1);

    assert.match(quickBlock, /'codex_cli_headers_passthrough'/);
    assert.match(quickBlock, /'codex_desktop_headers_passthrough'/);
    assert.match(quickBlock, /'claude_cli_headers_passthrough'/);
    assert.match(quickBlock, /'openai_sdk_headers_passthrough'/);
    assert.match(quickBlock, /'aws_bedrock_anthropic_beta_override'/);
    assert.match(quickBlock, /'remove_image_generation_tool'/);
    assert.doesNotMatch(
      quickBlock,
      /operations_default|legacy_default|gemini_cli_headers_passthrough|qwen_code_headers_passthrough|droid_cli_headers_passthrough|pass_headers_auth/,
    );
  }
});

test("param override preset names stay scenario-scoped and product-accurate", () => {
  const defaultEditorSource = readRepoFile(
    "web/default/src/features/channels/components/dialogs/param-override-editor-dialog.tsx",
  );
  const classicEditorSource = readRepoFile(
    "web/classic/src/components/table/channels/modals/ParamOverrideEditorModal.jsx",
  );
  const defaultAffinitySource = readRepoFile(
    "web/default/src/features/system-settings/general/channel-affinity/constants.ts",
  );
  const classicAffinitySource = readRepoFile(
    "web/classic/src/constants/channel-affinity-template.constants.js",
  );

  for (const source of [
    defaultEditorSource,
    classicEditorSource,
    defaultAffinitySource,
    classicAffinitySource,
  ]) {
    assert.doesNotMatch(
      source,
      /Codex Desktop Compat: Remove Image Generation Tool/,
    );
    assert.doesNotMatch(source, /Claude CLI Header Passthrough/);
  }

  assert.match(
    defaultEditorSource,
    /label: 'Upstream Compat: Remove Image Generation Tool'/,
  );
  assert.match(classicEditorSource, /label: '上游兼容：移除图片生成工具'/);
  for (const source of [defaultAffinitySource, classicAffinitySource]) {
    assert.match(
      source,
      /label: 'Upstream Compat: Remove Image Generation Tool'/,
    );
    assert.match(source, /label: 'Claude Code Header Passthrough'/);
    assert.match(source, /name: 'claude code trace'/);
    assert.doesNotMatch(source, /name: 'claude cli trace'/);
  }
});

test("prune_objects simple mode does not expose advanced controls", () => {
  const defaultSource = readRepoFile(
    "web/default/src/features/channels/components/dialogs/param-override-editor-dialog.tsx",
  );
  const classicSource = readRepoFile(
    "web/classic/src/components/table/channels/modals/ParamOverrideEditorModal.jsx",
  );

  assert.match(defaultSource, /draft\.simpleMode \? \(/);
  assert.match(defaultSource, /<div className='space-y-3'>/);
  assert.match(classicSource, /!pruneObjectsDraft\.simpleMode \? \(/);
});
