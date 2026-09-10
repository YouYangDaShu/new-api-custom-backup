import { createFileRoute, redirect } from '@tanstack/react-router'

import { TokenRankingAdminPage } from '@/features/rankings/admin'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute(
  '/_authenticated/token-ranking-management/'
)({
  beforeLoad: () => {
    if ((useAuthStore.getState().auth.user?.role ?? 0) < ROLE.ADMIN) {
      throw redirect({ to: '/403' })
    }
  },
  component: TokenRankingAdminPage,
})
