import { useQuery } from '@tanstack/react-query'

import { getSelfConcurrency } from './api'
import { normalizeConcurrencyStatus } from './types'

export const SELF_CONCURRENCY_QUERY_KEY = ['self-concurrency'] as const

export function useSelfConcurrency(enabled = true) {
  return useQuery({
    queryKey: SELF_CONCURRENCY_QUERY_KEY,
    queryFn: async () => {
      const response = await getSelfConcurrency()
      return {
        ...response,
        data: response.data
          ? normalizeConcurrencyStatus(response.data)
          : undefined,
      }
    },
    enabled,
    staleTime: 30 * 1000,
  })
}
