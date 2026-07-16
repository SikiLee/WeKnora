import assert from 'node:assert/strict'
import test from 'node:test'
import { ref } from 'vue'

import type {
  ChunkFeedbackDetail,
  ChunkFeedbackListItem,
  ChunkFeedbackListParams,
  ChunkFeedbackWeightLog,
} from '../api/feedback'
import { canGovernChunkFeedback, useChunkFeedbackGovernance } from './useChunkFeedbackGovernance'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((next) => { resolve = next })
  return { promise, resolve }
}

const listItem: ChunkFeedbackListItem = {
  chunk_id: 'chunk-1',
  knowledge_id: 'knowledge-1',
  knowledge_title: 'Guide',
  chunk_index: 2,
  chunk_type: 'text',
  content_preview: 'preview',
  like_count: 1,
  dislike_count: 3,
  session_count: 2,
  positive_rate: 0.25,
  recall_weight: 0.8,
  needs_optimization: false,
}

const detail: ChunkFeedbackDetail = {
  ...listItem,
  content: 'full content',
  reason_counts: [{ reason_code: 'incorrect', count: 2 }],
}

const log: ChunkFeedbackWeightLog = {
  id: 'log-1',
  old_weight: 1,
  new_weight: 0.8,
  source: 'user_feedback',
  source_action: 'dislike',
  created_at: '2026-07-15T08:30:00Z',
}

test('governance visibility mirrors tenant and shared editor permissions', () => {
  const base = { accessResolved: true, vectorStoreSource: 'user' }
  assert.equal(canGovernChunkFeedback({
    ...base, accessResolved: false, role: 'contributor',
  }), false)
  assert.equal(canGovernChunkFeedback({ ...base, role: 'owner' }), true)
  assert.equal(canGovernChunkFeedback({ ...base, role: 'admin' }), true)
  assert.equal(canGovernChunkFeedback({ ...base, role: 'contributor' }), true)
  assert.equal(canGovernChunkFeedback({ ...base, role: 'viewer' }), false)
  assert.equal(canGovernChunkFeedback({
    ...base, creatorId: 'user-1', userId: 'user-1',
  }), true)
  assert.equal(canGovernChunkFeedback({
    ...base, role: 'viewer', creatorId: 'user-1', userId: 'user-1',
  }), false)
  assert.equal(canGovernChunkFeedback({
    ...base, role: 'contributor', vectorStoreSource: 'shared', permission: 'editor',
  }), true)
  assert.equal(canGovernChunkFeedback({
    ...base, role: 'admin', vectorStoreSource: 'shared', permission: 'viewer',
  }), false)
  assert.equal(canGovernChunkFeedback({
    ...base, role: 'viewer', vectorStoreSource: 'shared', permission: 'editor',
  }), false)
})

test('governance list sends filters, sort, and pagination to the server', async () => {
  let received: ChunkFeedbackListParams | undefined
  const model = useChunkFeedbackGovernance({
    kbId: 'kb-1',
    api: {
      list: async (kbId, params) => {
        assert.equal(kbId, 'kb-1')
        received = params
        return { success: true, data: { total: 1, page: 2, page_size: 50, data: [listItem] } }
      },
      detail: async () => ({ success: true, data: detail }),
      logs: async () => ({ success: true, data: { total: 0, page: 1, page_size: 20, data: [] } }),
      reset: async () => ({ success: true, data: detail }),
    },
  })
  model.page.value = 2
  model.pageSize.value = 50
  model.filters.keyword = '  guide  '
  model.filters.feedbackStatus = 'low'
  model.filters.optimization = 'yes'
  model.filters.sortBy = 'positive_rate'
  model.filters.sortOrder = 'asc'

  assert.equal(await model.loadList(), true)
  assert.deepEqual(received, {
    page: 2,
    page_size: 50,
    keyword: 'guide',
    feedback_status: 'low',
    needs_optimization: true,
    sort_by: 'positive_rate',
    sort_order: 'asc',
  })
  assert.deepEqual(model.items.value, [listItem])
  assert.equal(model.total.value, 1)
})

