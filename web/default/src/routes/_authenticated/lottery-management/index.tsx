import { createFileRoute, redirect } from '@tanstack/react-router'

import { LotteryAdmin } from '@/features/lottery/admin'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated/lottery-management/')({
  beforeLoad: () => { if ((useAuthStore.getState().auth.user?.role ?? 0) < ROLE.ADMIN) throw redirect({ to: '/403' }) },
  component: LotteryAdmin,
})
