const LEGACY_SETTING_TABS: Record<string, string> = {
  operation: '/system-settings/general/behavior',
  dashboard: '/system-settings/content/dashboard',
  chats: '/system-settings/content/chat',
  drawing: '/system-settings/content/drawing',
  payment: '/system-settings/integrations/payment',
  ratio: '/system-settings/models/ratio',
  ratelimit: '/system-settings/request-limits/rate-limit',
  models: '/system-settings/models/global',
  'model-deployment': '/models/deployments',
  performance: '/system-settings/maintenance/performance',
  system: '/system-settings/auth/basic-auth',
  other: '/system-settings/general/system-info',
}

function rewriteLegacySettingPath(url: URL): boolean {
  const tab = url.searchParams.get('tab')
  const target =
    (tab && LEGACY_SETTING_TABS[tab]) || '/system-settings/general/behavior'
  url.pathname = target
  url.searchParams.delete('tab')
  return true
}

/**
 * Convert Classic frontend URLs to their Default frontend equivalents.
 * The rewrite runs only while parsing an incoming browser URL; links emitted
 * by Default continue to use the canonical paths.
 */
export function rewriteLegacyPath(url: URL): URL {
  const { pathname } = url

  if (pathname === '/console' || pathname === '/console/') {
    url.pathname = '/dashboard'
    return url
  }

  const staticPaths: Record<string, string> = {
    '/console/models': '/models/metadata',
    '/console/deployment': '/models/deployments',
    '/console/subscription': '/subscriptions',
    '/console/channel': '/channels',
    '/console/token': '/keys',
    '/console/playground': '/playground',
    '/console/redemption': '/redemption-codes',
    '/console/user': '/users',
    '/console/personal': '/profile',
    '/console/topup': '/wallet',
    '/console/log': '/usage-logs/common',
    '/console/midjourney': '/usage-logs/drawing',
    '/console/task': '/usage-logs/task',
    '/login': '/sign-in',
    '/register': '/sign-up',
    '/forbidden': '/403',
  }

  if (pathname === '/console/setting' || pathname === '/console/setting/') {
    rewriteLegacySettingPath(url)
    return url
  }

  const chatMatch = pathname.match(/^\/console\/chat(?:\/(\d+))?\/?$/)
  if (chatMatch) {
    url.pathname = `/chat/${chatMatch[1] ?? '0'}`
    return url
  }

  const target = staticPaths[pathname.replace(/\/$/, '')]
  if (target) {
    url.pathname = target
  }

  return url
}