test('opening detail loads reason aggregation and weight logs on demand', async () => {
  const calls: string[] = []
  const model = useChunkFeedbackGovernance({
    kbId: 'kb-1',
    api: {
      list: async () => ({ success: true, data: { total: 0, page: 1, page_size: 20, data: [] } }),
      detail: async (_kbId, chunkId) => {
        calls.push(`detail:${chunkId}`)
        return { success: true, data: detail }
      },
      logs: async (_kbId, chunkId, page) => {
        calls.push(`logs:${chunkId}:${page}`)
        return { success: true, data: { total: 1, page: 1, page_size: 20, data: [log] } }
      },
      reset: async () => ({ success: true, data: detail }),
    },
  })

  assert.equal(await model.openDetail('chunk-1'), true)
  assert.equal(model.detailVisible.value, true)
  assert.deepEqual(model.selected.value?.reason_counts, detail.reason_counts)
  assert.deepEqual(model.logs.value, [log])
  assert.deepEqual(calls, ['detail:chunk-1', 'logs:chunk-1:1'])
})

test('reset refreshes list and logs while retaining the selected detail', async () => {
  const calls: string[] = []
  const resetDetail = { ...detail, like_count: 0, dislike_count: 0, positive_rate: null, recall_weight: 1 }
  const model = useChunkFeedbackGovernance({
    kbId: 'kb-1',
    api: {
      list: async () => {
        calls.push('list')
        return { success: true, data: { total: 1, page: 1, page_size: 20, data: [resetDetail] } }
      },
      detail: async () => ({ success: true, data: detail }),
      logs: async () => {
        calls.push('logs')
        return { success: true, data: { total: 1, page: 1, page_size: 20, data: [log] } }
      },
      reset: async (_kbId, chunkId, reason) => {
        calls.push(`reset:${chunkId}:${reason}`)
        return { success: true, data: resetDetail }
      },
    },
  })
  model.selected.value = detail

  assert.equal(await model.resetSelected('  reviewed  '), true)
  assert.equal(model.selected.value?.like_count, 0)
  assert.deepEqual(calls, ['reset:chunk-1:reviewed', 'list', 'logs'])
})

test('failed list and reset operations preserve the visible server state', async () => {
  const model = useChunkFeedbackGovernance({
    kbId: 'kb-1',
    api: {
      list: async () => { throw new Error('list unavailable') },
      detail: async () => ({ success: true, data: detail }),
      logs: async () => ({ success: true, data: { total: 0, page: 1, page_size: 20, data: [] } }),
      reset: async () => { throw new Error('reset unavailable') },
    },
  })
  model.items.value = [listItem]
  model.selected.value = detail

  assert.equal(await model.loadList(), false)
  assert.deepEqual(model.items.value, [listItem])
  assert.match(String(model.listError.value), /list unavailable/)
  assert.equal(await model.resetSelected(), false)
  assert.equal(model.selected.value?.like_count, detail.like_count)
  assert.match(String(model.detailError.value), /reset unavailable/)
})

test('older list responses cannot overwrite the latest filters', async () => {
  const firstResponse = deferred<{ data: { total: number; page: number; page_size: number; data: ChunkFeedbackListItem[] } }>()
  const latestItem = { ...listItem, chunk_id: 'chunk-latest', content_preview: 'latest' }
  let calls = 0
  const model = useChunkFeedbackGovernance({
    kbId: 'kb-1',
    api: {
      list: async () => {
        calls += 1
        if (calls === 1) return firstResponse.promise
        return { data: { total: 1, page: 1, page_size: 20, data: [latestItem] } }
      },
      detail: async () => ({ data: detail }),
      logs: async () => ({ data: { total: 0, page: 1, page_size: 20, data: [] } }),
      reset: async () => ({ data: detail }),
    },
  })

  const olderLoad = model.loadList()
  model.filters.feedbackStatus = 'high'
  assert.equal(await model.loadList(), true)
  firstResponse.resolve({ data: { total: 1, page: 1, page_size: 20, data: [listItem] } })
  assert.equal(await olderLoad, true)
  assert.deepEqual(model.items.value, [latestItem])
})

