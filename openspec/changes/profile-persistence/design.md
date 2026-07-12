# Design: Profile Persistence + Read/Edit API

## Technical Approach

Two independent, additive fixes plus one new hexagonal Go package and one real
web page. No cross-language shared code, no new deps, no destructive schema
change.

- **(A) CV merge-on-ingest** is prompt-level only. `ingest-cv.mjs` reads the
  existing `cv_markdown` before calling Claude and passes it into a new merge
  variant of the ingest system prompt. The `===CV_MARKDOWN===` /
  `===PROFILE_JSON===` delimiter contract and `parseIngestResponse` are
  untouched. A sanity guard skips the destructive `UPDATE users` when Claude
  returns a parse error over an already-good profile.
- **(B) Manual edits** live in a new `users.profile_overrides jsonb` column,
  never written by ingestion. The **effective profile** =
  `{ ...profile_json, ...profile_overrides }` (top-level key overlay), computed
  at read time in TWO places: the Go read endpoint and `worker/lib/prompt.mjs`.
  The merge is ~5 lines each — duplicated deliberately (Decision D2), same
  tradeoff already accepted for the SSRF allowlist in `job-content-fetch`.
- **`profile_edits`** is a new RLS-forced ledger table. Every `PATCH` writes one
  override key AND one ledger row in a single `WithTenantTx`. It powers the
  visible/undoable "your manual edits" list from day one and is generic enough
  (`source`, `status`) to be reused by the future chat editor with no schema
  change.

The control/data-plane split holds: the Go API owns read/edit/undo (no LLM, no
scraping); the worker owns ingestion and evaluation. Both compute the same
effective profile independently.

**Note on hexagonal shape:** the existing `cv` package is `handler.go` +
`service.go` only — there is NO `repo.go`; the service calls sqlc `db.Queries`
directly inside `WithTenantTx`. The new `profile` package MIRRORS THAT ACTUAL
pattern (two files), not the aspirational three-file one.

## Architecture Decisions

### D1 — CV merge is a prompt variant selected by existing-CV presence; parser and delimiter contract unchanged

**Choice**: `buildIngestPrompt` gains a second parameter,
`existingCvMarkdown`. When it is empty (first ingest) the current
`INGEST_SYSTEM_PROMPT` is used verbatim. When it is non-empty, a new
`INGEST_MERGE_SYSTEM_PROMPT` is used and the existing markdown is injected as an
additional labeled block in the user message. Both prompts emit the SAME two
delimited sections, so `parseIngestResponse` (the regex split) does not change.

`ingest-cv.mjs` reads the existing row before building the prompt:

```js
const existing = await tenantQuery(
  user_id,
  `SELECT cv_markdown, profile_json FROM users WHERE id = $1::uuid`,
  [user_id]
)
const existingCv = existing.rows[0]?.cv_markdown || ''
const existingProfile = existing.rows[0]?.profile_json || {}
const prompt = buildIngestPrompt(raw_cv, existingCv)
```

**Exact new prompt text** (`worker/lib/ingest-prompt.mjs`):

```
export const INGEST_MERGE_SYSTEM_PROMPT = `You are a CV ingestion assistant merging NEW resume text into an EXISTING
career profile. You are given the candidate's existing CV (already structured
markdown) and newly pasted raw text. Produce a COMPREHENSIVE SUPERSET that
keeps every fact from the existing CV and folds in everything new.

Merge rules:
- NEVER drop a role, employer, project, skill, or achievement that is in the
  existing CV but absent from the new text. The new text is often a tailored
  subset — treat missing items as omitted-for-brevity, not deleted.
- If the new text describes the SAME entry as an existing one (same employer,
  overlapping dates, or an evident promotion/title change at the same company),
  UPDATE that single entry in place — merge the details, keep the most recent
  title and the widest date range. Do NOT emit two entries for one real job.
- If the new text adds a genuinely new role, employer, or skill, ADD it.
- Prefer the more specific / more recent value on any direct conflict (title,
  dates, metrics); never invent facts to reconcile a conflict.
- Keep the result in the same clean markdown structure.

Then produce the two delimited sections in this EXACT format and nothing else:

===CV_MARKDOWN===
<the merged, comprehensive CV as clean markdown>

===PROFILE_JSON===
` + '```json' + `
{ "candidate": { "full_name": "...", "email": "...", "phone": "...", "location": "...",
    "linkedin": "...", "github": "...", "portfolio_url": "..." },
  "target_roles": { "primary": ["..."], "archetypes": [{ "name": "...", "level": "...", "fit": "..." }] },
  "salary_target": { "min": 0, "max": 0, "currency": "..." },
  "narrative": "..." }
