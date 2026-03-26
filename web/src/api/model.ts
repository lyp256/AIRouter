import api from './index'
import type { Model, ModelWithUpstreams, Upstream, ApiResponse } from './types'

export const modelApi = {
  list(): Promise<ApiResponse<Model[]>> {
    return api.get('/models')
  },

  get(id: string): Promise<ApiResponse<ModelWithUpstreams>> {
    return api.get(`/models/${id}`)
  },

  create(data: Partial<Model>): Promise<ApiResponse<Model>> {
    return api.post('/models', data)
  },

  update(id: string, data: Partial<Model>): Promise<ApiResponse<Model>> {
    return api.put(`/models/${id}`, data)
  },

  delete(id: string): Promise<ApiResponse<void>> {
    return api.delete(`/models/${id}`)
  },

  toggle(id: string): Promise<ApiResponse<Model>> {
    return api.post(`/models/${id}/toggle`)
  }
}

// 上游模型管理 API
export const upstreamApi = {
  list(): Promise<ApiResponse<Upstream[]>> {
    return api.get('/upstreams')
  },

  get(id: string): Promise<ApiResponse<Upstream>> {
    return api.get(`/upstreams/${id}`)
  },

  listByModel(modelId: string): Promise<ApiResponse<Upstream[]>> {
    return api.get(`/models/${modelId}/upstreams`)
  },

  create(modelId: string, data: Partial<Upstream>): Promise<ApiResponse<Upstream>> {
    return api.post(`/models/${modelId}/upstreams`, data)
  },

  update(id: string, data: Partial<Upstream>): Promise<ApiResponse<Upstream>> {
    return api.put(`/upstreams/${id}`, data)
  },

  delete(id: string): Promise<ApiResponse<void>> {
    return api.delete(`/upstreams/${id}`)
  },

  toggle(id: string): Promise<ApiResponse<Upstream>> {
    return api.post(`/upstreams/${id}/toggle`)
  }
}