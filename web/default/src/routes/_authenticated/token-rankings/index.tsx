import { createFileRoute } from '@tanstack/react-router'

import { TokenRankings } from '@/features/rankings/token-rankings'

export const Route = createFileRoute('/_authenticated/token-rankings/')({
  component: TokenRankings,
})
