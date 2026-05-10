import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const classicRoot = resolve(import.meta.dirname, '../classic/src');

const readClassicSource = (relativePath) =>
  readFileSync(resolve(classicRoot, relativePath), 'utf8');

const assertNoStaticVisactorImport = (relativePath) => {
  const source = readClassicSource(relativePath);
  assert.doesNotMatch(
    source,
    /from\s+['"]@visactor\//,
    `${relativePath} must not statically import VisActor packages`,
  );
};

test('dashboard shell keeps VisActor out of eagerly loaded modules', () => {
  [
    'components/dashboard/StatsCards.jsx',
    'components/dashboard/ChartsPanel.jsx',
    'components/dashboard/DashboardDrilldownModal.jsx',
    'hooks/dashboard/useDashboardCharts.jsx',
  ].forEach(assertNoStaticVisactorImport);
});

test('dashboard chart hook does not trigger the chart runtime directly', () => {
  const source = readClassicSource('hooks/dashboard/useDashboardCharts.jsx');
  assert.doesNotMatch(source, /import\(['"]@visactor\//);
});

test('dashboard chart engine loads through the deferred chart boundary', () => {
  const source = readClassicSource('components/dashboard/LazyVChart.jsx');
  assert.match(source, /import\(['"]@visactor\/react-vchart['"]\)/);
  assert.match(source, /import\(['"]@visactor\/vchart-semi-theme['"]\)/);
  assert.match(source, /requestIdleCallback|setTimeout/);
});
