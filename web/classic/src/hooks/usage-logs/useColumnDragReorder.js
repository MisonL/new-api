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

import React from 'react';

const EMPTY_DRAG_STATE = {
  sourceKey: '',
  targetKey: '',
  position: 'before',
};

export function getNextPointerDragState(currentState, sourceKey, target) {
  const nextState = target
    ? { sourceKey, ...target }
    : { ...EMPTY_DRAG_STATE, sourceKey };

  if (
    currentState.sourceKey === nextState.sourceKey &&
    currentState.targetKey === nextState.targetKey &&
    currentState.position === nextState.position
  ) {
    return null;
  }

  return nextState;
}

function getMovableRows(list, sourceKey, movableColumnKeySet) {
  if (!list) {
    return [];
  }

  return Array.from(list.querySelectorAll('[data-column-key]'))
    .map((row) => ({
      key: row.getAttribute('data-column-key'),
      rect: row.getBoundingClientRect(),
    }))
    .filter(
      ({ key }) => key && key !== sourceKey && movableColumnKeySet.has(key),
    );
}

function getDropTargetFromRows(rows, clientY) {
  if (rows.length === 0) {
    return null;
  }

  const firstRow = rows[0];
  const lastRow = rows[rows.length - 1];
  if (clientY < firstRow.rect.top) {
    return { targetKey: firstRow.key, position: 'before' };
  }
  if (clientY > lastRow.rect.bottom) {
    return { targetKey: lastRow.key, position: 'after' };
  }

  const matchedRow = rows.find(
    ({ rect }) => clientY >= rect.top && clientY <= rect.bottom,
  );
  if (!matchedRow) {
    return null;
  }

  const position =
    clientY - matchedRow.rect.top > matchedRow.rect.height / 2
      ? 'after'
      : 'before';
  return { targetKey: matchedRow.key, position };
}

function scrollListNearPointer(list, clientY) {
  if (!list) {
    return;
  }

  const rect = list.getBoundingClientRect();
  if (clientY < rect.top + 32) {
    list.scrollTop -= 16;
  } else if (clientY > rect.bottom - 32) {
    list.scrollTop += 16;
  }
}

export function useColumnDragReorder(movableColumnKeys, onReorder) {
  const orderListRef = React.useRef(null);
  const dragStateRef = React.useRef(EMPTY_DRAG_STATE);
  const pointerDragRef = React.useRef({
    active: false,
    pointerId: null,
    sourceKey: '',
  });
  const [dragState, setDragState] = React.useState(EMPTY_DRAG_STATE);
  const movableColumnKeySet = React.useMemo(
    () => new Set(movableColumnKeys),
    [movableColumnKeys],
  );

  const updateDragState = React.useCallback((nextState) => {
    dragStateRef.current = nextState;
    setDragState(nextState);
  }, []);

  const resetDragState = React.useCallback(() => {
    pointerDragRef.current = { active: false, pointerId: null, sourceKey: '' };
    updateDragState(EMPTY_DRAG_STATE);
  }, [updateDragState]);

  const updatePointerDropTarget = React.useCallback(
    (clientY, sourceKey) => {
      const list = orderListRef.current;
      scrollListNearPointer(list, clientY);
      const rows = getMovableRows(list, sourceKey, movableColumnKeySet);
      const target = getDropTargetFromRows(rows, clientY);
      const nextState = getNextPointerDragState(
        dragStateRef.current,
        sourceKey,
        target,
      );
      if (!nextState) {
        return;
      }
      updateDragState(nextState);
    },
    [movableColumnKeySet, updateDragState],
  );

  const handleDragPointerDown = React.useCallback(
    (event, columnKey, isMovable) => {
      if (!isMovable || movableColumnKeys.length < 2) {
        return;
      }

      event.preventDefault();
      if (event.isTrusted && Number.isFinite(event.pointerId)) {
        event.currentTarget.setPointerCapture?.(event.pointerId);
      }
      pointerDragRef.current = {
        active: true,
        pointerId: event.pointerId,
        sourceKey: columnKey,
      };
      updateDragState({ ...EMPTY_DRAG_STATE, sourceKey: columnKey });
    },
    [movableColumnKeys.length, updateDragState],
  );

  const handleDragPointerMove = React.useCallback(
    (event) => {
      const pointerDrag = pointerDragRef.current;
      if (!pointerDrag.active || pointerDrag.pointerId !== event.pointerId) {
        return;
      }

      event.preventDefault();
      updatePointerDropTarget(event.clientY, pointerDrag.sourceKey);
    },
    [updatePointerDropTarget],
  );

  const finishPointerDrag = React.useCallback(
    (event) => {
      const pointerDrag = pointerDragRef.current;
      if (!pointerDrag.active || pointerDrag.pointerId !== event.pointerId) {
        return;
      }

      const current = dragStateRef.current;
      if (current.targetKey) {
        onReorder(
          pointerDrag.sourceKey,
          current.targetKey,
          current.position,
          movableColumnKeys,
        );
      }
      resetDragState();
    },
    [movableColumnKeys, onReorder, resetDragState],
  );

  return {
    dragState,
    finishPointerDrag,
    handleDragPointerDown,
    handleDragPointerMove,
    movableColumnKeySet,
    orderListRef,
    resetDragState,
  };
}
