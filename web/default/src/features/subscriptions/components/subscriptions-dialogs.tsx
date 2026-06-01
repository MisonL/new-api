import { LazyMount } from '@/components/lazy-mount'
import { ToggleStatusDialog } from './dialogs/toggle-status-dialog'
import { SubscriptionsMutateDrawer } from './subscriptions-mutate-drawer'
import { useSubscriptions } from './subscriptions-provider'

export function SubscriptionsDialogs() {
  const { open, setOpen, currentRow } = useSubscriptions()
  const isUpdate = open === 'update'

  return (
    <>
      <LazyMount open={open === 'create' || isUpdate}>
        <SubscriptionsMutateDrawer
          open={open === 'create' || isUpdate}
          onOpenChange={(isOpen) => !isOpen && setOpen(null)}
          currentRow={isUpdate ? currentRow || undefined : undefined}
        />
      </LazyMount>
      <ToggleStatusDialog />
    </>
  )
}
