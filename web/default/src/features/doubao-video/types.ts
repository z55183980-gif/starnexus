/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/

export type ApiResponse<T> = {
  success: boolean
  message?: string
  data: T
}

export type MaterialGroup = {
  id: number
  provider_group_id: string
  name: string
  description: string
  group_type: string
  status: string
  asset_count: number
  created_at: number
}

export type MaterialAsset = {
  id: number
  asset_group_id: number
  provider_asset_id: string
  name: string
  asset_type: string
  status: string
  error_message: string
  last_synced_at: number
  created_at: number
}

export type PageData<T> = {
  items: T[]
  total: number
}

export type MaterialStorageUsage = {
  used_bytes: number
  limit_bytes: number
  remaining_bytes: number
}

export type AccessKey = {
  id: number
  name: string
  access_key_id: string
  status: number
  last_used_at: number
  created_at: number
}

export type CreatedAccessKey = {
  key: AccessKey
  secret_access_key: string
}
