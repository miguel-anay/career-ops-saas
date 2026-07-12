# Exploration: candidate-profile-kb — Persistent, Editable Candidate Profile with Interactive Learning

> **Revision note (post-explore, pre-proposal)**: after reviewing this exploration, the user clarified two points that change Part 2 below: (1) `cv_markdown` must stay a **single, ever-more-comprehensive** field — verified against the `career-ops` CLI, which has exactly one `cv.md` master document that every mode (`pdf.md`, `latex.md`, `apply.md`, `interview-prep.md`) reads and tailors output FROM, never multiple stored versions. Re-ingestion must therefore **merge** newly pasted text into the existing `cv_markdown` (add/update, never silently drop prior detail), not regenerate it from scratch. (2) Manual overrides to the derived profile (`target_roles`, `narrative`, etc.) must be **visible and reversible** in the UI — a "your manual edits" list where each override can be individually undone, reverting that field to CV-derived behavior. Both points are folded into Part 2 below; the original (incorrect) "multi-version CV corpus" idea from the first pass has been removed.

## Current State

### SaaS today (verified against code, not just the pre-supplied summary)

- **`users` table** (`db/schema.sql:32-43`): `cv_markdown text`, `profile_json jsonb NOT NULL DEFAULT '{}'`. RLS: `tenant_users` policy on `id = current_user_id` (`db/rls.sql:40-42`), `FORCE ROW LEVEL SECURITY` set.
- **Ingestion** (`worker/jobs/ingest-cv.mjs:90-94`): one `UPDATE users SET cv_markdown = $1, profile_json = $2::jsonb WHERE id = $3` — a **full overwrite** of both columns on every re-ingestion. `parseIngestResponse` never throws; on any parse failure it stores `{parse_error: true, raw}` into `profile_json`, so a bad Claude response can genuinely **wipe** a previously good `profile_json` (verified — no merge, no version check, no length/sanity guard before the UPDATE). This same overwrite pattern applies to `cv_markdown` today — pasting a shorter, job-tailored CV can silently drop detail (e.g. an older role) that a comprehensive master `cv.md`-style document should retain. This is the core bug the user wants fixed (see Revision note above).
- **`profile_json` shape today** (`worker/lib/ingest-prompt.mjs:16-22`):
  ```json
  { "candidate": {...}, "target_roles": { "primary": [...], "archetypes": [...] },
    "salary_target": {...}, "narrative": "..." }
  ```
  No `deal_breakers`, no `comp_targets` beyond a single min/max/currency, no location/timezone/visa fields, no proof-points/superpowers.