` + '```' + `

Rules:
- Output the markers VERBATIM, in order: ===CV_MARKDOWN=== then ===PROFILE_JSON===.
- profile_json MUST be valid JSON inside a ` + '```json' + ` fence.
- Use null/empty for unknown fields; never fabricate contact info or salary.
- The merged CV must be a superset: it may never be shorter in factual coverage
  than the existing CV.`
```

The existing raw text goes into the user message alongside the new text so the
model sees both clearly labeled:

```js
messages: [{
  role: 'user',
  content: existingCvMarkdown
    ? `Here is my EXISTING CV (already structured):\n\n${existingCvMarkdown}\n\n---\n\nHere is NEWLY pasted CV text to merge in:\n\n${rawCV}`
    : `Here is my raw CV:\n\n${rawCV}`,
}]
```

| Option | Tradeoff | Decision |
|--------|----------|----------|
| Prompt-level merge, existing CV as context | Zero schema/parser change; LLM handles promotion/supersede reasoning | **Chosen** |
| Programmatic diff/merge of two markdowns | Deterministic but can't reason about "promotion = same job" without structure we don't have | Rejected — no structured entries to diff |
| Append new text, dedupe later | Fast but produces duplicate roles immediately | Rejected — violates "never duplicate" |

**Prompt caching note:** the merge system block is still `cache_control:
ephemeral`, but the existing-CV text is now in the USER message (variable per
user), so it is NOT part of the cached prefix. Acceptable — ingestion is
low-frequency and not latency-critical.

### D2 — Sanity guard: skip the whole UPDATE on parse-error-over-good-profile

**Choice**: In `ingest-cv.mjs`, immediately BEFORE the `UPDATE users`, guard:

```js
const parseErrored = profileJson.parse_error === true
const hadGoodProfile =
  existingProfile && !existingProfile.parse_error &&
  Object.keys(existingProfile).length > 0
const hadGoodCv = existingCv.trim().length > 0

if (parseErrored && (hadGoodProfile || hadGoodCv)) {
  // Do NOT overwrite good data with a parse-error blob.
  await tenantQuery(user_id,
    `UPDATE cv_ingestions SET status = 'failed', finished_at = NOW() WHERE id = $1::uuid`,
    [run_id])
  await notify(client, run_id, 'ingest.failed', { error: 'parse_error_preserved_existing' })
  return
}
// else: fall through to the existing UPDATE users (first ingest, or an empty
// prior profile — nothing valuable to protect).
```

The check lives in the handler (not the parser) because only the handler has
both the fresh parse result AND the pre-read existing row. `parseIngestResponse`
stays a pure never-throw function.

**Why `failed`, not `completed`:** nothing was persisted, so `failed` is honest
and the web ingest-status poller shows the user their paste did not take — they
can retry. The prior good `cv_markdown`/`profile_json` remain intact.

### D3 — `profile_overrides jsonb` column + shallow merge duplicated in Go and JS

