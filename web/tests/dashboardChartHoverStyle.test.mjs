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

test("模型消耗分布柱图 hover 时轻微放大柱体", () => {
  const defaultSource = fs.readFileSync(
    path.join(testDir, "../default/src/features/dashboard/lib/charts.ts"),
    "utf8",
  );
  const classicSource = fs.readFileSync(
    path.join(testDir, "../classic/src/hooks/dashboard/useDashboardCharts.jsx"),
    "utf8",
  );

  assert.match(
    defaultSource,
    /spec_line:\s*\{[\s\S]*?bar:\s*\{[\s\S]*?hover:\s*\{[\s\S]*?scaleX:\s*CHART_BAR_HOVER_SCALE[\s\S]*?scaleY:\s*CHART_BAR_HOVER_SCALE/,
  );
  assert.match(
    classicSource,
    /const \[spec_line[\s\S]*?bar:\s*\{[\s\S]*?hover:\s*\{[\s\S]*?scaleX:\s*DASHBOARD_CHART_BAR_HOVER_SCALE[\s\S]*?scaleY:\s*DASHBOARD_CHART_BAR_HOVER_SCALE/,
  );
});
