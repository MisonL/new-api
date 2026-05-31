import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const SAFARI_AMBIGUOUS_DECIMAL_PATTERN = /\?\.(\d)/g;

export function rewriteSafariAmbiguousDecimals(code) {
  return code.replace(SAFARI_AMBIGUOUS_DECIMAL_PATTERN, "?0.$1");
}

export function hasSafariAmbiguousDecimals(code) {
  return /\?\.(\d)/.test(code);
}

export function isJavaScriptAssetName(assetName) {
  return (
    assetName.endsWith(".js") ||
    assetName.endsWith(".mjs") ||
    assetName.endsWith(".cjs")
  );
}

export function rewriteJavaScriptFile(filePath) {
  const original = fs.readFileSync(filePath, "utf8");
  const rewritten = rewriteSafariAmbiguousDecimals(original);
  if (rewritten !== original) {
    fs.writeFileSync(filePath, rewritten, "utf8");
    return true;
  }
  return false;
}

export function rewriteJavaScriptFilesInDirectory(rootDir) {
  if (!fs.existsSync(rootDir)) {
    return 0;
  }

  let changedCount = 0;
  const stack = [rootDir];

  while (stack.length > 0) {
    const currentDir = stack.pop();
    if (!currentDir) {
      continue;
    }

    for (const entry of fs.readdirSync(currentDir, { withFileTypes: true })) {
      const entryPath = path.join(currentDir, entry.name);
      if (entry.isDirectory()) {
        stack.push(entryPath);
        continue;
      }
      if (!entry.isFile() || !isJavaScriptAssetName(entry.name)) {
        continue;
      }
      if (rewriteJavaScriptFile(entryPath)) {
        changedCount += 1;
      }
    }
  }

  return changedCount;
}

const isDirectRun =
  process.argv[1] &&
  fileURLToPath(import.meta.url) === path.resolve(process.argv[1]);

if (isDirectRun) {
  const targetDirs = process.argv.slice(2);
  if (targetDirs.length === 0) {
    console.error("Usage: node safari-compatibility.mjs <dist-dir>...");
    process.exit(1);
  }

  let changedCount = 0;
  for (const targetDir of targetDirs) {
    changedCount += rewriteJavaScriptFilesInDirectory(path.resolve(targetDir));
  }

  console.log(`Safari compatibility rewrite updated ${changedCount} file(s)`);
}
