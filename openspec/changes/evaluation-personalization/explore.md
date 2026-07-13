# Exploration: `evaluation-personalization` — porting `career-ops` CLI's `_profile.md` richness into the SaaS evaluator

## Part 0 — Resolving the ambiguity (mandatory first task)

Read in full: `career-ops/modes/_profile.md`, `_shared.md`, `deep.md`, `scan.md`, `pipeline.md`, `apply.md`, `interview-prep.md`, `followup.md`, `docs/ARCHITECTURE.md`, `docs/CUSTOMIZATION.md`, `docs/mapa-perfil.md`, plus one real generated evaluation report that hit a SKIP path (`reports/041-ntt-data-2026-06-22.md`) and one full A-G report (`reports/044-banco-gnb-2026-07-03.md`).

**Correction to the orchestrator's framing**: `deep.md` is *not* the CLI's evaluation mode — it's an interview-prep company-research prompt generator (6-axis Perplexity/ChatGPT brief). The actual evaluation mode is `pipeline.md` ("Execute full auto-pipeline: Evaluation A-F"), which inherits its base rules from `_shared.md`.

`_shared.md` (read before `_profile.md` on every mode, per its own header) contains the smoking gun:
- `## Sources of Truth` table: `_profile.md` → "ALWAYS (user archetypes, narrative, negotiation)"
- `## Global Rules → ALWAYS`, rule 1: **"Read cv.md, _profile.md, and article-digest.md (if exists) before evaluating"**
- `## Scoring System`: one of the 6 scored dimensions is explicitly **"North Star alignment — How well the role fits the user's target archetypes (from `_profile.md`)"**
- `## Archetype Detection`: "After detecting archetype, read `modes/_profile.md` for the user's specific framing and proof points for that archetype."

Real-world confirmation: `reports/041-ntt-data-2026-06-22.md` shows the Location Policy SKIP rule firing for real, aborting the full A-G evaluation ("Full A-G evaluation skipped per the candidate's own location policy... Confirmed with candidate before skipping"). `reports/044-banco-gnb-2026-07-03.md` Section C/E/F reproduce the CTO/Exit-Narrative framing from `_profile.md` almost verbatim ("El título refleja que construí una empresa, no que busca un rol ejecutivo").

Also confirmed independently: `scan.md`'s `title_filter`/`location_filter` (read from `portals.yml`) is a **separate, earlier-stage** gate (pre-discovery, before a URL even reaches `pipeline.md`) — it does not overlap with `_profile.md`'s Scoring Adjustments, which fire *inside* `pipeline.md`'s per-URL evaluation. Two real, non-overlapping gates, both genuinely wired in — the orchestrator's suspicion of possible overlap/confusion was reasonable to check but the mechanisms are cleanly separated in practice.

**Per-section verdict:**

| Section | Evidence found | Verdict |
|---|---|---|
| **Scoring Adjustments** (Boost/Penalizar/SKIP) | `_shared.md` ALWAYS rule 1 + "North Star alignment" scoring dimension + real report 041 SKIP firing | **Real, systematically applied.** The SKIP tier is provably wired into `pipeline.md`'s actual output. Boost/Penalizar deltas are folded *narratively* into the holistic score — no report (across 40+ inspected) shows literal "+0.3/-0.3" arithmetic; even the reference CLI's LLM absorbs these as prose instructions, never as a separate addend. |
| **Location Policy** | Same `_shared.md` path + "Cultural signals" (remote policy) scoring dimension + report 041 (direct SKIP trigger) + report 044's red-flag question confirms location was checked | **Real, same enforcement path as Scoring Adjustments** — it's a specialization of it, not a separate mechanism. |
| **Adaptive Framing** | `_shared.md` "Archetype Detection" instructs reading `_profile.md` for framing after archetype detection; report 044 Section C/E visibly apply the archetype-specific downlevel framing | **Real, but feeds narrative/customization text (blocks C/E/F), not the numeric score.** |
| **Exit Narrative** | Same mechanism; report 044's CTO reframing is lifted near-verbatim | **Real, same as Adaptive Framing** — narrative shaping, not scoring. |
| **Negotiation Scripts** | `docs/CUSTOMIZATION.md` frames it explicitly as human-editable "frameworks... replace with your own"; not named in `_shared.md`'s ALWAYS list or the 6-block rubric; zero of 40+ inspected reports reproduce the canned scripts (Block D generates bespoke comp text instead) | **Weakest evidence of automated pipeline consumption.** Reads as candidate-facing reference material for the human to use live in a real negotiation conversation — not something `pipeline.md` systematically pulls in. |

Conclusion: porting Scoring Adjustments/Location Policy is **replicating a proven mechanism**. Porting Adaptive Framing/Exit Narrative is also replicating a real (if narrative-only) mechanism. Porting Negotiation Scripts verbatim into the evaluator would be **building behavior the CLI itself doesn't systematically apply either** — it's just documentation for the human.

