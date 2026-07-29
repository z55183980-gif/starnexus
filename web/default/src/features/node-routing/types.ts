export interface RoutingNode {
  id: number
  key: string
  name: string
  origin: string
  type: 'application' | 'database'
  enabled: boolean
  sort: number
  monitor_enabled: boolean
  monitor_configured: boolean
  monitor_status?: RoutingNodeMonitorStatus
  binding_count: number
  created_at: number
  updated_at: number
}

export interface RoutingNodeInput {
  key: string
  name: string
  origin: string
  type: 'application' | 'database'
  enabled: boolean
  sort: number
  monitor_enabled: boolean
}

export interface RoutingNodeMonitorStatus {
  node_id: number
  node_name: string
  cpu_usage: number
  cpu_cores: number
  load_one: number
  load_percent: number
  memory_used: number
  memory_total: number
  memory_percent: number
  disk_used: number
  disk_total: number
  disk_percent: number
  network_bytes_sent: number
  network_bytes_received: number
  network_upload_bps: number
  network_download_bps: number
  network_samples?: RoutingNodeMonitorNetworkSample[]
  uptime_seconds: number
  app_version: string
  reported_at: number
  updated_at: number
}

export interface RoutingNodeMonitorNetworkSample {
  upload_bps: number
  download_bps: number
  reported_at: number
}

export interface RoutingNodeMonitorEnrollment {
  token: string
  node_key: string
  node_name: string
  report_path: string
}

export interface RoutingNodeMonitorSharedEnrollment {
  token: string
  enroll_path: string
  report_path: string
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
