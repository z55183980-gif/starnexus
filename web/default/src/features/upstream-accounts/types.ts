/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/

export type UpstreamStatus = 'active' | 'inactive' | 'error' | 'expired'
export type UpstreamPlatform = 'openai' | 'anthropic'
export type UpstreamAccountType =
  | 'oauth'
  | 'setup_token'
  | 'apikey'
  | 'bedrock'
  | 'service_account'
export type UpstreamOAuthRefreshOwner = 'external' | 'starnexus'
export type UpstreamOAuthRefreshState =
  | 'oauth_refresh_pending'
  | 'oauth_refresh_failed'
  | 'oauth_refresh_permanent'
export type UpstreamProxyProtocol = 'http' | 'https' | 'socks5' | 'socks5h'
export type UpstreamProxyFallback = 'none' | 'direct' | 'proxy'
export type UpstreamProxyStatus = 'active' | 'inactive' | 'expired'

export interface UpstreamAccountPool {
  id: number
  name: string
  description: string
  platform: UpstreamPlatform
  credential_type: UpstreamAccountType | 'mixed'
  status: 'active' | 'inactive'
  default_proxy_id?: number | null
  scheduler_config: string
  account_count: number
  active_count: number
  ready_count: number
  temporarily_limited_count: number
  scheduler_available: boolean
  channel_count: number
  published_channel_id?: number | null
  published_channel_status?: number | null
  published_model_count: number
  attempt_count_24h: number
  success_count_24h: number
  error_count_24h: number
  last_event_at?: number | null
  created_at: number
  updated_at: number
}

export interface UpstreamAccountPoolCapabilities {
  models: string[]
  account_count: number
  schedulable_account_count: number
  unreadable_account_count: number
  wildcard_model_account_count: number
  passthrough_account_count: number
  header_override_account_count: number
  proxy_configured_account_count: number
  published_channel_id?: number | null
  published_groups: string[]
}

export interface UpstreamAccountPoolPublishResult {
  channel_id: number
  created: boolean
  models: string[]
  groups: string[]
}

export interface UpstreamAccountPoolMember {
  pool_id: number
  account_id: number
  priority: number
  weight: number
  created_at: number
  name: string
  platform: UpstreamPlatform
  type: UpstreamAccountType
  status: UpstreamStatus
  schedulable: boolean
}

export interface UpstreamAccount {
  id: number
  name: string
  notes?: string | null
  platform: UpstreamPlatform
  type: UpstreamAccountType
  credential_configured: boolean
  credential_version: number
  extra: string
  proxy_id?: number | null
  concurrency: number
  current_concurrency: number
  priority: number
  weight: number
  load_factor?: number | null
  rate_multiplier?: number | null
  status: UpstreamStatus
  schedulable: boolean
  error_message: string
  last_used_at?: number | null
  rate_limited_at?: number | null
  rate_limit_reset_at?: number | null
  overload_until?: number | null
  temp_unschedulable_until?: number | null
  temp_unschedulable_reason: string
  expires_at?: number | null
  auto_pause_on_expired: boolean
  oauth_refresh_owner: UpstreamOAuthRefreshOwner
  pool_ids: number[]
  metadata: {
    email?: string
    plan_type?: string
    privacy_mode?: string
    compact_mode?: string
    compact_supported: boolean
    credential_readable: boolean
    credential_read_error?: string
    base_url?: string
    model_mapping?: Record<string, string>
    compact_model_mapping?: Record<string, string>
    openai_capabilities?: string[]
    intercept_warmup_requests: boolean
    header_override_enabled: boolean
    header_overrides?: Record<string, string>
    bedrock_auth_mode?: string
    aws_region?: string
    aws_access_key_id?: string
    vertex_project_id?: string
    vertex_client_email?: string
    vertex_location?: string
  }
  created_at: number
  updated_at: number
}

export function upstreamOAuthRefreshBlocksScheduling(
  reason: string | null | undefined
): reason is UpstreamOAuthRefreshState {
  return (
    reason === 'oauth_refresh_pending' ||
    reason === 'oauth_refresh_failed' ||
    reason === 'oauth_refresh_permanent'
  )
}

export interface UpstreamAccountRateLimitWindow {
  used_percent: number
  limit_window_seconds: number
  reset_after_seconds: number
  reset_at: number
  window_stats?: UpstreamAccountWindowStats | null
}

export interface UpstreamAccountWindowStats {
  requests: number
  tokens: number
  cost: number
  user_cost: number
}

export interface UpstreamAccountRateLimit {
  plan_type?: string
  allowed: boolean
  limit_reached: boolean
  primary_window?: UpstreamAccountRateLimitWindow | null
  secondary_window?: UpstreamAccountRateLimitWindow | null
}

