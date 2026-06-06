import test from "node:test";
import assert from "node:assert/strict";

import {
  buildDashboardDrilldown,
  createDashboardChartAreaClickGuard,
  getDashboardBucketLogRange,
  getDashboardChartAreaDrilldownTarget,
  getDashboardDimensionDrilldownTarget,
  getDashboardDistributionLogRow,
  getDashboardDrilldownTarget,
  getDashboardLegendDrilldownTarget,
} from "../classic/src/helpers/dashboardDrilldown.js";
import { formatDashboardTimeBucket } from "../classic/src/helpers/dashboardTimeBucket.js";
import {
  buildDashboardLogInitialFilters,
  normalizeDashboardLogFilters,
} from "../classic/src/helpers/dashboardLogs.js";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const translate = (text) => text;
const testDir = path.dirname(fileURLToPath(import.meta.url));

const rows = [
  {
    created_at: 1714550400,
    model_name: "gpt-4o",
    quota: 120,
    count: 3,
    token_used: 3000,
  },
  {
    created_at: 1714550400,
    model_name: "gpt-4o-mini",
    quota: 80,
    count: 5,
    token_used: 2400,
  },
  {
    created_at: 1714636800,
    model_name: "gpt-4o",
    quota: 40,
    count: 1,
    token_used: 900,
  },
];

test("buildDashboardDrilldown aggregates one time bucket by model", () => {
  const detail = buildDashboardDrilldown({
    quotaData: rows,
    targetTime: "05-01",
    granularity: "day",
    t: translate,
  });

  assert.equal(detail.time, "05-01");
  assert.equal(detail.totalQuota, 200);
  assert.equal(detail.totalCount, 8);
  assert.equal(detail.totalTokens, 5400);
  assert.equal(detail.startTimestamp, 1714550400);
  assert.equal(detail.endTimestamp, 1714636799);
  assert.deepEqual(
    detail.rows.map((item) => ({
      model: item.model,
      logModelName: item.logModelName,
      quota: item.quota,
      count: item.count,
      tokens: item.tokens,
      ratio: item.ratio,
    })),
    [
      {
        model: "gpt-4o",
        logModelName: "gpt-4o",
        quota: 120,
        count: 3,
        tokens: 3000,
        ratio: 0.6,
      },
      {
        model: "gpt-4o-mini",
        logModelName: "gpt-4o-mini",
        quota: 80,
        count: 5,
        tokens: 2400,
        ratio: 0.4,
      },
    ],
  );
  assert.deepEqual(detail.distribution, [
    { type: "gpt-4o", value: 120 },
    { type: "gpt-4o-mini", value: 80 },
  ]);
});

test("buildDashboardDrilldown keeps empty model filter scoped", () => {
  const detail = buildDashboardDrilldown({
    quotaData: rows,
    targetTime: "05-01",
    granularity: "day",
    models: [],
    t: translate,
  });

  assert.equal(detail.totalQuota, 0);
  assert.deepEqual(detail.rows, []);
});

test("buildDashboardDrilldown keeps raw model name for log filters", () => {
  const detail = buildDashboardDrilldown({
    quotaData: [
      {
        created_at: 1714550400,
        model_name: "",
        quota: 10,
        count: 1,
        token_used: 20,
      },
    ],
    targetTime: "05-01",
    granularity: "day",
    t: translate,
  });

  assert.equal(detail.rows[0].model, "未知模型");
  assert.equal(detail.rows[0].logModelName, "");
});

test("getDashboardDistributionLogRow maps clicked pie datum to log row", () => {
  const detail = buildDashboardDrilldown({
    quotaData: rows,
    targetTime: "05-01",
    granularity: "day",
    t: translate,
  });

  const row = getDashboardDistributionLogRow({
    datum: { type: "gpt-4o-mini", value: 80 },
    rows: detail.rows,
  });

  assert.equal(row.model, "gpt-4o-mini");
  assert.equal(row.logModelName, "gpt-4o-mini");
});

test("getDashboardDistributionLogRow reads nested vchart item datum", () => {
  const row = getDashboardDistributionLogRow({
    datum: null,
    item: {
      getDatum: () => ({ type: "未知模型", value: 10 }),
    },
    rows: [
      {
        model: "未知模型",
        logModelName: "",
        quota: 10,
        count: 1,
        tokens: 20,
        ratio: 1,
      },
    ],
  });

  assert.equal(row.model, "未知模型");
  assert.equal(row.logModelName, "");
});