test('older detail and log responses cannot overwrite the current chunk', async () => {
  const firstDetail = deferred<{ data: ChunkFeedbackDetail }>()
  const firstLogs = deferred<{ data: { total: number; page: number; page_size: number; data: ChunkFeedbackWeightLog[] } }>()
  const detailTwo = { ...detail, chunk_id: 'chunk-2', content: 'second chunk' }
  const logTwo = { ...log, id: 'log-2', source_action: 'like' }
  let detailCalls = 0
  let logCalls = 0
  const model = useChunkFeedbackGovernance({
    kbId: 'kb-1',
    api: {
      list: async () => ({ data: { total: 0, page: 1, page_size: 20, data: [] } }),
      detail: async () => {
        detailCalls += 1
        return detailCalls === 1 ? firstDetail.promise : { data: detailTwo }
      },
      logs: async (_kbID, chunkID, page) => {
        logCalls += 1
        if (logCalls === 1 && chunkID === 'chunk-2') return firstLogs.promise
        return { data: { total: 1, page: page || 1, page_size: 20, data: [logTwo] } }
      },
      reset: async () => ({ data: detailTwo }),
    },
  })

  const olderDetail = model.openDetail('chunk-1')
  const latestDetail = model.openDetail('chunk-2')
  firstDetail.resolve({ data: detail })
  firstLogs.resolve({ data: { total: 1, page: 1, page_size: 20, data: [log] } })
  assert.equal(await olderDetail, true)
  assert.equal(await latestDetail, true)
  assert.equal(model.selected.value?.chunk_id, 'chunk-2')
  assert.deepEqual(model.logs.value, [log])

  const olderLogs = deferred<{ data: { total: number; page: number; page_size: number; data: ChunkFeedbackWeightLog[] } }>()
  let paginationCalls = 0
  const paginationModel = useChunkFeedbackGovernance({
    kbId: 'kb-1',
    api: {
      list: async () => ({ data: { total: 0, page: 1, page_size: 20, data: [] } }),
      detail: async () => ({ data: detail }),
      logs: async (_kbID, _chunkID, page) => {
        paginationCalls += 1
        if (paginationCalls === 1) return olderLogs.promise
        return { data: { total: 1, page: page || 1, page_size: 20, data: [logTwo] } }
      },
      reset: async () => ({ data: detail }),
    },
  })
  paginationModel.selected.value = detail
  const olderLogLoad = paginationModel.loadLogs(1)
  assert.equal(await paginationModel.loadLogs(2), true)
  olderLogs.resolve({ data: { total: 1, page: 1, page_size: 20, data: [log] } })
  assert.equal(await olderLogLoad, true)
  assert.equal(paginationModel.logPage.value, 2)
  assert.deepEqual(paginationModel.logs.value, [logTwo])
})

test('reset reports a partial refresh failure without losing the successful reset', async () => {
  const resetDetail = { ...detail, like_count: 0, dislike_count: 0, positive_rate: null, recall_weight: 1 }
  const model = useChunkFeedbackGovernance({
    kbId: 'kb-1',
    api: {
      list: async () => { throw new Error('list refresh unavailable') },
      detail: async () => ({ data: detail }),
      logs: async () => ({ data: { total: 0, page: 1, page_size: 20, data: [] } }),
      reset: async () => ({ data: resetDetail }),
    },
  })
  model.selected.value = detail

  assert.equal(await model.resetSelected(), true)
  assert.equal(model.selected.value?.like_count, 0)
  assert.equal(model.resetRefreshFailed.value, true)
  assert.match(String(model.listError.value), /list refresh unavailable/)
})

test('switching knowledge bases clears state and rejects an older list response', async () => {
  const kbId = ref('kb-a')
  const listA = deferred<{ data: { total: number; page: number; page_size: number; data: ChunkFeedbackListItem[] } }>()
  const itemB = { ...listItem, chunk_id: 'chunk-b', knowledge_title: 'KB B' }
  const model = useChunkFeedbackGovernance({
    kbId,
    api: {
      list: async (targetKBID) => targetKBID === 'kb-a'
        ? listA.promise
        : { data: { total: 1, page: 1, page_size: 20, data: [itemB] } },
      detail: async () => ({ data: detail }),
      logs: async () => ({ data: { total: 0, page: 1, page_size: 20, data: [] } }),
      reset: async () => ({ data: detail }),
    },
  })
  model.items.value = [listItem]
  model.total.value = 1
  model.page.value = 3
  model.detailVisible.value = true
  model.selected.value = detail
  model.logs.value = [log]
  model.logPage.value = 2
  model.listError.value = new Error('old list error')
  model.detailError.value = new Error('old detail error')
  model.resetRefreshFailed.value = true

  const loadA = model.loadList()
  kbId.value = 'kb-b'

  assert.deepEqual(model.items.value, [])
  assert.equal(model.total.value, 0)
  assert.equal(model.page.value, 1)
  assert.equal(model.detailVisible.value, false)
  assert.equal(model.selected.value, null)
  assert.deepEqual(model.logs.value, [])
  assert.equal(model.logPage.value, 1)
  assert.equal(model.listError.value, null)
  assert.equal(model.detailError.value, null)
  assert.equal(model.resetRefreshFailed.value, false)

  assert.equal(await model.loadList(), true)
  listA.resolve({ data: { total: 1, page: 3, page_size: 20, data: [listItem] } })
  assert.equal(await loadA, true)
  assert.deepEqual(model.items.value, [itemB])
  assert.equal(model.total.value, 1)
})

