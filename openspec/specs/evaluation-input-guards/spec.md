# Spec: Evaluation Input Guards (Content Validation Before Enqueue)

## Purpose

This capability spec documents the guards that prevent token-burning evaluations when critical input (user CV or job description) is absent. The Go API rejects requests at the service layer, before enqueuing, returning machine-readable 422 codes so the web client can render actionable feedback.

## Context

The evaluate endpoint (`POST /api/jobs/{id}/evaluate`) previously had no input validation for CV or job content. This allowed:
- Jobs without scraped content (manual/email jobs, pre-Playwright-scraping) to burn tokens on empty input
- Users without a CV to enqueue evaluation runs that fail during LLM processing

These guards gate the enqueue, enforcing that evaluation only proceeds when both CV and job description exist.

## Requirements

### Requirement: Block evaluation when CV is missing

The system MUST reject `POST /api/jobs/{id}/evaluate` with HTTP 422 and machine-readable code `cv_missing` when `users.cv_markdown` is NULL or empty for the requesting user, BEFORE enqueueing `evaluate-job` or spending any LLM tokens. Mirrors `emailingest.ErrGmailNotConnected` (`api/internal/emailingest/service.go:21-24`, `handler.go:48-51`).

#### Scenario: User has no CV

- GIVEN an authenticated user whose `users.cv_markdown` is NULL
- WHEN they POST `/api/jobs/{id}/evaluate` for a job they own
- THEN the API responds 422 with `{"error": "...", "code": "cv_missing"}`
- AND no pg-boss `evaluate-job` is enqueued
- AND zero LLM tokens are spent

#### Scenario: User has a CV

- GIVEN an authenticated user whose `users.cv_markdown` is a non-empty string
- WHEN they POST `/api/jobs/{id}/evaluate` for a job with non-empty `scraped_content`
- THEN the guard passes and evaluation proceeds to the existing usage-limit check and enqueue

### Requirement: Block evaluation when job description content is missing

The system MUST reject `POST /api/jobs/{id}/evaluate` with HTTP 422 and code `job_content_missing` when the target job's `scraped_content` is NULL or empty, BEFORE enqueue. This guard applies regardless of ingestion source (manual, email, or ATS scan) — the check is purely on `scraped_content` presence.

#### Scenario: Manual/email job with no scraped content

- GIVEN a job added manually or via email ingestion whose `scraped_content` is NULL
- WHEN the owning user POSTs `/api/jobs/{id}/evaluate`
- THEN the API responds 422 with `{"error": "...", "code": "job_content_missing"}`
- AND no `evaluate-job` is enqueued

#### Scenario: ATS-scraped job with content present

- GIVEN a job populated by one of the 6 ATS providers with non-empty `scraped_content`
- WHEN the owning user POSTs `/api/jobs/{id}/evaluate` and their CV is present
- THEN the guard passes and evaluation is enqueued as before

### Requirement: Existing error precedence is preserved

The new content guards MUST run only after existing ownership (`ErrNotFound`) and usage-limit (`ErrUsageLimitExceeded`) checks continue to behave identically for jobs that fail those checks — this change MUST NOT alter their status codes or response shape (404 `not_found`, 402 `usage_limit_exceeded`).

#### Scenario: Job not owned by user

- GIVEN a job ID that exists but belongs to another user
- WHEN the requester POSTs `/api/jobs/{id}/evaluate`
- THEN the API responds 404 with code `not_found`, unaffected by the CV/JD guards

## Files

- **Canonical spec**: `openspec/specs/evaluation-input-guards/spec.md` (this file)
- **Change artifacts**: `openspec/changes/archive/2026-07-11-evaluation-quality/` (proposal, design, tasks, apply-progress, verify-report, archive-report)
- **Implementation**: `api/internal/evaluate/service.go` (guards), `api/internal/evaluate/handler.go` (422 mapping)