test("getDashboardBucketLogRange returns bucket start and end seconds", () => {
  assert.deepEqual(
    getDashboardBucketLogRange({
      quotaData: rows,
      targetTime: "05-01",
      granularity: "day",
    }),
    {
      startTimestamp: 1714550400,
      endTimestamp: 1714636799,
    },
  );

  assert.deepEqual(
    getDashboardBucketLogRange({
      quotaData: rows,
      targetTime: formatDashboardTimeBucket(1714550400, "hour", false),
      granularity: "hour",
    }),
    {
      startTimestamp: 1714550400,
      endTimestamp: 1714553999,
    },
  );
});

test("getDashboardDrilldownTarget expands collapsed other models", () => {
  const target = getDashboardDrilldownTarget({
    datum: {
      Time: "2024-05-01",
      Model: "其他",
      CollapsedModels: ["rare-a", "rare-b"],
    },
    otherLabel: "其他",
  });

  assert.deepEqual(target, {
    time: "2024-05-01",
    models: ["rare-a", "rare-b"],
  });
});

test("getDashboardDrilldownTarget keeps empty collapsed other scoped", () => {
  const target = getDashboardDrilldownTarget({
    datum: {
      Time: "2024-05-01",
      Model: "其他",
      CollapsedModels: [],
    },
    otherLabel: "其他",
  });

  assert.deepEqual(target, {
    time: "2024-05-01",
    models: [],
  });
});

test("getDashboardDrilldownTarget accepts nested vchart datum arrays", () => {
  const target = getDashboardDrilldownTarget({
    datum: [
      [
        {
          Time: "05-01",
          Model: "gpt-4o",
        },
      ],
    ],
    otherLabel: "其他",
  });

  assert.deepEqual(target, {
    time: "05-01",
    models: null,
  });
});

test("getDashboardDimensionDrilldownTarget uses clicked dimension value", () => {
  const target = getDashboardDimensionDrilldownTarget({
    dimensionInfo: [
      {
        value: "05-01",
        data: [
          {
            datum: [
              {
                Time: "05-01",
                Model: "gpt-4o",
              },
            ],
          },
        ],
      },
    ],
  });

  assert.deepEqual(target, {
    time: "05-01",
    models: null,
  });
});

test("getDashboardDimensionDrilldownTarget prefers nested datum time", () => {
  const target = getDashboardDimensionDrilldownTarget({
    dimensionInfo: [
      {
        value: 0,
        data: [
          {
            datum: [
              {
                Time: "05-02",
                Model: "gpt-4o",
              },
            ],
          },
        ],
      },
    ],
  });

  assert.deepEqual(target, {
    time: "05-02",
    models: null,
  });
});

test("getDashboardDimensionDrilldownTarget prefers scoped other datum", () => {
  const target = getDashboardDimensionDrilldownTarget({
    otherLabel: "其他",
    dimensionInfo: [
      {
        value: "05-01",
        data: [
          {
            datum: {
              Time: "05-01",
              Model: "gpt-4o",
            },
          },
          {
            datum: {
              Time: "05-01",
              Model: "其他",
              CollapsedModels: ["rare-a"],
            },
          },
        ],
      },
    ],
  });

  assert.deepEqual(target, {
    time: "05-01",
    models: ["rare-a"],
  });
});

test("getDashboardDimensionDrilldownTarget keeps scoped other first", () => {
  const target = getDashboardDimensionDrilldownTarget({
    otherLabel: "其他",
    dimensionInfo: [
      {
        value: "05-01",
        data: [
          {
            datum: {
              Time: "05-01",
              Model: "其他",
              CollapsedModels: ["rare-a"],
            },
          },
          {
            datum: {
              Time: "05-01",
              Model: "gpt-4o",
            },
          },
        ],
      },
    ],
  });

  assert.deepEqual(target, {
    time: "05-01",
    models: ["rare-a"],
  });
});

test("getDashboardDimensionDrilldownTarget uses first scoped other datum", () => {
  const target = getDashboardDimensionDrilldownTarget({
    otherLabel: "其他",
    dimensionInfo: [
      {
        value: "05-01",
        data: [
          {
            datum: {
              Time: "05-01",
              Model: "其他",
              CollapsedModels: ["rare-a"],
            },
          },
          {
            datum: {
              Time: "05-01",
              Model: "其他",
              CollapsedModels: ["rare-b"],
            },
          },
        ],
      },
    ],
  });

  assert.deepEqual(target, {
    time: "05-01",
    models: ["rare-a"],
  });
});

