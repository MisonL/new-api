import * as React from 'react'
import { useNavigate, useLocation } from '@tanstack/react-router'
import { ChevronsUpDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { useStatus } from '@/hooks/use-status'
import { useSystemConfig } from '@/hooks/use-system-config'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from '@/components/ui/sidebar'
import { useWorkspace } from '../context/workspace-context'
import { getWorkspaceByPath, WORKSPACE_IDS } from '../lib/workspace-registry'
import { type Workspace } from '../types'

type SystemBrandProps = {
  workspaces: Workspace[]
  defaultName?: string
  defaultVersion?: string
}

/**
 * System brand component
 * Shows the current system identity and keeps system settings navigation available
 * for super administrators.
 */
export function SystemBrand({
  workspaces,
  defaultName = 'New API',
  defaultVersion,
}: SystemBrandProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const { isMobile } = useSidebar()
  const { status } = useStatus()
  const { logo } = useSystemConfig()
  const isSuperAdmin = useAuthStore(
    (state) => state.auth.user?.role === ROLE.SUPER_ADMIN
  )
  const { activeWorkspace, setActiveWorkspace } = useWorkspace()

  // Build brand targets from the existing workspace registry and permissions.
  const availableWorkspaces = React.useMemo(
    () =>
      workspaces
        .map((workspace, index) =>
          index === 0
            ? {
                ...workspace,
                name: status?.system_name || defaultName,
                plan: status?.version || defaultVersion || t('Unknown version'),
              }
            : workspace
        )
        .filter(
          (workspace) =>
            isSuperAdmin || workspace.id !== WORKSPACE_IDS.SYSTEM_SETTINGS
        ),
    [
      workspaces,
      status?.system_name,
      status?.version,
      defaultName,
      defaultVersion,
      isSuperAdmin,
      t,
    ]
  )

  // Keep the active brand synchronized with the current route.
  React.useEffect(() => {
    const detectedWorkspace = getWorkspaceByPath(pathname)

    if (detectedWorkspace.id === WORKSPACE_IDS.SYSTEM_SETTINGS) {
      const systemSettingsWorkspace = availableWorkspaces.find(
        (w) => w.id === WORKSPACE_IDS.SYSTEM_SETTINGS
      )
      if (systemSettingsWorkspace) {
        setActiveWorkspace(systemSettingsWorkspace)
      }
    } else {
      const mainWorkspace =
        availableWorkspaces.find((w) => w.id === WORKSPACE_IDS.DEFAULT) ||
        availableWorkspaces[0]
      if (mainWorkspace) {
        setActiveWorkspace(mainWorkspace)
      }
    }
  }, [pathname, availableWorkspaces, setActiveWorkspace])

  const handleWorkspaceChange = (workspace: Workspace) => {
    if (workspace.id === WORKSPACE_IDS.SYSTEM_SETTINGS) {
      navigate({ to: '/system-settings/general' })
    } else {
      navigate({ to: '/dashboard' })
    }
  }

  if (!activeWorkspace) {
    return null
  }

  const canSwitchWorkspace = availableWorkspaces.length > 1
  const brandButtonContent = (
    <>
      {activeWorkspace.id === WORKSPACE_IDS.SYSTEM_SETTINGS ? (
        <div className='bg-sidebar-primary text-sidebar-primary-foreground flex aspect-square size-8 items-center justify-center rounded-lg'>
          <activeWorkspace.logo className='size-4' />
        </div>
      ) : (
        <div className='flex aspect-square size-8 items-center justify-center overflow-hidden rounded-lg'>
          <img
            src={logo}
            alt={t('Logo')}
            className='size-full rounded-lg object-cover'
          />
        </div>
      )}
      <div className='grid flex-1 text-start text-sm leading-tight group-data-[collapsible=icon]:hidden'>
        <span className='truncate font-semibold'>{activeWorkspace.name}</span>
        <span className='truncate text-xs'>{activeWorkspace.plan}</span>
      </div>
      {canSwitchWorkspace && (
        <ChevronsUpDown className='ms-auto group-data-[collapsible=icon]:hidden' />
      )}
    </>
  )

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        {canSwitchWorkspace ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <SidebarMenuButton
                size='lg'
                className='data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground'
              >
                {brandButtonContent}
              </SidebarMenuButton>
            </DropdownMenuTrigger>
            <DropdownMenuContent
              className='w-(--radix-dropdown-menu-trigger-width) min-w-56 rounded-lg'
              align='start'
              side={isMobile ? 'bottom' : 'right'}
              sideOffset={4}
            >
              <DropdownMenuLabel className='text-muted-foreground text-xs'>
                {t('System')}
              </DropdownMenuLabel>
              {availableWorkspaces.map((workspace, index) => (
                <DropdownMenuItem
                  key={workspace.id}
                  onClick={() => handleWorkspaceChange(workspace)}
                  className='gap-2 p-2'
                >
                  {index === 0 ? (
                    <div className='flex size-6 items-center justify-center overflow-hidden rounded-sm border'>
                      <img
                        src={logo}
                        alt='Logo'
                        className='size-full object-cover'
                      />
                    </div>
                  ) : (
                    <div className='flex size-6 items-center justify-center rounded-sm border'>
                      <workspace.logo className='size-4 shrink-0' />
                    </div>
                  )}
                  {workspace.name}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        ) : (
          <SidebarMenuButton
            asChild
            size='lg'
            className='hover:text-sidebar-foreground active:text-sidebar-foreground cursor-default hover:bg-transparent active:bg-transparent'
          >
            <div>{brandButtonContent}</div>
          </SidebarMenuButton>
        )}
      </SidebarMenuItem>
    </SidebarMenu>
  )
}