**Choice**: New column on `users`, defaulting `'{}'::jsonb`, NOT NULL. Never
written by ingestion (only by the `profile` package). Effective profile is a
top-level key overlay: an overridden key REPLACES the whole `profile_json` key
(shallow, per-top-level-key — the proposal's accepted first-slice limitation).

Go (`api/internal/profile/service.go`):

```go
// mergeProfile overlays override keys onto the base profile (shallow, per
// top-level key). Both args are raw jsonb bytes from users.
func mergeProfile(base, overrides []byte) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	if len(base) > 0 {
		_ = json.Unmarshal(base, &out)
	}
	ov := map[string]json.RawMessage{}
	if len(overrides) > 0 {
		_ = json.Unmarshal(overrides, &ov)
	}
	for k, v := range ov {
		out[k] = v // whole-key replace
	}
	return out, nil
}
```

JS (`worker/lib/prompt.mjs`):

```js
// ponytail: 4-line shallow merge, duplicated per D2/job-content-fetch precedent
function mergeProfile(profileJson, profileOverrides) {
  const base = typeof profileJson === 'string' ? JSON.parse(profileJson || '{}') : (profileJson || {})
  const ov = typeof profileOverrides === 'string' ? JSON.parse(profileOverrides || '{}') : (profileOverrides || {})
  return { ...base, ...ov }
}
```

`buildEvaluationPrompt` adds `profile_overrides` to its user SELECT and feeds
`JSON.stringify(mergeProfile(...))` into `cvAndProfileBlock` in place of the raw
`profileJson`. Output stays a plain injectable JSON blob — no format change.

| Option | Tradeoff | Decision |
|--------|----------|----------|
| Duplicated 4-line merge (Go + JS) | No cross-runtime coupling; trivial to keep in sync | **Chosen** |
| Postgres computes merge (`||` operator) | `profile_json \|\| profile_overrides` in SQL, one source | Rejected — Go still needs the map to serialize per-key edits list; SQL `\|\|` is also shallow so no fidelity gain |
| Shared config/service | Overkill for 4 lines | Rejected — YAGNI |

**Note:** Postgres jsonb `||` IS shallow-merge and IS used for the WRITE side
(D5). It is not used for the read-merge only because both consumers also need
the parsed map for other work (edits list / prompt injection).

### D4 — `profile_edits` ledger table, migration 007

**Choice**: New table mirroring the `tenant_cvs` RLS shape. Migration file
`db/migrations/007_profile_persistence.sql` (next sequential number; this
project uses numbered per-change migration files, confirmed via `db/migrations/`
listing 001-006). The migration ALSO adds the `profile_overrides` column (D3)
and the `GRANT ... TO app_user` that migration 006 established as convention.
`db/schema.sql` and `db/rls.sql` are updated in parallel to stay canonical.

```sql
-- db/migrations/007_profile_persistence.sql

-- 1. Manual override column on users (users already FORCE RLS -> no policy change)
ALTER TABLE users ADD COLUMN profile_overrides jsonb NOT NULL DEFAULT '{}'::jsonb;

-- 2. profile_edits ledger
CREATE TABLE profile_edits (
  id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  field_path  text        NOT NULL,          -- top-level key for this slice (e.g. "salary_target")
  old_value   jsonb,                          -- effective value BEFORE this edit (null if none)
  new_value   jsonb,                          -- value written into profile_overrides
  source      text        NOT NULL DEFAULT 'manual'
                            CHECK (source IN ('manual','ai_suggestion')),
  status      text        NOT NULL DEFAULT 'accepted'
                            CHECK (status IN ('accepted','proposed','undone')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz
);
CREATE INDEX idx_profile_edits_user ON profile_edits(user_id, created_at DESC);

-- 3. RLS (NULLIF-hardened, mirrors tenant_cvs)
ALTER TABLE profile_edits ENABLE ROW LEVEL SECURITY;
ALTER TABLE profile_edits FORCE  ROW LEVEL SECURITY;

CREATE POLICY tenant_profile_edits ON profile_edits
  USING      (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON profile_edits TO app_user;
```

`source`/`status` vocabularies include `ai_suggestion`/`proposed` now so the
future chat editor needs no migration — but only `manual`/`accepted`/`undone`
are exercised by this slice.

**sqlc queries** (`db/queries/profile_edits.sql` — new file):

```sql
-- name: InsertProfileEdit :one
INSERT INTO profile_edits (user_id, field_path, old_value, new_value, source, status)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListProfileEditsByUser :many
SELECT * FROM profile_edits
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: GetProfileEdit :one
SELECT * FROM profile_edits
WHERE id = $1
LIMIT 1;

-- name: MarkProfileEditUndone :one
UPDATE profile_edits
SET status = 'undone', resolved_at = NOW()
WHERE id = $1
RETURNING *;
```

**`db/queries/users.sql` changes** — retire `UpdateUserProfileJSON` (dead:
its only caller was the retired path), add:

```sql
-- name: GetUserProfile :one
SELECT cv_markdown, profile_json, profile_overrides
FROM users WHERE id = $1 LIMIT 1;

-- name: SetProfileOverrideKey :one
UPDATE users
SET profile_overrides = profile_overrides || jsonb_build_object($2::text, $3::jsonb)
WHERE id = $1
RETURNING profile_overrides;

-- name: DropProfileOverrideKey :one
UPDATE users
SET profile_overrides = profile_overrides - $2::text
WHERE id = $1
RETURNING profile_overrides;
```

Requires `cd db && sqlc generate` after edits.

### D5 — `api/internal/profile/` package: 3 endpoints, PATCH+ledger atomic in one tx

**Choice**: New package with `handler.go` + `service.go` (no `repo.go`, matching
`cv`). Wired in `main.go` alongside the other handlers:
`profile.NewHandler(profile.NewService(pool)).RegisterRoutes(r)`.

Routes:
```
GET  /api/me/profile
PATCH /api/me/profile
POST /api/me/profile-edits/{id}/undo
```

Servicer interface:
```go
type Servicer interface {
	GetProfile(ctx context.Context, userID uuid.UUID) (*EffectiveProfile, error)
	ApplyOverride(ctx context.Context, userID uuid.UUID, fieldPath string, value json.RawMessage) (*db.ProfileEdit, error)
	UndoEdit(ctx context.Context, userID, editID uuid.UUID) error
}
```

Response shapes:
```go
type EffectiveProfile struct {
	CVMarkdown string                     `json:"cv_markdown"`
	Profile    map[string]json.RawMessage `json:"profile"`      // effective (merged)
	Edits      []db.ProfileEdit           `json:"edits"`        // ledger, newest first
}
// PATCH body:   { "field_path": "salary_target", "value": { ... } }
// PATCH 200 ->  the created db.ProfileEdit
// POST undo -> 204 No Content
```

`ApplyOverride` — override write + ledger insert in ONE `WithTenantTx`
(same idiom as `cv.EnqueueIngest`'s usage-check+insert+increment, minus the
post-commit enqueue since there is no queue work here):
```go
func (s *Service) ApplyOverride(ctx, userID, fieldPath, value) (*db.ProfileEdit, error) {
	// validate fieldPath is an allowed top-level key (allowlist below)
	var edit db.ProfileEdit
	err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		// 1. read current effective value of this key -> old_value for the ledger
		u, err := q.GetUserProfile(ctx, userID)               // base + overrides
		if err != nil { return err }
		oldVal := currentKey(u.ProfileJson, u.ProfileOverrides, fieldPath) // may be nil
		// 2. write the override key
		if _, err := q.SetProfileOverrideKey(ctx, db.SetProfileOverrideKeyParams{
			ID: userID, Column2: fieldPath, Column3: value,
		}); err != nil { return err }
		// 3. write the ledger row (source=manual, status=accepted)
		edit, err = q.InsertProfileEdit(ctx, db.InsertProfileEditParams{
			UserID: userID, FieldPath: fieldPath, OldValue: oldVal,
			NewValue: value, Source: "manual", Status: "accepted",
		})
		return err
	})
	return &edit, err
}
```

`fieldPath` allowlist (top-level keys only, rejects arbitrary jsonb path
injection at the trust boundary): `target_roles`, `salary_target`, `narrative`,
`candidate`, `deal_breakers`, `comp_targets`. Anything else → 400.

`UndoEdit` — drop the key + flip ledger status, one tx:
```go
err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
	edit, err := q.GetProfileEdit(ctx, editID)       // RLS-scoped; not-found = wrong tenant
	if err != nil { return err }                     // sql.ErrNoRows -> ErrNotFound
	if _, err := q.DropProfileOverrideKey(ctx, db.DropProfileOverrideKeyParams{
		ID: userID, Column2: edit.FieldPath,
	}); err != nil { return err }
	_, err = q.MarkProfileEditUndone(ctx, editID)
	return err
})
```

Dropping the override key means the next read falls back to the `profile_json`
value — exactly the "revert to CV-derived value" success criterion.

**Undo edge case (documented, not solved this slice):** if a key was PATCHed
twice, undoing the newer edit drops the whole key rather than restoring the
prior override. Acceptable for the first slice — the shallow-per-key model has
no override history stack. Surgical/historical undo is deferred to the chat
editor. `// ponytail:` this is the known ceiling; upgrade = replay accepted
edits on undo if it ever matters.

### D6 — Web `/perfil` page: read-render + plain form + undoable list

**Choice**: Replace the `ComingSoon` stub with a client page that fetches
`GET /api/me/profile` via the existing `apiGet` (auth/refresh already handled by
`lib/api.ts`; auth-guard already handled by the `(app)/layout.tsx` group).

Component breakdown (lean — enough for `sdd-tasks` to slice into files):

| Component | File | Renders |
|-----------|------|---------|
| `PerfilPage` | `web/app/(app)/perfil/page.tsx` | Client page: fetch effective profile on mount, hold state, compose the three below |
| `CvMarkdownView` | `web/components/perfil/cv-markdown-view.tsx` | Read-only render of `cv_markdown` (reuse whatever markdown renderer the report view already uses; plain `<pre>` if none) |
| `ProfileEditForm` | `web/components/perfil/profile-edit-form.tsx` | Plain inputs/textareas for the allowlisted top-level keys; on save → `apiPatch('/api/me/profile', {field_path, value})`, refetch |
| `ManualEditsList` | `web/components/perfil/manual-edits-list.tsx` | Maps `edits` (status `accepted` only shown as active); per-row Undo → `apiPost('/api/me/profile-edits/{id}/undo')`, refetch |

No WYSIWYG, no rich editing, no chat (all out of scope). Plain inputs only.
State is local `useState` + refetch-on-mutation — no global store needed for one
page.

## Data Flow

    ── Ingestion (merge) ──────────────────────────────────────────────
    ingest-cv job ─ tenantQuery SELECT cv_markdown, profile_json  (existing)
                  ─ buildIngestPrompt(raw_cv, existingCv)          (merge variant if existing)
                  ─ Claude → parseIngestResponse (unchanged)
                  ─ parse_error over good profile? ─ yes → mark 'failed', NO write
                                                   ─ no  → UPDATE users SET cv_markdown, profile_json
                       (profile_overrides NEVER touched here)

    ── Read / Edit (Go API) ───────────────────────────────────────────
    GET  /api/me/profile ─ WithTenantTx GetUserProfile ─ mergeProfile ─ {cv, effective, edits}
    PATCH /api/me/profile ─ WithTenantTx [ read old → SetProfileOverrideKey → InsertProfileEdit ]  (atomic)
    POST /api/me/profile-edits/{id}/undo ─ WithTenantTx [ GetProfileEdit → DropProfileOverrideKey → MarkProfileEditUndone ]

    ── Evaluation (consume) ───────────────────────────────────────────
    evaluate-job ─ buildEvaluationPrompt ─ SELECT ...+profile_overrides ─ mergeProfile ─ injected JSON blob

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `db/migrations/007_profile_persistence.sql` | New | `profile_overrides` column + `profile_edits` table + RLS + grants |
| `db/schema.sql` | Modify | Canonical: add column + `profile_edits` table |
| `db/rls.sql` | Modify | Canonical: enable/force + `tenant_profile_edits` policy |
| `db/queries/users.sql` | Modify | Retire `UpdateUserProfileJSON`; add `GetUserProfile`, `SetProfileOverrideKey`, `DropProfileOverrideKey` |
| `db/queries/profile_edits.sql` | New | Insert/List/Get/MarkUndone |
| `api/internal/db/*` | Regen | `sqlc generate` output (do not hand-edit) |
| `api/internal/profile/handler.go` | New | 3 routes, Servicer interface, request/response shapes |
| `api/internal/profile/service.go` | New | `GetProfile`/`ApplyOverride`/`UndoEdit`, `mergeProfile`, fieldPath allowlist |
| `api/cmd/api/main.go` | Modify | Wire `profile.NewHandler(profile.NewService(pool))` |
| `worker/lib/ingest-prompt.mjs` | Modify | `INGEST_MERGE_SYSTEM_PROMPT`; `buildIngestPrompt(rawCV, existingCvMarkdown)` |
| `worker/jobs/ingest-cv.mjs` | Modify | Pre-read existing row; sanity guard before UPDATE |
| `worker/lib/prompt.mjs` | Modify | `mergeProfile`; SELECT `profile_overrides`; inject effective |
| `web/app/(app)/perfil/page.tsx` | Modify | Real page (replaces `ComingSoon`) |
| `web/components/perfil/*.tsx` | New | `CvMarkdownView`, `ProfileEditForm`, `ManualEditsList` |

## Open Questions / Follow-ups — resolved

- **CV-merge prompt robustness (proposal risk #1)** — RESOLVED as D1: concrete
  `INGEST_MERGE_SYSTEM_PROMPT` with explicit promotion/supersede/superset rules,
  as testable as the existing prompt. Real multi-version CV validation is a
  `tasks`-phase test concern (feed an existing structured CV + a shorter
  tailored paste, assert no roles dropped and no duplicate for a promotion), not
  a design blocker.
- **Sanity-guard condition (proposal risk #2)** — RESOLVED as D2:
  `profile_json.parse_error === true` AND (existing profile good OR existing CV
  non-empty) → skip the whole `UPDATE users`, mark ingestion `failed`. Lives in
  `ingest-cv.mjs` before the UPDATE; parser stays pure.

## Deviations from proposal

- Proposal names a three-file hexagonal package (`handler.go`+`service.go`+
  `repo.go`). The `cv` template it points at has NO `repo.go` — service uses
  sqlc `db.Queries` directly through `WithTenantTx`. The `profile` package
  follows the ACTUAL two-file pattern. No behavioral difference; recorded so
  `tasks` does not scaffold an empty `repo.go`.
- Undo multi-edit-per-key limitation (D5) is called out explicitly as an
  accepted first-slice ceiling; the proposal implied simple revert without
  addressing the double-edit case.