## Current SaaS state (verified this session)

- `worker/lib/prompt.mjs::buildEvaluationPrompt` — 2–3 cached `system` blocks: `[0]` static 7-block instructions, `[1]` `cvAndProfileBlock` (CV + `JSON.stringify(mergeProfile(profile_json, profile_overrides))`), `[2]` optional digest block.
- `mergeProfile` (both `worker/lib/prompt.mjs` and `api/internal/profile/service.go`) is a **generic shallow spread over whatever top-level keys exist** — it does not care about key names. Adding a new top-level key to the profile requires **zero changes to the merge/prompt-building code**, only a Go allowlist entry (`api/internal/profile/service.go:36-43`, `allowedFieldPaths`).
- **Critical finding — `worker/domain/EvaluationParser.mjs:43-45`**: per-block scores ARE parsed by the regex internally but are explicitly **discarded** — comment reads *"Per-block score is parsed but dropped from the emitted shape (YAGNI — no consumer reads it; the overall score below is kept)."* Only a single overall float survives into `Evaluation`. This directly kills the "cheap version" of Option C below.
- `api/internal/evaluate/service.go::EnqueueEvaluation` already has a pre-enqueue guard pattern: `ErrCVMissing`/`ErrJobContentMissing` → 422, checked *before* the usage-limit check and enqueue, inside one `WithTenantTx`. This is the exact template a new SKIP guard would extend.
- `jobs` table (`db/schema.sql:83-97`) has `received_at` (structured, already used for posting-age) but **no structured `location` column** — location only exists inside free-text `scraped_content`.
- `applications` table has no archetype column (confirmed, matches the earlier `candidate-profile-kb` finding).

## Storage shape

