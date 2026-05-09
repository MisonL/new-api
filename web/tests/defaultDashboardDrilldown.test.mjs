import test from "node:test";
import assert from "node:assert/strict";

import {
  buildDashboardDrilldown,
  createDashboardChartAreaClickGuard,
  getDashboardChartAreaDrilldownTarget,
  getDashboardDimensionDrilldownTarget,
  getDashboardDrilldownTarget,
} from "../default/src/features/dashboard/lib/drilldown.ts";
import { processChartData } from "../default/src/features/dashboard/lib/charts.ts";

const rows = [
  {
    created_at: 1714550400,
    model_name: "gpt-5.4",
    quota: 120,
    count: 3,
    token_used: 3000,
  },
  {
    created_at: 1714550400,
    model_name: "gpt-5.3-codex",
    quota: 80,
    count: 5,
    token_used: 2400,
  },
  {
    created_at: 1714636800,
    model_name: "gpt-5.4",
    quota: 40,
    count: 1,
    token_used: 900,
  },
];

test("default dashboard drilldown aggregates one time bucket by model", () => {
  const detail = buildDashboardDrilldown({
    data: rows,
    targetTime: "05-01",
    granularity: "day",
  });

  assert.equal(detail.time, "05-01");
  assert.equal(detail.totalQuota, 200);
  assert.equal(detail.totalCount, 8);
  assert.equal(detail.totalTokens, 5400);
  assert.deepEqual(
    detail.rows.map((item) => ({
      model: item.model,
      quota: item.quota,
      count: item.count,
      tokens: item.tokens,
      ratio: item.ratio,
    })),
    [
      {
        model: "gpt-5.4",
        quota: 120,
        count: 3,
        tokens: 3000,
        ratio: 0.6,
      },
      {
        model: "gpt-5.3-codex",
        quota: 80,
        count: 5,
        tokens: 2400,
        ratio: 0.4,
      },
    ],
  );
});

test("default dashboard drilldown target accepts nested vchart datum arrays", () => {
  const target = getDashboardDrilldownTarget({
    datum: [
      [
        {
          Time: "05-01",
          Model: "gpt-5.4",
        },
      ],
    ],
    otherLabel: "Other",
  });

  assert.deepEqual(target, {
    time: "05-01",
    models: null,
  });
});

test("default dashboard drilldown target preserves collapsed other models", () => {
  const target = getDashboardDrilldownTarget({
    datum: {
      Time: "05-01",
      Model: "Other",
      CollapsedModels: ["model-16", "model-17"],
    },
    otherLabel: "Other",
  });

  assert.deepEqual(target, {
    time: "05-01",
    models: ["model-16", "model-17"],
  });
});

test("default dashboard drilldown keeps empty other collapsed target scoped", () => {
  const target = getDashboardDrilldownTarget({
    datum: {
      Time: "05-01",
      Model: "Other",
      CollapsedModels: [],
    },
    otherLabel: "Other",
  });

  assert.deepEqual(target, {
    time: "05-01",
    models: [],
  });
});

test("default dashboard dimension target falls back to clicked dimension value", () => {
  const target = getDashboardDimensionDrilldownTarget({
    dimensionInfo: [
      {
        value: "05-01",
        data: [],
      },
    ],
  });

  assert.deepEqual(target, {
    time: "05-01",
    models: null,
  });
});

test("default dashboard dimension target preserves other collapsed models", () => {
  const target = getDashboardDimensionDrilldownTarget({
    otherLabel: "Other",
    dimensionInfo: [
      {
        value: "05-01",
        data: [
          {
            datum: {
              Time: "05-01",
              Model: "Other",
              CollapsedModels: ["model-16", "model-17"],
            },
          },
        ],
      },
    ],
  });

  assert.deepEqual(target, {
    time: "05-01",
    models: ["model-16", "model-17"],
  });
});

test("default dashboard dimension target prefers scoped other datum", () => {
  const target = getDashboardDimensionDrilldownTarget({
    otherLabel: "Other",
    dimensionInfo: [
      {
        value: "05-01",
        data: [
          {
            datum: {
              Time: "05-01",
              Model: "model-01",
            },
          },
          {
            datum: {
              Time: "05-01",
              Model: "Other",
              CollapsedModels: ["model-16"],
            },
          },
        ],
      },
    ],
  });

  assert.deepEqual(target, {
    time: "05-01",
    models: ["model-16"],
  });
});

test("default dashboard dimension target keeps scoped other first", () => {
  const target = getDashboardDimensionDrilldownTarget({
    otherLabel: "Other",
    dimensionInfo: [
      {
        value: "05-01",
        data: [
          {
            datum: {
              Time: "05-01",
              Model: "Other",
              CollapsedModels: ["model-16"],
            },
          },
          {
            datum: {
              Time: "05-01",
              Model: "model-01",
            },
          },
        ],
      },
    ],
  });

  assert.deepEqual(target, {
    time: "05-01",
    models: ["model-16"],
  });
});

