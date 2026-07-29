# Issue #1248: minimal chunk feedback design

Status: accepted for implementation
Baseline: `4f7a36c7e607bcb21e265617bd80cfb98348fe22`

## Sources and acceptance matrix

This design is derived only from Tencent/WeKnora Issue #1248, the fixed
baseline above, and ordinary transactional database invariants. Candidate pull
requests are not design inputs.

| Issue requirement | Baseline integration point | Planned proof |
| --- | --- | --- |
| Persist answer-to-chunk attribution | `handler/session/qa.go:completeAssistantMessage`, `types.Message.KnowledgeReferences` | repository transaction tests |
| Like, dislike, switch, and clear | one idempotent message-feedback service/API | state-machine tests |
| Update every referenced chunk atomically | feedback repository transaction | rollback and concurrency tests |
| Expose chunk aggregates | existing chunk DTO/list query | repository and handler tests |
| Persist and apply recall weight | chunk projection; immediately before standard-QA final selection | ranking regression tests |
| Flag low-quality chunks | query-time `needs_optimization` from the one configured threshold | DTO/query tests |
| Reset one chunk and retain audit history | logical reset revision plus minimal audit table | reset/revision tests |
| Reverse aggregates on message/session deletion | feedback-aware delete transaction used by every deletion entry | lifecycle tests |

Agent chat, historical attribution guesses, lazy backfill, policy platforms,
workers, event sourcing, free-text reasons, bulk reset, export, and generic
retrieval/RBAC/UI framework changes are non-goals.

## ADR-1: answer completion and attribution

The standard QA handler creates an incomplete assistant message and receives
the final server-owned `KnowledgeReferences` before
`completeAssistantMessage`. The existing method updates `messages` and starts
indexing/suggestion goroutines even if that update fails.

`MessageService.CompleteAssistantMessageWithReferences` will replace that
standard-QA completion write. One repository transaction will:

1. lock the session-scoped message;
2. verify assistant role and incomplete/idempotent state;
3. resolve referenced IDs against live chunks, including parent/sub-chunk
   ownership, excluding web/history/non-KB references; shared-KB chunks retain
   their explicit owner tenant rather than being rewritten to the actor tenant;
4. deduplicate and sort `(chunk_tenant_id, chunk_id)`;
5. insert immutable `message_chunk_references`;
6. mark the message complete with its final content and references.

An already completed message is accepted only when its persisted reference set
is identical. It is never replaced in place. The handler emits success and
starts chat-history indexing or suggestions only after commit. A failed commit
leaves the message incomplete and starts no dependent work.

## ADR-2: deterministic single-chunk reset

Time is not an ordering primitive. Each feedback has a non-negative
`feedback_revision`; each chunk has `feedback_reset_revision`.

Feedback contributes to a chunk only when:

`message_feedbacks.feedback_revision > chunks.feedback_reset_revision`.

Reset locks the target chunk, advances its baseline to the maximum revision of
currently related feedback (without decreasing the existing baseline), clears
the aggregate projection, restores weight `1.0`, and writes an audit row in the
same transaction. A later identical user choice is a reconfirmation and gets a
revision greater than every referenced chunk baseline.

## ADR-3: audit

The baseline `audit_logs` repository always uses its own DB handle; it cannot
join the feedback transaction, does not model old/new weight, and cannot
separate actor tenant from chunk-owner tenant. It therefore does not meet the
transactional requirement.

A third, deliberately narrow `chunk_feedback_audits` table is justified. It
stores only chunk owner tenant/id, actor tenant/user, action, old/new weight,
trigger source, and creation time. Allowed actions are
`feedback_weight_changed` and `feedback_reset`; `trigger_source` records the
typed initiating operation without inferring it from the weight delta. Legacy
rows default to `legacy`. It has no generic metadata, update, or platform API.

## ADR-4: standard retrieval integration

For standard knowledge QA, model reranking occurs in
`chat_pipeline/rerank.go`; deterministic final truncation occurs in
`chat_pipeline/filter_top_k.go`. MMR currently narrows reranked candidates to
`RerankTopK` before the final filter.

The existing batched chunk load carries persisted weights into the
server-owned candidates. A local effective score (`base score * weight`) is
used once for final selection; `SearchResult.Score` is not mutated. Existing
relevance thresholds still run before weighting, so a low-relevance result
cannot cross them using weight alone. Deterministic baseline tie-breakers are
retained.

Score provenance is indivisible: parent enrichment and merged sequential
chunks carry the `RecallWeight`, match type, matched content, and score metadata
from the same result whose raw score wins. Equal scores retain the existing
stable representative.

The candidate window is not multiplied by a fixed factor. The rerank stage will
retain an additive bounded reserve only when candidates exist beyond
`RerankTopK`; the reserve is capped by the already-reranked set and the existing
hard limits. With all weights at `1.0`, the selected order remains the baseline
order. No rerank call count changes and no generic RRF/MMR/dedup framework is
introduced.

## Data model

- `message_feedbacks`: current `(tenant, user, message)` choice, optional fixed
  dislike reason, and logical revision.
- `message_chunk_references`: immutable completed-message attribution with
  separate message and chunk tenant IDs.
- `chunk_feedback_audits`: the narrow transactional audit described above.
- `chunks`: `like_count`, `dislike_count`, nullable `positive_rate`,
  `recall_weight`, and `feedback_reset_revision`.

`needs_optimization`, session count, and reason aggregates are computed on
read. Only the feedback repository writes the five chunk feedback projection
columns. Ordinary chunk saves use an explicit content-field allowlist.

## API and permission matrix

| Operation | API | Server authorization |
| --- | --- | --- |
| Read feedback state | existing message DTO | session owner; web user only for `my_feedback` |
| Set/clear personal feedback | `PUT /sessions/:session_id/messages/:message_id/feedback` | web-user principal, visible owned session, completed assistant, trusted references |
| Read chunk statistics | existing chunk list/detail DTO | existing KB read guard |
| Reset chunk | `POST /knowledge-bases/:kb_id/chunks/:chunk_id/feedback/reset` | JWT KB creator or Admin+, existing KB write guard |

Clients never supply chunk IDs to the personal-feedback API. Every chunk update
uses both owner tenant ID and chunk ID. Shared-KB feedback keeps actor tenant
and chunk-owner tenant distinct.

## Transaction and lock order

Feedback application locks: message, sorted referenced chunks, then the
caller's unique feedback row. Reset and deletion lock chunks in the same
`(tenant_id, id)` order. Projection recomputation and audit inserts occur before
commit. A failure rolls back the choice, every aggregate, every weight, and
every audit row.

Message deletion, session clear, single/batch/all-session deletion use the same
feedback-aware repository transaction before non-critical external cleanup.
Deleting references and feedback precedes projection recomputation; reset
baselines never move backward.

Chunk deletion similarly locks target chunks in stable ID order and physically
removes their immutable attribution rows in the same transaction as the Chunk
soft delete. Feedback tolerates and transactionally removes legacy stale
references, but still propagates every database error. With no surviving
attributable chunk it commits no feedback fact and returns the existing
not-eligible domain error.

## Scope budget

The implementation is limited to three small domain tables, two write APIs, no
new read API unless the existing chunk DTO proves insufficient, at most two
business Vue components, and local edits to existing completion, deletion,
chunk-list, and final-selection paths. If the production/migration/frontend
diff must exceed 50 files or any lifecycle entry cannot use the common
transaction, implementation stops for scope review.
