import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { deleteUser } from '../api'
import { ERROR_MESSAGES, isUserDeleted } from '../constants'
import { getUserActionMessage } from '../lib'
import { useUsers } from './users-provider'

export function UsersDeleteDialog() {
  const { t } = useTranslation()
  const { open, setOpen, currentRow, triggerRefresh } = useUsers()
  const [isDeleting, setIsDeleting] = useState(false)

  const handleDelete = async () => {
    if (!currentRow) return

    setIsDeleting(true)
    try {
      const result = await deleteUser(currentRow.id)
      if (result.success) {
        toast.success(t(getUserActionMessage('delete')))
        setOpen(null)
        triggerRefresh()
      } else {
        toast.error(result.message || t(ERROR_MESSAGES.DELETE_FAILED))
      }
    } catch (_error) {
      toast.error(t(ERROR_MESSAGES.UNEXPECTED))
    } finally {
      setIsDeleting(false)
    }
  }

  // 软删行（墓碑）仍占用邮箱 / OAuth 绑定，这里必须明确告诉管理员硬删会释放它们，
  // 否则容易被当成又一次普通注销。
  const isDeleted = currentRow != null && isUserDeleted(currentRow)
  const title = isDeleted
    ? t('Permanently delete this account?')
    : t('Are you sure?')
  const leading = isDeleted
    ? t('This will permanently delete the account record')
    : t('This will permanently delete user')
  const trailing = isDeleted
    ? t(
        '. It also releases the email address and third-party bindings this account still holds. This action cannot be undone.'
      )
    : t('. This action cannot be undone.')

  return (
    <AlertDialog
      open={open === 'delete'}
      onOpenChange={(open) => !open && setOpen(null)}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>
            {leading}{' '}
            <span className='font-semibold'>{currentRow?.username}</span>
            {trailing}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={isDeleting}>
            {t('Cancel')}
          </AlertDialogCancel>
          <AlertDialogAction
            onClick={handleDelete}
            disabled={isDeleting}
            className='bg-destructive text-destructive-foreground hover:bg-destructive/90'
          >
            {isDeleting ? 'Deleting...' : 'Delete'}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
