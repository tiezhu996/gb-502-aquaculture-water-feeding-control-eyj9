import { client, type ApiEnvelope } from './client'
import type { FeedBatch, FeedBatchInput, FeedConsumption, PageQuery, PageResult } from '@/types/models'

export const feedBatchApi = {
  async list(params: PageQuery = {}) {
    const response = await client.get<ApiEnvelope<PageResult<FeedBatch>>>('/feed-batches', { params })
    return response.data.data
  },
  async get(id: number) {
    const response = await client.get<ApiEnvelope<FeedBatch>>(`/feed-batches/${id}`)
    return response.data.data
  },
  async consumptions(id: number) {
    const response = await client.get<ApiEnvelope<PageResult<FeedConsumption>>>(`/feed-batches/${id}/consumptions`)
    return response.data.data
  },
  async create(input: FeedBatchInput) {
    const response = await client.post<ApiEnvelope<FeedBatch>>('/feed-batches', input)
    return response.data.data
  },
  async update(id: number, input: FeedBatchInput) {
    const response = await client.put<ApiEnvelope<FeedBatch>>(`/feed-batches/${id}`, input)
    return response.data.data
  },
}
