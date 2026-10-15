import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { deleteLogsByFilter, type DeleteLogsFilterPayload } from '../api'

interface LogDeleteControlsProps {
  payload: DeleteLogsFilterPayload
  isAdmin: boolean
  onDeleted?: () => void
}

export function LogDeleteControls(props: LogDeleteControlsProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [isLoading, setIsLoading] = useState(false)

  const handleConfirm = async () => {
    setIsLoading(true)
    try {
      const response = await deleteLogsByFilter(props.payload, props.isAdmin)
      if (!response.success) {
        toast.error(response.message || t('Failed to delete logs'))
        return
      }

      const deleted = response.data?.deleted
      toast.success(
        deleted == null
          ? t('Logs deleted')
          : t('{{count}} logs deleted', { count: deleted })
      )
      setOpen(false)
      props.onDeleted?.()
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['logs'] }),
        queryClient.invalidateQueries({ queryKey: ['usage-logs-stats'] }),
      ])
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('Failed to delete logs')
      )
    } finally {
      setIsLoading(false)
    }
  }

  return (
    <>
      <Button
        variant='outline'
        size='sm'
        className='text-destructive hover:text-destructive h-8'
        onClick={() => setOpen(true)}
        disabled={isLoading}
      >
        <Trash2 className='size-3.5' />
        {t('Delete filtered logs')}
      </Button>
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        title={t('Delete filtered logs?')}
        desc={t(
          'This permanently deletes all logs matching the current filters. This action cannot be undone.'
        )}
        confirmText={t('Delete')}
        destructive
        handleConfirm={handleConfirm}
        isLoading={isLoading}
      />
    </>
  )
}
