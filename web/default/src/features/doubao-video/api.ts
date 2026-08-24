/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { api } from '@/lib/api'
import type {
  AccessKey,
  ApiResponse,
  CreatedAccessKey,
  MaterialAsset,
  MaterialGroup,
  PageData,
} from './types'

export async function listMaterialGroups(keyword = '') {
  const response = await api.get<ApiResponse<MaterialGroup[]>>(
    '/api/doubao-video/material-groups',
    { params: { keyword } }
  )
  return response.data
}

export async function createMaterialGroup(input: {
  name: string
  description: string
}) {
  const response = await api.post<ApiResponse<MaterialGroup>>(
    '/api/doubao-video/material-groups',
    input
  )
  return response.data
}

export async function listMaterials(params: {
  groupId?: number
  keyword?: string
}) {
  const response = await api.get<ApiResponse<PageData<MaterialAsset>>>(
    '/api/doubao-video/materials',
    {
      params: {
        p: 1,
        size: 100,
        group_id: params.groupId || undefined,
        keyword: params.keyword || undefined,
      },
    }
  )
  return response.data
}

export async function uploadMaterial(input: {
  groupId: number
  name: string
  file: File
}) {
  const form = new FormData()
  form.set('group_id', String(input.groupId))
  form.set('name', input.name)
  form.set('file', input.file)
  const response = await api.post<ApiResponse<MaterialAsset>>(
    '/api/doubao-video/materials/upload',
    form
  )
  return response.data
}

export async function syncMaterials() {
  const response = await api.post<ApiResponse<{ synced: boolean }>>(
    '/api/doubao-video/materials/sync'
  )
  return response.data
}

export async function listAccessKeys() {
  const response = await api.get<ApiResponse<AccessKey[]>>(
    '/api/doubao-video/access-keys'
  )
  return response.data
}

export async function createAccessKey(name: string) {
  const response = await api.post<ApiResponse<CreatedAccessKey>>(
    '/api/doubao-video/access-keys',
    { name }
  )
  return response.data
}

export async function updateAccessKey(id: number, status: number) {
  const response = await api.patch<ApiResponse<{ updated: boolean }>>(
    `/api/doubao-video/access-keys/${id}`,
    { status }
  )
  return response.data
}

export async function deleteAccessKey(id: number) {
  const response = await api.delete<ApiResponse<{ deleted: boolean }>>(
    `/api/doubao-video/access-keys/${id}`
  )
  return response.data
}
