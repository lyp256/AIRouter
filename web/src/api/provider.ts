import api from './index'
import type { Provider, ProviderKey, ApiResponse } from './types'

export const providerApi = {
  list(): Promise<ApiResponse<Provider[]>> {
    return api.get('/providers')
  },

  get(id: string): Promise<ApiResponse<Provider> & { keys: ProviderKey[] }> {
    return api.get(`/providers/${id}`)
  },

  create(data: Partial<Provider>): Promise<ApiResponse<Provider>> {
    return api.post('/providers', data)
  },

  update(id: string, data: Partial<Provider>): Promise<ApiResponse<Provider>> {
    return api.put(`/providers/${id}`, data)
  },

  delete(id: string): Promise<ApiResponse<void>> {
    return api.delete(`/providers/${id}`)
  },

  // 供应商密钥
  listKeys(providerId: string): Promise<ApiResponse<ProviderKey[]>> {
    return api.get(`/providers/${providerId}/keys`)
  },

  createKey(providerId: string, data: Partial<ProviderKey> & { key: string }): Promise<ApiResponse<ProviderKey>> {
    return api.post(`/providers/${providerId}/keys`, data)
  },

  updateKey(keyId: string, data: Partial<ProviderKey>): Promise<ApiResponse<ProviderKey>> {
    return api.put(`/provider-keys/${keyId}`, data)
  },

  deleteKey(keyId: string): Promise<ApiResponse<void>> {
    return api.delete(`/provider-keys/${keyId}`)
  }
}