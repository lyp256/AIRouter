import api from './index'
import type { UsageLog, ApiResponse } from './types'

export interface DashboardStats {
  today_requests: number
  today_tokens: number
  today_cost: number
  active_users: number
  active_keys: number
  success_rate: number
}

export interface UsageTrend {
  date: string
  requests: number
  tokens: number
  cost: number
}

export interface ModelStats {
  model: string
  requests: number
  tokens: number
  cost: number
}

export interface UserStats {
  user_id: string
  username: string
  requests: number
  tokens: number
  cost: number
  last_used_at: string
}

export interface LogsResponse {
  data: UsageLog[]
  total: number
}

export const statsApi = {
  dashboard(): Promise<ApiResponse<DashboardStats>> {
    return api.get('/stats/dashboard')
  },

  trend(days = 7): Promise<ApiResponse<UsageTrend[]>> {
    return api.get('/stats/trend', { params: { days } })
  },

  models(days = 7): Promise<ApiResponse<ModelStats[]>> {
    return api.get('/stats/models', { params: { days } })
  },

  users(days = 7): Promise<ApiResponse<UserStats[]>> {
    return api.get('/stats/users', { params: { days } })
  },

  logs(params?: { user_id?: string; model?: string; status?: string }): Promise<LogsResponse> {
    return api.get('/stats/logs', { params })
  }
}