- **`UpdateUserProfileJSON`** (`db/queries/users.sql:23-27`) is a full-overwrite sqlc query. Confirmed **dead from the API's perspective** — no Go handler in `api/internal/cv` or elsewhere calls it. Only the worker writes `profile_json`.
- **No `GET /api/me/cv` or any profile-read endpoint exists.** `api/internal/cv/handler.go` only exposes: `POST/GET /api/jobs/{id}/cv` (PDF generation/download), `GET/POST /api/cvs` (a **separate**, already-RLS'd multi-CV table — see below), `POST /api/cv/ingest`, `GET /api/cv/ingest/{id}`. No web page renders `cv_markdown`/`profile_json` either.
- **Existing but unrelated `cvs` table** (`db/schema.sql:124-133`): `user_id, title, content_md, is_master`, RLS-protected (`tenant_cvs`), with `ListCVsByUser`/`InsertCV`/`SetMasterCV` already wired end-to-end in `api/internal/cv/service.go`. **Discovery/drift note**: this table is populated via the CV domain's CRUD but the evaluation prompt (`worker/lib/prompt.mjs:37-42`) reads `users.cv_markdown` directly — it never reads from `cvs`. This is pre-existing architectural drift, orthogonal to this feature, but structurally it's the closest analog for an `article_digests` table (see Part 4).
- **Evaluation prompt** (`worker/lib/prompt.mjs:33-60,107-115`): fetches `cv_markdown` + `profile_json` in one `tenantQuery`, JSON.stringifies `profile_json` raw into a **second cached system block** (`cache_control: ephemeral`), right after a static first system block. Any profile-shape redesign must keep this block a plain, directly-injectable JSON/text blob — not something requiring per-consumer unwrapping.
- **`applications` table** (`db/schema.sql:98-108`): `status app_status_t` (`Evaluated, Applied, Responded, Interview, Offer, Rejected, Discarded, SKIP`), `score double precision`, `notes text`, tied 1:1 to `jobs` via `job_id uuid UNIQUE`. This is the only outcome-signal source today. **Important gap**: there is no structured field today linking an application to which `target_roles.archetype` it was evaluated against. Any "learn from outcomes by archetype" mechanism needs this tag to exist first (see Part 3).

### CLI reference (`career-ops`, read-only) — data model it implies

| CLI file | Maps to | Key sub-structures |
|---|---|---|
| `config/profile.yml` | `profile_json.candidate` + `target_roles` + `salary_target` (existing) | `candidate.{full_name,email,phone,location,linkedin,portfolio_url,github,youtube,cip}`, `target_roles.primary[]`, `target_roles.archetypes[]{name,level,fit}`, `narrative.{headline,exit_story,superpowers[],proof_points[]{name,url,hero_metric}}`, `compensation.{target_range,currency,minimum,location_flexibility,notes}`, `location.{country,city,timezone,visa_status,onsite_availability}`, `languages[]`, `education[]` — **all of this is richer than the current `profile_json` shape**, especially `narrative.proof_points`/`superpowers` and `compensation.location_flexibility`.
| `modes/_profile.md` | **nothing exists yet** — this is the novel part | Explicitly documented (`_profile.md:3-11`): *"THIS FILE IS YOURS. It will NEVER be auto-updated."* Contains: archetype **priority table** (⭐ Alta/Media/Baja) with "adaptive framing" per archetype, hand-written **exit narrative** with hard "never say X" rules, **scoring adjustments** (`Boost +0.3` / `Penalizar -0.3` / auto-`SKIP` conditions), **negotiation scripts** (verbatim text to reuse), **location policy scoring rules**, and a dated free-text note. This is fundamentally **hand-curated, persistent, and deliberately never machine-overwritten** — a stronger guarantee than "manual field wins a merge," it's "this whole file is out of scope for any automation."
| `examples/article-digest-example.md` | **nothing exists yet** | Per-project entries: `## Project Name`, `**Hero metrics:**`, `**Architecture:**`, `**Key decisions:**` (bullet list), `**Proof points:**` (bullet list). Free-text markdown per entry, no strict schema.

**Takeaway for the data model**: the SaaS's current `profile_json` already covers `profile.yml` reasonably well (minus `proof_points`/`superpowers`/full `compensation`/`location` blocks). `_profile.md`'s content (scoring rules, negotiation scripts, adaptive framing, dated notes) is a different *kind* of data — narrative/rules text, not structured fields — and its **"never auto-updated" guarantee is the direct precedent for the merge/override problem in Part 2.** `article-digest.md` is a list of free-text entries, not a single blob — that shapes the table design in Part 4.

---

## Affected Areas

- `db/schema.sql`, `db/rls.sql`, `db/migrations/` — any new column/table needs RLS from day one (ADR-3).
- `db/queries/users.sql` — new merge-aware queries; `UpdateUserProfileJSON` likely gets replaced/retired.
- `worker/jobs/ingest-cv.mjs`, `worker/lib/ingest-prompt.mjs` — ingestion must stop clobbering manually-edited fields.
- `worker/lib/prompt.mjs` — evaluation prompt injection must consume the *effective* (merged) profile, not raw `profile_json`.
- `api/internal/cv/` (or a new domain package) — new read/write/edit endpoints.
- `web/` — currently has **zero** profile display/edit UI; this is greenfield on the frontend.
- `api/internal/tracker/service.go`, `db/schema.sql` (`applications`) — outcome-learning's data source; currently insufficient (no archetype tagging).

---

## Approaches

### Part 2 — Persistence / Merge Model

Two separate problems live here, confirmed against the CLI reference (see Revision note) — they need two different fixes, not one:

**Problem A: `cv_markdown` loses detail on re-ingestion.** The CLI's `cv.md` is a single, ever-growing master document — every mode reads it as-is and tailors output FROM it; there is no second copy or version history. The SaaS should work the same way: one `cv_markdown` field, but ingestion must **merge** rather than replace.

- **Fix**: change `worker/lib/ingest-prompt.mjs`'s prompt to include BOTH the existing `cv_markdown` (read first) and the newly pasted raw text, instructing Claude to produce an updated, comprehensive markdown that **adds new experience/roles/courses and updates changed details, without dropping anything the existing document already had** — the same discipline a person follows hand-editing `cv.md` to append a new job. `worker/jobs/ingest-cv.mjs`'s `UPDATE` stays a single-column overwrite mechanically (still one `UPDATE users SET cv_markdown = $1 ...`), but the VALUE being written is now a merge result, not a fresh regeneration. No schema change — `users.cv_markdown` stays exactly as it is today.
- Effort: **Low** — prompt + one extra read before the ingest job runs.

**Problem B: `profile_json` (derived fields: `target_roles`, `narrative`, `salary_target`, plus new `deal_breakers`/`comp_targets`) needs a way for manual edits to survive future CV re-ingestions, AND be visible/reversible.**

1. **`profile_overrides` jsonb column, merged over CV-derived data at read time**
   - `profile_json` keeps its current meaning: "latest CV-derived snapshot, regenerated by ingest from the now-merged `cv_markdown`." Add `profile_overrides jsonb NOT NULL DEFAULT '{}'`. One merge function — shallow, top-level-key overwrite — applied at read time by both consumers (worker's `buildEvaluationPrompt`, a new Go read endpoint): `effective_profile = { ...profile_json, ...profile_overrides }` per top-level key.
   - **Visibility requirement (from user feedback)**: `profile_overrides` can't be a black box. The profile page/chat sidebar must list each active override (e.g. "target_roles: removed 'Data Engineer'") with an **Undo** action that deletes that key from `profile_overrides` — after undo, the field reverts to whatever `profile_json` (CV-derived) currently says. This means `profile_overrides` needs per-field provenance (which top-level keys are overridden and why/when), not just a merged blob — a small metadata sibling (e.g. `profile_overrides_meta jsonb` recording `{ field, edited_at, note }` per overridden key, or reuse the `profile_edits` ledger from Part 3 as the single source of "what's overridden and why" instead of a separate meta column — see Part 3, this is the same infrastructure).
   - Pros: smallest schema change; `cv_markdown`'s ingestion fix (Problem A) is independent and doesn't need this column; manual edits are structurally safe from ingestion since they live in a different column; merge logic is small enough to duplicate in Go and JS without drift risk.
   - Cons: shallow merge means editing one archetype inside `target_roles.archetypes` requires overriding the *whole* `target_roles` key — acceptable for a first slice (chat proposes whole-field replacements, not surgical array-element patches, per Part 3's smallest-viable-slice).
   - Effort: **Low**.

2. **Per-field `{value, source: 'cv'|'manual', locked_at}` wrapper inside `profile_json` itself** — rejected, unchanged from first pass: invasive (every consumer must unwrap), no benefit over Approach 1 at the granularity actually needed.

3. **Split into normalized relational columns/child tables** — rejected, unchanged from first pass: premature, biggest migration, no current need for relational analytics.

**Recommendation (exploration-stage, not final)**: `cv_markdown` gets a prompt-level merge fix (Problem A, no schema change). `profile_json`'s derived fields get a `profile_overrides` column (Problem B), with the override ledger doubling as the visible/undoable list the user needs — this ledger is the same `profile_edits` table Part 3 needs anyway (see below), so Problem B's visibility requirement and Part 3's propose/confirm infrastructure are naturally one piece of work, not two.

### Part 3 — Interactive Conversation + Outcome-Learning

**UI shape (from user feedback)**: a persistent right-hand chat sidebar, present across the profile area (not a one-off modal) — always available to say things like "quitá 'Data Engineer' de mis roles" or "cambiá mi narrativa a X", with live preview of the resulting profile before confirming. This same sidebar is where the Part 2 "your manual edits" list lives — reviewing/undoing an existing override and proposing a new one are the same surface, just different actions on the same underlying table.

1. **`profile_edits` table — dual purpose: proposal staging AND active-override ledger**
   - `profile_edits(user_id, field_path, old_value, new_value, source: 'cv_ingest'|'manual'|'ai_suggestion'|'outcome_learning', status: 'proposed'|'accepted'|'rejected'|'undone', created_at, resolved_at)`.
   - A chat turn proposing a change inserts a `status: 'proposed'` row; the user confirming it flips it to `'accepted'` AND applies it into `profile_overrides` (Part 2). The **sidebar's "your manual edits" list is simply `SELECT * FROM profile_edits WHERE user_id = $1 AND status = 'accepted'`** — clicking Undo there flips the row to `'undone'` and removes that key from `profile_overrides`, letting the CV-derived value take over again. This single table satisfies both Part 2's visibility requirement and Part 3's propose/confirm flow — it is not two pieces of infrastructure, just one, used two ways.
   - Effort: **Low-Medium**.

2. **Chat-style endpoint using the existing async worker + WS pattern** (not a new synchronous path)
   - Hard constraint from `CLAUDE.md`: *"The Go API never calls Anthropic."* A chat feature needs low latency, but the only Anthropic-calling surface in this codebase is the worker, and the worker's only invocation pattern today is pg-boss enqueue → WS `notify` (used by `scan-company`, `evaluate-job`, `ingest-cv`). Recommend reusing that exact shape: each chat turn → enqueue a `profile-chat` job → worker calls Claude → WS notifies the response.
   - Claude's turn ends with a structured, delimited patch block (same pattern as `ingest-cv.mjs`'s `===CV_MARKDOWN===`/`===PROFILE_JSON===` split) — inserted as a `proposed` row in `profile_edits`, never applied directly. The user must explicitly confirm (e.g. `PATCH /api/me/profile-edits/{id}/accept`) before it's merged into `profile_overrides`.

3. **Automated pass over `applications` status changes → surfaces a suggestion (never auto-applies)**
   - Would query `applications` where `status IN ('Rejected','Discarded')` grouped by archetype over a window, and above a threshold insert a `profile_edits` row with `source: 'outcome_learning'`, surfaced through the *same* sidebar.
   - **Real blocker discovered**: the evaluation output today is prose (7-block A–G markdown) with no structured per-archetype tag. To group rejections "by archetype" reliably, `evaluate.mjs`'s output contract would need a small addition — e.g. `applications.matched_archetype text`, populated by the evaluator at scoring time.

4. **Are these complementary or alternatives?** Complementary. Conversation is the only path that *commits* a profile change; outcome-analysis is one of several *triggers* that seed a proposed suggestion into that same sidebar.

**Smallest viable vs. speculative**:
- **Smallest viable slice**: chat sidebar with propose→confirm→undo, built on `profile_overrides` (Part 2) + `profile_edits` (this section, item 1) + the `cv_markdown` merge fix (Part 2, Problem A). This satisfies "editable, persistent, not solely CV-derived, via conversation, with visible/reversible edits" — the full literal requirement, minus outcome-learning.
- **Speculative / defer**: outcome-learning triggers. Real upstream dependency (`matched_archetype` tagging doesn't exist), open UX questions, and with only a handful of applications per user today, insufficient signal volume for a meaningful suggestion yet. Strongest YAGNI candidate in the whole ask — flagged, not cut unilaterally.

### Part 4 — Article-Digest Equivalent

- **New table, modeled directly on the existing `cvs` table pattern**:
  ```sql
  CREATE TABLE article_digests (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title      text        NOT NULL,
    content_md text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
  );
  -- + tenant_article_digests RLS policy identical in shape to tenant_cvs
  ```
  No `is_master` — digest entries are additive/list, not single-active.
- **Ingestion**: manual paste/edit, same UX as `CreateCV` (`POST {title, content_md}`) — **not** an R2 file upload. Digest entries are small free-text markdown, cheaper to store and re-read directly from Postgres.
- **Worker consumption**: mirror the existing two-cached-block pattern in `buildEvaluationPrompt` — add a third cached system block built from a bounded `SELECT ... FROM article_digests WHERE user_id = $1 ORDER BY created_at DESC LIMIT N` (need a char/entry cap for Anthropic context limits).
- Where it lives in the Go API: leaning toward a small **new package** (e.g. `api/internal/digest`) to keep single-responsibility per this project's hexagonal-per-domain convention.

---

## Recommendation

**Persistence (Part 2)**: two independent fixes. (A) `cv_markdown` ingestion becomes a merge (existing + newly pasted text → comprehensive update), no schema change — fixes the "re-pasting a tailored CV loses detail" problem, matching how the CLI's single `cv.md` master document behaves. (B) `profile_overrides jsonb` column + shallow top-level-key merge for derived fields (`target_roles`, `narrative`, `salary_target`, `deal_breakers`, `comp_targets`), applied identically by the worker's prompt builder and a new Go read endpoint.

**Conversational editor + outcome-learning (Part 3)**: build the propose→confirm→undo chat sidebar on the existing async-worker + WS pattern (reuse, don't invent a new call path), backed by one `profile_edits` table that serves as both the proposal queue and the visible, undoable ledger of active overrides. Treat outcome-learning as a *trigger into* that same sidebar, not a separate automated writer — and treat it as genuinely deferred work pending an upstream `matched_archetype` tag and enough application volume to matter.

**Article-digest (Part 4)**: new `article_digests` table, directly reusing the `cvs` table's already-proven shape and RLS pattern — this is the lowest-risk, most mechanical piece of the whole ask.

---

## Scope / Slicing Recommendation (Part 5)

This is a 4-domain feature spanning DB + Go API + worker + web. Recommend **four sequenced changes, not one proposal**:

| # | Change | Scope | Depends on |
|---|---|---|---|
| 1 | **Profile persistence + read/edit API** | `cv_markdown` merge-on-ingest fix (Problem A, no schema change) + `profile_overrides` column + `profile_edits` ledger table (used from day one, not just by the future chat) + merge util (worker's prompt builder + new Go read endpoint) + `GET /api/me/profile` (closes issue #45, expanded to the merged view) + a basic `PATCH /api/me/profile` for manual top-level-key edits with a visible "your manual edits" list + Undo — a plain form UI, no chat yet, but the visibility/reversibility requirement is real from this slice, not deferred to #3. | none |
| 2 | **Article-digest** | New table + CRUD (new small Go package) + evaluation-prompt injection (3rd cached block). | none — can ship in parallel with or before #1; digests are additive, no merge conflict. |
| 3 | **Conversational profile editor** | New `profile-chat` worker job type + WS wiring (reuse `ingest-cv`'s notify pattern) + propose/confirm endpoints writing into the SAME `profile_edits` table from #1 (chat becomes a second *source* of proposed edits, `source: 'ai_suggestion'`, alongside #1's direct manual edits) + persistent right-side chat sidebar UI with live preview. | #1 (needs `profile_overrides` + `profile_edits` to write into); benefits from #2 existing but doesn't require it. |
| 4 | **Outcome-learning suggestions** (defer, revisit later) | `matched_archetype` tagging in `evaluate.mjs` + scheduled/triggered analysis over `applications` + suggestion injection into #3's sidebar. | #3 (needs the conversational surface to present suggestions) + real application-volume data to be meaningful. |

**Rationale**: each change is independently shippable and reviewable within a reasonable PR budget; #1 alone already satisfies the full literal user requirement for persistence ("editable and persistent, not solely CV-derived, visible and reversible") via a plain form instead of chat — #3 adds the conversational *input method* on top of infrastructure #1 already built, rather than #1 being an incomplete stub waiting on #3; #4 is explicitly not dropped from the roadmap, just sequenced last because it has a real unmet upstream dependency and no data to learn from yet.

---

## Risks

- **`profile_json` full-overwrite bug is live today**: a malformed Claude ingestion response can already silently null out a working profile (`parse_error: true` replaces the whole object) — this predates and motivates the persistence redesign, worth flagging as a pre-existing correctness gap, not something introduced by this feature.
- **CV merge quality depends entirely on the ingest prompt** (Part 2, Problem A): asking Claude to merge old + new CV text instead of regenerating from scratch is a prompt-engineering problem with a failure mode of its own — it could hallucinate a merge that duplicates a role under slightly different wording, or fail to recognize that new text supersedes (rather than adds to) an old entry (e.g. a promotion at the same company). This needs real test cases with actual multi-version CV text during design/apply, not just unit tests on the merge util.
- **Pre-existing drift**: the `cvs` table (multi-CV, RLS'd, already has CRUD) is disconnected from what the evaluation worker actually reads (`users.cv_markdown`). Not in scope to fix here, but any new profile/digest work should not deepen this inconsistency.
- **Chat feature is architecturally unlike anything else in this codebase** — the three existing pg-boss job types are all fire-and-forget batch/async; a chat UX forces a latency trade-off (queue round-trip per turn) that should be validated with the user/UX before committing to it in a design phase.
- **Outcome-learning has a real upstream dependency** (`matched_archetype` doesn't exist) that a proposal for change #4 would need to scope explicitly.

## Ready for Proposal

**Yes**, for slice #1 (persistence + read/edit API) and slice #2 (article-digest) — both have enough clarity to move to `sdd-propose` now. Slice #3 (conversational editor) needs a design-phase decision on the chat latency trade-off before proposing. Slice #4 (outcome-learning) is **not** ready — recommend explicitly deferring it rather than proposing it prematurely.
