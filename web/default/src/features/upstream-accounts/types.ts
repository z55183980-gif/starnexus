/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/

export type UpstreamStatus = 'active' | 'inactive' | 'error'
export type UpstreamAccountType = 'oauth' | 'apikey'
export type UpstreamOAuthRefreshOwner = 'external' | 'starnexus'
export type UpstreamProxyProtocol = 'http' | 'https' | 'socks5' | 'socks5h'
export type UpstreamProxyFallback = 'none' | 'direct' | 'proxy'

export interface UpstreamAccountPool {
  id: number
  name: string
  description: string
  platform: 'openai'
  credential_type: UpstreamAccountType | 'mixed'
  status: Exclude<UpstreamStatus, 'error'>
  default_proxy_id?: number | null
  scheduler_config: string
  account_count: number
  active_count: number
  channel_count: number
  attempt_count_24h: number
  success_count_24h: number
  error_count_24h: number
  last_event_at?: number | null
  created_at: number
  updated_at: number
}

export interface UpstreamAccountPoolMember {
  pool_id: number
  account_id: number
  priority: number
  weight: number
  created_at: number
  name: string
  platform: 'openai'
  type: UpstreamAccountType
  status: UpstreamStatus
  schedulable: boolean
}

export interface UpstreamAccount {
  id: number
  name: string
  notes?: string | null
  platform: 'openai'
  type: UpstreamAccountType
  credential_configured: boolean
  credential_version: number
  extra: string
  proxy_id?: number | null
  concurrency: number
  priority: number
  weight: number
  load_factor?: number | null
  rate_multiplier?: number | null
  status: UpstreamStatus
  schedulable: boolean
  error_message: string
  last_used_at?: number | null
  rate_limit_reset_at?: number | null
  temp_unschedulable_until?: number | null
  expires_at?: number | null
  auto_pause_on_expired: boolean
  oauth_refresh_owner: UpstreamOAuthRefreshOwner
  pool_ids: number[]
  created_at: number
  updated_at: number
}

export interface UpstreamProxy {
  id: number
  name: string
  protocol: UpstreamProxyProtocol
  host: string
  port: number
  auth_configured: boolean
  status: Exclude<UpstreamStatus, 'error'>
  expires_at?: number | null
  fallback_mode: UpstreamProxyFallback
  backup_proxy_id?: number | null
  expiry_warn_days: number
  last_test_at?: number | null
  latency_ms?: number | null
  latency_status: string
  latency_message: string
  observed_ip: string
  observed_country: string
  observed_region: string
  observed_city: string
  account_count: number
  pool_count: number
  backup_count: number
  created_at: number
  updated_at: number
}

export interface UpstreamAccountPayload {
  name: string
  notes?: string
  platform: 'openai'
  type: UpstreamAccountType
  credentials?: Record<string, string>
  extra: string
  proxy_id?: number | null
  concurrency: number
  priority: number
  weight: number
  load_factor?: number | null
  rate_multiplier?: number | null
  status: UpstreamStatus
  schedulable: boolean
  expires_at?: number | null
  auto_pause_on_expired: boolean
  oauth_refresh_owner: UpstreamOAuthRefreshOwner
  pool_ids: number[]
}

export interface UpstreamPoolPayload {
  name: string
  description: string
  platform: 'openai'
  credential_type: UpstreamAccountType | 'mixed'
  status: Exclude<UpstreamStatus, 'error'>
  default_proxy_id?: number | null
  scheduler_config: string
}

export interface UpstreamProxyPayload {
  name: string
  protocol: UpstreamProxyProtocol
  host: string
  port: number
  auth?: { username: string; password: string }
  status: Exclude<UpstreamStatus, 'error'>
  expires_at?: number | null
  fallback_mode: UpstreamProxyFallback
  backup_proxy_id?: number | null
  expiry_warn_days: number
}

export interface ApiResponse<T> {
  success: boolean
  message?: string
  data?: T
}

export interface UpstreamBatchResult {
  success_ids: number[]
  failures: Array<{ index?: number; id?: number; message: string }>
}
