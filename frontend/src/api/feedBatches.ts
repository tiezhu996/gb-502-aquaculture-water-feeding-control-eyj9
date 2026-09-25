import { client, type ApiEnvelope } from './client'
import type { FeedBatch, FeedBatchDetail, FeedBatchInput, PageQuery, PageResult } from '@/types/models'

export const feedBatchApi = {
  async list(params: PageQuery = {}) {
    const response = await client.get<ApiEnvelope<PageResult<FeedBatch>>>('/feed-batches', { params })
    return response.data.data
  },
  async get(id: number) {
    const response = await client.get<ApiEnvelope<FeedBatchDetail>>(`/feed-batches/${id}`)
    return response.data.data
  },
  async available(feedType: string) {
    const response = await client.get<ApiEnvelope<{ items: FeedBatch[] }>>('/feed-batches/available', { params: { feedType } })
    return response.data.data.items
  },
  async create(input: FeedBatchInput) {
    const response = await client.post<ApiEnvelope<FeedBatch>>('/feed-batches', input)
    return response.data.data
  },
  async update(id: number, input: FeedBatchInput) {
    const response = await client.put<ApiEnvelope<FeedBatch>>(`/feed-batches/${id}`, input)
    return response.data.data
  },
  async setEnabled(id: number, enabled: boolean) {
    const response = await client.patch<ApiEnvelope<FeedBatch>>(`/feed-batches/${id}/enabled`, { enabled })
    return response.data.data
  },
  async remove(id: number) {
    await client.delete(`/feed-batches/${id}`)
  },
}
