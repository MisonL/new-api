/**
 * Column definitions factory
 */
import type { ColumnDef } from '@/lib/table-compat'
import { useCommonLogsColumns } from '../components/columns/common-logs-columns'
import { useDrawingLogsColumns } from '../components/columns/drawing-logs-columns'
import { useTaskLogsColumns } from '../components/columns/task-logs-columns'
import type { LogCategory } from '../types'

/**
 * Get column definitions based on log category.
 *
 * The three log categories carry different row shapes (UsageLog,
 * MidjourneyLog, TaskLog) but the caller renders them through one table, so the
 * column set is widened to a generic record. The casts below are the type-level
 * half of that widening; the runtime shape is unchanged.
 */
export function useColumnsByCategory(
  logCategory: LogCategory,
  isAdmin: boolean
): ColumnDef<Record<string, unknown>>[] {
  const commonColumns = useCommonLogsColumns(isAdmin)
  const drawingColumns = useDrawingLogsColumns(isAdmin)
  const taskColumns = useTaskLogsColumns(isAdmin)

  switch (logCategory) {
    case 'common':
      return commonColumns as ColumnDef<Record<string, unknown>>[]
    case 'drawing':
      return drawingColumns as ColumnDef<Record<string, unknown>>[]
    case 'task':
      return taskColumns as ColumnDef<Record<string, unknown>>[]
    default:
      return commonColumns as ColumnDef<Record<string, unknown>>[]
  }
}
