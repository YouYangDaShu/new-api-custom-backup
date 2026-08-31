import { api } from '@/lib/api'

export interface LotteryPrize {
  id: string
  name: string
  weight?: number
  amount: number
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
}
export interface LotteryDraw {
  id: number
  user_id: number
  prize_id: string
  prize_name: string
  prize_amount: number
  created_at: number
}

export async function getLottery(): Promise<{
  success: boolean
  data: LotteryProgress
}> {
  const res = await api.get('/api/lottery/')
  return res.data
}

export async function drawLottery(): Promise<{
  success: boolean
  data: LotteryDraw
}> {
  const res = await api.post('/api/lottery/draw')
  return res.data
}

export interface LotteryAdminData {
  config: {
    enabled: boolean
    threshold_display_amount: number
    prizes: LotteryPrize[]
  }
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
): Promise<{ success: boolean }> {
  const res = await api.put('/api/lottery/admin/', config)
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
