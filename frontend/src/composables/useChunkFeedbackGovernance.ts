import { computed, reactive, ref, toValue, type MaybeRefOrGetter } from 'vue'
import type {
  ChunkFeedbackDetail,
  ChunkFeedbackListItem,
  ChunkFeedbackListParams,
  ChunkFeedbackWeightLog,
} from '../api/feedback'

type GovernanceAPI = {
  list: (kbId: string, params: ChunkFeedbackListParams) => Promise<{ data: { total: number; page: number; page_size: number; data: ChunkFeedbackListItem[] } }>
  detail: (kbId: string, chunkId: string) => Promise<{ data: ChunkFeedbackDetail }>
  logs: (kbId: string, chunkId: string, page?: number, pageSize?: number) => Promise<{ data: { total: number; page: number; page_size: number; data: ChunkFeedbackWeightLog[] } }>
  reset: (kbId: string, chunkId: string, reason?: string) => Promise<{ data: ChunkFeedbackDetail }>
}

export function canGovernChunkFeedback(input: {
  vectorStoreSource?: string
  role?: string
  creatorId?: string
  userId?: string
}) {
  if (input.vectorStoreSource === 'shared') return false
  const roleLevel = { viewer: 10, contributor: 20, admin: 30, owner: 40 }[
    String(input.role || '').toLowerCase() as 'viewer' | 'contributor' | 'admin' | 'owner'
  ] || 0
  if (roleLevel >= 30) return true
  return roleLevel >= 20 && Boolean(input.creatorId && input.userId && input.creatorId === input.userId)
}

export function useChunkFeedbackGovernance(options: {
  kbId: MaybeRefOrGetter<string>
  api?: GovernanceAPI
}) {
  const api: GovernanceAPI = options.api || {
    list: async (...args) => (await import('../api/feedback')).listChunkFeedback(...args),
    detail: async (...args) => (await import('../api/feedback')).getChunkFeedbackDetail(...args),
    logs: async (...args) => (await import('../api/feedback')).listChunkFeedbackWeightLogs(...args),
    reset: async (...args) => (await import('../api/feedback')).resetChunkFeedback(...args),
  }
  const filters = reactive({
    keyword: '',
    feedbackStatus: 'all' as NonNullable<ChunkFeedbackListParams['feedback_status']>,
    optimization: 'all' as 'all' | 'yes' | 'no',
    sortBy: 'feedback_updated_at' as NonNullable<ChunkFeedbackListParams['sort_by']>,
    sortOrder: 'desc' as NonNullable<ChunkFeedbackListParams['sort_order']>,
  })
  const items = ref<ChunkFeedbackListItem[]>([])
  const page = ref(1)
  const pageSize = ref(20)
  const total = ref(0)
  const loading = ref(false)
  const listError = ref<unknown>(null)

  const detailVisible = ref(false)
  const detailLoading = ref(false)
  const selected = ref<ChunkFeedbackDetail | null>(null)
  const detailError = ref<unknown>(null)
  const logs = ref<ChunkFeedbackWeightLog[]>([])
  const logPage = ref(1)
  const logPageSize = ref(20)
  const logTotal = ref(0)
  const logsLoading = ref(false)
  const resetting = ref(false)
  const resetRefreshFailed = ref(false)

  let listRequestID = 0
  let detailRequestID = 0
  let logRequestID = 0

  const kbId = computed(() => toValue(options.kbId).trim())

  const listParams = (): ChunkFeedbackListParams => ({
    page: page.value,
    page_size: pageSize.value,
    keyword: filters.keyword.trim() || undefined,
    feedback_status: filters.feedbackStatus,
    needs_optimization: filters.optimization === 'all' ? undefined : filters.optimization === 'yes',
    sort_by: filters.sortBy,
    sort_order: filters.sortOrder,
  })

  const loadList = async (resetPage = false) => {
    if (!kbId.value) return false
    if (resetPage) page.value = 1
    const requestID = ++listRequestID
    const targetKBID = kbId.value
    const params = listParams()
    loading.value = true
    listError.value = null
    try {
      const response = await api.list(targetKBID, params)
      if (requestID !== listRequestID) return true
      items.value = response.data.data || []
      total.value = response.data.total || 0
      page.value = response.data.page || page.value
      pageSize.value = response.data.page_size || pageSize.value
      return true
    } catch (error) {
      if (requestID !== listRequestID) return true
      listError.value = error
      return false
    } finally {
      if (requestID === listRequestID) loading.value = false
    }
  }

  const loadLogs = async (nextPage = logPage.value) => {
    if (!kbId.value || !selected.value?.chunk_id) return false
    const requestID = ++logRequestID
    const targetKBID = kbId.value
    const targetChunkID = selected.value.chunk_id
    logPage.value = nextPage
    logsLoading.value = true
    try {
      const response = await api.logs(targetKBID, targetChunkID, nextPage, logPageSize.value)
      if (requestID !== logRequestID || selected.value?.chunk_id !== targetChunkID) return true
      logs.value = response.data.data || []
      logTotal.value = response.data.total || 0
      logPage.value = response.data.page || logPage.value
      logPageSize.value = response.data.page_size || logPageSize.value
      return true
    } catch (error) {
      if (requestID !== logRequestID || selected.value?.chunk_id !== targetChunkID) return true
      detailError.value = error
      return false
    } finally {
      if (requestID === logRequestID) logsLoading.value = false
    }
  }

  const openDetail = async (chunkId: string) => {
    if (!kbId.value || !chunkId) return false
    const requestID = ++detailRequestID
    ++logRequestID
    const targetKBID = kbId.value
    detailVisible.value = true
    detailLoading.value = true
    detailError.value = null
    selected.value = null
    logs.value = []
    logTotal.value = 0
    try {
      const response = await api.detail(targetKBID, chunkId)
      if (requestID !== detailRequestID) return true
      selected.value = response.data
      return await loadLogs(1)
    } catch (error) {
      if (requestID !== detailRequestID) return true
      detailError.value = error
      return false
    } finally {
      if (requestID === detailRequestID) detailLoading.value = false
    }
  }

  const resetSelected = async (reason = '') => {
    if (!kbId.value || !selected.value?.chunk_id || resetting.value) return false
    const targetChunkID = selected.value.chunk_id
    resetting.value = true
    resetRefreshFailed.value = false
    detailError.value = null
    try {
      const response = await api.reset(kbId.value, targetChunkID, reason.trim())
      if (selected.value?.chunk_id === targetChunkID) selected.value = response.data
      const [listLoaded, logsLoaded] = await Promise.all([
        loadList(false),
        selected.value?.chunk_id === targetChunkID ? loadLogs(1) : Promise.resolve(true),
      ])
      resetRefreshFailed.value = !listLoaded || !logsLoaded
      return true
    } catch (error) {
      detailError.value = error
      return false
    } finally {
      resetting.value = false
    }
  }

  const closeDetail = () => {
    ++detailRequestID
    ++logRequestID
    detailVisible.value = false
    detailLoading.value = false
    logsLoading.value = false
    selected.value = null
    logs.value = []
    detailError.value = null
  }

  return {
    filters,
    items,
    page,
    pageSize,
    total,
    loading,
    listError,
    detailVisible,
    detailLoading,
    selected,
    detailError,
    logs,
    logPage,
    logPageSize,
    logTotal,
    logsLoading,
    resetting,
    resetRefreshFailed,
    loadList,
    loadLogs,
    openDetail,
    resetSelected,
    closeDetail,
  }
}
