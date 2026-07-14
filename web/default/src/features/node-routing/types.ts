export interface RoutingNode {
  id: number
  key: string
  name: string
  origin: string
  enabled: boolean
  sort: number
  binding_count: number
  created_at: number
  updated_at: number
}

export interface RoutingNodeInput {
  key: string
  name: string
  origin: string
  enabled: boolean
  sort: number
}

export interface RoutingNodesResponse {
  success: boolean
  message?: string
  data?: RoutingNode[]
}
