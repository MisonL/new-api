import * as z from 'zod'

export const perfSchema = z.object({
  'performance_setting.disk_cache_enabled': z.boolean(),
  'performance_setting.disk_cache_threshold_mb': z.coerce.number().min(1),
  'performance_setting.disk_cache_max_size_mb': z.coerce.number().min(100),
  'performance_setting.disk_cache_path': z.string().optional(),
  'performance_setting.monitor_enabled': z.boolean(),
  'performance_setting.monitor_cpu_threshold': z.coerce.number().min(0),
  'performance_setting.monitor_memory_threshold': z.coerce
    .number()
    .min(0)
    .max(100),
  'performance_setting.monitor_disk_threshold': z.coerce
    .number()
    .min(0)
    .max(100),
  'perf_metrics_setting.enabled': z.boolean(),
  'perf_metrics_setting.flush_interval': z.coerce.number().min(1),
  'perf_metrics_setting.bucket_time': z.enum(['minute', '5min', 'hour']),
  'perf_metrics_setting.retention_days': z.coerce.number().min(0),
})

export type PerfFormValues = z.infer<typeof perfSchema>

export type LogInfo = {
  enabled: boolean
  log_dir: string
  file_count: number
  total_size: number
  oldest_time?: string
  newest_time?: string
}

export type PerformanceStats = {
  cache_stats?: {
    current_disk_usage_bytes: number
    disk_cache_max_bytes: number
    active_disk_files: number
    disk_cache_hits: number
    current_memory_usage_bytes: number
    active_memory_buffers: number
    memory_cache_hits: number
  }
  disk_space_info?: {
    total: number
    free: number
    used: number
    used_percent: number
  }
  memory_stats?: {
    alloc: number
    total_alloc: number
    sys: number
    num_gc: number
    num_goroutine: number
  }
  disk_cache_info?: {
    path: string
    file_count: number
    total_size: number
  }
  config?: {
    is_running_in_container: boolean
  }
}

export function formatBytes(bytes: number, decimals = 2): string {
  if (!bytes || isNaN(bytes)) return '0 Bytes'
  if (bytes === 0) return '0 Bytes'
  if (bytes < 0) return '-' + formatBytes(-bytes, decimals)
  const k = 1024
  const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(Math.abs(bytes)) / Math.log(k))
  if (i < 0 || i >= sizes.length) return bytes + ' Bytes'
  return parseFloat((bytes / Math.pow(k, i)).toFixed(decimals)) + ' ' + sizes[i]
}
