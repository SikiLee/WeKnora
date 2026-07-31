# Issue #1248 final UI evidence manifest

- Tested implementation SHA: `9e7a887a7a2ae22c46677f869705f44b1a7cd938`
- Environment: exact candidate backend and production frontend, ParadeDB/PostgreSQL 17, deterministic synthetic standard knowledge-base QA fixtures.
- Automated result: PASS. Feedback refresh restoration, dislike reason selection, governance pending filtering, scoped reset isolation, locale verification, and console/network assertions passed.

| File | SHA-256 | Assertion |
|---|---|---|
| `01-feedback-liked-restored.png` | `a768221fc05caf63b3597fafa008067421fa5691ac98b90060cb226b967a9451` | Liked state persists after a full-page refresh. |
| `02-dislike-reasons.png` | `f746aad5d8d5f0fb1ee270c0a216b967e0341b2b5d561af483726b4a4421db25` | Dislike exposes localized reason choices. |
| `03-governance-filtered-chunks.png` | `d44b2bed61c758b39039872b422cd8574b1b3751c90d8736222d5073f22177fa` | Owner low-quality filtering displays affected chunks and aggregate metrics. |
| `04-governance-detail-reset.png` | `cfa019bc5d628e15db1418e1587e4e62ecd91d47db15a9bc8689d02dfa0a2205` | Detail displays sessions, reason aggregation, audit provenance, and reset. |
| `05-reset-isolation.png` | `197b6ee3374e2db18a8ba6922a99e022314db1616c2e484984cbcfc728e0da12` | Reset removes only Chunk A while Chunk B remains affected. |
| `06-mobile-390-en-US.png` | `11917bb64cd8d8db6f059db08981ea028cdfccf692cf071ae9cd3518ffd001fb` | `en-US` reason menu was exercised at 390 px. |
| `results.json` | `1ae87c69243f11093924d0dcb22534640ad522d4c1eca1f212a4fd6bac6baa6e` | Machine-readable scenario results and tested SHA. |

## Browser findings

- No unexpected browser errors were observed. Known deterministic-fixture baseline responses were classified separately: configured-login auto-setup 403, synthetic suggestions 404, and synthetic document-preview 500.
- The 390 px run found `document/body scrollWidth=600`. This is caused by Tencent `main`'s pre-existing `.main { min-width: 600px; }` in `frontend/src/views/platform/index.vue`, which is outside this PR's diff. The feedback toolbar remained within its standard-QA stream; the whole-platform mobile layout limitation is recorded as an upstream baseline limitation, not represented as a PR pass.