test("getDashboardChartAreaDrilldownTarget maps click position to time bucket", () => {
  const target = getDashboardChartAreaDrilldownTarget({
    clientX: 260,
    rect: {
      left: 100,
      width: 400,
    },
    chartValues: [
      { Time: "05-01", Model: "a" },
      { Time: "05-01", Model: "b" },
      { Time: "05-02", Model: "a" },
      { Time: "05-03", Model: "a" },
    ],
  });

  assert.deepEqual(target, {
    time: "05-02",
    models: null,
  });
});

test("dashboard legend target uses full chart range for one model", () => {
  const target = getDashboardLegendDrilldownTarget({
    event: { event: { detail: { data: { id: "gpt-4o" } } } },
    otherLabel: "其他",
    chartValues: [
      { Time: "05-01", Model: "gpt-4o" },
      { Time: "05-01", Model: "gpt-4o-mini" },
      { Time: "05-02", Model: "gpt-4o" },
    ],
  });

  assert.deepEqual(target, {
    time: "05-01 - 05-02",
    times: ["05-01", "05-02"],
    models: ["gpt-4o"],
  });
});

test("dashboard legend target expands other models across chart range", () => {
  const target = getDashboardLegendDrilldownTarget({
    event: { data: { id: "其他" } },
    otherLabel: "其他",
    chartValues: [
      { Time: "05-01", Model: "其他", CollapsedModels: ["rare-a"] },
      { Time: "05-02", Model: "其他", CollapsedModels: ["rare-a", "rare-b"] },
    ],
  });

  assert.deepEqual(target, {
    time: "05-01 - 05-02",
    times: ["05-01", "05-02"],
    models: ["rare-a", "rare-b"],
  });
});

test("buildDashboardDrilldown aggregates full legend time range", () => {
  const detail = buildDashboardDrilldown({
    quotaData: rows,
    targetTime: "05-01 - 05-02",
    targetTimes: ["05-01", "05-02"],
    granularity: "day",
    models: ["gpt-4o"],
    t: translate,
  });

  assert.equal(detail.time, "05-01 - 05-02");
  assert.equal(detail.totalQuota, 160);
  assert.equal(detail.totalCount, 4);
  assert.equal(detail.totalTokens, 3900);
  assert.equal(detail.startTimestamp, 1714550400);
  assert.equal(detail.endTimestamp, 1714723199);
  assert.deepEqual(
    detail.rows.map((item) => item.model),
    ["gpt-4o"],
  );
});

test("dashboard chart area click guard skips clicks already handled by chart datum", () => {
  const guard = createDashboardChartAreaClickGuard();

  assert.equal(guard.shouldHandleAreaClick(), true);
  guard.markChartClickHandled({
    time: "05-01",
    models: null,
  });
  assert.equal(guard.shouldHandleAreaClick(), false);
  assert.equal(guard.shouldHandleAreaClick(), true);
});

test("dashboard chart area click guard ignores chart clicks without target", () => {
  const guard = createDashboardChartAreaClickGuard();

  guard.markChartClickHandled(null);

  assert.equal(guard.shouldHandleAreaClick(), true);
  assert.equal(guard.shouldHandleAreaClick(), true);
});

test("dashboard chart area click guard preserves legacy handled clicks", () => {
  const guard = createDashboardChartAreaClickGuard();

  guard.markChartClickHandled();

  assert.equal(guard.shouldHandleAreaClick(), false);
  assert.equal(guard.shouldHandleAreaClick(), true);
});

test("dashboard log filters preserve inherited scope fields", () => {
  const initial = buildDashboardLogInitialFilters(
    {
      logType: 2,
      username: "mison",
      token_name: "desktop",
      model_name: "gpt-5.4",
      channel: "7",
      group: "default",
      request_id: "req-1",
      fast_page: true,
      compact: true,
      startTimestamp: 1714550400,
      endTimestamp: 1714636799,
    },
    {
      startTimestamp: 1,
      endTimestamp: 2,
    },
  );

  assert.equal(initial.logType, 2);
  assert.equal(initial.username, "mison");
  assert.equal(initial.token_name, "desktop");
  assert.equal(initial.model_name, "gpt-5.4");
  assert.equal(initial.channel, "7");
  assert.equal(initial.group, "default");
  assert.equal(initial.request_id, "req-1");
  assert.equal(initial.fast_page, true);
  assert.equal(initial.compact, true);

  const normalized = normalizeDashboardLogFilters(initial);
  assert.deepEqual(
    {
      type: normalized.type,
      username: normalized.username,
      token_name: normalized.token_name,
      model_name: normalized.model_name,
      channel: normalized.channel,
      group: normalized.group,
      request_id: normalized.request_id,
      fast_page: normalized.fast_page,
      compact: normalized.compact,
      start_timestamp: normalized.start_timestamp,
      end_timestamp: normalized.end_timestamp,
    },
    {
      type: 2,
      username: "mison",
      token_name: "desktop",
      model_name: "gpt-5.4",
      channel: "7",
      group: "default",
      request_id: "req-1",
      fast_page: "true",
      compact: "true",
      start_timestamp: 1714550400,
      end_timestamp: 1714636799,
    },
  );
});

