import { useState } from 'react'
import { Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { type Table } from '@/lib/table-compat'
import { Button } from '@/components/ui/button'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTableBulkActions as BulkActionsToolbar } from '@/components/data-table'
import { deleteDeployment } from '../api'
import type { Deployment } from '../types'

interface DeploymentsBulkActionsProps {
  table: Table<Deployment>
  onDeleted: () => void
}

export function DeploymentsBulkActions(props: DeploymentsBulkActionsProps) {
  const { t } = useTranslation()
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [isDeleting, setIsDeleting] = useState(false)
  const selectedRows = props.table.getFilteredSelectedRowModel().rows
  const selectedDeployments = selectedRows.map((row) => row.original)

  const handleDelete = async () => {
    if (selectedDeployments.length === 0) return
    setIsDeleting(true)
    const results = await Promise.allSettled(
      selectedDeployments.map((deployment) => deleteDeployment(deployment.id))
    )
    const successCount = results.filter(
      (result) => result.status === 'fulfilled' && result.value.success
    ).length
    const failureCount = results.length - successCount

    if (successCount > 0) {
      toast.success(
        t('{{count}} deployment(s) deleted', { count: successCount })
      )
      props.table.resetRowSelection()
      props.onDeleted()
    }
    if (failureCount > 0) {
      toast.error(
        t('{{count}} deployment(s) failed to delete', { count: failureCount })
      )
    }

    setIsDeleting(false)
    setConfirmOpen(false)
  }

  return (
    <>
      <BulkActionsToolbar table={props.table} entityName='deployment'>
        <Button
          type='button'
          variant='destructive'
          size='icon'
          className='size-8'
          aria-label={t('Delete selected deployments')}
          title={t('Delete selected deployments')}
          onClick={() => setConfirmOpen(true)}
        >
          <Trash2 />
          <span className='sr-only'>{t('Delete selected deployments')}</span>
        </Button>
      </BulkActionsToolbar>

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Delete selected deployments?')}
        desc={t(
          'Are you sure you want to delete {{count}} selected deployment(s)? This action cannot be undone.',
          { count: selectedDeployments.length }
        )}
        confirmText={t('Delete')}
        destructive
        isLoading={isDeleting}
        handleConfirm={handleDelete}
      />
    </>
  )
}