test('detail and log responses from an old knowledge base cannot reopen the drawer', async () => {
  const kbId = ref('kb-a')
  const logsA = deferred<{
    data: { total: number; page: number; page_size: number; data: ChunkFeedbackWeightLog[] }
  }>()
  const logsAStarted = deferred<void>()
  const detailB = { ...detail, chunk_id: 'chunk-b', knowledge_title: 'KB B' }
  const logB = { ...log, id: 'log-b', source_action: 'like' }
  const model = useChunkFeedbackGovernance({
    kbId,
    api: {
      list: async () => ({ data: { total: 0, page: 1, page_size: 20, data: [] } }),
      detail: async (targetKBID) => ({ data: targetKBID === 'kb-a' ? detail : detailB }),
      logs: async (targetKBID) => {
        if (targetKBID === 'kb-a') {
          logsAStarted.resolve()
          return logsA.promise
        }
        return { data: { total: 1, page: 1, page_size: 20, data: [logB] } }
      },
      reset: async () => ({ data: detailB }),
    },
  })

  const openA = model.openDetail('chunk-1')
  await logsAStarted.promise
  kbId.value = 'kb-b'
  assert.equal(model.detailVisible.value, false)
  assert.equal(model.selected.value, null)
  assert.deepEqual(model.logs.value, [])

  assert.equal(await model.openDetail('chunk-b'), true)
  logsA.resolve({ data: { total: 1, page: 1, page_size: 20, data: [log] } })
  assert.equal(await openA, true)

  assert.equal(model.detailVisible.value, true)
  assert.equal((model.selected.value as ChunkFeedbackDetail | null)?.chunk_id, 'chunk-b')
  assert.deepEqual(model.logs.value, [logB])
})

test('a reset response from an old knowledge base cannot mutate or refresh the new one', async () => {
  const kbId = ref('kb-a')
  const resetA = deferred<{ data: ChunkFeedbackDetail }>()
  const detailB = { ...detail, chunk_id: 'chunk-b', knowledge_title: 'KB B' }
  const resetDetailA = {
    ...detail,
    like_count: 0,
    dislike_count: 0,
    positive_rate: null,
    recall_weight: 1,
  }
  let listCalls = 0
  let logCalls = 0
  const model = useChunkFeedbackGovernance({
    kbId,
    api: {
      list: async () => {
        listCalls += 1
        return { data: { total: 0, page: 1, page_size: 20, data: [] } }
      },
      detail: async () => ({ data: detailB }),
      logs: async () => {
        logCalls += 1
        return { data: { total: 0, page: 1, page_size: 20, data: [] } }
      },
      reset: async (targetKBID) => {
        assert.equal(targetKBID, 'kb-a')
        return resetA.promise
      },
    },
  })
  model.detailVisible.value = true
  model.selected.value = detail

  const pendingReset = model.resetSelected('reviewed')
  kbId.value = 'kb-b'
  model.detailVisible.value = true
  model.selected.value = detailB
  resetA.resolve({ data: resetDetailA })

  assert.equal(await pendingReset, true)
  assert.equal(model.selected.value?.chunk_id, 'chunk-b')
  assert.equal(model.resetting.value, false)
  assert.equal(model.detailError.value, null)
  assert.equal(model.resetRefreshFailed.value, false)
  assert.equal(listCalls, 0)
  assert.equal(logCalls, 0)
})