test("dashboard log filters encode empty model scope explicitly", () => {
  const initial = buildDashboardLogInitialFilters(
    {
      logType: 2,
      model_name: "",
      model_name_empty: true,
      model_name_empty_label: "未知模型",
      startTimestamp: 1714550400,
      endTimestamp: 1714636799,
    },
    {
      startTimestamp: 1,
      endTimestamp: 2,
    },
  );

  assert.equal(initial.model_name, "未知模型");
  assert.equal(initial.model_name_empty, true);
  assert.equal(initial.model_name_empty_label, "未知模型");

  const normalized = normalizeDashboardLogFilters(initial);
  assert.equal(normalized.model_name, "");
  assert.equal(normalized.model_name_empty, "true");
  assert.equal(normalized.start_timestamp, 1714550400);
  assert.equal(normalized.end_timestamp, 1714636799);
});

test("dashboard log refresh controls do not submit the filter form", () => {
  const modalSource = fs.readFileSync(
    path.join(
      testDir,
      "../classic/src/components/dashboard/DashboardLogsModal.jsx",
    ),
    "utf8",
  );

  assert.match(
    modalSource,
    /htmlType='button'[\s\S]{0,240}cancelScheduledAutoLoad\(\);[\s\S]{0,80}loadLogs\(page, pageSize\);/,
  );
  assert.match(
    modalSource,
    /htmlType='button'[\s\S]{0,120}onClick=\{handleReset\}/,
  );
});

test("dashboard log modal does not force desktop horizontal overflow", () => {
  const modalSource = fs.readFileSync(
    path.join(
      testDir,
      "../classic/src/components/dashboard/DashboardLogsModal.jsx",
    ),
    "utf8",
  );

  assert.doesNotMatch(modalSource, /const tableScrollX/);
  assert.doesNotMatch(modalSource, /x:\s*(?:1210|1330|tableScrollX)/);
  assert.match(modalSource, /x:\s*'max-content'/);
});

test("dashboard distribution chart opens scoped row logs", () => {
  const modalSource = fs.readFileSync(
    path.join(
      testDir,
      "../classic/src/components/dashboard/DashboardDrilldownModal.jsx",
    ),
    "utf8",
  );

  assert.match(modalSource, /const handleDistributionClick = \(event\) => \{/);
  assert.match(modalSource, /getDashboardDistributionLogRow\(\{/);
  assert.doesNotMatch(modalSource, /getDashboardDistributionClickLogRow/);
  assert.match(modalSource, /onOpenRowLogs\?\.\(row\)/);
  assert.doesNotMatch(modalSource, /onClick=\{handleDistributionAreaClick\}/);
  assert.match(
    modalSource,
    /onClick=\{handleDistributionClick\}[\s\S]{0,120}onPointerTap=\{handleDistributionClick\}/,
  );
  assert.match(
    modalSource,
    /className='h-\[clamp\(220px,calc\(100dvh-360px\),360px\)\] cursor-pointer/,
  );
});

test("dashboard distribution chart enlarges hovered ring segment", () => {
  const modalSource = fs.readFileSync(
    path.join(
      testDir,
      "../classic/src/components/dashboard/DashboardDrilldownModal.jsx",
    ),
    "utf8",
  );

  assert.match(modalSource, /pie:\s*\{/);
  assert.match(modalSource, /style:\s*\{\s*cursor:\s*'pointer'/);
  assert.match(modalSource, /state:\s*\{\s*hover:\s*\{/);
  assert.match(modalSource, /outerRadius:\s*0\.83/);
  assert.match(modalSource, /DASHBOARD_CHART_HOVER_STROKE/);
  assert.match(modalSource, /stroke:\s*DASHBOARD_CHART_HOVER_STROKE/);
  assert.match(modalSource, /lineWidth:\s*1/);
});
