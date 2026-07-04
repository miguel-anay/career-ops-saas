# Design: Evaluation Quality

## Technical Approach

Four independent edits, zero new deps/infra/schema. Go gains two content
guards that return 422 before any enqueue (0 tokens burned). The worker flips
`blocks_json` to the array shape the web already expects, and enriches
prompt text only. Web surfaces the two 422 codes as actionable states. Every
change reuses an existing pattern already in the repo.

## Architecture Decisions

### Decision 1 — Go content guards, no new sqlc query

**Choice**: Inside the existing `WithTenantTx` in `EnqueueEvaluation`
(`service.go:53`), after the ownership check, guard both inputs using queries
that already exist: `GetJobByID` (already called — `job.ScrapedContent` is
`sql.NullString`) and a new `GetUserByID` call (exists, used by emailingest —
`user.CvMarkdown` is `sql.NullString`). Add typed errors `ErrCVMissing` and
`ErrJobContentMissing`. Guard order: CV first (always), then JD. Empty test =
`!x.Valid || strings.TrimSpace(x.String) == ""`.

| Option | Tradeoff | Decision |
|--------|----------|----------|
| Reuse GetJobByID + GetUserByID | 1 extra read, no `sqlc generate` | **Chosen** |
| New `GetEvalPreconditions` query | Fewer round-trips, regen + churn | Rejected — premature |
| Guard in worker | Tokens already committed at enqueue | Rejected — defeats 0-token goal |

Handler maps both to **422** with machine-readable codes `cv_missing` /
`job_content_missing`, mirroring the `ErrGmailNotConnected` switch
(`handler.go:54-61`).

### Decision 2 — `blocks_json` array flip in the parser

**Choice**: Transform in `EvaluationParser.parse` (`EvaluationParser.mjs:46`),
not the repo write. Build an **array of `{label, content}` sorted A→G** by the
captured block letter, instead of the keyed `{blockA:{title,content,score}}`
object. `Evaluation.fromBlocks` stays a dumb container (arrays pass its
non-empty `Object.keys` invariant); the repo's 5-call `save()` is untouched —
`JSON.stringify(evaluation.blocks)` now serializes an array.

| Option | Tradeoff | Decision |
|--------|----------|----------|
| Transform in parser | Ordering owned where letters are known | **Chosen** |
| Transform in repo write | Domain stays object, repo does view logic | Rejected — shape belongs to parse |
| Change Evaluation container | Ripples through domain tests | Rejected — no need |

**Ordering A→G**: `BLOCK_PATTERN` matches in document order; the LLM may
reorder, so collect into a letter-keyed map then emit `A..G` in fixed order.
`label` = parsed block title; web falls back to `BLOCK_LABELS[letter]` by index.
`parseError` keeps its `{parse_error, raw}` object — web's
`blocks_json?.length > 0` guard is falsy for it and correctly falls back to
`content_md`.

**Migration**: none. Existing object-shaped rows have no `.length` → web
already falls back to `content_md` (degraded, not broken). Re-evaluation
DELETE+INSERTs the report (new save shape), upgrading rows on demand.

### Decision 3 — Prompt enrichment, provider-agnostic text only

**Choice**: Add `received_at` to the job SELECT in `prompt.mjs:31`. Inject
posting age (`now − received_at`, in days) as a Block-G data point in
`outputContract`. Add STAR-mapping + negotiation guidance sentences to
`staticSystemPrompt`. A→G schema and the two-block cache structure unchanged.
Pure text, so it flows identically through `openai-compat.mjs`
(EVALUATOR=qwen) and Anthropic — no provider branching.

The Go guard makes `prompt.mjs`'s `|| ''` fallbacks dead code, but they stay
as harmless defense-in-depth.

### Decision 4 — Web 422 surfacing via typed ApiError

**Choice**: In `lib/api.ts` throw an `ApiError { status, code }` (parse the
JSON body's `code` best-effort) instead of the current generic
`Error("API error …")`. `handleEvaluate` inspects `err.code` and sets an
`evalError` state; render two panels: **cv_missing** → "Upload your CV first"
copy + placeholder link (deep-links to issue #45 once built);
**job_content_missing** → "No readable job description yet" actionable copy.
Other callers keep working (they ignore `.code`).

## Data Flow

    POST /evaluate ─▶ EnqueueEvaluation (WithTenantTx)
                        │  GetJobByID  → JD empty?  ─▶ ErrJobContentMissing
                        │  GetUserByID → CV empty?  ─▶ ErrCVMissing
                        ▼ (both present)
                     queue.Enqueue ─▶ worker evaluate-job
                                        │ prompt.mjs (+age,+STAR/nego)
                                        ▼ LLM ─▶ EvaluationParser (→ A→G array)
                                                 ─▶ PgEvaluationRepository.save
    handler err ─▶ 422 {code} ─▶ ApiError ─▶ page state (cv/JD panels)

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `api/internal/evaluate/service.go` | Modify | `ErrCVMissing`/`ErrJobContentMissing`; guards + `GetUserByID` in tx |
| `api/internal/evaluate/handler.go` | Modify | Map both errors → 422 with codes |
| `worker/domain/EvaluationParser.mjs` | Modify | Build A→G `{label,content}` array |
| `worker/lib/prompt.mjs` | Modify | `received_at` SELECT + age + STAR/nego text |
| `web/lib/api.ts` | Modify | `ApiError{status,code}` |
| `web/app/jobs/[id]/page.tsx` | Modify | Two 422 states |

## Testing Strategy

| Layer | What | Approach |
|-------|------|----------|
| Go unit | 422 on empty CV / empty JD, 0 enqueue | `testify/mock`, `SetUserID` |
| Worker unit | parser emits array sorted A→G; parseError → content_md fallback | update `evaluation-parser` + `pg-evaluation-repository` tests |
| Web unit | 422 code → correct panel; array blocks render | vitest, existing array mock |

## Migration / Rollout

No schema migration. Per-PR revert: each of the four edits is an independent
commit. Likely two chained PRs (Go+worker, then web) to stay under 400 lines.

## Open Questions

- [ ] Keep per-block `score` in the array shape, or drop it (web ignores it)?
      Leaning drop (YAGNI) until a UI consumes it.
