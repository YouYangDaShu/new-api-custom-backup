export type HistoryRow = {
  week_start: string
  user_id: number
  display_name: string
  rank: number
  total_tokens: number
  reward_quota: number
  reward_status: string
  created_at: number
}

export function formatHistoryRecipient(
  row: Pick<HistoryRow, 'user_id' | 'display_name'>,
  userIdLabel: string
): string {
  const displayName = row.display_name.trim() || '-'
  return `${userIdLabel}: ${row.user_id} | ${displayName}`
}

export function formatTokenUsageInBillions(totalTokens: number): string {
  return `${(totalTokens / 1_000_000_000).toFixed(2)} B`
}
