# Issue #1248 UI evidence

These screenshots were generated from and are bound to implementation commit:

`dca1ca3a74fd4d957e3d892c75024abf2b0fb92c`

The environment used the exact candidate backend and production frontend with ParadeDB/PostgreSQL 17 and deterministic synthetic standard knowledge-base QA fixtures.

| Screenshot | Verified behavior |
|---|---|
| `01-feedback-liked-restored.png` | Liked state persists after full-page refresh. |
| `02-dislike-reasons.png` | Dislike exposes localized reason choices. |
| `03-governance-filtered-chunks.png` | Owner low-quality filtering displays the affected chunks and aggregate metrics. |
| `04-governance-detail-reset.png` | Detail displays sessions, reason aggregation, audit provenance, and reset. |
| `05-reset-isolation.png` | Reset removes only Chunk A; Chunk B remains affected. |
| `06-mobile-390-en-US.png` | The reason menu is complete at 390px in `en-US`. |

This evidence branch is isolated from the implementation branch and contains no credentials, logs, dumps, environment files, or application source.
