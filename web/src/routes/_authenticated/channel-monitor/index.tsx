import { createFileRoute, redirect } from '@tanstack/react-router'

import { ChannelMonitorPage } from '@/features/channel-monitor'
import { getStatus } from '@/lib/api'

export const Route = createFileRoute('/_authenticated/channel-monitor/')({
  beforeLoad: async () => {
    if ((await getStatus())?.channel_monitor_enabled === false) {
      throw redirect({ to: '/' })
    }
  },
  component: ChannelMonitorPage,
})
