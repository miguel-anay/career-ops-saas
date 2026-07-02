// Package emailingest implements the Gmail job-alert ingestion trigger:
// it creates an email_ingest_runs row and enqueues an "ingest-email" worker
// job. It mirrors api/internal/scan/ (control-plane trigger + read-status,
// data-plane does the actual work).
package emailingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/platform"
	"github.com/miguel-anay/career-ops-saas/api/internal/queue"
)

// ErrGmailNotConnected is returned by TriggerIngest when the user has never
// completed the Gmail incremental-consent flow (users.google_refresh_token
// is NULL or empty).
var ErrGmailNotConnected = errors.New("gmail not connected")

// Service contains business logic for the email-ingest domain.
type Service struct {
	pool *pgxpool.Pool
}

// NewService creates a new emailingest Service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// ingestEmailPayload is the pg-boss job payload for "ingest-email".
type ingestEmailPayload struct {
	UserID      uuid.UUID `json:"user_id"`
	IngestRunID uuid.UUID `json:"ingest_run_id"`
}

// TriggerIngest creates an email_ingest_runs row and enqueues a single
// "ingest-email" job. Returns ErrGmailNotConnected (no run created, no
// enqueue) when the user has no stored Gmail refresh token.
//
// The token check and the run insert run inside a single
// platform.WithTenantTx so both are RLS-scoped to userID. queue.Enqueue
// stays OUTSIDE the tx (after commit) — pgboss.* has no RLS policy and
// must stay on the raw pool (mirrors scan.Service.TriggerScan).
func (s *Service) TriggerIngest(ctx context.Context, userID uuid.UUID) (uuid.UUID, error) {
	var run db.EmailIngestRun

	err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		user, err := q.GetUserByID(ctx, userID)
		if err != nil {
			return fmt.Errorf("get user: %w", err)
		}
		if !user.GoogleRefreshToken.Valid || user.GoogleRefreshToken.String == "" {
			return ErrGmailNotConnected
		}

		run, err = q.InsertEmailIngestRun(ctx, userID)
		if err != nil {
			return fmt.Errorf("insert email_ingest_run: %w", err)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrGmailNotConnected) {
			return uuid.Nil, ErrGmailNotConnected
		}
		return uuid.Nil, err
	}

	payload, err := json.Marshal(ingestEmailPayload{UserID: userID, IngestRunID: run.ID})
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal ingest-email payload: %w", err)
	}
	if err := queue.Enqueue(ctx, s.pool, queue.Job{
		Name: "ingest-email",
		Data: json.RawMessage(payload),
	}); err != nil {
		return uuid.Nil, fmt.Errorf("enqueue ingest-email: %w", err)
	}

	return run.ID, nil
}

// GetIngestRun returns the email_ingest_run record by ID, scoped to the
// requesting user. The lookup runs inside a platform.WithTenantTx scoped to
// userID, so a run owned by another tenant is invisible at the DB layer
// (RLS USING denial) and GetEmailIngestRunByID itself returns
// sql.ErrNoRows — independent of the app-layer run.UserID != userID check
// below, kept as defense-in-depth (mirrors scan.Service.GetScanRun / the
// PR #8 IDOR fix).
func (s *Service) GetIngestRun(ctx context.Context, userID, ingestRunID uuid.UUID) (*db.EmailIngestRun, error) {
	var run db.EmailIngestRun

	err := platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
		var err error
		run, err = q.GetEmailIngestRunByID(ctx, ingestRunID)
		if err != nil {
			return fmt.Errorf("get email_ingest_run: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if run.UserID != userID {
		return nil, sql.ErrNoRows
	}
	return &run, nil
}
