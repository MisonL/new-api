import { useState } from 'react'
import { getRouteApi } from '@tanstack/react-router'
import { Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { useIsAdmin } from '@/hooks/use-admin'
import { buildApiParams } from '../lib/utils'
import type { LogCategory } from '../types'
import { LogDeleteControls } from './log-delete-controls'
import { UsageLogsFilterDialog } from './dialogs/usage-logs-filter-dialog'

const route = getRouteApi('/_authenticated/usage-logs/$section')

interface UsageLogsPrimaryButtonsProps {
  logCategory: LogCategory
}

export function UsageLogsPrimaryButtons({
  logCategory,
}: UsageLogsPrimaryButtonsProps) {
  const { t } = useTranslation()
  const isAdmin = useIsAdmin()
  const searchParams = route.useSearch()
  const [filterDialogOpen, setFilterDialogOpen] = useState(false)

  // 删除范围必须与列表当前筛选一致，所以复用列表构造查询参数的同一套函数，
  // 只保留 batch_delete 接受的字段（分页参数与删除无关，不发送）。
  const params = buildApiParams({
    page: 1,
    pageSize: 1,
    searchParams,
    isAdmin,
  })

  return (
    <div className='flex gap-2'>
      <Button size='sm' onClick={() => setFilterDialogOpen(true)}>
        <Search className='h-4 w-4' />
        {t('Search')}
      </Button>

      {/* 按条件删除只对通用日志开放：绘图 / 任务日志走各自的重试与清理接口，
          没有对应的批量删除端点。 */}
      {logCategory === 'common' && (
        <LogDeleteControls
          payload={{
            type: params.type,
            start_timestamp: params.start_timestamp,
            end_timestamp: params.end_timestamp,
            username: params.username,
            token_name: params.token_name,
            model_name: params.model_name,
            channel: params.channel,
            group: params.group,
            request_id: params.request_id,
          }}
          isAdmin={isAdmin}
        />
      )}

      <UsageLogsFilterDialog
        open={filterDialogOpen}
        onOpenChange={setFilterDialogOpen}
        logCategory={logCategory}
      />
    </div>
  )
}