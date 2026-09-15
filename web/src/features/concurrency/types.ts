export type ConcurrencySource =
  | 'default'
  | 'custom'
  | 'unlimited'
  | 'unavailable'
  | 'admin_exempt'

export interface ConcurrencyStatus {
  current: number
  limit: number
  remaining: number
  queued: number
  source: ConcurrencySource
  admin_exempt: boolean
}

export interface ConcurrencyStatusInput {
  current?: number
  used?: number
  limit?: number
  remaining?: number
  queued?: number
  source?: ConcurrencySource | string
  status?: string
  unlimited?: boolean
  admin_exempt?: boolean
}

export interface ConcurrencyResponse {
  success: boolean
  message?: string
  data?: ConcurrencyStatus
}

export interface UpdateUserConcurrencyPayload {
  source: 'default' | 'custom'
  limit: number
}

export interface BatchUserConcurrencyPayload
  extends UpdateUserConcurrencyPayload {
  user_ids: number[]
}

export interface ConcurrencyMutationResponse {
  success: boolean
  message?: string
  data?: ConcurrencyStatus
}

export function normalizeConcurrencyStatus(
  value: ConcurrencyStatusInput | null | undefined
): ConcurrencyStatus {
  const current = Math.max(
    0,
    Number(value?.current ?? value?.used) || 0
  )
  const limit = Math.max(0, Number(value?.limit) || 0)
  const queued = Math.max(0, Number(value?.queued) || 0)
  const adminExempt =
    value?.admin_exempt === true || value?.status === 'admin_exempt'
  const unavailable = value?.status === 'unavailable'
  const unlimited = value?.unlimited === true || limit === 0
  const source: ConcurrencySource = adminExempt
    ? 'admin_exempt'
    : unavailable
      ? 'unavailable'
      : unlimited
        ? 'unlimited'
        : value?.source === 'custom'
          ? 'custom'
          : 'default'
  return {
    current,
    limit,
    remaining:
      limit === 0
        ? 0
        : Math.max(0, Number(value?.remaining ?? limit - current) || 0),
    queued,
    source,
    admin_exempt: adminExempt,
  }
}
