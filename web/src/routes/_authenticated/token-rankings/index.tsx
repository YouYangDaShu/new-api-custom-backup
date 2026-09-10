import { createFileRoute, redirect } from '@tanstack/react-router'

import { TokenRankings } from '@/features/rankings/token-rankings'
import { getStatus } from '@/lib/api'
import { getFreshModuleAccess } from '@/lib/nav-modules'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated/token-rankings/')({
  beforeLoad: async ({ location }) => {
    const access = await getFreshModuleAccess('rankings')
    const status = await getStatus()
    if (status?.token_ranking_enabled === false || !access.enabled) {
      throw redirect({ to: '/' })
    }
    if (access.requireAuth && !useAuthStore.getState().auth.user) {
      throw redirect({ to: '/sign-in', search: { redirect: location.href } })
    }
  },
  component: TokenRankings,
})
