# Proposal: `evaluation-personalization`

## Problem

The `career-ops` reference CLI evaluates jobs against a rich `_profile.md` that
carries per-user **Scoring Adjustments** (Boost/Penalize) and a **Location/posting
policy** that can hard-SKIP an evaluation entirely. Exploration confirmed these
are not decorative: `_shared.md`'s ALWAYS rules force `_profile.md` to be read
before every evaluation, "North Star alignment" is one of the six scored
dimensions, and a real generated report (`reports/041-ntt-data-2026-06-22.md`)
shows the SKIP tier firing for real and aborting a full A–G evaluation before any
tokens were spent.

The SaaS evaluator today has no equivalent. A user can store a `narrative` and
`target_roles`, but there is no way to tell the evaluator "boost roles that match
X", "penalize Y", or "don't even evaluate postings older than N days." The felt
gap is twofold:

1. **Quality**: evaluations don't reflect the user's own boost/penalize
   priorities, so the holistic score drifts from what the candidate actually cares
   about.
2. **Cost**: every posting burns LLM tokens even when the user would reject it
   on a deterministic rule (stale posting) they could have declared up front —
   exactly the class of waste the already-shipped `evaluation-input-guards`
   capability was built to prevent for missing CV / job content.

Now is the right time because the plumbing already exists: `profile_overrides` is
jsonb, `mergeProfile` is key-agnostic on both the Go and JS sides, and the
pre-enqueue 422 guard pattern (`ErrCVMissing`/`ErrJobContentMissing`) is an
established, tested template. This change replicates a **proven** CLI mechanism
over existing rails, not a speculative new one.

## Scope — capabilities delivered

1. **`scoring_rules` profile key.** Add `scoring_rules` to the profile
   allowlist (`api/internal/profile/service.go`'s `allowedFieldPaths`). Shape:
   `{ boost: [{condition}], penalize: [{condition}], max_posting_age_days: number|null }`.
   `condition` is **free-text prose**, not a structured predicate — the reference
   CLI hands prose to its LLM and lets it judge; a rule-matching engine here would
   be more mechanism than what we're replicating (explore Option A). No DB
   migration: the key flows through the existing merge/read/write path untouched.

2. **Narrative boost/penalize application.** The static system prompt block in
   `worker/lib/prompt.mjs` (`staticSystemPrompt`) gains a short instruction telling
   the LLM to apply `scoring_rules.boost`/`.penalize` narratively when present,
   mirroring how `_shared.md` frames it. This edits the STATIC cache block (a
   one-time cache invalidation), consistent with prior prompt-enrichment changes
   (`evaluation-quality`'s STAR/negotiation guidance) that already touched this
   same block.

3. **Posting-age pre-enqueue guard.** New sentinel `ErrStalePosting` in
   `api/internal/evaluate/service.go`, mirroring the exact `ErrCVMissing` /
   `ErrJobContentMissing` pattern (same file, checked before the usage-limit check,
   inside the same `WithTenantTx`). It compares `jobs.received_at` against the
   user's `scoring_rules.max_posting_age_days`. If that field is null/absent the
   guard is a no-op and evaluation proceeds exactly as today. On trip: HTTP `422`
   with code `stale_posting`, zero tokens spent — matching the existing 422
   contract in `openspec/specs/evaluation-input-guards/spec.md`. The corresponding
   `case errors.Is(err, ErrStalePosting):` → 422 mapping is added to
   `api/internal/evaluate/handler.go`.

4. **Web edit affordance.** The `/perfil` page's `ProfileEditForm` gains
   `scoring_rules` as a 7th editable field-path, same plain-textarea pattern as
   the existing six. No new component.

## Non-goals (deferred, with reasoning)

- **Adaptive Framing as a dedicated key** — explore found it already works today
  by nesting inside the existing `narrative` key (the allowlist gates by top-level
  key name, not internal shape). No engineering needed; revisit only if nesting
  proves insufficient.
- **Exit Narrative** — same reasoning: already expressible via `narrative` today.
  Purely a documentation/UX note, not a code change.
- **Negotiation Scripts** — weakest evidence of systematic pipeline consumption
  (not in `_shared.md`'s ALWAYS list, absent from 40+ inspected reports). It reads
  as candidate-facing reference material for a live negotiation, a different future
  surface (chat/apply-assist), not the evaluator.
- **Location-based Go-side SKIP** — no structured `location` column exists on
  `jobs` (only free-text `scraped_content`). A reliable gate would require touching
  every ATS provider's scraping code — a separate, much larger change. Location
  stays inside the LLM's narrative judgment (Block E) via `scoring_rules` prose.
- **Any structured rule-predicate engine/DSL** for `condition` matching —
  explicitly rejected by explore; more mechanism than the CLI itself uses.
- **Conversational/chat profile editor** (`conversational-profile-editor`) —
  a separate already-identified future slice, unrelated here.

## Technical approach (summary)

Option A from exploration: extend the existing merged-profile JSON that already
feeds `cvAndProfileBlock`, gated by one new allowlist entry, and let the LLM apply
the rules narratively — the same way the reference CLI does. `mergeProfile` needs
no changes because it is key-agnostic. The only deterministic, code-side piece is
the posting-age guard, which reuses the proven pre-enqueue sentinel/422 pattern
and the already-present `jobs.received_at` column.

Every new Go error path follows this project's `errors.Is` + sentinel convention
(as used in `cv`, `profile`, `digest`). Exact Go signatures, exact prompt text,
and exact web field wiring are deferred to `sdd-design`.

## Risks

- **Free-text `condition` is unverifiable.** Boost/penalize application is as
  untestable as the reference CLI's own — an accepted tradeoff of replicating its
  approach, not a new risk. We can test the guard and the allowlist, not the LLM's
  narrative judgment.
- **Posting-age guard overlaps Block G.** The deterministic
  `max_posting_age_days` gate conceptually overlaps the existing qualitative
  staleness signal in Block G (Posting Legitimacy). Frame it as a **hard opt-in
  per-user gate** (skips the LLM call entirely, saves tokens) vs. Block G's softer
  legitimacy tiering (still runs for everything not gated). Only trips when the
  user explicitly sets the field.
- **The `narrative` key already sidesteps the allowlist.** Because the allowlist
  gates on top-level key names and not internal shape, a user editing `narrative`
  freely can already inject Adaptive Framing / Exit Narrative content. Worth
  flagging to the product owner: the allowlist is a weaker boundary than it looks.

## Rollback plan

Low blast radius, revertible in isolation:
- Remove the `scoring_rules` allowlist entry → the key is silently dropped on
  write and ignored on read; existing profiles with the key stored are inert.
- Revert the `staticSystemPrompt` instruction → one-time cache re-warm, no data
  change.
- Remove `ErrStalePosting` guard + handler case → evaluation reverts to today's
  behavior (no posting-age gate). No migration to undo (jsonb, no schema change).
- Revert the web textarea → the other six fields are unaffected.

## Success criteria

- A user can set `scoring_rules` via the profile override endpoint and the web
  `/perfil` form; the key persists and round-trips through merge/read.
- Evaluations for users with `scoring_rules.boost`/`.penalize` reflect those
  priorities narratively in the generated report.
- `POST /api/jobs/{id}/evaluate` returns `422 stale_posting` with zero tokens
  spent when `jobs.received_at` exceeds the user's `max_posting_age_days`; when
  the field is null/absent, behavior is byte-for-byte unchanged from today.
- Existing `cv_missing` / `job_content_missing` / usage-limit precedence is
  preserved.
