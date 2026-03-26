import api from './index'
import type { User } from './types'

export interface LoginRequest {
  username: string
  password: string
}

export interface LoginResponse {
  token: string
  user: User
}

export const authApi = {
  login(data: LoginRequest): Promise<LoginResponse> {
    return api.post('/auth/login', data)
  },

  logout(): Promise<void> {
    return api.post('/auth/logout')
  },

  getCurrentUser(): Promise<{ data: User }> {
    return api.get('/auth/me')
  },

  changePassword(oldPassword: string, newPassword: string): Promise<void> {
    return api.put('/auth/password', { old_password: oldPassword, new_password: newPassword })
  }
}