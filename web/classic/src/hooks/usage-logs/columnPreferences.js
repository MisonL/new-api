/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

export function normalizeColumnOrder(savedOrder, defaultOrder) {
  if (!Array.isArray(defaultOrder) || defaultOrder.length === 0) {
    return [];
  }

  if (!Array.isArray(savedOrder) || savedOrder.length === 0) {
    return [...defaultOrder];
  }

  const defaultKeys = new Set(defaultOrder);
  const ordered = [];
  const orderedKeys = new Set();

  savedOrder.forEach((key) => {
    if (defaultKeys.has(key) && !orderedKeys.has(key)) {
      ordered.push(key);
      orderedKeys.add(key);
    }
  });

  defaultOrder.forEach((key) => {
    if (!orderedKeys.has(key)) {
      ordered.push(key);
      orderedKeys.add(key);
    }
  });

  return ordered;
}

export function moveColumnKey(columnOrder, columnKey, direction, movableKeys) {
  if (!Array.isArray(columnOrder) || !columnOrder.includes(columnKey)) {
    return Array.isArray(columnOrder) ? [...columnOrder] : [];
  }

  const movableKeySet = new Set(movableKeys || columnOrder);
  if (!movableKeySet.has(columnKey)) {
    return [...columnOrder];
  }

  const visibleOrder = columnOrder.filter((key) => movableKeySet.has(key));
  const visibleIndex = visibleOrder.indexOf(columnKey);
  const offset = direction === 'up' ? -1 : direction === 'down' ? 1 : 0;
  const targetVisibleIndex = visibleIndex + offset;

  if (
    offset === 0 ||
    targetVisibleIndex < 0 ||
    targetVisibleIndex >= visibleOrder.length
  ) {
    return [...columnOrder];
  }

  const targetKey = visibleOrder[targetVisibleIndex];
  const nextOrder = [...columnOrder];
  const sourceIndex = nextOrder.indexOf(columnKey);
  const targetIndex = nextOrder.indexOf(targetKey);
  nextOrder[sourceIndex] = targetKey;
  nextOrder[targetIndex] = columnKey;

  return nextOrder;
}

function getFixedEdge(column) {
  if (column?.fixed === 'right') {
    return 'right';
  }
  if (column?.fixed === true || column?.fixed === 'left') {
    return 'left';
  }
  return 'none';
}

export function getMovableColumnKeys(columns) {
  if (!Array.isArray(columns)) {
    return [];
  }
  return columns
    .filter((column) => column?.key && getFixedEdge(column) === 'none')
    .map((column) => column.key);
}

function keepFixedColumnsAtEdges(columns) {
  const leftFixedColumns = [];
  const normalColumns = [];
  const rightFixedColumns = [];

  columns.forEach((column) => {
    const fixedEdge = getFixedEdge(column);
    if (fixedEdge === 'left') {
      leftFixedColumns.push(column);
      return;
    }
    if (fixedEdge === 'right') {
      rightFixedColumns.push(column);
      return;
    }
    normalColumns.push(column);
  });

  return [...leftFixedColumns, ...normalColumns, ...rightFixedColumns];
}

export function applyColumnOrder(columns, visibleColumns, columnOrder) {
  const columnMap = new Map();
  columns.forEach((column) => {
    if (column?.key) {
      columnMap.set(column.key, column);
    }
  });

  const defaultOrder = columns.map((column) => column.key).filter(Boolean);
  const orderedKeys = normalizeColumnOrder(columnOrder, defaultOrder);

  const orderedColumns = orderedKeys
    .map((key) => columnMap.get(key))
    .filter((column) => {
      if (!column) {
        return false;
      }
      if (!visibleColumns) {
        return true;
      }
      return Boolean(visibleColumns[column.key]);
    });

  return keepFixedColumnsAtEdges(orderedColumns);
}
