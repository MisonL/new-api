import type {
  LegacyColumnDef,
  LegacyRow,
  LegacyCell,
  LegacyColumn,
  LegacyReactTable,
} from '@tanstack/react-table/legacy'
import type { RowData } from '@tanstack/table-core'

/**
 * TanStack Table v8 兼容桥。
 *
 * v9 重写了 API：`useReactTable` 更名为 `useTable`（配 `features` 槽位），
 * 行模型工厂从 `get*RowModel()` 改为 `create*RowModel()`，状态类型从
 * `@tanstack/react-table` 迁到 `@tanstack/table-core` 并改名
 * （`VisibilityState` -> `ColumnVisibilityState`）。
 *
 * v9 官方保留了 `@tanstack/react-table/legacy` 入口，提供 v8 同名 API 与
 * `LegacyFeatures` 特征集。这里把该入口按 v8 的名字重新导出，使既有调用点
 * 保持原样；迁移到 v9 原生 API 是后续独立任务，不与本次依赖升级混做。
 */
export {
  useLegacyTable as useReactTable,
  getCoreRowModel,
  getFilteredRowModel,
  getSortedRowModel,
  getPaginationRowModel,
  getExpandedRowModel,
  getGroupedRowModel,
  getFacetedRowModel,
  getFacetedMinMaxValues,
  getFacetedUniqueValues,
  legacyCreateColumnHelper as createColumnHelper,
} from '@tanstack/react-table/legacy'

export { flexRender } from '@tanstack/react-table'

export type {
  SortingState,
  PaginationState,
  RowSelectionState,
  ExpandedState,
  ColumnFiltersState,
  ColumnVisibilityState as VisibilityState,
  OnChangeFn,
} from '@tanstack/table-core'

/**
 * v8 的泛型参数没有行数据类型约束，调用方常直接传 interface；v9 要求
 * `TData extends RowData`，而 TypeScript 的 interface 不带隐式索引签名，
 * 直接约束会让所有以 interface 为行类型的调用点报 TS2344。
 *
 * 这里用不分配的条件类型（`[T] extends [RowData]`）把不满足约束的类型补成
 * `T & RowData`：联合类型不会被展开，行对象上的自有属性仍然保留类型。
 */
type RowDataCompat<T> = [T] extends [RowData] ? T : T & RowData

export type ColumnDef<TData = unknown, TValue = unknown> = LegacyColumnDef<
  RowDataCompat<TData>,
  TValue
>
export type Table<TData = unknown> = LegacyReactTable<RowDataCompat<TData>>
export type Row<TData = unknown> = LegacyRow<RowDataCompat<TData>>
export type Cell<TData = unknown, TValue = unknown> = LegacyCell<
  RowDataCompat<TData>,
  TValue
>
export type Column<TData = unknown, TValue = unknown> = LegacyColumn<
  RowDataCompat<TData>,
  TValue
>
