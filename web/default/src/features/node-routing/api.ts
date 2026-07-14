import { api } from '@/lib/api'
import type {
  RoutingNode,
  RoutingNodeInput,
  RoutingNodesResponse,
} from './types'

export async function getRoutingNodes(
  includeDisabled = false
): Promise<RoutingNodesResponse> {
  const res = await api.get('/api/node-routing/nodes', {
    params: includeDisabled ? { all: true } : undefined,
  })
  const result = res.data as RoutingNodesResponse
  if (!result.success) {
    throw new Error(result.message || 'Failed to load routing nodes')
  }
  return result
}

export async function createRoutingNode(
  input: RoutingNodeInput
): Promise<{ success: boolean; message?: string; data?: RoutingNode }> {
  const res = await api.post('/api/node-routing/nodes', input, {
    skipBusinessError: true,
    skipErrorHandler: true,
  } as Record<string, unknown>)
  return res.data
}

export async function updateRoutingNode(
  id: number,
  input: RoutingNodeInput
): Promise<{ success: boolean; message?: string; data?: RoutingNode }> {
  const res = await api.put(`/api/node-routing/nodes/${id}`, input, {
    skipBusinessError: true,
    skipErrorHandler: true,
  } as Record<string, unknown>)
  return res.data
}

export async function deleteRoutingNode(
  id: number
): Promise<{ success: boolean; message?: string }> {
  const res = await api.delete(`/api/node-routing/nodes/${id}`, {
    skipBusinessError: true,
    skipErrorHandler: true,
  } as Record<string, unknown>)
  return res.data
}

export async function reconcileRoutingNodes(): Promise<{
  success: boolean
  message?: string
  data?: { started: boolean }
}> {
  const res = await api.post('/api/node-routing/reconcile', undefined, {
    skipBusinessError: true,
    skipErrorHandler: true,
  } as Record<string, unknown>)
  return res.data
}