export interface UpstreamAccountQuotaUsage {
  user_id?: string
  account_id?: string
  email?: string
  plan_type?: string
  rate_limit?: UpstreamAccountRateLimit | null
  additional_rate_limits?: Array<{
    limit_name?: string
    metered_feature?: string
    rate_limit?: UpstreamAccountRateLimit | null
  }>
  rate_limit_reset_credits?: {
    available_count: number
    credits?: Array<{ expires_at?: string }>
  } | null
  fetched_at: number
}

export interface UpstreamAccountQuotaResetResult {
  code: string
  windows_reset: number
}

export interface UpstreamAccountUsage {
  source: 'active' | 'estimated'
  five_hour?: UpstreamAccountRateLimitWindow | null
  seven_day?: UpstreamAccountRateLimitWindow | null
  seven_day_sonnet?: UpstreamAccountRateLimitWindow | null
  seven_day_fable?: UpstreamAccountRateLimitWindow | null
  fetched_at: number
}

export interface UpstreamAccountTestHistory {
  success: boolean
  status_code: number
  latency_ms: number
  first_output_latency_ms: number
  result: string
  model: string
  created_at: number
}

export interface UpstreamAccountStats {
  days: number
  selected_count: number
  success_count: number
  error_count: number
  test_count: number
  success_rate: number
  recent_tests: UpstreamAccountTestHistory[]
}

export interface UpstreamAccountScheduledTestPlan {
  id: number
  account_id: number
  name: string
  model: string
  interval_minutes: number
  enabled: boolean
  auto_recover: boolean
  next_run_at?: number | null
  last_run_at?: number | null
  created_at: number
  updated_at: number
}

export interface UpstreamAccountScheduledTestResult {
  id: number
  plan_id: number
  account_id: number
  success: boolean
  status_code: number
  latency_ms: number
  first_output_latency_ms: number
  result: string
  model: string
  created_at: number
}

export interface UpstreamAccountScheduledTestPlanPayload {
  name: string
  model: string
  interval_minutes: number
  enabled: boolean
  auto_recover: boolean
}

export interface UpstreamProxy {
  id: number
  name: string
  protocol: UpstreamProxyProtocol
  host: string
  port: number
  auth_configured: boolean
  username: string
  password?: string
  status: UpstreamProxyStatus
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
  platform: UpstreamPlatform
  type: UpstreamAccountType
  credentials?: Record<string, unknown>
  credential_patch?: Record<string, unknown>
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
  platform: UpstreamPlatform
  credential_type: UpstreamAccountType | 'mixed'
  status: 'active' | 'inactive'
  default_proxy_id?: number | null
  scheduler_config: string
}

export interface UpstreamProxyPayload {
  name: string
  protocol: UpstreamProxyProtocol
  host: string
  port: number
  auth?: { username: string; password: string }
  status: Exclude<UpstreamProxyStatus, 'expired'>
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

export interface UpstreamAccountExport {
  type: 'sub2api-data'
  version: 1
  exported_at: string
  proxies: unknown[]
  accounts: unknown[]
}

export interface UpstreamDataImportResult {
  proxy_created: number
  proxy_reused: number
  proxy_failed: number
  account_created: number
  account_failed: number
  errors: Array<{
    kind: string
    name?: string
    proxy_key?: string
    message: string
  }>
}

export interface CRSSyncInput {
  base_url: string
  username: string
  password: string
  sync_proxies: boolean
  selected_account_ids?: string[]
}

export interface CRSPreviewAccount {
  crs_account_id: string
  kind: string
  name: string
  platform: UpstreamPlatform
  type: UpstreamAccountType
}

export interface CRSPreviewResult {
  new_accounts: CRSPreviewAccount[]
  existing_accounts: CRSPreviewAccount[]
  skipped: number
}

export interface CRSSyncResult {
  created: number
  updated: number
  skipped: number
  failed: number
  items: Array<{
    crs_account_id: string
    kind: string
    name: string
    action: 'created' | 'updated' | 'skipped' | 'failed'
    error?: string
  }>
}

export interface UpstreamAccountTestResult {
  account_id: number
  success: boolean
  status_code: number
  latency_ms: number
  first_output_latency_ms: number
  proxy_id: number
  result: string
  credential_type: string
  protocol: string
  model: string
  terminal_type?: string
  event_types?: string[]
  http_version?: string
  content_type?: string
  content_encoding?: string
  body_bytes?: number
  mode: 'default' | 'compact'
  output_text?: string
}
