import fs from "fs";
import path from "path";
import {
  hasSafariAmbiguousDecimals,
  isJavaScriptAssetName,
} from "./safari-compatibility.mjs";

const roots = process.argv.slice(2);

if (roots.length === 0) {
  console.error(
    "Usage: node check-browser-compatibility.mjs <dist-dir> [<dist-dir>...]",
  );
  process.exit(1);
}

const violations = [];

const scanDirectory = (rootDir) => {
  if (!fs.existsSync(rootDir)) {
    return;
  }

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

      const content = fs.readFileSync(entryPath, "utf8");
      if (hasSafariAmbiguousDecimals(content)) {
        violations.push(entryPath);
      }
    }
  }
};

for (const rootDir of roots) {
  scanDirectory(rootDir);
}

if (violations.length > 0) {
  console.error("Safari incompatible decimal literals found in built assets:");
  for (const filePath of violations) {
    console.error(filePath);
  }
  process.exit(1);
}

console.log(`Safari compatibility check passed for ${roots.join(", ")}`);
