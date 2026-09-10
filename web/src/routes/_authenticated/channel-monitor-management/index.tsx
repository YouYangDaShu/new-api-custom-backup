import { createFileRoute, redirect } from '@tanstack/react-router'

import { ChannelMonitorAdminPage } from '@/features/channel-monitor/admin'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute(
  '/_authenticated/channel-monitor-management/'
)({
  beforeLoad: () => {
    if ((useAuthStore.getState().auth.user?.role ?? 0) < ROLE.ADMIN) {
      throw redirect({ to: '/403' })
    }
  },
  component: ChannelMonitorAdminPage,
})