| In-scope content | Shape | Fits existing model? |
|---|---|---|
| Scoring Adjustments + Location Policy (merge these — Location Policy is one axis of Scoring Adjustments) | New top-level key, e.g. `scoring_rules: { boost: [{condition, delta}], penalize: [{condition, delta}], skip: [{condition}], max_posting_age_days: 30 }` | Yes — one new allowlist entry. Keep `condition` as free text, same as the CLI. Do **not** build a rule-predicate DSL/engine: the reference CLI itself hands prose to the LLM and lets it judge — a structured predicate evaluator in the SaaS would be *more* mechanism than what's being "replicated." |
| Exit Narrative | Single narrative text | **Already expressible today with zero code changes** — the SaaS allowlist already has a `narrative` key (per `docs/mapa-perfil.md`'s own mapping: `narrative` ← "headline, exit story, superpowers, proof points"), and the allowlist gates by top-level key name, not by internal shape. A user can already nest an exit-narrative story inside `narrative` via the existing `PATCH` override endpoint. This may be a documentation/UX gap, not an engineering gap. |
| Adaptive Framing | Per-archetype table (`[{if_role, emphasize, source}]`) | New shape not covered by any existing key today — but *could* also nest inside the existing `narrative` value instead of a new top-level key, since the allowlist doesn't inspect internal shape. |
| Negotiation Scripts | Free text | Out of scope for the evaluator (see recommendation) — if built later, it's a different surface (chat/apply-assist), not `profile_overrides` consumed by `prompt.mjs`. |

## Injection mechanism — the core question

1. **Option A — extend `cvAndProfileBlock` (block 1)**: Block 1 is *already* rebuilt from the full merged profile on every call — its cache key already invalidates on ANY profile edit today (editing `narrative` already busts this block's cache; that's the current, accepted behavior). Adding `scoring_rules`/`adaptive_framing` as more keys in the same JSON blob adds **no new cache-invalidation risk beyond what already exists** — same block, same invalidation surface. Cost: one Go allowlist line, zero prompt.mjs changes (since `mergeProfile` is key-agnostic).

2. **Option B — new dedicated 4th block**: Only justified if these keys change at a *different cadence* than the rest of the profile. They don't — same override endpoint, same PATCH flow as `narrative`/`target_roles`. The article-digest precedent (Decision 5) split digests into their own block specifically *because* digests have a different update cadence than CV/profile — that reasoning doesn't transfer here; splitting buys nothing and adds a second code path + more tests to keep in sync.

3. **Option C — post-hoc code-side score adjustment**: Ruled out for now. `EvaluationParser.mjs` deliberately discards per-block scores as YAGNI. Reviving that plus building a deterministic condition-matcher for prose conditions ("stack mentions .NET, Node.js...") in Go/JS would mean re-implementing, in brittle string matching, the exact judgment the LLM already does for free — objectively *more* engineering than the reference CLI itself does. Only worth reconsidering if score-consistency/gaming becomes a measured problem later.

**Recommendation: Option A.** Extend `cvAndProfileBlock`, gate with a new `scoring_rules` (and optionally reuse existing `narrative`) allowlist entry, let the LLM apply the rules narratively — exactly how the reference CLI's own LLM does it. This is "replicate the proven mechanism," not "invent a new one."

## The SKIP case specifically

- **Posting-age SKIP**: cheap, real, Go-side feasible *today* — `jobs.received_at` already exists and is already used for posting-age text in `worker/lib/prompt.mjs::describePostingAge`. A Go-side guard mirroring `ErrCVMissing`/`ErrJobContentMissing` exactly (`ErrStalePosting` → 422 `stale_posting`, checked pre-enqueue against a new `scoring_rules.max_posting_age_days` numeric field) is the highest-value, lowest-effort SKIP slice — real LLM-token savings, fully deterministic, no NL parsing needed.
- **Location/on-site SKIP**: **not cheap today.** There is no structured `location` column on `jobs` — the only source is unstructured `scraped_content`. A Go-side gate would need either a new schema field (touches every ATS provider's scraping code — a separate, larger change) or brittle substring matching (false-positive risk: "remote-first, occasional on-site" would wrongly trip a naive "on-site" match). Recommend leaving location inside the LLM evaluation via Option A (`scoring_rules` prose + existing Block E — Culture & Location) rather than building an unreliable Go gate now.
- **Stack-mismatch SKIP**: same free-text matching risk as location — leave to the LLM (already covered by Block B — Technical Match).

## Scope/slicing recommendation

Given this project's lean/incremental delivery convention, and the evidence above:

1. **Build now (if anything)**: `scoring_rules` top-level key (Boost/Penalize as free-text prose consumed narratively via Option A) + `max_posting_age_days` as a separate structured numeric field consumed by a new Go pre-enqueue guard mirroring the existing `ErrCVMissing` pattern. This is the slice with the strongest evidence (real SKIP fired in a real CLI report), the clearest cost benefit (skips an LLM call), and the smallest diff (one allowlist line + one guard function).
2. **Skip the code, just document it**: Exit Narrative — tell the user it already works today via the existing `narrative` override key; no engineering needed, possibly just a UX affordance (a labeled sub-field in the profile-edit UI).
3. **Defer**: Adaptive Framing as a dedicated new key — evaluate first whether nesting it inside the existing `narrative` value is "good enough" before adding new allowlist surface.
4. **Defer entirely, and not to this change**: Negotiation Scripts and location-based Go-side SKIP gating. Negotiation Scripts because the CLI itself doesn't systematically feed them into evaluation (weak evidence, different surface — chat/apply-assist, not the evaluator). Location Go-gating because it requires structured location data that doesn't exist yet (a separate schema change with broader blast radius across every ATS provider).

## Affected areas (if slice 1 is built)

- `api/internal/profile/service.go:36-43` — add `scoring_rules` to `allowedFieldPaths`
- `api/internal/evaluate/service.go:61-128` — new pre-enqueue guard (`ErrStalePosting`) following the exact `ErrCVMissing`/`ErrJobContentMissing` pattern (lines 77-89)
- `api/internal/evaluate/handler.go:52-67` — new `case errors.Is(err, ErrStalePosting):` → 422 `stale_posting`
- `worker/lib/prompt.mjs:169-177` — no change needed to `mergeProfile`/`cvAndProfileBlock` structure itself; the new key flows through automatically once allowed. Only the static system prompt (`staticSystemPrompt`, lines 124-167) may want a line telling the LLM to apply `scoring_rules` narratively, mirroring `_shared.md`'s framing.
- `db/schema.sql` — no migration needed (`profile_overrides` is already jsonb).

## Risks

- Free-text `condition` strings in `scoring_rules` mean the LLM's application of Boost/Penalize is unverifiable/untestable in the same way the reference CLI's is — this is an accepted tradeoff of replicating the CLI's own approach, not a new risk introduced by this change.
- If `max_posting_age_days` guard is added, it competes conceptually with the *existing* generic staleness signal already inside Block G (Posting Legitimacy) — should be framed as "hard opt-in per-user gate" (saves tokens, no LLM call at all) vs. Block G's softer qualitative legitimacy tiering (which still runs for everything that isn't gated).
- Nesting Adaptive Framing/Exit Narrative inside the existing `narrative` key sidesteps the allowlist by design (the allowlist gates on top-level key names, not on internal shape) — worth flagging explicitly to the user/product owner since it means the "allowlist" is a weaker boundary than it may appear for anyone editing `narrative` freely.

## Ready for Proposal

**Yes** — recommend proposing exactly slice 1 (`scoring_rules` + posting-age Go gate) as the first `evaluation-personalization` change, with Exit Narrative documented as already-available and Adaptive Framing/Negotiation Scripts explicitly deferred/out-of-scope in the proposal's Non-Goals.
