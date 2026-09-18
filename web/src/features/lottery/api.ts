import { api } from '@/lib/api'

export interface LotteryPrize {
  id: string
  name: string
  prize_level: number
  weight: number
  amount: number
  enabled: boolean
  sort_order: number
}
export interface LotteryUserPrize {
  id: string
  name: string
  amount: number
}
export interface LotteryProgress {
  enabled: boolean
  threshold: number
  accumulated: number
  available_draws: number
  prizes: LotteryUserPrize[]
  recent_draw?: LotteryDraw
  history: {
    items: LotteryDraw[]
    page: number
    page_size: number
    total: number
    total_pages: number
  }
}
export interface LotteryDraw {
  id: number
  prize_id: string
  prize_name: string
  prize_amount: number
  created_at: number
}

export interface LotteryAdminConfig {
  enabled: boolean
  threshold_display_amount: number
  prizes: LotteryPrize[]
  pity_enabled: boolean
  pity_target_prize_level: number
  pity_base_rate: number
  pity_base_end: number
  pity_stage_1_start: number
  pity_stage_1_end: number
  pity_stage_1_rates: number[]
  pity_stage_2_start: number
  pity_stage_2_end: number
  pity_stage_2_rates: number[]
  pity_hard_threshold: number
}

export async function getLottery(page = 1): Promise<{
  success: boolean
  data: LotteryProgress
}> {
  const res = await api.get('/api/lottery/', {
    params: { page, page_size: 10 },
  })
  return res.data
}

export async function drawLottery(
  count = 1
): Promise<{ success: boolean; data: LotteryDraw | LotteryDraw[] }> {
  const res = await api.post('/api/lottery/draw', { count })
  if (!res.data?.success) {
    throw new Error(res.data?.message || 'Lottery draw failed')
  }
  return res.data
}

export interface LotteryAdminData {
  config: LotteryAdminConfig
  users: Array<{
    user_id: number
    username: string
    accumulated: number
    available_draws: number
    threshold: number
    draw_count: number
  }>
  users_total: number
  total_draws: number
  total_amount: number
  draws: Array<{
    id: number
    user_id: number
    username: string
    prize_name: string
    prize_amount: number
    created_at: number
  }>
  page: number
  page_size: number
}
export async function getLotteryAdmin(
  page = 1,
  pageSize = 10
): Promise<{ success: boolean; data: LotteryAdminData }> {
  const res = await api.get('/api/lottery/admin/', {
    params: { p: page, page_size: pageSize },
  })
  return res.data
}
export async function updateLotteryAdmin(
  config: LotteryAdminData['config']
): Promise<{ success: boolean; data?: LotteryAdminConfig }> {
  const res = await api.put('/api/lottery/admin/', config)
  if (!res.data?.success) {
    throw new Error(res.data?.message || 'Failed to save lottery settings')
  }
  return res.data
}
export async function resetLotteryAdmin(
  userId?: number
): Promise<{ success: boolean }> {
  const res = await api.post(
    userId ? `/api/lottery/admin/reset/${userId}` : '/api/lottery/admin/reset'
  )
  return res.data
}
