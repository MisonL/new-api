import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const testDir = path.dirname(fileURLToPath(import.meta.url));
const dashboardChartFiles = [
  "../classic/src/constants/dashboard.constants.js",
  "../classic/src/hooks/dashboard/useDashboardCharts.jsx",
  "../classic/src/components/dashboard/DashboardDrilldownModal.jsx",
  "../default/src/features/dashboard/lib/charts.ts",
];

test("dashboard 图表 hover/selected 状态不使用黑色描边", () => {
  for (const relativeFile of dashboardChartFiles) {
    const source = fs.readFileSync(path.join(testDir, relativeFile), "utf8");

    assert.doesNotMatch(
      source,
      /stroke:\s*["']#000["']/,
      `${relativeFile} should not use black chart hover strokes`,
    );
  }
});
