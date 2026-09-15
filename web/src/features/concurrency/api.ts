import { api } from '@/lib/api'

import type {
  BatchUserConcurrencyPayload,
  ConcurrencyMutationResponse,
  ConcurrencyResponse,
  UpdateUserConcurrencyPayload,
} from './types'

export async function getSelfConcurrency(): Promise<ConcurrencyResponse> {
  const res = await api.get<ConcurrencyResponse>('/api/user/self/concurrency')
  return res.data
}

export async function updateUserConcurrency(
  userId: number,
  payload: UpdateUserConcurrencyPayload
): Promise<ConcurrencyMutationResponse> {
  const res = await api.patch<ConcurrencyMutationResponse>(
    `/api/user/${userId}/concurrency`,
    payload
  )
  return res.data
}

export async function batchUpdateUserConcurrency(
  payload: BatchUserConcurrencyPayload
): Promise<ConcurrencyMutationResponse> {
  const res = await api.post<ConcurrencyMutationResponse>(
    '/api/user/batch-limits',
    payload
  )
  return res.data
}

export async function resetAllUserConcurrency(): Promise<ConcurrencyMutationResponse> {
  const res = await api.post<ConcurrencyMutationResponse>(
    '/api/user/concurrency/reset'
  )
  return res.data
}
