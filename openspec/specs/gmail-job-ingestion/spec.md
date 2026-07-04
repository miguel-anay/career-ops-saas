# Gmail Job Ingestion Specification

## Purpose

Ingest job postings from the user's own Gmail job-alert emails (LinkedIn, Computrabajo, Bumeran, Indeed) and upsert them into `jobs`. Covers the LatAm local market unreachable via the existing 6 ATS providers, with zero LLM cost by default.

## Requirements

### Requirement: Gmail Incremental Consent

The system MUST support an opt-in `gmail.readonly` consent flow independent of the main login flow. Existing users MUST NOT be forced to re-authenticate to access this feature. The system MUST persist the Google refresh token on the `users` record, isolated by RLS. The system MUST NOT request `gmail.modify` or `gmail.labels` under any circumstance.

#### Scenario: First-time Gmail connection

- GIVEN an authenticated user without a stored Gmail refresh token
- WHEN they complete `POST /auth/google/gmail` (incremental consent with `prompt=consent&access_type=offline`)
- THEN the callback stores the `refresh_token` on the user record and returns HTTP 200

#### Scenario: Re-consent replaces existing token

- GIVEN a user with an existing refresh token
- WHEN they complete the incremental consent flow again
- THEN the new refresh token replaces the previous one

#### Scenario: Ingest attempted without Gmail token

- GIVEN a user has never connected Gmail
- WHEN `POST /api/email-ingest` is called
- THEN the API returns HTTP 422 with `{ "error": "gmail_not_connected" }` and creates no run

#### Scenario: Refresh token revoked at Google

- GIVEN the stored refresh token has been revoked by the user at Google
- WHEN the worker attempts a token exchange
- THEN the ingest run is recorded as status `error` with reason `token_revoked`
- AND the system does NOT clear `google_refresh_token` (reconnection is user-initiated)

---

### Requirement: Ingest Trigger

The system MUST expose `POST /api/email-ingest`, accessible only to authenticated users and scoped by RLS. The endpoint MUST atomically create an `email_ingest_runs` record and enqueue an `ingest-email` worker job. The response MUST include the `ingest_run_id`.

#### Scenario: Successful trigger

- GIVEN a user with a valid Gmail refresh token
- WHEN `POST /api/email-ingest` is called
- THEN the API returns HTTP 202 with `{ "ingest_run_id": "<uuid>" }`
- AND an `ingest-email` job is enqueued in pg-boss

---

### Requirement: Email Reading — Sender Allowlist

The worker MUST query Gmail using a `q=` filter constructed at runtime from each registered parser's `senderMatch` value. Messages outside the allowlist MUST NOT be fetched or processed. The exact sender addresses per platform (LinkedIn, Computrabajo, Bumeran, Indeed) are constants inside each parser file; the mechanism is specified here, the values are deferred to design.

#### Scenario: Gmail query is scoped to allowlist

- GIVEN the worker holds a valid access token
- WHEN it queries Gmail
- THEN the request includes a `q=` parameter with `from:` terms for all registered senders only

#### Scenario: Unrecognized sender in response

- GIVEN Gmail returns a message not matching any parser's `senderMatch`
- WHEN the worker processes it
- THEN the message is silently skipped; the run continues unaffected

---

### Requirement: Parsing and Job Upsert

Each recognized email MUST be parsed to extract: `title`, `company`, `url`, and `platform` (derived from sender). The raw URL MUST be normalized via `normalizeJobUrl` before upsert to strip per-sender tracking parameters. The system MUST upsert into `jobs` on `UNIQUE(user_id, url)` with `ON CONFLICT DO NOTHING`; existing jobs MUST NOT be overwritten or duplicated.

#### Scenario: Net-new job

- GIVEN a parsed, normalized URL is not present in `jobs` for this user
- WHEN the upsert executes
- THEN a new row is inserted and `new_jobs_count` increments on the ingest run

#### Scenario: Duplicate job

- GIVEN the normalized URL already exists in `jobs` for this user
- WHEN the upsert executes
- THEN no row is inserted or modified; `duplicate_count` increments

#### Scenario: URL extraction failure

- GIVEN a parser matches a sender but cannot extract a valid URL
- WHEN the parser returns
- THEN the job is skipped; `parse_error_count` increments; the run continues

---

### Requirement: Cost Invariant

The default ingest path MUST produce zero LLM API calls. Parsers are deterministic. An LLM fallback MAY be activated via `EMAIL_PARSER_LLM_FALLBACK=true` in the worker environment; when absent or `false`, the LLM MUST NOT be called.

#### Scenario: Default run (no flag)

- GIVEN `EMAIL_PARSER_LLM_FALLBACK` is unset or `false`
- WHEN emails are processed
- THEN the Anthropic API is not called; all parsing is deterministic

#### Scenario: LLM fallback enabled

- GIVEN `EMAIL_PARSER_LLM_FALLBACK=true`
- WHEN a parser cannot extract structured data from a matched email
- THEN the worker MAY invoke the LLM; the result is subject to the same upsert and dedup rules

---

### Requirement: Run Tracking and Graceful Degradation

The system MUST record one `email_ingest_runs` row per trigger, RLS-scoped, with fields: `status`, `new_jobs_count`, `duplicate_count`, `parse_error_count`, and timestamps. A failure in one sender MUST NOT abort processing of other senders.

| Status | Condition |
|--------|-----------|
| `completed` | All senders processed; zero unrecoverable errors |
| `partial` | At least one sender failed; at least one succeeded |
| `error` | Token exchange failed before processing, or all senders failed |

#### Scenario: All senders succeed

- GIVEN all registered parsers complete without error
- THEN the run status is `completed`

#### Scenario: One sender fails, others succeed

- GIVEN one sender's Gmail fetch raises an error
- WHEN the worker continues to remaining senders
- THEN the run status is `partial` and per-sender error detail is recorded

#### Scenario: Token exchange fails before any processing

- GIVEN the refresh token is invalid or revoked
- WHEN the worker attempts the exchange
- THEN the run status is `error` and no Gmail calls are made

---

### Requirement: Read-Only Privacy Constraint

The system MUST use only `gmail.readonly` for all Gmail operations. The system MUST NOT issue any modify, label, send, or delete operations via Gmail. Email body content MUST be read only to the extent needed to extract job fields (`title`, `company`, `url`, `platform`).

#### Scenario: Scope at incremental consent

- GIVEN the incremental consent flow is triggered
- WHEN the OAuth redirect is constructed
- THEN the scope includes `gmail.readonly` (plus the existing `openid email profile`) and nothing beyond that

## Open Questions — Resolution

| Question | Resolution |
|----------|------------|
| Exact sender addresses (e.g. `jobalert@linkedin.com`) | Deferred to design. Spec defines the mechanism: each parser declares a `senderMatch` constant; the `q=` filter is the runtime union. |
| `email_ingest_runs` status vocabulary | Resolved in this spec: `completed`, `partial`, `error`. Column names and schema deferred to design. |
