import type { AddChannelRequest } from '@/features/channels/types'
import type { Deployment } from '../types'

type DeploymentContainer = Record<string, unknown>

/** Normalize an upstream container URL before storing it as a channel base URL. */
export function normalizeDeploymentPublicUrl(
  container: DeploymentContainer
): string | null {
  const raw =
    typeof container.public_url === 'string' ? container.public_url.trim() : ''
  if (!raw) return null

  try {
    const url = new URL(raw)
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null
    if (!url.hostname || url.username || url.password) return null
    url.hash = ''
    url.search = ''
    return url.toString().replace(/\/$/, '')
  } catch {
    return null
  }
}

function getDeploymentName(deployment: Deployment): string {
  const rawName =
    deployment.container_name ||
    deployment.deployment_name ||
    deployment.name ||
    deployment.id
  return (
    String(rawName || 'ionet')
      .trim()
      .slice(0, 60) || 'ionet'
  )
}

/** Build the IO.NET channel payload used by the deployment console. */
export function buildDeploymentChannelPayload(
  deployment: Deployment,
  container: DeploymentContainer,
  key: string
): AddChannelRequest | null {
  const baseUrl = normalizeDeploymentPublicUrl(container)
  if (!baseUrl || !key.trim()) return null

  const deploymentName = getDeploymentName(deployment)
  const deploymentId = String(deployment.id)
  const containerId =
    typeof container.container_id === 'string' ? container.container_id : null

  return {
    mode: 'single',
    channel: {
      name: `[IO.NET] ${deploymentName}`,
      type: 4,
      key: key.trim(),
      base_url: baseUrl,
      group: 'default',
      tag: 'ionet',
      remark: `[IO.NET] Auto-synced from deployment ${deploymentId}`,
      other_info: JSON.stringify({
        source: 'ionet',
        deployment_id: deployment.id,
        deployment_name: deploymentName,
        container_id: containerId,
        public_url: baseUrl,
      }),
    },
  }
}
