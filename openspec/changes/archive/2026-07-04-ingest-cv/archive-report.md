# Archive Report — ingest-cv

**Archived:** 2026-07-04 · **Verdict:** shipped (verified empirically, no formal verify run)

## Cycle

- Planning + apply completed in June 2026 (see apply-progress.md); feature has been in production since.
- No formal `sdd-verify` was run. Closure evidence instead: live end-to-end verification on 2026-07-03 — real CV ingested via the qwen provider path (run `d4453354`, `users.cv_markdown` 8658 chars, structured `profile_json`, no parse_error), after fixing a stale worker image. Worker unit tests green.
- Post-ship fixes that touched this flow landed via TBD: EVALUATOR provider routing for ingestCV (#44 / commit 548b78e), pg-boss v10 batch unwrap (#42).

## Spec promotion

Delta spec merged as canonical `openspec/specs/ingest-cv/spec.md` (new capability — no prior spec existed).

## Known follow-ons

- Ingested CV is invisible in the UI — tracked as issue #45 (`GET /api/me/cv` + page).
- `handleIngestCV` swallows LLM errors (WS notify only, nothing in logs) — debugging gap noted in engram.
- Refactor backlog: issue #29 (DDD-lite mirror of evaluate-job).
