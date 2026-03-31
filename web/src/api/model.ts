import api from './index'
import type { Model, ModelWithUpstreams, Upstream, ApiResponse, OpenAIModelsResponse, UpstreamTestResult } from './types'

export const modelApi = {
  // 获取模型列表（OpenAI 兼容格式，调用 /v1/models）
  async list(): Promise<OpenAIModelsResponse> {
    const resp = await fetch('/v1/models')
    if (!resp.ok) throw new Error(`获取模型列表失败: ${resp.status}`)
    return resp.json()
  },

  // 获取模型列表（管理 API，完整信息）
  adminList(): Promise<ApiResponse<Model[]>> {
    return api.get('/models')
  },

  // 获取模型详情（管理 API）
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
  },

  testUpstreams(id: string): Promise<ApiResponse<UpstreamTestResult[]>> {
    return api.post(`/models/${id}/test-upstreams`)
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
  },

  resetStatus(id: string): Promise<ApiResponse<Upstream>> {
    return api.post(`/upstreams/${id}/reset-status`)
  },

  test(id: string): Promise<ApiResponse<UpstreamTestResult>> {
    return api.post(`/upstreams/${id}/test`)
  }
}