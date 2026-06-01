import { LazyMount } from '@/components/lazy-mount'
import { ApiKeysDeleteDialog } from './api-keys-delete-dialog'
import { ApiKeysMutateDrawer } from './api-keys-mutate-drawer'
import { useApiKeys } from './api-keys-provider'
import { CCSwitchDialog } from './dialogs/cc-switch-dialog'

export function ApiKeysDialogs() {
  const { open, setOpen, currentRow, resolvedKey } = useApiKeys()

  return (
    <>
      <LazyMount open={open === 'create'}>
        <ApiKeysMutateDrawer
          open={open === 'create'}
          onOpenChange={(isOpen) => !isOpen && setOpen(null)}
          currentRow={undefined}
          side='left'
        />
      </LazyMount>
      <LazyMount open={open === 'update'}>
        <ApiKeysMutateDrawer
          open={open === 'update'}
          onOpenChange={(isOpen) => !isOpen && setOpen(null)}
          currentRow={currentRow || undefined}
          side='right'
        />
      </LazyMount>
      <LazyMount open={open === 'delete'}>
        <ApiKeysDeleteDialog />
      </LazyMount>
      <LazyMount open={open === 'cc-switch'}>
        <CCSwitchDialog
          open={open === 'cc-switch'}
          onOpenChange={(isOpen) => !isOpen && setOpen(null)}
          tokenKey={resolvedKey}
        />
      </LazyMount>
    </>
  )
}
