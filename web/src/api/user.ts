import api from './index'
import type { User, UserKey, ApiResponse, PaginatedResponse } from './types'

export const userApi = {
  list(page = 1, pageSize = 20): Promise<PaginatedResponse<User>> {
    return api.get('/users', { params: { page, page_size: pageSize } })
  },

  get(id: string): Promise<ApiResponse<User> & { keys: UserKey[] }> {
    return api.get(`/users/${id}`)
  },

  create(data: Partial<User> & { password: string }): Promise<ApiResponse<User>> {
    return api.post('/users', data)
  },

  update(id: string, data: Partial<User>): Promise<ApiResponse<User>> {
    return api.put(`/users/${id}`, data)
  },

  delete(id: string): Promise<ApiResponse<void>> {
    return api.delete(`/users/${id}`)
  },

  // 用户密钥
  listKeys(userId: string): Promise<ApiResponse<UserKey[]>> {
    return api.get('/user-keys', { params: { user_id: userId } })
  },

  // 获取当前用户的密钥列表（用于聊天功能）
  getMyKeys(): Promise<ApiResponse<UserKey[]>> {
    return api.get('/user-keys/me')
  },

  createKey(data: Partial<UserKey> & { user_id: string }): Promise<ApiResponse<UserKey> & { raw_key: string }> {
    return api.post('/user-keys', data)
  },

  updateKey(keyId: string, data: Partial<UserKey>): Promise<ApiResponse<UserKey>> {
    return api.put(`/user-keys/${keyId}`, data)
  },

  deleteKey(keyId: string): Promise<ApiResponse<void>> {
    return api.delete(`/user-keys/${keyId}`)
  },

  regenerateKey(keyId: string): Promise<ApiResponse<UserKey> & { raw_key: string }> {
    return api.post(`/user-keys/${keyId}/regenerate`)
  }
}