test("default dashboard dimension target uses first scoped other datum", () => {
  const target = getDashboardDimensionDrilldownTarget({
    otherLabel: "Other",
    dimensionInfo: [
      {
        value: "05-01",
        data: [
          {
            datum: {
              Time: "05-01",
              Model: "Other",
              CollapsedModels: ["model-16"],
            },
          },
          {
            datum: {
              Time: "05-01",
              Model: "Other",
              CollapsedModels: ["model-17"],
            },
          },
        ],
      },
    ],
  });

  assert.deepEqual(target, {
    time: "05-01",
    models: ["model-16"],
  });
});

test("default dashboard area target maps click position to time bucket", () => {
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

test("default dashboard area other points carry collapsed models", () => {
  const visibleCount = 15;
  const extraForCollapse = 2;
  const totalItems = visibleCount + extraForCollapse;
  const data = Array.from({ length: totalItems }, (_, index) => ({
    created_at: 1714550400,
    model_name: `model-${String(index + 1).padStart(2, "0")}`,
    quota: 170 - index,
    count: 1,
    token_used: 100,
  }));
  const chartData = processChartData(data, "day", (key) => key);
  const otherPoint = chartData.spec_area.data[0].values.find(
    (item) => item.Time === "05-01" && item.Model === "Other",
  );

  assert.deepEqual(otherPoint.CollapsedModels, ["model-16", "model-17"]);

  const target = getDashboardDrilldownTarget({
    datum: otherPoint,
    otherLabel: "Other",
  });
  const detail = buildDashboardDrilldown({
    data,
    targetTime: target.time,
    granularity: "day",
    models: target.models,
  });

  assert.deepEqual(
    detail.rows.map((item) => item.model),
    ["model-16", "model-17"],
  );
});

test("default dashboard drilldown rejects invalid timestamps", () => {
  for (const createdAt of [Number.NaN, undefined, null, -1, "invalid string"]) {
    assert.throws(
      () =>
        buildDashboardDrilldown({
          data: [
            {
              created_at: createdAt,
              model_name: "gpt-5.4",
              quota: 1,
              count: 1,
              token_used: 1,
            },
          ],
          targetTime: "05-01",
          granularity: "day",
        }),
      /Invalid timestamp/,
    );
  }
});

test("default dashboard drilldown target falls back without collapsed other", () => {
  const target = getDashboardDrilldownTarget({
    datum: {
      Time: "05-01",
      Model: "Other",
    },
    otherLabel: "Other",
  });

  assert.deepEqual(target, {
    time: "05-01",
    models: null,
  });
});

test("default dashboard drilldown target traverses deeply nested arrays", () => {
  const target = getDashboardDrilldownTarget({
    datum: [
      [
        [
          {
            Time: "05-01",
            Model: "Other",
            CollapsedModels: ["model-16"],
          },
        ],
      ],
    ],
    otherLabel: "Other",
  });

  assert.deepEqual(target, {
    time: "05-01",
    models: ["model-16"],
  });
});

test("default dashboard drilldown target returns null without datum time", () => {
  const target = getDashboardDrilldownTarget({
    datum: [[{ Model: "Other", CollapsedModels: ["model-16"] }]],
    otherLabel: "Other",
  });

  assert.equal(target, null);
});

test("default dashboard drilldown preserves unscoped timestamp zero", () => {
  const detail = buildDashboardDrilldown({
    data: [
      {
        created_at: 0,
        model_name: "gpt-5.4",
        quota: 1,
        count: 1,
        token_used: 1,
      },
    ],
    targetTime: "01-01",
    granularity: "day",
  });

  assert.equal(detail.totalQuota, 1);
});

test("default dashboard drilldown keeps empty model filter scoped", () => {
  const detail = buildDashboardDrilldown({
    data: rows,
    targetTime: "05-01",
    granularity: "day",
    models: [],
  });

  assert.equal(detail.totalQuota, 0);
  assert.deepEqual(detail.rows, []);
});

test("default dashboard drilldown treats null model filter as unscoped", () => {
  for (const models of [undefined, null]) {
    const detail = buildDashboardDrilldown({
      data: rows,
      targetTime: "05-01",
      granularity: "day",
      models,
    });

    assert.equal(detail.totalQuota, 200);
    assert.deepEqual(
      detail.rows.map((item) => item.model),
      ["gpt-5.4", "gpt-5.3-codex"],
    );
  }
});

test("default dashboard drilldown rejects null created_at in matching data", () => {
  assert.throws(
    () =>
      buildDashboardDrilldown({
        data: [
          {
            created_at: null,
            model_name: "gpt-5.4",
            quota: 1,
            count: 1,
            token_used: 1,
          },
        ],
        targetTime: "05-01",
        granularity: "day",
      }),
    /Invalid timestamp/,
  );
});

test("default dashboard area click guard ignores chart clicks without target", () => {
  const guard = createDashboardChartAreaClickGuard();

  guard.markChartClickHandled(null);

  assert.equal(guard.shouldHandleAreaClick(), true);
});
