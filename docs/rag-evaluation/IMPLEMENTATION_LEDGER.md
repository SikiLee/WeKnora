# RAG Evaluation MVP Implementation Ledger

## Repository baseline

- Branch: `codex/rag-evaluation-mvp`.
- Base HEAD: `7b2c9e73bf6950edde8165e4fe352714b7e5c16e`.
- Initial worktree: only `docs/rag-evaluation/` was untracked; no tracked user changes were modified or removed.
- Migration head before this work: PostgreSQL `000069_resource_registry`; SQLite `000000_init`.
- Toolchain observed on 2026-07-16: Go `1.26.1`, Node `24.14.1`, npm `11.11.0`.
- Docker Desktop daemon was unavailable (`dockerDesktopLinuxEngine` named pipe missing), so PostgreSQL, Redis, app services, and the supplied end-to-end baseline could not be re-run.

## Authoritative inputs

Implementation decisions use this priority:

1. Tencent project topic.
2. `CODEBASE_MAP.md` code facts, refreshed against the current worktree where exact interfaces are required.
3. `BASELINE_REPORT.md` runtime facts.
4. The seven specifications in this directory.
5. `PROJECT_RESEARCH_REVIEW.md` method guidance.

The development directive fixes the MVP boundary: read-only Recommendation only; no confirm/apply/reparse/index switch/rollback, no legacy Evaluation API migration, no automated three-low-run experiment creation, and no automatic 90/365-day cleanup executor.

## Current code facts verified

- The legacy Evaluation implementation is `internal/application/service/evaluation.go`; it stores tasks in a process-local map and starts work with a bare goroutine.
- The legacy HTTP route is `/api/v1/evaluation`; its handler uses the repository's existing `{success,data}` and error middleware conventions.
- GORM repositories use the shared `*gorm.DB`; SQL migrations are authoritative for PostgreSQL and SQLite.
- Asynq task types and queue topology are centralized in `internal/types/task.go`; Redis-disabled Lite mode uses `router.SyncTaskExecutor`.
- Existing scheduler code uses `robfig/cron/v3` with seconds enabled. Evaluation schedules must still persist a versioned run template and idempotent time slot.
- Existing `types.Chunk` and `types.SearchResult` carry execution `KnowledgeID`, `StartAt`, and `EndAt`, but not source-document hash or normalization version. Shadow experiments therefore require an explicit source/execution mapping snapshot.
- Existing Langfuse integration supports Trace, Span, and Generation ingestion; Score ingestion is absent.
- Current role/API-key symbols exist, but Evaluation subresources must reuse existing tenant/KB guards and default-deny scoped API keys until each route is explicitly mapped.

## Runtime baseline status

- The supplied baseline remains authoritative: parsing reached 96 in-memory chunks, then failed because no Embedding model was configured; no chunks/index were persisted and the document remained `processing`.
- The current environment cannot repeat the run because Docker is stopped. No retrieval, answer, citation, Judge, or Langfuse success is claimed.
- The baseline code path is fixed: when a KB requires embeddings and `GetEmbeddingModel` fails, processing persists `failed`, a visible error message, and `EMBEDDING_PROVIDER_FAIL` stage evidence instead of returning in `processing`.

## Implementation phases

| Phase | Status | Evidence |
|---|---|---|
| Repository and environment audit | completed | Branch/toolchain/migration/router/RBAC/Langfuse/source-field facts recorded above |
| Prototype A: stable Gold Evidence positions | completed for Markdown/plain text | `internal/rageval/source.go`; CRLF/LF, NFC, Chinese, emoji and round-trip tests |
| Prototype B: source/execution mapping | completed for direct normalized copy | Explicit mapping snapshot and refusal to guess after source/hash changes; backend-wide support remains Pending Verification |
| Prototype C: pointwise Judge calibration | completed as executable gate | Strict parser plus macro-F1/agreement/kappa/repeat/parser-failure gates; no pairwise path |
| Prototype D: persistent task recovery | in progress | Pure run/item terminal folding, no-resurrection rule and retry-scope tests pass; repository restart proof follows schema wiring |
| Schema and repositories | in progress | PostgreSQL 000070 and SQLite 000001 drafted; repository implementation follows |
| Services, HTTP API, and workers | pending | - |
| Metrics, diagnosis, experiments, Langfuse Score | in progress | Rule metrics, unanswerable abstention, versioned diagnosis, candidate restriction and recommendation guards implemented as pure domain code |
| Vue UI | pending | - |
| Integration/E2E and final audit | pending | - |

## Pending Verification

- Docker-backed PostgreSQL/Redis migrations and end-to-end runtime.
- Available Embedding, Chat, and Judge model credentials.
- Langfuse service/project credentials and UI trace links.
- Stable source normalization for document types other than the prototyped Markdown/plain-text path.
- Whether every configured vector backend preserves copied-document source mapping for shadow experiments.
- Provider-reported embedding/index cost fields.

## Deviations and decisions

- The specification files still contain items identified by the latest maintainer audit. Implementation follows the newer explicit directive: Draft Generation Profile Lock, unanswerable metric abstention, EvalScheduleSlot, conditional RunItem snapshots, pointwise-only calibration, deterministic sampling, valid-coverage guard, and sensitive-data defaults. Specs will be updated as documentation, not treated as proof that code exists.
- The PostgreSQL incremental migration is `000070`; SQLite uses its own `000001` track because its migrator selects `migrations/sqlite` and previously contained only `000000`.
- MVP hard run limit is 200 cases. An experiment has one baseline and at most two candidates.

## Test and runtime evidence

- `go test ./internal/rageval` - **passed**. Covers normalization/evidence round-trip, source mapping, six retrieval metrics, unanswerable abstention, strict pointwise Judge parsing/calibration, run state/retry scopes, diagnosis/candidate dimensions, tuning selection, and holdout recommendation guards.
- Python standard-library SQLite executed `000000_init.up.sql` then `000001_rag_evaluation_mvp.up.sql`: **passed**, 15 Evaluation tables created. The matching down migration removed all 15 tables. This is SQLite DDL syntax evidence only.
- `go test ./internal/application/service ./internal/handler ./internal/router ./internal/tracing/langfuse` - **build blocked before tests**. This checkout has `CGO_ENABLED=0` on Windows and no gcc/clang; `internal/utils/inject.go` references cgo-backed `pg_query.Parse/Deparse`, unavailable in the no-cgo build. Narrow pure packages remain testable.

## Commit ledger

No implementation commits yet.
