import { get, post, put } from '@/utils/request'

export type FeedbackType = 'like' | 'dislike' | 'none'
export type DislikeReason = 'incorrect' | 'outdated' | 'irrelevant' | 'incomplete' | 'unclear' | 'other'

export interface MessageFeedbackInput {
  feedback_type: FeedbackType
  reason_code?: DislikeReason
  reason_text?: string
}

export interface MessageFeedbackState {
  feedback_type: Exclude<FeedbackType, 'none'>
  reason_code?: DislikeReason
  reason_text?: string
  feedback_at: string
}

export interface ChunkFeedbackListItem {
  chunk_id: string
  knowledge_id: string
  knowledge_title: string
  chunk_index: number
  chunk_type: string
  content_preview: string
  like_count: number
  dislike_count: number
  session_count: number
  positive_rate: number | null
  recall_weight: number
  needs_optimization: boolean
  feedback_reset_at?: string | null
  feedback_updated_at?: string | null
}

export interface ChunkFeedbackReasonCount {
  reason_code: string
  count: number
}

export interface ChunkFeedbackDetail extends ChunkFeedbackListItem {
  content: string
  reason_counts: ChunkFeedbackReasonCount[]
}

export interface ChunkFeedbackWeightLog {
  id: string
  old_weight: number
  new_weight: number
  source: string
  source_action: string
  source_message_id?: string
  source_feedback_id?: string
  reason?: string
  created_at: string
}

export interface PageResult<T> {
  total: number
  page: number
  page_size: number
  data: T[]
}

interface APIResponse<T> {
  success: boolean
  data: T
  message?: string
}

export interface ChunkFeedbackListParams {
  page?: number
  page_size?: number
  keyword?: string
  feedback_status?: 'all' | 'rated' | 'high' | 'normal' | 'low' | 'unrated'
  needs_optimization?: boolean
  sort_by?: 'feedback_updated_at' | 'like_count' | 'dislike_count' | 'positive_rate' | 'recall_weight' | 'chunk_index'
  sort_order?: 'asc' | 'desc'
}

const encodeID = (value: string) => encodeURIComponent(value)

export function setMessageFeedback(sessionId: string, messageId: string, input: MessageFeedbackInput) {
  return put<APIResponse<MessageFeedbackState | null>>(
    `/api/v1/messages/${encodeID(sessionId)}/${encodeID(messageId)}/feedback`,
    input,
  )
}

export function listChunkFeedback(kbId: string, params: ChunkFeedbackListParams) {
  const query = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== '') query.set(key, String(value))
  })
  const suffix = query.toString() ? `?${query.toString()}` : ''
  return get<APIResponse<PageResult<ChunkFeedbackListItem>>>(
    `/api/v1/knowledge-bases/${encodeID(kbId)}/chunk-feedback${suffix}`,
  )
}

export function getChunkFeedbackDetail(kbId: string, chunkId: string) {
  return get<APIResponse<ChunkFeedbackDetail>>(
    `/api/v1/knowledge-bases/${encodeID(kbId)}/chunk-feedback/${encodeID(chunkId)}`,
  )
}

export function listChunkFeedbackWeightLogs(kbId: string, chunkId: string, page = 1, pageSize = 20) {
  return get<APIResponse<PageResult<ChunkFeedbackWeightLog>>>(
    `/api/v1/knowledge-bases/${encodeID(kbId)}/chunk-feedback/${encodeID(chunkId)}/weight-logs?page=${page}&page_size=${pageSize}`,
  )
}

export function resetChunkFeedback(kbId: string, chunkId: string, reason = '') {
  return post<APIResponse<ChunkFeedbackDetail>>(
    `/api/v1/knowledge-bases/${encodeID(kbId)}/chunk-feedback/${encodeID(chunkId)}/reset`,
    { reason },
  )
}
