import { api } from '@/lib/api'

export type MonitorStat = {
  status: string
  latency_ms: number
  availability: Record<string, number>
  history?: Array<{ status: string; latency_ms: number; checked_at: number }>
}
export type AdminMonitorStat = MonitorStat & {
  error_summary?: string
  last_checked: number
}
export type PublicChannelMonitor = {
  name: string
  group: string
  models: Record<string, MonitorStat>
}
export type AdminChannelMonitor = PublicChannelMonitor & {
  id: number
  primary_model: string
  extra_models: string[]
  interval_seconds: number
  enabled: boolean
  last_checked_at: number
  created_at: number
  updated_at: number
  module_enabled: boolean
  models: Record<string, AdminMonitorStat>
}
export type MonitorInput = {
  name: string
  group: string
  primary_model: string
  extra_models: string[]
  interval_seconds: number
  enabled: boolean
}
type ApiResponse<T> = {
  success: boolean
  message?: string
  data: T
}

function requireSuccessfulResponse<T>(response: ApiResponse<T>): T {
  if (!response.success) {
    throw new Error(response.message || 'Request failed')
  }
  return response.data
}

export async function getChannelMonitors(): Promise<{
  success: boolean
  data: PublicChannelMonitor[]
}> {
  const res = await api.get('/api/channel-monitor/')
  return res.data
}
export async function getAdminChannelMonitors(): Promise<{
  success: boolean
  data: AdminChannelMonitor[]
}> {
  const res = await api.get('/api/channel-monitor/admin/')
  return res.data
}
export async function getChannelMonitorModels(
  group: string
): Promise<string[]> {
  const res = await api.get<ApiResponse<string[]>>(
    '/api/channel-monitor/admin/models',
    { params: { group } }
  )
  return requireSuccessfulResponse(res.data)
}
export async function createChannelMonitor(input: MonitorInput): Promise<void> {
  const res = await api.post<ApiResponse<unknown>>(
    '/api/channel-monitor/admin/',
    input
  )
  requireSuccessfulResponse(res.data)
}
export async function updateChannelMonitor(
  id: number,
  input: Partial<MonitorInput>
): Promise<void> {
  const res = await api.patch<ApiResponse<unknown>>(
    `/api/channel-monitor/admin/${id}`,
    input
  )
  requireSuccessfulResponse(res.data)
}
export async function deleteChannelMonitor(id: number): Promise<void> {
  await api.delete(`/api/channel-monitor/admin/${id}`)
}
export async function runChannelMonitor(id: number): Promise<void> {
  await api.post(`/api/channel-monitor/admin/${id}/run`)
}
export async function updateChannelMonitorConfig(
  enabled: boolean
): Promise<void> {
  await api.put('/api/channel-monitor/admin/config', { enabled })
}
