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

export interface RoutingNodeBoundUser {
  user_id: number
  username: string
  display_name: string
}

export interface RoutingNodeBoundUsersResponse {
  success: boolean
  message?: string
  data?: {
    items: RoutingNodeBoundUser[]
    total: number
    page: number
    page_size: number
  }
}
